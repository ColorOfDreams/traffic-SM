package traffic

import (
	"math"
	"strings"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

func IsValid(input matching.MatchedObservation) bool {
	if strings.TrimSpace(input.GraphVersion) == "" {
		return false
	}
	if strings.TrimSpace(input.DriverID) == "" {
		return false
	}
	if input.PointIndex < 0 || input.RecordedAt.IsZero() {
		return false
	}
	if input.Speed < 0 || math.IsNaN(input.Speed) || math.IsInf(input.Speed, 0) {
		return false
	}
	if input.Status != "IN TRIP" && input.Status != "ONLINE" {
		return false
	}
	if input.VehicleType != "car" && input.VehicleType != "bike" {
		return false
	}
	return true
}

func CanAggregate(input matching.MatchedObservation) bool {
	if !IsValid(input) {
		return false
	}
	return input.Status == "IN TRIP" &&
		input.Matched &&
		input.TrafficEligible &&
		input.TraversalKey != nil &&
		*input.TraversalKey >= 0
}
