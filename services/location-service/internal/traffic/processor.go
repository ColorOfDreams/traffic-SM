package traffic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

type Processor struct {
	mu             sync.Mutex
	driverState    *DriverStateBuffer
	aggregator     *Aggregator
	calculator     *Calculator
	references     ReferenceSpeedStore
	pendingWindows []SegmentWindow
}

func NewProcessor(
	windowDuration time.Duration,
	references ReferenceSpeedStore,
	calculatorConfig CalculatorConfig,
) (*Processor, error) {
	aggregator, err := NewAggregator(windowDuration)
	if err != nil {
		return nil, err
	}
	calculator, err := NewCalculator(references, calculatorConfig)
	if err != nil {
		return nil, err
	}
	return &Processor{
		driverState: NewDriverStateBuffer(),
		aggregator:  aggregator,
		calculator:  calculator,
		references:  references,
	}, nil
}

// Kiểm tra observation khi qua driverstate buffer có hợ lệ ko mới đưa vô aggregator
func (processor *Processor) Add(
	input matching.MatchedObservation,
	now time.Time,
) error {
	if processor == nil || processor.driverState == nil || processor.aggregator == nil {
		return fmt.Errorf("traffic processor is not initialized")
	}

	processor.mu.Lock()
	defer processor.mu.Unlock()
	if recorder, ok := processor.references.(ReferenceSpeedRecorder); ok {
		if err := recorder.Record(input); err != nil {
			return fmt.Errorf("record reference speed: %w", err)
		}
	}

	observation, shouldAggregate, err := processor.driverState.Add(input, now)
	if err != nil {
		return err
	}
	if !shouldAggregate {
		return nil
	}
	return processor.aggregator.Add(observation)
}

// Đóng các window hết hạn gọi calculator trả về congestoinState
func (processor *Processor) Flush(
	ctx context.Context,
	now time.Time,
) ([]CongestionState, error) {
	if processor == nil || processor.aggregator == nil || processor.calculator == nil {
		return nil, fmt.Errorf("traffic processor is not initialized")
	}
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if now.IsZero() {
		return nil, fmt.Errorf("flush time is required")
	}

	processor.mu.Lock()
	defer processor.mu.Unlock()

	processor.pendingWindows = append(
		processor.pendingWindows,
		processor.aggregator.Close(now)...,
	)

	states := make([]CongestionState, 0, len(processor.pendingWindows))
	remaining := make([]SegmentWindow, 0, len(processor.pendingWindows))
	var calculationErrors []error
	for _, window := range processor.pendingWindows {
		state, err := processor.calculator.Calculate(ctx, window, now)
		if err != nil {
			remaining = append(remaining, window)
			calculationErrors = append(calculationErrors, fmt.Errorf(
				"calculate graph=%s traversal=%d vehicle=%s: %w",
				window.Key.GraphVersion,
				window.Key.TraversalKey,
				window.Key.VehicleType,
				err,
			))
			continue
		}
		states = append(states, state)
	}

	processor.pendingWindows = remaining
	return states, errors.Join(calculationErrors...)
}
