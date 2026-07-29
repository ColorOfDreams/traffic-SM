package gps

import (
	"strings"
	"testing"
	"time"
)

func TestConsumerStateBuildsTraceFromCanonicalEvents(t *testing.T) {
	first, err := DecodeCanonicalEvent(strings.NewReader(validGPSJSON))
	if err != nil {
		t.Fatalf("decode first GPS: %v", err)
	}
	second := first
	second.TTimestamp = "55:09.6"
	second.Lat = 20.989013
	third := second
	third.TTimestamp = "55:14.6"
	third.Lat = 20.989113

	state, err := NewConsumerState(ConsumerStateConfig{
		TracePoints:        3,
		OverlapPoints:      1,
		InactivityTimeout:  time.Minute,
		MaxBufferedPoints:  10,
		MaxMatchAttempts:   3,
		MinDisplacementM:   1,
		MaxImpliedSpeedMPS: 0,
	})
	if err != nil {
		t.Fatalf("create state: %v", err)
	}

	receivedAt := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC)
	if trace, err := state.Add(first, receivedAt); err != nil || trace != nil {
		t.Fatalf("first GPS should not create trace: trace=%v err=%v", trace, err)
	}
	if trace, err := state.Add(first, receivedAt); err != nil || trace != nil {
		t.Fatalf("duplicate GPS should be ignored: trace=%v err=%v", trace, err)
	}
	if trace, err := state.Add(second, receivedAt.Add(time.Second)); err != nil || trace != nil {
		t.Fatalf("second GPS should not create trace: trace=%v err=%v", trace, err)
	}
	trace, err := state.Add(third, receivedAt.Add(2*time.Second))
	if err != nil {
		t.Fatalf("add third GPS: %v", err)
	}
	if trace == nil {
		t.Fatal("third GPS should create a trace")
	}
	if len(trace.Points) != 3 {
		t.Fatalf("expected 3 trace points, got %d", len(trace.Points))
	}
	if trace.StartedAt.UnixMilli() != 1784768104600 {
		t.Fatalf("unexpected trace start %d", trace.StartedAt.UnixMilli())
	}
	if trace.EndedAt.UnixMilli() != 1784768114600 {
		t.Fatalf("unexpected trace end %d", trace.EndedAt.UnixMilli())
	}
}
