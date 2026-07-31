package graphhopper

import (
	"strings"
	"testing"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
)

func TestAdaptResponseMapsPointsInTraceOrder(t *testing.T) {
	input := testTrace()
	traversalKey := int64(145)
	snappedLat := 21.02805
	snappedLon := 105.83412
	snapDistanceM := 10.4

	response := matchResponse{
		MatchedPoints: []matchedPointResponse{
			{
				PointIndex: 1,
				Matched:    false,
			},
			{
				PointIndex:    0,
				Matched:       true,
				TraversalKey:  &traversalKey,
				SnappedLat:    &snappedLat,
				SnappedLon:    &snappedLon,
				SnapDistanceM: &snapDistanceM,
			},
		},
	}

	observations, err := adaptResponse(input, response)
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
	assertInt64Pointer(t, "TraversalKey", matched.TraversalKey, traversalKey)
	assertFloat64Pointer(t, "SnappedLat", matched.SnappedLat, snappedLat)
	assertFloat64Pointer(t, "SnappedLng", matched.SnappedLng, snappedLon)
	assertFloat64Pointer(
		t,
		"SnapDistanceM",
		matched.SnapDistanceM,
		snapDistanceM,
	)

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
	if unmatched.SnappedLat != nil {
		t.Errorf("unmatched SnappedLat = %f, want nil", *unmatched.SnappedLat)
	}
	if unmatched.SnappedLng != nil {
		t.Errorf("unmatched SnappedLng = %f, want nil", *unmatched.SnappedLng)
	}
	if unmatched.SnapDistanceM != nil {
		t.Errorf(
			"unmatched SnapDistanceM = %f, want nil",
			*unmatched.SnapDistanceM,
		)
	}
}

func TestAdaptResponseCopiesMatchingValues(t *testing.T) {
	input := testTraceWithPointCount(1)
	traversalKey := int64(145)
	snappedLat := 21.02805
	snappedLon := 105.83412
	snapDistanceM := 10.4

	response := matchResponse{
		MatchedPoints: []matchedPointResponse{
			{
				PointIndex:    0,
				Matched:       true,
				TraversalKey:  &traversalKey,
				SnappedLat:    &snappedLat,
				SnappedLon:    &snappedLon,
				SnapDistanceM: &snapDistanceM,
			},
		},
	}

	observations, err := adaptResponse(input, response)
	if err != nil {
		t.Fatalf("adaptResponse() error = %v, want nil", err)
	}

	traversalKey = 999
	snappedLat = 1
	snappedLon = 2
	snapDistanceM = 3

	assertInt64Pointer(t, "TraversalKey", observations[0].TraversalKey, 145)
	assertFloat64Pointer(t, "SnappedLat", observations[0].SnappedLat, 21.02805)
	assertFloat64Pointer(t, "SnappedLng", observations[0].SnappedLng, 105.83412)
	assertFloat64Pointer(t, "SnapDistanceM", observations[0].SnapDistanceM, 10.4)
}

func TestAdaptResponseRejectsPointCountMismatch(t *testing.T) {
	input := testTrace()
	response := matchResponse{
		MatchedPoints: []matchedPointResponse{
			{PointIndex: 0},
		},
	}

	observations, err := adaptResponse(input, response)

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

			observations, err := adaptResponse(input, response)

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

	observations, err := adaptResponse(input, response)

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

	observations, err := adaptResponse(input, response)

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

func TestAdaptResponseRejectsMatchedPointWithMissingFields(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*matchedPointResponse)
	}{
		{
			name: "traversal key",
			configure: func(point *matchedPointResponse) {
				point.TraversalKey = nil
			},
		},
		{
			name: "snapped latitude",
			configure: func(point *matchedPointResponse) {
				point.SnappedLat = nil
			},
		},
		{
			name: "snapped longitude",
			configure: func(point *matchedPointResponse) {
				point.SnappedLon = nil
			},
		},
		{
			name: "snap distance",
			configure: func(point *matchedPointResponse) {
				point.SnapDistanceM = nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := testTraceWithPointCount(1)
			point := validMatchedPointResponse(0)
			test.configure(&point)
			response := matchResponse{
				MatchedPoints: []matchedPointResponse{point},
			}

			observations, err := adaptResponse(input, response)

			if err == nil {
				t.Fatal("adaptResponse() error = nil, want missing fields error")
			}
			if !strings.Contains(
				err.Error(),
				"matched point 0 is missing matching fields",
			) {
				t.Fatalf(
					"adaptResponse() error = %q, want missing fields error",
					err,
				)
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
	snappedLat := 21.02805
	snappedLon := 105.83412
	snapDistanceM := 10.4

	return matchedPointResponse{
		PointIndex:    pointIndex,
		Matched:       true,
		TraversalKey:  &traversalKey,
		SnappedLat:    &snappedLat,
		SnappedLon:    &snappedLon,
		SnapDistanceM: &snapDistanceM,
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

func assertFloat64Pointer(
	t *testing.T,
	name string,
	got *float64,
	want float64,
) {
	t.Helper()

	if got == nil {
		t.Fatalf("%s = nil, want %f", name, want)
	}
	if *got != want {
		t.Errorf("%s = %f, want %f", name, *got, want)
	}
}
