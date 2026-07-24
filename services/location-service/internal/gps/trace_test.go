package gps

import (
	"fmt"
	"testing"
	"time"
)

func TestNewTraceBuilderAcceptsValidConfig(t *testing.T) {
	buffer := NewBufferManager()
	config := validTraceConfig()

	builder, err := NewTraceBuilder(buffer, config)
	if err != nil {
		t.Fatal(err)
	}
	if builder.buffer != buffer {
		t.Fatal("builder does not reference the supplied buffer")
	}
	if builder.config != config {
		t.Fatalf("unexpected config: %#v", builder.config)
	}
}

func TestNewTraceBuilderRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		buffer *BufferManager
		config TraceConfig
	}{
		{
			name:   "nil buffer",
			buffer: nil,
			config: validTraceConfig(),
		},
		{
			name:   "zero window",
			buffer: NewBufferManager(),
			config: withTraceConfig(func(config *TraceConfig) {
				config.Window = 0
			}),
		},
		{
			name:   "fewer than two points",
			buffer: NewBufferManager(),
			config: withTraceConfig(func(config *TraceConfig) {
				config.MinimumPoints = 1
			}),
		},
		{
			name:   "zero inactivity timeout",
			buffer: NewBufferManager(),
			config: withTraceConfig(func(config *TraceConfig) {
				config.InactivityTimeout = 0
			}),
		},
		{
			name:   "negative overlap",
			buffer: NewBufferManager(),
			config: withTraceConfig(func(config *TraceConfig) {
				config.OverlapPoints = -1
			}),
		},
		{
			name:   "overlap equals minimum",
			buffer: NewBufferManager(),
			config: withTraceConfig(func(config *TraceConfig) {
				config.OverlapPoints = config.MinimumPoints
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewTraceBuilder(test.buffer, test.config); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestTraceBuilderCollectsWindowAndKeepsOverlap(t *testing.T) {
	buffer := NewBufferManager()
	builder, err := NewTraceBuilder(buffer, validTraceConfig())
	if err != nil {
		t.Fatal(err)
	}

	startedAt := time.Date(2026, 7, 23, 4, 0, 0, 0, time.UTC)
	for index := 0; index < 8; index++ {
		recordedAt := startedAt.Add(time.Duration(index) * 5 * time.Second)
		event := testEvent(
			fmt.Sprintf("event-%03d", index),
			"vehicle-001",
			recordedAt.Format(time.RFC3339Nano),
		)
		if _, err := buffer.AddAt(event, recordedAt); err != nil {
			t.Fatal(err)
		}
	}

	traces := builder.CollectReady(startedAt.Add(36 * time.Second))
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}

	trace := traces[0]
	if trace.TraceID != "vehicle-001:event-000:event-006" {
		t.Fatalf("unexpected trace ID %q", trace.TraceID)
	}
	if len(trace.Points) != 7 {
		t.Fatalf("expected 7 trace points, got %d", len(trace.Points))
	}

	remaining := buffer.Events("vehicle-001")
	if len(remaining) != 3 ||
		remaining[0].EventID != "event-005" ||
		remaining[1].EventID != "event-006" ||
		remaining[2].EventID != "event-007" {
		t.Fatalf("unexpected overlap buffer: %#v", remaining)
	}
}

func TestTraceBuilderFlushesInactiveVehicle(t *testing.T) {
	buffer := NewBufferManager()
	builder, err := NewTraceBuilder(buffer, validTraceConfig())
	if err != nil {
		t.Fatal(err)
	}

	startedAt := time.Date(2026, 7, 23, 4, 0, 0, 0, time.UTC)
	for index := 0; index < 4; index++ {
		recordedAt := startedAt.Add(time.Duration(index) * 5 * time.Second)
		event := testEvent(
			fmt.Sprintf("event-%03d", index),
			"vehicle-001",
			recordedAt.Format(time.RFC3339Nano),
		)
		if _, err := buffer.AddAt(event, recordedAt); err != nil {
			t.Fatal(err)
		}
	}

	traces := builder.CollectReady(startedAt.Add(40 * time.Second))
	if len(traces) != 1 || len(traces[0].Points) != 4 {
		t.Fatalf("unexpected inactive traces: %#v", traces)
	}
	if events := buffer.Events("vehicle-001"); len(events) != 0 {
		t.Fatalf("expected inactive vehicle buffer to be removed, got %#v", events)
	}
}

func TestTraceBuilderDiscardsInactiveVehicleWithTooFewPoints(t *testing.T) {
	buffer := NewBufferManager()
	builder, err := NewTraceBuilder(buffer, validTraceConfig())
	if err != nil {
		t.Fatal(err)
	}

	startedAt := time.Date(2026, 7, 23, 4, 0, 0, 0, time.UTC)
	event := testEvent("event-001", "vehicle-001", startedAt.Format(time.RFC3339Nano))
	if _, err := buffer.AddAt(event, startedAt); err != nil {
		t.Fatal(err)
	}

	traces := builder.CollectReady(startedAt.Add(30 * time.Second))
	if len(traces) != 0 {
		t.Fatalf("expected no trace, got %#v", traces)
	}
	if events := buffer.Events("vehicle-001"); len(events) != 0 {
		t.Fatalf("expected insufficient inactive buffer to be removed, got %#v", events)
	}

	result, err := buffer.AddAt(event, startedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Duplicate || result.VehicleEventCount != 0 {
		t.Fatalf("unexpected retry result after cleanup: %#v", result)
	}
}

func validTraceConfig() TraceConfig {
	return TraceConfig{
		Window:            30 * time.Second,
		MinimumPoints:     4,
		InactivityTimeout: 20 * time.Second,
		OverlapPoints:     2,
	}
}

func withTraceConfig(change func(*TraceConfig)) TraceConfig {
	config := validTraceConfig()
	change(&config)
	return config
}
