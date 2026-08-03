package traffic

import (
	"math"
	"testing"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

func TestIsValidAcceptsTrafficStateEvents(t *testing.T) {
	tests := []struct {
		name        string
		observation matching.MatchedObservation
	}{
		{
			name:        "online event without map matching",
			observation: validObservation("ONLINE"),
		},
		{
			name: "in trip event with zero speed",
			observation: func() matching.MatchedObservation {
				observation := validObservation("IN TRIP")
				observation.Speed = 0
				return observation
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !IsValid(test.observation) {
				t.Fatal("expected observation to be valid")
			}
		})
	}
}

func TestIsValidRejectsInvalidTrafficStateEvents(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*matching.MatchedObservation)
	}{
		{name: "missing graph version", modify: func(input *matching.MatchedObservation) { input.GraphVersion = " " }},
		{name: "blank driver", modify: func(input *matching.MatchedObservation) { input.DriverID = " " }},
		{name: "negative point index", modify: func(input *matching.MatchedObservation) { input.PointIndex = -1 }},
		{name: "zero timestamp", modify: func(input *matching.MatchedObservation) { input.RecordedAt = time.Time{} }},
		{name: "negative speed", modify: func(input *matching.MatchedObservation) { input.Speed = -1 }},
		{name: "NaN speed", modify: func(input *matching.MatchedObservation) { input.Speed = math.NaN() }},
		{name: "infinite speed", modify: func(input *matching.MatchedObservation) { input.Speed = math.Inf(1) }},
		{name: "unsupported status", modify: func(input *matching.MatchedObservation) { input.Status = "OFFLINE" }},
		{name: "unsupported vehicle", modify: func(input *matching.MatchedObservation) { input.VehicleType = "truck" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation := validObservation("IN TRIP")
			test.modify(&observation)
			if IsValid(observation) {
				t.Fatal("expected observation to be invalid")
			}
		})
	}
}

func TestCanAggregate(t *testing.T) {
	zeroKey := int64(0)
	negativeKey := int64(-1)

	tests := []struct {
		name        string
		observation matching.MatchedObservation
		want        bool
	}{
		{
			name: "matched in trip event with traversal key zero",
			observation: func() matching.MatchedObservation {
				observation := validObservation("IN TRIP")
				observation.Matched = true
				observation.TraversalKey = &zeroKey
				return observation
			}(),
			want: true,
		},
		{
			name: "matched eligible traversal",
			observation: func() matching.MatchedObservation {
				observation := validObservation("IN TRIP")
				observation.Matched = true
				observation.TraversalKey = &zeroKey
				return observation
			}(),
			want: true,
		},
		{
			name: "GraphHopper marks traversal ineligible for traffic",
			observation: func() matching.MatchedObservation {
				observation := validObservation("IN TRIP")
				observation.Matched = true
				observation.TraversalKey = &zeroKey
				observation.TrafficEligible = false
				return observation
			}(),
			want: false,
		},
		{name: "online event", observation: validObservation("ONLINE"), want: false},
		{name: "unmatched in trip event", observation: validObservation("IN TRIP"), want: false},
		{
			name: "matched in trip event without traversal key",
			observation: func() matching.MatchedObservation {
				observation := validObservation("IN TRIP")
				observation.Matched = true
				return observation
			}(),
			want: false,
		},
		{
			name: "matched in trip event with negative traversal key",
			observation: func() matching.MatchedObservation {
				observation := validObservation("IN TRIP")
				observation.Matched = true
				observation.TraversalKey = &negativeKey
				return observation
			}(),
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CanAggregate(test.observation); got != test.want {
				t.Fatalf("CanAggregate() = %t, want %t", got, test.want)
			}
		})
	}
}

func validObservation(status string) matching.MatchedObservation {
	return matching.MatchedObservation{
		GraphVersion:    "vietnam-20260730-motorcycle-v1",
		DriverID:        "driver-1",
		PointIndex:      0,
		RecordedAt:      time.Date(2026, time.July, 24, 5, 0, 0, 0, time.UTC),
		Speed:           10,
		Status:          status,
		VehicleType:     "bike",
		TrafficEligible: true,
	}
}
