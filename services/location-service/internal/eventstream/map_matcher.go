package eventstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
	"github.com/twmb/franz-go/pkg/kgo"
)

type pendingTrace struct {
	record *kgo.Record
	done   bool
}

type matchJob struct {
	pending *pendingTrace
	trace   gps.Trace
}

type matchResult struct {
	pending  *pendingTrace
	trace    gps.Trace
	match    matching.MatchedTrace
	attempts int
	err      error
}

// runMapMatcher chuyển trace thành kết quả map matching.
func (stream *Kafka) runMapMatcher(ctx context.Context, matcher Matcher) error {
	jobs := make(chan matchJob, stream.config.JobQueueSize)
	results := make(chan matchResult, stream.config.JobQueueSize)
	pending := make(map[int32][]*pendingTrace)
	inFlight := 0

	var workers sync.WaitGroup
	for index := 0; index < stream.config.Workers; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			stream.runWorker(ctx, matcher, jobs, results)
		}()
	}
	defer func() {
		close(jobs)
		workers.Wait()
	}()

	for {
		if inFlight >= stream.config.JobQueueSize {
			select {
			case <-ctx.Done():
				stream.commit(context.Background(), stream.traceConsumer)
				return nil
			case result := <-results:
				if err := stream.handleMatchResult(ctx, pending, result); err != nil {
					return err
				}
				inFlight--
			}
			continue
		}

		select {
		case <-ctx.Done():
			stream.commit(context.Background(), stream.traceConsumer)
			return nil
		case result := <-results:
			if err := stream.handleMatchResult(ctx, pending, result); err != nil {
				return err
			}
			inFlight--
			continue
		default:
		}

		available := stream.config.JobQueueSize - inFlight
		fetches := stream.poll(stream.traceConsumer, ctx, available)
		if ctx.Err() != nil {
			stream.commit(context.Background(), stream.traceConsumer)
			return nil
		}
		logFetchErrors("gps.traces", fetches)

		for _, record := range fetches.Records() {
			item := &pendingTrace{record: record}
			pending[record.Partition] = append(pending[record.Partition], item)

			trace, err := decodeTrace(record)
			if err != nil {
				deadLetter := deadLetterEvent{
					Stage:      "trace",
					Error:      err.Error(),
					FailedAtMS: time.Now().UnixMilli(),
					RawEvent:   append([]byte(nil), record.Value...),
				}
				if _, publishErr := stream.publishDeadLetterWithRetry(
					ctx,
					string(record.Key),
					deadLetter,
				); publishErr != nil {
					return publishErr
				}
				item.done = true
				stream.markCompleted(pending, record.Partition)
				continue
			}

			jobs <- matchJob{pending: item, trace: trace}
			inFlight++
		}
	}
}

func decodeTrace(record *kgo.Record) (gps.Trace, error) {
	var trace gps.Trace
	decoder := json.NewDecoder(bytes.NewReader(record.Value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&trace); err != nil {
		return gps.Trace{}, fmt.Errorf("decode trace JSON: %w", err)
	}
	if strings.TrimSpace(trace.TraceID) == "" || strings.TrimSpace(trace.DriverID) == "" {
		return gps.Trace{}, errors.New("trace_id and driver_id are required")
	}
	if string(record.Key) != trace.DriverID {
		return gps.Trace{}, fmt.Errorf(
			"Kafka key %q does not match trace driver_id %q",
			string(record.Key),
			trace.DriverID,
		)
	}
	if len(trace.Points) < 2 {
		return gps.Trace{}, errors.New("trace requires at least two GPS points")
	}
	var previousRecordedAt time.Time
	for index, point := range trace.Points {
		if point.DriverID != trace.DriverID {
			return gps.Trace{}, fmt.Errorf(
				"trace point %d belongs to driver %q instead of %q",
				index,
				point.DriverID,
				trace.DriverID,
			)
		}
		recordedAt, err := point.RecordedAt()
		if err != nil {
			return gps.Trace{}, fmt.Errorf("derive trace point %d time: %w", index, err)
		}
		if index > 0 && !recordedAt.After(previousRecordedAt) {
			return gps.Trace{}, fmt.Errorf(
				"trace timestamps must increase: point %d is not newer than point %d",
				index,
				index-1,
			)
		}
		previousRecordedAt = recordedAt
	}
	return trace, nil
}

func (stream *Kafka) runWorker(
	ctx context.Context,
	matcher Matcher,
	jobs <-chan matchJob,
	results chan<- matchResult,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, open := <-jobs:
			if !open {
				return
			}

			result := matchResult{
				pending: job.pending,
				trace:   job.trace,
			}
			for attempt := 1; attempt <= stream.config.MaxMatchAttempts; attempt++ {
				result.attempts = attempt
				result.match, result.err = matcher.Match(ctx, job.trace)
				if result.err == nil || ctx.Err() != nil {
					break
				}
				log.Printf(
					"map match trace=%s attempt=%d/%d failed: %v",
					job.trace.TraceID,
					attempt,
					stream.config.MaxMatchAttempts,
					result.err,
				)
			}

			select {
			case results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (stream *Kafka) handleMatchResult(
	ctx context.Context,
	pending map[int32][]*pendingTrace,
	result matchResult,
) error {
	if result.err == nil {
		if err := stream.publishMatched(ctx, result.match); err != nil {
			return err
		}
	} else {
		deadLetter := deadLetterEvent{
			Stage:      "map_matching",
			Error:      result.err.Error(),
			FailedAtMS: time.Now().UnixMilli(),
			Trace:      &result.trace,
		}
		if _, err := stream.publishDeadLetterWithRetry(
			ctx,
			result.trace.DriverID,
			deadLetter,
		); err != nil {
			return err
		}
	}

	result.pending.done = true
	stream.markCompleted(pending, result.pending.record.Partition)
	return nil
}

func (stream *Kafka) markCompleted(
	pending map[int32][]*pendingTrace,
	partition int32,
) {
	queue := pending[partition]
	completed := 0
	var last *kgo.Record
	for completed < len(queue) && queue[completed].done {
		last = queue[completed].record
		completed++
	}
	if last == nil {
		return
	}

	stream.traceConsumer.MarkCommitRecords(last)
	if completed == len(queue) {
		delete(pending, partition)
		return
	}
	pending[partition] = queue[completed:]
}

func (stream *Kafka) publishMatched(
	ctx context.Context,
	result matching.MatchedTrace,
) error {
	value, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode matched trace %s: %w", result.TraceID, err)
	}
	if err := stream.produceWithRetry(ctx, stream.matchedTopic, result.DriverID, value); err != nil {
		return fmt.Errorf("produce matched trace %s: %w", result.TraceID, err)
	}
	return nil
}
