package traffic

import (
	"testing"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

func TestNewAggregatorRejectsInvalidDuration(t *testing.T) {
	if _, err := NewAggregator(0); err == nil {
		t.Fatal("expected invalid window duration error")
	}
}

func TestAggregatorKeepsAllSpeedsAndDistinctDrivers(t *testing.T) {
	aggregator, err := NewAggregator(time.Minute)
	if err != nil {
		t.Fatalf("NewAggregator() error = %v", err)
	}

	observations := []matching.MatchedObservation{
		aggregatorObservation("driver-1", 12, 0, testTime(5), "bike"),
		aggregatorObservation("driver-1", 12, 0, testTime(10), "bike"),
		aggregatorObservation("driver-1", 12, 10, testTime(15), "bike"),
		aggregatorObservation("driver-2", 12, 5, testTime(20), "bike"),
	}
	for _, observation := range observations {
		if err := aggregator.Add(observation); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	if len(aggregator.windows) != 1 {
		t.Fatalf("window count = %d, want 1", len(aggregator.windows))
	}
	for key, window := range aggregator.windows {
		if key.VehicleType != "motorcycle" {
			t.Fatalf("vehicle type = %q, want motorcycle", key.VehicleType)
		}
		if len(window.Speeds) != 4 {
			t.Fatalf("speed count = %d, want 4", len(window.Speeds))
		}
		if window.Speeds[0] != 0 || window.Speeds[1] != 0 {
			t.Fatal("zero-speed samples must be retained")
		}
		if len(window.DriverIDs) != 2 {
			t.Fatalf("distinct driver count = %d, want 2", len(window.DriverIDs))
		}
	}
}

func TestAggregatorSeparatesBucketKeys(t *testing.T) {
	aggregator, err := NewAggregator(10 * time.Second)
	if err != nil {
		t.Fatalf("NewAggregator() error = %v", err)
	}

	observations := []matching.MatchedObservation{
		aggregatorObservation("driver-1", 12, 1, testTime(1), "bike"),
		aggregatorObservation("driver-1", 13, 2, testTime(1), "bike"),
		aggregatorObservation("driver-1", 12, 3, testTime(1), "car"),
		aggregatorObservation("driver-1", 12, 4, testTime(11), "bike"),
	}
	differentGraph := aggregatorObservation("driver-1", 12, 5, testTime(1), "bike")
	differentGraph.GraphVersion = "vietnam-20260801-v2"
	observations = append(observations, differentGraph)
	for _, observation := range observations {
		if err := aggregator.Add(observation); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	if len(aggregator.windows) != 5 {
		t.Fatalf("window count = %d, want 5", len(aggregator.windows))
	}
}

func TestAggregatorCloseReturnsAndDeletesOnlyFinishedWindows(t *testing.T) {
	aggregator, err := NewAggregator(10 * time.Second)
	if err != nil {
		t.Fatalf("NewAggregator() error = %v", err)
	}
	if err := aggregator.Add(aggregatorObservation("driver-1", 12, 0, testTime(5), "bike")); err != nil {
		t.Fatalf("add first window: %v", err)
	}
	if err := aggregator.Add(aggregatorObservation("driver-1", 12, 10, testTime(15), "bike")); err != nil {
		t.Fatalf("add second window: %v", err)
	}

	closed := aggregator.Close(testTime(10))
	if len(closed) != 1 {
		t.Fatalf("closed window count = %d, want 1", len(closed))
	}
	if !closed[0].Key.WindowStart.Equal(testTime(0)) {
		t.Fatalf("closed window start = %v, want %v", closed[0].Key.WindowStart, testTime(0))
	}
	if len(aggregator.windows) != 1 {
		t.Fatalf("remaining window count = %d, want 1", len(aggregator.windows))
	}
}

func TestAggregatorRejectsStateOnlyObservation(t *testing.T) {
	aggregator, err := NewAggregator(time.Minute)
	if err != nil {
		t.Fatalf("NewAggregator() error = %v", err)
	}
	if err := aggregator.Add(validObservation("ONLINE")); err == nil {
		t.Fatal("expected ONLINE observation to be rejected")
	}
}

func aggregatorObservation(
	driverID string,
	traversalKey int64,
	speed float64,
	recordedAt time.Time,
	vehicleType string,
) matching.MatchedObservation {
	observation := validObservation("IN TRIP")
	observation.DriverID = driverID
	observation.RecordedAt = recordedAt
	observation.Speed = speed
	observation.VehicleType = vehicleType
	observation.Matched = true
	observation.TraversalKey = &traversalKey
	return observation
}
