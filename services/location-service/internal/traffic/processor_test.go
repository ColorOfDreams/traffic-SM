package traffic

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProcessorRunsTrafficFlowEndToEnd(t *testing.T) {
	store := &fakeReferenceSpeedStore{
		reference: ReferenceSpeed{SpeedMPS: 10, Source: "edge_fallback"},
		found:     true,
	}
	processor, err := NewProcessor(10*time.Second, store, CalculatorConfig{
		MinSamples: 4,
		MinDrivers: 2,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}

	observations := []struct {
		driver string
		speed  float64
		second int
	}{
		{driver: "driver-1", speed: 0, second: 1},
		{driver: "driver-1", speed: 0, second: 2},
		{driver: "driver-2", speed: 5, second: 3},
		{driver: "driver-2", speed: 10, second: 4},
	}
	for _, item := range observations {
		observation := aggregatorObservation(
			item.driver,
			12,
			item.speed,
			testTime(item.second),
			"bike",
		)
		if err := processor.Add(observation, testTime(item.second+1)); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	states, err := processor.Flush(context.Background(), testTime(10))
	if err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("state count = %d, want 1", len(states))
	}
	state := states[0]
	if state.CurrentSpeedMPS != 2.5 || state.SpeedRatio != 0.25 {
		t.Fatalf("unexpected state: %#v", state)
	}
	if state.Level != LevelCongested || state.Confidence != 0.5 {
		t.Fatalf("level=%q confidence=%v, want CONGESTED and 0.5", state.Level, state.Confidence)
	}
}

func TestProcessorKeepsFailedWindowForRetry(t *testing.T) {
	store := &fakeReferenceSpeedStore{
		err: errors.New("temporary store failure"),
	}
	processor, err := NewProcessor(10*time.Second, store, CalculatorConfig{
		MinSamples: 1,
		MinDrivers: 1,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	observation := aggregatorObservation("driver-1", 12, 5, testTime(1), "bike")
	if err := processor.Add(observation, testTime(2)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	states, err := processor.Flush(context.Background(), testTime(10))
	if err == nil {
		t.Fatal("expected calculation error")
	}
	if len(states) != 0 || len(processor.pendingWindows) != 1 {
		t.Fatalf("states=%d pending=%d, want 0 and 1", len(states), len(processor.pendingWindows))
	}

	store.err = nil
	store.found = true
	store.reference = ReferenceSpeed{SpeedMPS: 10}
	states, err = processor.Flush(context.Background(), testTime(11))
	if err != nil {
		t.Fatalf("retry Flush() error = %v", err)
	}
	if len(states) != 1 || len(processor.pendingWindows) != 0 {
		t.Fatalf("states=%d pending=%d, want 1 and 0", len(states), len(processor.pendingWindows))
	}
}

func TestProcessorKeepsStateOnlyObservationOutOfAggregator(t *testing.T) {
	store := &fakeReferenceSpeedStore{
		reference: ReferenceSpeed{SpeedMPS: 10},
		found:     true,
	}
	processor, err := NewProcessor(10*time.Second, store, CalculatorConfig{
		MinSamples: 1,
		MinDrivers: 1,
	})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	online := validObservation("ONLINE")
	online.RecordedAt = testTime(1)
	if err := processor.Add(online, testTime(2)); err != nil {
		t.Fatalf("Add ONLINE error = %v", err)
	}

	states, err := processor.Flush(context.Background(), testTime(10))
	if err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if len(states) != 0 || store.calls != 0 {
		t.Fatalf("states=%d store calls=%d, want 0 and 0", len(states), store.calls)
	}
}
