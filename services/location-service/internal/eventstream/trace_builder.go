package eventstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/twmb/franz-go/pkg/kgo"
)

// runTraceBuilder chuyển các GPS event trong raw topic thành trace theo tài xế.
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
