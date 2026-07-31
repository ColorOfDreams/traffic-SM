package mapview

import (
	"sort"
	"time"
)

const demoTraversalKey int64 = 366218

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
			DriverID:       "driver-101",
			PointIndex:     1,
			Latitude:       11.49208,
			Longitude:      106.49915,
			SpeedMPS:       2.2,
			BehaviorState:  "TRAFFIC_WAIT",
			Eligible:       true,
			DecisionReason: "Xe vẫn IN_TRIP và nhiều xe cùng traversal đều chậm.",
		},
		{
			DriverID:       "driver-205",
			PointIndex:     1,
			Latitude:       11.49252,
			Longitude:      106.49963,
			SpeedMPS:       3.0,
			BehaviorState:  "TRAFFIC_WAIT",
			Eligible:       true,
			DecisionReason: "Observation phù hợp dòng xe đang di chuyển chậm.",
		},
		{
			DriverID:       "driver-309",
			PointIndex:     1,
			Latitude:       11.4923234,
			Longitude:      106.4994076,
			SpeedMPS:       0,
			BehaviorState:  "BUSINESS_STOP",
			Eligible:       false,
			DecisionReason: "Status transition xác nhận dừng đón/trả khách.",
		},
	}
	referenceSpeed := 10.0
	currentSpeed := medianEligibleSpeed(observations)
	ratio := currentSpeed / referenceSpeed

	return DemoFlow{
		TraceID:      "demo-hcm-secondary-001",
		VehicleType:  "motorcycle",
		TraversalKey: demoTraversalKey,
		RoadClass:    "secondary",
		GeneratedAt:  now.UTC(),
		Observations: observations,
		Aggregation: DemoAggregation{
			EligibleDriverCount: 2,
			TotalDriverCount:    len(observations),
			CurrentSpeedMPS:     currentSpeed,
			ReferenceSpeedMPS:   referenceSpeed,
			SpeedRatio:          ratio,
			CongestionScore:     1 - ratio,
			Level:               "SEVERE",
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
