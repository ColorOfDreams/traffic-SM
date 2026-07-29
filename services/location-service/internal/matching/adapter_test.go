package matching

import (
	"strings"
	"testing"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
)

const graphHopperResponseFixture = `{
  "hints": {},
  "info": {
    "copyrights": ["GraphHopper", "OpenStreetMap contributors"],
    "took": 178,
    "road_data_timestamp": "2026-07-20T20:21:16Z"
  },
  "paths": [{
    "distance": 780.314,
    "weight": 9.223372036854775e12,
    "time": 98886,
    "transfers": 0,
    "legs": [],
    "points_encoded": true,
    "points_encoded_multiplier": 100000,
    "snapped_waypoints": ""
  }],
  "map_matching": {
    "original_distance": 96.67467234565662,
    "distance": 780.314,
    "time": 98886
  },
  "traversal_keys": [5501961, 5501956, 5501944, 5501942, 5501961],
  "matched_transitions": [{
    "from_point_index": 0,
    "to_point_index": 1,
    "segments": [{
      "traversal_key": 5501961,
      "matched_distance_m": 22.361,
      "baseline_duration_ms": 1342
    }, {
      "traversal_key": 5501956,
      "matched_distance_m": 8.884,
      "baseline_duration_ms": 533
    }]
  }, {
    "from_point_index": 1,
    "to_point_index": 2,
    "segments": [{
      "traversal_key": 5501944,
      "matched_distance_m": 165.896,
      "baseline_duration_ms": 9954
    }]
  }]
}`

func TestAdaptGraphHopperResponse(t *testing.T) {
	startedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	matchedAt := startedAt.Add(25 * time.Second)
	trace := gps.Trace{
		TraceID:   "trace-1",
		DriverID:  "driver-1",
		StartedAt: startedAt,
		EndedAt:   startedAt.Add(20 * time.Second),
		Points: []gps.CanonicalEvent{
			{
				Time: "7/29/2026 17:00", TTimestamp: "00:00.0",
				Speed: 10, SpeedAcc: 1.1, Status: "IN TRIP",
			},
			{
				Time: "7/29/2026 17:00", TTimestamp: "00:05.0",
				Speed: 11, SpeedAcc: 1.2, Status: "IN TRIP",
			},
			{
				Time: "7/29/2026 17:00", TTimestamp: "00:10.0",
				Speed: 12, SpeedAcc: 1.3, Status: "OFFLINE",
			},
		},
	}

	got, err := adaptGraphHopperResponse(
		trace,
		"vietnam-20260722",
		matchedAt,
		[]byte(graphHopperResponseFixture),
	)
	if err != nil {
		t.Fatalf("adapt response: %v", err)
	}

	if got.TraceID != trace.TraceID || got.DriverID != trace.DriverID {
		t.Fatalf("unexpected identity: %#v", got)
	}
	if got.StartedAtMS != startedAt.UnixMilli() ||
		got.EndedAtMS != trace.EndedAt.UnixMilli() ||
		got.ObservedDurationMS != 20_000 {
		t.Fatalf("unexpected trace timing: %#v", got)
	}
	if got.OriginalDistanceM != 96.67467234565662 ||
		got.MatchedDistanceM != 780.314 ||
		got.BaselineTimeMS != 98_886 {
		t.Fatalf("unexpected matching metrics: %#v", got)
	}
	if got.GraphVersion != "vietnam-20260722" ||
		got.RoadDataTimestamp != "2026-07-20T20:21:16Z" ||
		got.GraphHopperTookMS != 178 ||
		got.MatchedAtMS != matchedAt.UnixMilli() {
		t.Fatalf("unexpected matching metadata: %#v", got)
	}
	if len(got.GPSPoints) != 3 {
		t.Fatalf("expected 3 matched GPS points, got %#v", got.GPSPoints)
	}
	if got.GPSPoints[0] != (MatchedGPSPoint{
		PointIndex:  0,
		TimestampMS: startedAt.UnixMilli(),
		Time:        "7/29/2026 17:00",
		TTimestamp:  "00:00.0",
		Speed:       10,
		SpeedAcc:    1.1,
		Status:      "IN TRIP",
	}) {
		t.Fatalf("unexpected first matched GPS point: %#v", got.GPSPoints[0])
	}
	if got.GPSPoints[2].PointIndex != 2 ||
		got.GPSPoints[2].TimestampMS != startedAt.Add(10*time.Second).UnixMilli() ||
		got.GPSPoints[2].Speed != 12 ||
		got.GPSPoints[2].SpeedAcc != 1.3 ||
		got.GPSPoints[2].Status != "OFFLINE" {
		t.Fatalf("unexpected last matched GPS point: %#v", got.GPSPoints[2])
	}
	wantKeys := []int64{5501961, 5501956, 5501944, 5501942, 5501961}
	if len(got.TraversalKeys) != len(wantKeys) {
		t.Fatalf("unexpected traversal key count: %v", got.TraversalKeys)
	}
	for index := range wantKeys {
		if got.TraversalKeys[index] != wantKeys[index] {
			t.Fatalf("unexpected traversal keys: %v", got.TraversalKeys)
		}
	}
	if len(got.Fragments) != 3 {
		t.Fatalf("expected 3 traversal fragments, got %#v", got.Fragments)
	}
	first := got.Fragments[0]
	if first.TransitionIndex != 0 ||
		first.FragmentIndex != 0 ||
		first.FromPointIndex != 0 ||
		first.ToPointIndex != 1 ||
		first.TransitionDurationMS != 5_000 ||
		first.TraversalKey != 5501961 ||
		first.EdgeID != 2750980 ||
		first.Forward ||
		first.TrafficSegmentID != "vietnam-20260722_e2750980_r" ||
		first.MatchedDistanceM != 22.361 ||
		first.RoutingDurationMS != 1_342 {
		t.Fatalf("unexpected first traversal fragment: %#v", first)
	}
	if first.FromTimestampMS != startedAt.UnixMilli() ||
		first.ToTimestampMS != startedAt.Add(5*time.Second).UnixMilli() {
		t.Fatalf("fragment timestamps did not come from trace points: %#v", first)
	}
	forward := got.Fragments[1]
	if forward.TraversalKey != 5501956 ||
		forward.EdgeID != 2750978 ||
		!forward.Forward ||
		forward.TrafficSegmentID != "vietnam-20260722_e2750978_f" {
		t.Fatalf("unexpected forward traversal identity: %#v", forward)
	}
	last := got.Fragments[2]
	if last.TransitionIndex != 1 ||
		last.FragmentIndex != 0 ||
		last.FromPointIndex != 1 ||
		last.ToPointIndex != 2 ||
		last.TransitionDurationMS != 5_000 {
		t.Fatalf("unexpected last traversal fragment: %#v", last)
	}
}

func TestAdaptGraphHopperResponseRejectsMissingTraversalKeys(t *testing.T) {
	startedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	trace := gps.Trace{
		TraceID:   "trace-1",
		DriverID:  "driver-1",
		StartedAt: startedAt,
		EndedAt:   startedAt.Add(time.Second),
		Points: []gps.CanonicalEvent{
			{Time: "7/29/2026 17:00", TTimestamp: "00:00.0"},
			{Time: "7/29/2026 17:00", TTimestamp: "00:01.0"},
			{Time: "7/29/2026 17:00", TTimestamp: "00:02.0"},
		},
	}
	response := strings.Replace(
		graphHopperResponseFixture,
		`"traversal_keys": [5501961, 5501956, 5501944, 5501942, 5501961]`,
		`"traversal_keys": []`,
		1,
	)

	_, err := adaptGraphHopperResponse(
		trace,
		"vietnam-20260722",
		startedAt.Add(2*time.Second),
		[]byte(response),
	)
	if err == nil || !strings.Contains(err.Error(), "no traversal keys") {
		t.Fatalf("expected missing traversal keys error, got %v", err)
	}
}

func TestAdaptGraphHopperResponseRejectsTransitionOutsideTrace(t *testing.T) {
	startedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	trace := gps.Trace{
		TraceID:   "trace-1",
		DriverID:  "driver-1",
		StartedAt: startedAt,
		EndedAt:   startedAt.Add(5 * time.Second),
		Points: []gps.CanonicalEvent{
			{Time: "7/29/2026 17:00", TTimestamp: "00:00.0"},
			{Time: "7/29/2026 17:00", TTimestamp: "00:05.0"},
		},
	}
	response := strings.Replace(
		graphHopperResponseFixture,
		`"to_point_index": 2`,
		`"to_point_index": 3`,
		1,
	)

	_, err := adaptGraphHopperResponse(
		trace,
		"vietnam-20260722",
		startedAt.Add(6*time.Second),
		[]byte(response),
	)
	if err == nil || !strings.Contains(err.Error(), "invalid point indexes") {
		t.Fatalf("expected invalid transition indexes error, got %v", err)
	}
}
