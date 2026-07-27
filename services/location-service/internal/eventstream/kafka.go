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

type Config struct {
	Brokers           []string
	RawTopic          string
	TraceTopic        string
	MatchedTopic      string
	DeadLetterTopic   string
	TraceBuilderGroup string
	MatcherGroup      string
	Workers           int
	JobQueueSize      int
	MaxMatchAttempts  int
	CommitInterval    time.Duration
	State             gps.ConsumerStateConfig
}

type Kafka struct {
	producer        *kgo.Client
	rawConsumer     *kgo.Client
	traceConsumer   *kgo.Client
	rawTopic        string
	traceTopic      string
	matchedTopic    string
	deadLetterTopic string
	config          Config
}

type Matcher interface {
	Match(context.Context, gps.Trace) (matching.Result, error)
}

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
	match    matching.Result
	attempts int
	err      error
}

type matchedEvent struct {
	TraceID             string          `json:"trace_id"`
	DriverID            string          `json:"driver_id"`
	StartedAtMS         int64           `json:"started_at_ms"`
	EndedAtMS           int64           `json:"ended_at_ms"`
	PointCount          int             `json:"point_count"`
	MatchedAtMS         int64           `json:"matched_at_ms"`
	GraphHopperTookMS   int64           `json:"graphhopper_took_ms"`
	GraphHopperResponse json.RawMessage `json:"graphhopper_response"`
}

type deadLetterEvent struct {
	Stage      string     `json:"stage"`
	Error      string     `json:"error"`
	FailedAtMS int64      `json:"failed_at_ms"`
	Trace      *gps.Trace `json:"trace,omitempty"`
	RawEvent   []byte     `json:"raw_event,omitempty"`
}

func New(config Config) (*Kafka, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	producer, err := kgo.NewClient(
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID("traffic-location-producer"),
		kgo.ProducerBatchMaxBytes(1*1024*1024),
		kgo.ProducerLinger(5*time.Millisecond),
		kgo.RequiredAcks(kgo.AllISRAcks()),
	)
	if err != nil {
		return nil, fmt.Errorf("create Kafka producer: %w", err)
	}

	rawConsumer, err := newConsumer(
		config,
		"traffic-trace-builder",
		config.RawTopic,
		config.TraceBuilderGroup,
	)
	if err != nil {
		producer.Close()
		return nil, err
	}

	traceConsumer, err := newConsumer(
		config,
		"traffic-map-matcher",
		config.TraceTopic,
		config.MatcherGroup,
	)
	if err != nil {
		rawConsumer.Close()
		producer.Close()
		return nil, err
	}

	return &Kafka{
		producer:        producer,
		rawConsumer:     rawConsumer,
		traceConsumer:   traceConsumer,
		rawTopic:        config.RawTopic,
		traceTopic:      config.TraceTopic,
		matchedTopic:    config.MatchedTopic,
		deadLetterTopic: config.DeadLetterTopic,
		config:          config,
	}, nil
}

func validateConfig(config Config) error {
	if len(config.Brokers) == 0 {
		return errors.New("at least one Kafka broker is required")
	}
	if config.RawTopic == "" || config.TraceTopic == "" ||
		config.MatchedTopic == "" || config.DeadLetterTopic == "" {
		return errors.New("Kafka topic names are required")
	}
	if config.TraceBuilderGroup == "" || config.MatcherGroup == "" {
		return errors.New("Kafka consumer group names are required")
	}
	if config.TraceBuilderGroup == config.MatcherGroup {
		return errors.New("trace builder and map matcher must use different consumer groups")
	}
	if config.Workers < 1 {
		return errors.New("GraphHopper worker count must be at least one")
	}
	if config.JobQueueSize < config.Workers {
		return errors.New("match job queue must be at least worker count")
	}
	if config.MaxMatchAttempts < 1 {
		return errors.New("maximum match attempts must be at least one")
	}
	if config.CommitInterval <= 0 {
		return errors.New("Kafka commit interval must be positive")
	}
	return nil
}

func newConsumer(config Config, clientID string, topic string, group string) (*kgo.Client, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID(clientID),
		kgo.ConsumeTopics(topic),
		kgo.ConsumerGroup(group),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.AutoCommitMarks(),
		kgo.AutoCommitInterval(config.CommitInterval),
	)
	if err != nil {
		return nil, fmt.Errorf("create Kafka consumer %s: %w", clientID, err)
	}
	return client, nil
}

func (stream *Kafka) Ping(ctx context.Context) error {
	if err := stream.producer.Ping(ctx); err != nil {
		return fmt.Errorf("ping Kafka: %w", err)
	}
	return nil
}

func (stream *Kafka) Close() {
	stream.traceConsumer.Close()
	stream.rawConsumer.Close()
	stream.producer.Close()
}

func (stream *Kafka) PublishGPS(ctx context.Context, event gps.CanonicalEvent) error {
	value, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode GPS event: %w", err)
	}
	if err := stream.produceSync(ctx, stream.rawTopic, event.DriverID, value); err != nil {
		return fmt.Errorf("produce GPS event: %w", err)
	}
	return nil
}

func (stream *Kafka) Run(ctx context.Context, matcher Matcher) error {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	errorsChannel := make(chan error, 2)
	go func() {
		errorsChannel <- stream.runTraceBuilder(runContext)
	}()
	go func() {
		errorsChannel <- stream.runMapMatcher(runContext, matcher)
	}()

	var runErr error
	for completed := 0; completed < 2; completed++ {
		err := <-errorsChannel
		if err != nil && runErr == nil {
			runErr = err
			cancel()
		}
	}
	return runErr
}

func (stream *Kafka) runTraceBuilder(ctx context.Context) error {
	states := make(map[int32]*gps.ConsumerState)

	for {
		fetches := stream.poll(stream.rawConsumer, ctx, 500)
		if ctx.Err() != nil {
			stream.commit(context.Background(), stream.rawConsumer)
			return nil
		}
		logFetchErrors("gps.raw", fetches)

		for _, record := range fetches.Records() {
			state, err := stream.partitionState(states, record.Partition)
			if err != nil {
				return err
			}

			accepted, err := stream.consumeRawRecord(ctx, state, record)
			if err != nil {
				if ctx.Err() != nil {
					stream.commit(context.Background(), stream.rawConsumer)
					return nil
				}
				log.Printf(
					"consume gps.raw partition=%d offset=%d: %v",
					record.Partition,
					record.Offset,
					err,
				)
			}
			if accepted {
				stream.rawConsumer.MarkCommitRecords(record)
			}
		}
	}
}

func (stream *Kafka) partitionState(
	states map[int32]*gps.ConsumerState,
	partition int32,
) (*gps.ConsumerState, error) {
	if state := states[partition]; state != nil {
		return state, nil
	}
	state, err := gps.NewConsumerState(stream.config.State)
	if err != nil {
		return nil, fmt.Errorf("create state for Kafka partition %d: %w", partition, err)
	}
	states[partition] = state
	return state, nil
}

func (stream *Kafka) consumeRawRecord(
	ctx context.Context,
	state *gps.ConsumerState,
	record *kgo.Record,
) (bool, error) {
	event, err := gps.DecodeCanonicalEvent(bytes.NewReader(record.Value))
	if err != nil {
		deadLetter := deadLetterEvent{
			Stage:      "canonical",
			Error:      err.Error(),
			FailedAtMS: time.Now().UnixMilli(),
			RawEvent:   append([]byte(nil), record.Value...),
		}
		return stream.publishDeadLetterWithRetry(ctx, string(record.Key), deadLetter)
	}
	if string(record.Key) != event.DriverID {
		keyError := fmt.Errorf(
			"Kafka key %q does not match driver_id %q",
			string(record.Key),
			event.DriverID,
		)
		deadLetter := deadLetterEvent{
			Stage:      "partition_key",
			Error:      keyError.Error(),
			FailedAtMS: time.Now().UnixMilli(),
			RawEvent:   append([]byte(nil), record.Value...),
		}
		accepted, publishErr := stream.publishDeadLetterWithRetry(ctx, event.DriverID, deadLetter)
		if publishErr != nil {
			return accepted, publishErr
		}
		return true, keyError
	}

	trace, err := state.Add(event, time.Now().UTC())
	if err != nil {
		return false, err
	}
	for trace != nil {
		if err := stream.publishTraceWithRetry(ctx, *trace); err != nil {
			return false, err
		}
		completion, err := state.CompleteMatch(trace.DriverID, trace.TraceID, true)
		if err != nil {
			return false, fmt.Errorf("complete emitted trace %s: %w", trace.TraceID, err)
		}
		trace = completion.NextTrace
	}
	return true, nil
}

func (stream *Kafka) publishTraceWithRetry(ctx context.Context, trace gps.Trace) error {
	value, err := json.Marshal(trace)
	if err != nil {
		return fmt.Errorf("encode trace %s: %w", trace.TraceID, err)
	}
	return stream.produceWithRetry(ctx, stream.traceTopic, trace.DriverID, value)
}

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
	for index, point := range trace.Points {
		if point.DriverID != trace.DriverID {
			return gps.Trace{}, fmt.Errorf(
				"trace point %d belongs to driver %q instead of %q",
				index,
				point.DriverID,
				trace.DriverID,
			)
		}
		if index > 0 && point.RecordedAtMS <= trace.Points[index-1].RecordedAtMS {
			return gps.Trace{}, fmt.Errorf(
				"trace timestamps must increase: point %d is not newer than point %d",
				index,
				index-1,
			)
		}
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
		if err := stream.publishMatched(ctx, result.trace, result.match); err != nil {
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
	trace gps.Trace,
	result matching.Result,
) error {
	value, err := json.Marshal(matchedEvent{
		TraceID:             trace.TraceID,
		DriverID:            trace.DriverID,
		StartedAtMS:         trace.StartedAt.UnixMilli(),
		EndedAtMS:           trace.EndedAt.UnixMilli(),
		PointCount:          len(trace.Points),
		MatchedAtMS:         time.Now().UnixMilli(),
		GraphHopperTookMS:   result.TookMS,
		GraphHopperResponse: append(json.RawMessage(nil), result.ResponseJSON...),
	})
	if err != nil {
		return fmt.Errorf("encode matched trace %s: %w", trace.TraceID, err)
	}
	if err := stream.produceWithRetry(ctx, stream.matchedTopic, trace.DriverID, value); err != nil {
		return fmt.Errorf("produce matched trace %s: %w", trace.TraceID, err)
	}
	return nil
}

func (stream *Kafka) publishDeadLetterWithRetry(
	ctx context.Context,
	key string,
	event deadLetterEvent,
) (bool, error) {
	value, err := json.Marshal(event)
	if err != nil {
		return false, fmt.Errorf("encode dead-letter event: %w", err)
	}
	if err := stream.produceWithRetry(ctx, stream.deadLetterTopic, key, value); err != nil {
		return false, err
	}
	return true, nil
}

func (stream *Kafka) produceWithRetry(
	ctx context.Context,
	topic string,
	key string,
	value []byte,
) error {
	for {
		err := stream.produceSync(ctx, topic, key, value)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("produce topic=%s key=%s failed; retrying: %v", topic, key, err)
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (stream *Kafka) produceSync(
	ctx context.Context,
	topic string,
	key string,
	value []byte,
) error {
	results := stream.producer.ProduceSync(ctx, &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: value,
	})
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("produce topic=%s: %w", topic, err)
	}
	return nil
}

func (stream *Kafka) poll(
	client *kgo.Client,
	ctx context.Context,
	maxRecords int,
) kgo.Fetches {
	pollContext, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	return client.PollRecords(pollContext, maxRecords)
}

func logFetchErrors(topic string, fetches kgo.Fetches) {
	for _, fetchError := range fetches.Errors() {
		if errors.Is(fetchError.Err, context.DeadlineExceeded) ||
			errors.Is(fetchError.Err, context.Canceled) {
			continue
		}
		log.Printf(
			"poll Kafka topic=%s partition=%d: %v",
			topic,
			fetchError.Partition,
			fetchError.Err,
		)
	}
}

func (stream *Kafka) commit(parent context.Context, client *kgo.Client) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	if err := client.CommitMarkedOffsets(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("commit Kafka offsets: %v", err)
	}
}
