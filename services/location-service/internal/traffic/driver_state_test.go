package traffic

import (
	"testing"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

func TestDriverStateBufferPassesEveryAggregatableObservation(t *testing.T) {
	buffer := NewDriverStateBuffer()
	key := int64(12)
	first := aggregatableObservation("driver-1", key, 0, testTime(0))
	second := aggregatableObservation("driver-1", key, 10, testTime(5))

	for _, observation := range []matching.MatchedObservation{first, second} {
		output, aggregate, err := buffer.Add(observation, observation.RecordedAt.Add(time.Second))
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}
		if !aggregate {
			t.Fatal("expected observation to be passed to the aggregator")
		}
		if output.Speed != observation.Speed {
			t.Fatalf("output speed = %v, want %v", output.Speed, observation.Speed)
		}
	}
}

func TestDriverStateBufferKeepsOnlineForStateOnly(t *testing.T) {
	buffer := NewDriverStateBuffer()
	key := int64(24)
	inTrip := aggregatableObservation("driver-1", key, 0, testTime(0))
	if _, _, err := buffer.Add(inTrip, testTime(1)); err != nil {
		t.Fatalf("add IN TRIP observation: %v", err)
	}

	online := validObservation("ONLINE")
	online.DriverID = "driver-1"
	online.RecordedAt = testTime(5)
	output, aggregate, err := buffer.Add(online, testTime(6))
	if err != nil {
		t.Fatalf("add ONLINE observation: %v", err)
	}
	if aggregate {
		t.Fatal("ONLINE observation must not be aggregated")
	}
	if output != (matching.MatchedObservation{}) {
		t.Fatal("state-only observation should not produce aggregator output")
	}

	state := buffer.states["driver-1"]
	if state.LastStatus != "ONLINE" {
		t.Fatalf("last status = %q, want ONLINE", state.LastStatus)
	}
	if state.LastGraphVersion != inTrip.GraphVersion {
		t.Fatalf("last graph version = %q, want %q", state.LastGraphVersion, inTrip.GraphVersion)
	}
	if state.LastTraversalKey == nil || *state.LastTraversalKey != key {
		t.Fatal("ONLINE transition must retain the last IN TRIP traversal key")
	}
}

func TestDriverStateBufferIgnoresDuplicateAndRejectsOlderObservation(t *testing.T) {
	buffer := NewDriverStateBuffer()
	key := int64(36)
	original := aggregatableObservation("driver-1", key, 5, testTime(5))
	if _, _, err := buffer.Add(original, testTime(6)); err != nil {
		t.Fatalf("add original observation: %v", err)
	}

	duplicate := original
	duplicate.Speed = 0
	if _, aggregate, err := buffer.Add(duplicate, testTime(7)); err != nil || aggregate {
		t.Fatalf("duplicate result: aggregate=%t error=%v", aggregate, err)
	}
	if !buffer.states["driver-1"].LastSeenAt.Equal(testTime(6)) {
		t.Fatal("duplicate must not refresh driver state")
	}

	older := original
	older.RecordedAt = testTime(4)
	if _, _, err := buffer.Add(older, testTime(8)); err == nil {
		t.Fatal("expected an error for an out-of-order observation")
	}
}

func TestDriverStateBufferKeepsDriversIndependent(t *testing.T) {
	buffer := NewDriverStateBuffer()
	firstKey := int64(48)
	secondKey := int64(60)

	first := aggregatableObservation("driver-1", firstKey, 3, testTime(0))
	second := aggregatableObservation("driver-2", secondKey, 7, testTime(0))
	if _, _, err := buffer.Add(first, testTime(1)); err != nil {
		t.Fatalf("add first driver: %v", err)
	}
	if _, _, err := buffer.Add(second, testTime(1)); err != nil {
		t.Fatalf("add second driver: %v", err)
	}

	if got := *buffer.states["driver-1"].LastTraversalKey; got != firstKey {
		t.Fatalf("first driver traversal key = %d, want %d", got, firstKey)
	}
	if got := *buffer.states["driver-2"].LastTraversalKey; got != secondKey {
		t.Fatalf("second driver traversal key = %d, want %d", got, secondKey)
	}
}

func TestDriverStateBufferRejectsInvalidInput(t *testing.T) {
	buffer := NewDriverStateBuffer()
	invalid := validObservation("OFFLINE")
	if _, _, err := buffer.Add(invalid, testTime(1)); err == nil {
		t.Fatal("expected invalid observation error")
	}
	if _, _, err := buffer.Add(validObservation("ONLINE"), time.Time{}); err == nil {
		t.Fatal("expected missing processing time error")
	}
}

func aggregatableObservation(
	driverID string,
	traversalKey int64,
	speed float64,
	recordedAt time.Time,
) matching.MatchedObservation {
	observation := validObservation("IN TRIP")
	observation.DriverID = driverID
	observation.RecordedAt = recordedAt
	observation.Speed = speed
	observation.Matched = true
	observation.TraversalKey = &traversalKey
	return observation
}

func testTime(second int) time.Time {
	return time.Date(2026, time.July, 24, 5, 0, second, 0, time.UTC)
}
