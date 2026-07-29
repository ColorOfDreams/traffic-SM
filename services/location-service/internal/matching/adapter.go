package matching

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
)

func adaptGraphHopperResponse(
	trace gps.Trace,
	graphVersion string,
	matchedAt time.Time,
	responseBody []byte,
) (MatchedTrace, error) {
	var response graphHopperResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return MatchedTrace{}, fmt.Errorf("decode GraphHopper response: %w", err)
	}

	if strings.TrimSpace(trace.TraceID) == "" || strings.TrimSpace(trace.DriverID) == "" {
		return MatchedTrace{}, errors.New("trace_id and driver_id are required")
	}
	if !trace.EndedAt.After(trace.StartedAt) {
		return MatchedTrace{}, errors.New("trace ended_at must be later than started_at")
	}
	graphVersion = strings.TrimSpace(graphVersion)
	if graphVersion == "" {
		return MatchedTrace{}, errors.New("graph version is required")
	}
	if matchedAt.IsZero() {
		return MatchedTrace{}, errors.New("matched_at is required")
	}
	if len(response.Paths) == 0 {
		return MatchedTrace{}, errors.New("GraphHopper response has no matched path")
	}
	if response.MapMatching == nil {
		return MatchedTrace{}, errors.New("GraphHopper response has no map_matching result")
	}
	if len(response.TraversalKeys) == 0 {
		return MatchedTrace{}, errors.New("GraphHopper response has no traversal keys")
	}
	if len(response.MatchedTransitions) == 0 {
		return MatchedTrace{}, errors.New("GraphHopper response has no matched transitions")
	}
	if response.Info.TookMS < 0 {
		return MatchedTrace{}, errors.New("GraphHopper took must not be negative")
	}
	if err := validateDistance("path distance", response.Paths[0].DistanceM, false); err != nil {
		return MatchedTrace{}, err
	}
	if response.Paths[0].TimeMS < 0 {
		return MatchedTrace{}, errors.New("path time must not be negative")
	}
	if err := validateDistance(
		"original distance",
		response.MapMatching.OriginalDistanceM,
		false,
	); err != nil {
		return MatchedTrace{}, err
	}
	if err := validateDistance("matched distance", response.MapMatching.DistanceM, true); err != nil {
		return MatchedTrace{}, err
	}
	if response.MapMatching.TimeMS <= 0 {
		return MatchedTrace{}, errors.New("map-matching time must be positive")
	}
	for index, key := range response.TraversalKeys {
		if key < 0 {
			return MatchedTrace{}, fmt.Errorf("traversal key at index %d must not be negative", index)
		}
	}

	roadDataTimestamp := strings.TrimSpace(response.Info.RoadDataTimestamp)
	if roadDataTimestamp == "" {
		return MatchedTrace{}, errors.New("road data timestamp is required")
	}
	if _, err := time.Parse(time.RFC3339, roadDataTimestamp); err != nil {
		return MatchedTrace{}, fmt.Errorf("parse road data timestamp: %w", err)
	}

	fragments, err := adaptTraversalFragments(
		trace,
		graphVersion,
		response.MatchedTransitions,
	)
	if err != nil {
		return MatchedTrace{}, err
	}
	gpsPoints, err := adaptGPSPoints(trace)
	if err != nil {
		return MatchedTrace{}, err
	}

	return MatchedTrace{
		TraceID:            trace.TraceID,
		DriverID:           trace.DriverID,
		StartedAtMS:        trace.StartedAt.UnixMilli(),
		EndedAtMS:          trace.EndedAt.UnixMilli(),
		ObservedDurationMS: trace.EndedAt.Sub(trace.StartedAt).Milliseconds(),
		OriginalDistanceM:  response.MapMatching.OriginalDistanceM,
		MatchedDistanceM:   response.MapMatching.DistanceM,
		BaselineTimeMS:     response.MapMatching.TimeMS,
		TraversalKeys:      append([]int64(nil), response.TraversalKeys...),
		GraphVersion:       graphVersion,
		RoadDataTimestamp:  roadDataTimestamp,
		GraphHopperTookMS:  response.Info.TookMS,
		MatchedAtMS:        matchedAt.UTC().UnixMilli(),
		GPSPoints:          gpsPoints,
		Fragments:          fragments,
	}, nil
}

func adaptGPSPoints(trace gps.Trace) ([]MatchedGPSPoint, error) {
	points := make([]MatchedGPSPoint, len(trace.Points))
	for pointIndex, point := range trace.Points {
		recordedAt, err := point.RecordedAt()
		if err != nil {
			return nil, fmt.Errorf("derive matched GPS point %d time: %w", pointIndex, err)
		}
		points[pointIndex] = MatchedGPSPoint{
			PointIndex:  pointIndex,
			TimestampMS: recordedAt.UnixMilli(),
			Time:        point.Time,
			TTimestamp:  point.TTimestamp,
			Speed:       point.Speed,
			SpeedAcc:    point.SpeedAcc,
			Status:      point.Status,
		}
	}
	return points, nil
}

func adaptTraversalFragments(
	trace gps.Trace,
	graphVersion string,
	transitions []graphHopperMatchedTransition,
) ([]TraversalFragment, error) {
	if len(trace.Points) < 2 {
		return nil, errors.New("trace requires at least two points for matched transitions")
	}

	fragments := make([]TraversalFragment, 0)
	for transitionIndex, transition := range transitions {
		if transition.FromPointIndex < 0 ||
			transition.ToPointIndex <= transition.FromPointIndex ||
			transition.ToPointIndex >= len(trace.Points) {
			return nil, fmt.Errorf(
				"matched transition %d has invalid point indexes %d -> %d for %d trace points",
				transitionIndex,
				transition.FromPointIndex,
				transition.ToPointIndex,
				len(trace.Points),
			)
		}
		if len(transition.Segments) == 0 {
			return nil, fmt.Errorf("matched transition %d has no segments", transitionIndex)
		}

		fromTimestamp, err := trace.Points[transition.FromPointIndex].RecordedAt()
		if err != nil {
			return nil, fmt.Errorf(
				"derive matched transition %d from-point time: %w",
				transitionIndex,
				err,
			)
		}
		toTimestamp, err := trace.Points[transition.ToPointIndex].RecordedAt()
		if err != nil {
			return nil, fmt.Errorf(
				"derive matched transition %d to-point time: %w",
				transitionIndex,
				err,
			)
		}
		if !toTimestamp.After(fromTimestamp) {
			return nil, fmt.Errorf("matched transition %d duration must be positive", transitionIndex)
		}

		for fragmentIndex, segment := range transition.Segments {
			if segment.TraversalKey < 0 {
				return nil, fmt.Errorf(
					"matched transition %d segment %d traversal key must not be negative",
					transitionIndex,
					fragmentIndex,
				)
			}
			if err := validateDistance(
				fmt.Sprintf(
					"matched transition %d segment %d distance",
					transitionIndex,
					fragmentIndex,
				),
				segment.MatchedDistanceM,
				true,
			); err != nil {
				return nil, err
			}
			if segment.BaselineDurationMS < 0 {
				return nil, fmt.Errorf(
					"matched transition %d segment %d baseline duration must not be negative",
					transitionIndex,
					fragmentIndex,
				)
			}

			edgeID, forward, trafficSegmentID := traversalIdentity(
				graphVersion,
				segment.TraversalKey,
			)
			fragments = append(fragments, TraversalFragment{
				TransitionIndex:      transitionIndex,
				FragmentIndex:        fragmentIndex,
				FromPointIndex:       transition.FromPointIndex,
				ToPointIndex:         transition.ToPointIndex,
				FromTimestampMS:      fromTimestamp.UnixMilli(),
				ToTimestampMS:        toTimestamp.UnixMilli(),
				TransitionDurationMS: toTimestamp.Sub(fromTimestamp).Milliseconds(),
				TraversalKey:         segment.TraversalKey,
				EdgeID:               edgeID,
				Forward:              forward,
				TrafficSegmentID:     trafficSegmentID,
				MatchedDistanceM:     segment.MatchedDistanceM,
				RoutingDurationMS:    segment.BaselineDurationMS,
			})
		}
	}
	return fragments, nil
}

func traversalIdentity(graphVersion string, traversalKey int64) (int64, bool, string) {
	edgeID := traversalKey / 2
	forward := traversalKey%2 == 0
	directionSuffix := "r"
	if forward {
		directionSuffix = "f"
	}
	return edgeID, forward, fmt.Sprintf(
		"%s_e%d_%s",
		graphVersion,
		edgeID,
		directionSuffix,
	)
}

func validateDistance(name string, value float64, positive bool) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s must be finite", name)
	}
	if positive && value <= 0 {
		return fmt.Errorf("%s must be positive", name)
	}
	if !positive && value < 0 {
		return fmt.Errorf("%s must not be negative", name)
	}
	return nil
}
