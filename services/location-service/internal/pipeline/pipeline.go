package pipeline

// Điều phối luồng
import (
	"context"
	"fmt"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/traffic"
)

type Result struct {
	TraceEmitted            bool
	MatchedObservationCount int
	CongestionStates        []traffic.CongestionState
}

type Pipeline struct {
	buffer    *trace.Buffer
	matcher   matching.Strategy
	processor *traffic.Processor
}

func New(
	buffer *trace.Buffer,
	matcher matching.Strategy,
	processor *traffic.Processor,
) (*Pipeline, error) {
	if buffer == nil {
		return nil, fmt.Errorf("trace buffer is required")
	}
	if matcher == nil {
		return nil, fmt.Errorf("matcher is required")
	}
	if processor == nil {
		return nil, fmt.Errorf("traffic processor is required")
	}

	return &Pipeline{
		buffer:    buffer,
		matcher:   matcher,
		processor: processor,
	}, nil
}

// Điều phối
func (pipeline *Pipeline) Process(
	ctx context.Context,
	event gps.CanonicalEvent,
	now time.Time,
) (Result, error) {
	if pipeline == nil || pipeline.buffer == nil ||
		pipeline.matcher == nil || pipeline.processor == nil {
		return Result{}, fmt.Errorf("pipeline is not initialized")
	}
	if ctx == nil {
		return Result{}, fmt.Errorf("context is required")
	}
	if now.IsZero() {
		return Result{}, fmt.Errorf("processing time is required")
	}
	// Nhận event từ handler sau đố đă vào buffer trace, nếu có trace mới thì match với
	result := Result{}
	emittedTrace, err := pipeline.buffer.Add(event)
	if err != nil {
		return Result{}, fmt.Errorf("add event to trace buffer: %w", err)
	}
	// matcher nhận trace trả về []matchedObservations, nếu có thì add vô traffic processor
	if emittedTrace != nil {
		result.TraceEmitted = true
		observations, err := pipeline.matcher.Match(ctx, *emittedTrace)
		if err != nil {
			return Result{}, fmt.Errorf("match trace: %w", err)
		}
		// Kiểm tra, cập nhật driver stage và gom obervation vào aggregator, nếu có thì trả về congestion state
		for index, observation := range observations {
			if err := pipeline.processor.Add(observation, now); err != nil {
				return Result{}, fmt.Errorf(
					"add matched observation %d: %w",
					index,
					err,
				)
			}
		}
		result.MatchedObservationCount = len(observations)
	}
	// Hoàn tất các window hết hạn và tính toán congestion state
	states, err := pipeline.processor.Flush(ctx, now)
	result.CongestionStates = states
	if err != nil {
		return result, fmt.Errorf("flush traffic processor: %w", err)
	}

	return result, nil
}
