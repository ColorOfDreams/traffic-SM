package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/traffic"
)

type fakeMatcher struct {
	observations []matching.MatchedObservation
	calls        int
}

func (matcher *fakeMatcher) Match(
	_ context.Context,
	_ trace.Trace,
) ([]matching.MatchedObservation, error) {
	matcher.calls++
	return matcher.observations, nil
}

type fakeReferenceStore struct{}

func (fakeReferenceStore) Get(
	_ context.Context,
	_ string,
	_ int64,
	_ string,
) (traffic.ReferenceSpeed, bool, error) {
	return traffic.ReferenceSpeed{SpeedMPS: 10}, true, nil
}

func TestProcessWaitsForTraceBeforeMatching(t *testing.T) {
	pipeline, matcher := newTestPipeline(t, nil)
	now := time.Date(2026, time.August, 3, 8, 0, 1, 0, time.UTC)

	result, err := pipeline.Process(
		context.Background(),
		canonicalEvent("driver-1", now),
		now,
	)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.TraceEmitted || result.MatchedObservationCount != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if matcher.calls != 0 {
		t.Fatalf("matcher calls = %d, want 0", matcher.calls)
	}
}

func TestProcessRunsTraceMatchingAndTraffic(t *testing.T) {
	startedAt := time.Date(2026, time.August, 3, 8, 0, 1, 0, time.UTC)
	traversalKey := int64(12)
	observations := []matching.MatchedObservation{
		matchedObservation("driver-1", 0, 0, startedAt, &traversalKey),
		matchedObservation("driver-1", 1, 0, startedAt.Add(time.Second), &traversalKey),
	}
	pipeline, matcher := newTestPipeline(t, observations)

	if _, err := pipeline.Process(
		context.Background(),
		canonicalEvent("driver-1", startedAt),
		startedAt,
	); err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	result, err := pipeline.Process(
		context.Background(),
		canonicalEvent("driver-1", startedAt.Add(time.Second)),
		startedAt.Add(10*time.Second),
	)
	if err != nil {
		t.Fatalf("second Process() error = %v", err)
	}

	if !result.TraceEmitted || result.MatchedObservationCount != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if matcher.calls != 1 {
		t.Fatalf("matcher calls = %d, want 1", matcher.calls)
	}
	if len(result.CongestionStates) != 1 {
		t.Fatalf("state count = %d, want 1", len(result.CongestionStates))
	}
	state := result.CongestionStates[0]
	if state.Level != traffic.LevelCongested || state.CurrentSpeedMPS != 0 {
		t.Fatalf("unexpected congestion state: %#v", state)
	}
}

func newTestPipeline(
	t *testing.T,
	observations []matching.MatchedObservation,
) (*Pipeline, *fakeMatcher) {
	t.Helper()

	buffer, err := trace.NewBuffer(trace.BufferConfig{
		MaxPoints:     2,
		MaxDuration:   30 * time.Second,
		MinPoints:     2,
		OverlapPoints: 1,
	})
	if err != nil {
		t.Fatalf("NewBuffer() error = %v", err)
	}
	processor, err := traffic.NewProcessor(
		10*time.Second,
		fakeReferenceStore{},
		traffic.CalculatorConfig{MinSamples: 2, MinDrivers: 1},
	)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	matcher := &fakeMatcher{observations: observations}
	pipeline, err := New(buffer, matcher, processor)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return pipeline, matcher
}

func canonicalEvent(driverID string, recordedAt time.Time) gps.CanonicalEvent {
	return gps.CanonicalEvent{
		DriverID:    driverID,
		Lat:         21.0285,
		Lng:         105.8542,
		Speed:       0,
		Status:      "IN TRIP",
		VehicleType: "car",
		RecordedAt:  recordedAt,
	}
}

func matchedObservation(
	driverID string,
	pointIndex int,
	speed float64,
	recordedAt time.Time,
	traversalKey *int64,
) matching.MatchedObservation {
	return matching.MatchedObservation{
		GraphVersion:    "vietnam-20260730-car-v1",
		DriverID:        driverID,
		PointIndex:      pointIndex,
		RecordedAt:      recordedAt,
		Speed:           speed,
		Status:          "IN TRIP",
		VehicleType:     "car",
		Matched:         true,
		TrafficEligible: true,
		TraversalKey:    traversalKey,
	}
}
