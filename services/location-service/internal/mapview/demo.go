package mapview

import (
	"sort"
	"time"
)

const demoTraversalKey int64 = 11916856

type DemoFlow struct {
	TraceID      string            `json:"trace_id"`
	VehicleType  string            `json:"vehicle_type"`
	TraversalKey int64             `json:"traversal_key"`
	RoadClass    string            `json:"road_class"`
	GeneratedAt  time.Time         `json:"generated_at"`
	Observations []DemoObservation `json:"observations"`
	Aggregation  DemoAggregation   `json:"aggregation"`
}

type DemoObservation struct {
	DriverID       string  `json:"driver_id"`
	PointIndex     int     `json:"point_index"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	SpeedMPS       float64 `json:"speed_mps"`
	BehaviorState  string  `json:"behavior_state"`
	Eligible       bool    `json:"eligible_for_traffic"`
	DecisionReason string  `json:"decision_reason"`
}

type DemoAggregation struct {
	EligibleDriverCount int     `json:"eligible_driver_count"`
	TotalDriverCount    int     `json:"total_driver_count"`
	CurrentSpeedMPS     float64 `json:"current_speed_mps"`
	ReferenceSpeedMPS   float64 `json:"reference_speed_mps"`
	SpeedRatio          float64 `json:"speed_ratio"`
	CongestionScore     float64 `json:"congestion_score"`
	Level               string  `json:"level"`
}

func NewDemoFlow(now time.Time) DemoFlow {
	observations := []DemoObservation{
		{
			DriverID:       "1",
			PointIndex:     0,
			Latitude:       10.778242,
			Longitude:      106.701935,
			SpeedMPS:       0,
			BehaviorState:  "TRAFFIC_WAIT",
			Eligible:       true,
			DecisionReason: "IN TRIP, matched vào tertiary traversal 11916856; giữ speed=0.",
		},
		{
			DriverID:       "1",
			PointIndex:     5,
			Latitude:       10.778357,
			Longitude:      106.70176,
			SpeedMPS:       2.2753384,
			BehaviorState:  "ROAD_CLASS_FILTERED",
			Eligible:       false,
			DecisionReason: "GraphHopper match vào road_class=service nên không tính traffic.",
		},
		{
			DriverID:       "1",
			PointIndex:     8,
			Latitude:       10.778379,
			Longitude:      106.70173,
			SpeedMPS:       0.06876571,
			BehaviorState:  "ROAD_CLASS_FILTERED",
			Eligible:       false,
			DecisionReason: "GraphHopper match vào road_class=service nên không tính traffic.",
		},
	}
	referenceSpeed := 10.0
	currentSpeed := medianEligibleSpeed(observations)
	ratio := currentSpeed / referenceSpeed

	return DemoFlow{
		TraceID:      "fake-gps-driver-1-20260724-0210",
		VehicleType:  "motorcycle",
		TraversalKey: demoTraversalKey,
		RoadClass:    "tertiary",
		GeneratedAt:  now.UTC(),
		Observations: observations,
		Aggregation: DemoAggregation{
			EligibleDriverCount: 1,
			TotalDriverCount:    1,
			CurrentSpeedMPS:     currentSpeed,
			ReferenceSpeedMPS:   referenceSpeed,
			SpeedRatio:          ratio,
			CongestionScore:     1 - ratio,
			Level:               "CONGESTED",
		},
	}
}

func medianEligibleSpeed(observations []DemoObservation) float64 {
	speeds := make([]float64, 0, len(observations))
	for _, observation := range observations {
		if observation.Eligible {
			speeds = append(speeds, observation.SpeedMPS)
		}
	}
	if len(speeds) == 0 {
		return 0
	}
	sort.Float64s(speeds)
	middle := len(speeds) / 2
	if len(speeds)%2 == 1 {
		return speeds[middle]
	}
	return (speeds[middle-1] + speeds[middle]) / 2
}
