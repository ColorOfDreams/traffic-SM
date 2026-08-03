package graphhopper

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
)

const testGraphVersion = "vietnam-20260730-motorcycle-v1"

func TestAdaptResponseMapsPointsInTraceOrder(t *testing.T) {
	input := testTrace()
	traversalKey := int64(145)
	maxSpeedKMH := 50.0
	eligibleForTraffic := true

	response := matchResponse{
		MatchedPoints: []matchedPointResponse{
			{
				PointIndex: 1,
				Matched:    false,
			},
			{
				PointIndex:         0,
				Matched:            true,
				EligibleForTraffic: &eligibleForTraffic,
				TraversalKey:       &traversalKey,
				MaxSpeedKMH:        &maxSpeedKMH,
			},
		},
	}

	observations, err := adaptResponse(input, response, testGraphVersion)
	if err != nil {
		t.Fatalf("adaptResponse() error = %v, want nil", err)
	}

	if len(observations) != len(input.Points) {
		t.Fatalf(
			"len(observations) = %d, want %d",
			len(observations),
			len(input.Points),
		)
	}

	matched := observations[0]
	if matched.GraphVersion != testGraphVersion {
		t.Errorf("matched GraphVersion = %q, want %q", matched.GraphVersion, testGraphVersion)
	}
	if matched.PointIndex != 0 {
		t.Errorf("matched PointIndex = %d, want 0", matched.PointIndex)
	}
	if matched.DriverID != input.Points[0].DriverID {
		t.Errorf(
			"matched DriverID = %q, want %q",
			matched.DriverID,
			input.Points[0].DriverID,
		)
	}
	if !matched.RecordedAt.Equal(input.Points[0].RecordedAt) {
		t.Errorf(
			"matched RecordedAt = %s, want %s",
			matched.RecordedAt,
			input.Points[0].RecordedAt,
		)
	}
	if matched.Speed != input.Points[0].Speed {
		t.Errorf(
			"matched Speed = %f, want %f",
			matched.Speed,
			input.Points[0].Speed,
		)
	}
	if matched.Status != input.Points[0].Status {
		t.Errorf(
			"matched Status = %q, want %q",
			matched.Status,
			input.Points[0].Status,
		)
	}
	if matched.VehicleType != input.Points[0].VehicleType {
		t.Errorf(
			"matched VehicleType = %q, want %q",
			matched.VehicleType,
			input.Points[0].VehicleType,
		)
	}
	if !matched.Matched {
		t.Error("matched Matched = false, want true")
	}
	if !matched.TrafficEligible {
		t.Error("matched TrafficEligible = false, want true")
	}
	assertInt64Pointer(t, "TraversalKey", matched.TraversalKey, traversalKey)
	if matched.MaxSpeedMPS == nil || math.Abs(*matched.MaxSpeedMPS-50.0/3.6) > 1e-9 {
		t.Errorf("matched MaxSpeedMPS = %v, want %v", matched.MaxSpeedMPS, 50.0/3.6)
	}

	unmatched := observations[1]
	if unmatched.PointIndex != 1 {
		t.Errorf("unmatched PointIndex = %d, want 1", unmatched.PointIndex)
	}
	if unmatched.DriverID != input.Points[1].DriverID {
		t.Errorf(
			"unmatched DriverID = %q, want %q",
			unmatched.DriverID,
			input.Points[1].DriverID,
		)
	}
	if unmatched.Matched {
		t.Error("unmatched Matched = true, want false")
	}
	if unmatched.TraversalKey != nil {
		t.Errorf(
			"unmatched TraversalKey = %d, want nil",
			*unmatched.TraversalKey,
		)
	}
	if unmatched.GraphVersion != testGraphVersion {
		t.Errorf("unmatched GraphVersion = %q, want %q", unmatched.GraphVersion, testGraphVersion)
	}
}

func TestAdaptResponseCopiesMatchingValues(t *testing.T) {
	input := testTraceWithPointCount(1)
	traversalKey := int64(145)
	eligibleForTraffic := true

	response := matchResponse{
		MatchedPoints: []matchedPointResponse{
			{
				PointIndex:         0,
				Matched:            true,
				EligibleForTraffic: &eligibleForTraffic,
				TraversalKey:       &traversalKey,
			},
		},
	}

	observations, err := adaptResponse(input, response, testGraphVersion)
	if err != nil {
		t.Fatalf("adaptResponse() error = %v, want nil", err)
	}

	traversalKey = 999

	assertInt64Pointer(t, "TraversalKey", observations[0].TraversalKey, 145)
}

func TestAdaptResponseRejectsPointCountMismatch(t *testing.T) {
	input := testTrace()
	response := matchResponse{
		MatchedPoints: []matchedPointResponse{
			{PointIndex: 0},
		},
	}

	observations, err := adaptResponse(input, response, testGraphVersion)

	if err == nil {
		t.Fatal("adaptResponse() error = nil, want point count error")
	}
	if !strings.Contains(
		err.Error(),
		"matched point count 1 does not match trace point count 2",
	) {
		t.Fatalf("adaptResponse() error = %q, want point count error", err)
	}
	if observations != nil {
		t.Fatalf("adaptResponse() observations = %#v, want nil", observations)
	}
}

func TestAdaptResponseRejectsPointIndexOutsideTrace(t *testing.T) {
	tests := []struct {
		name       string
		pointIndex int
	}{
		{name: "negative", pointIndex: -1},
		{name: "equal to point count", pointIndex: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := testTraceWithPointCount(1)
			response := matchResponse{
				MatchedPoints: []matchedPointResponse{
					{PointIndex: test.pointIndex},
				},
			}

			observations, err := adaptResponse(input, response, testGraphVersion)

			if err == nil {
				t.Fatal("adaptResponse() error = nil, want bounds error")
			}
			if !strings.Contains(err.Error(), "is outside trace") {
				t.Fatalf("adaptResponse() error = %q, want bounds error", err)
			}
			if observations != nil {
				t.Fatalf(
					"adaptResponse() observations = %#v, want nil",
					observations,
				)
			}
		})
	}
}

func TestAdaptResponseRejectsDuplicatePointIndex(t *testing.T) {
	input := testTrace()
	response := matchResponse{
		MatchedPoints: []matchedPointResponse{
			{PointIndex: 0},
			{PointIndex: 0},
		},
	}

	observations, err := adaptResponse(input, response, testGraphVersion)

	if err == nil {
		t.Fatal("adaptResponse() error = nil, want duplicate index error")
	}
	if !strings.Contains(err.Error(), "point_index 0 appears more than once") {
		t.Fatalf("adaptResponse() error = %q, want duplicate index error", err)
	}
	if observations != nil {
		t.Fatalf("adaptResponse() observations = %#v, want nil", observations)
	}
}

func TestAdaptResponseRejectsPointFromAnotherDriver(t *testing.T) {
	input := testTraceWithPointCount(1)
	input.Points[0].DriverID = "driver-2"
	response := matchResponse{
		MatchedPoints: []matchedPointResponse{
			{PointIndex: 0},
		},
	}

	observations, err := adaptResponse(input, response, testGraphVersion)

	if err == nil {
		t.Fatal("adaptResponse() error = nil, want driver mismatch error")
	}
	if !strings.Contains(
		err.Error(),
		`belongs to driver "driver-2" instead of "driver-1"`,
	) {
		t.Fatalf("adaptResponse() error = %q, want driver mismatch error", err)
	}
	if observations != nil {
		t.Fatalf("adaptResponse() observations = %#v, want nil", observations)
	}
}

func TestAdaptResponseRejectsUnmatchedTrafficEligiblePoint(t *testing.T) {
	input := testTraceWithPointCount(1)
	eligibleForTraffic := true
	response := matchResponse{
		MatchedPoints: []matchedPointResponse{{
			PointIndex:         0,
			EligibleForTraffic: &eligibleForTraffic,
		}},
	}

	observations, err := adaptResponse(input, response, testGraphVersion)
	if err == nil {
		t.Fatal("adaptResponse() error = nil, want eligibility error")
	}
	if !strings.Contains(err.Error(), "unmatched point 0 cannot be eligible for traffic") {
		t.Fatalf("adaptResponse() error = %q, want eligibility error", err)
	}
	if observations != nil {
		t.Fatalf("adaptResponse() observations = %#v, want nil", observations)
	}
}

func TestAdaptResponseRejectsMatchedPointWithMissingFields(t *testing.T) {
	input := testTraceWithPointCount(1)
	point := validMatchedPointResponse(0)
	point.TraversalKey = nil
	response := matchResponse{
		MatchedPoints: []matchedPointResponse{point},
	}

	observations, err := adaptResponse(input, response, testGraphVersion)
	if err == nil {
		t.Fatal("adaptResponse() error = nil, want missing traversal key error")
	}
	if !strings.Contains(err.Error(), "matched point 0 is missing traversal_key") {
		t.Fatalf("adaptResponse() error = %q, want missing traversal key error", err)
	}
	if observations != nil {
		t.Fatalf("adaptResponse() observations = %#v, want nil", observations)
	}
}

func TestAdaptResponseRejectsMatchedPointWithoutTrafficEligibility(t *testing.T) {
	input := testTraceWithPointCount(1)
	point := validMatchedPointResponse(0)
	point.EligibleForTraffic = nil
	response := matchResponse{
		MatchedPoints: []matchedPointResponse{point},
	}

	observations, err := adaptResponse(input, response, testGraphVersion)
	if err == nil {
		t.Fatal("adaptResponse() error = nil, want missing eligibility error")
	}
	if !strings.Contains(err.Error(), "matched point 0 is missing eligible_for_traffic") {
		t.Fatalf("adaptResponse() error = %q, want missing eligibility error", err)
	}
	if observations != nil {
		t.Fatalf("adaptResponse() observations = %#v, want nil", observations)
	}
}

func TestAdaptResponseRejectsMissingGraphVersion(t *testing.T) {
	input := testTraceWithPointCount(1)
	response := matchResponse{
		MatchedPoints: []matchedPointResponse{{PointIndex: 0}},
	}

	observations, err := adaptResponse(input, response, " ")
	if err == nil || !strings.Contains(err.Error(), "graph version is required") {
		t.Fatalf("adaptResponse() error = %v, want graph version error", err)
	}
	if observations != nil {
		t.Fatalf("adaptResponse() observations = %#v, want nil", observations)
	}
}

func testTrace() trace.Trace {
	return testTraceWithPointCount(2)
}

func testTraceWithPointCount(pointCount int) trace.Trace {
	baseTime := time.Date(2026, time.July, 31, 4, 0, 0, 0, time.UTC)
	points := make([]gps.CanonicalEvent, pointCount)

	for index := range pointCount {
		points[index] = gps.CanonicalEvent{
			DriverID:    "driver-1",
			RecordedAt:  baseTime.Add(time.Duration(index) * time.Second),
			Speed:       8.5 + float64(index),
			Status:      "IN TRIP",
			VehicleType: "car",
		}
	}

	return trace.Trace{
		DriverID: "driver-1",
		Points:   points,
	}
}

func validMatchedPointResponse(pointIndex int) matchedPointResponse {
	traversalKey := int64(145)
	eligibleForTraffic := true

	return matchedPointResponse{
		PointIndex:         pointIndex,
		Matched:            true,
		EligibleForTraffic: &eligibleForTraffic,
		TraversalKey:       &traversalKey,
	}
}

func assertInt64Pointer(
	t *testing.T,
	name string,
	got *int64,
	want int64,
) {
	t.Helper()

	if got == nil {
		t.Fatalf("%s = nil, want %d", name, want)
	}
	if *got != want {
		t.Errorf("%s = %d, want %d", name, *got, want)
	}
}
