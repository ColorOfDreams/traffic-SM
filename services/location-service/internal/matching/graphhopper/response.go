package graphhopper

type matchResponse struct {
	MatchedPoints []matchedPointResponse `json:"matched_points"`
}

type matchedPointResponse struct {
	PointIndex         int      `json:"point_index"`
	Matched            bool     `json:"matched"`
	EligibleForTraffic *bool    `json:"eligible_for_traffic"`
	TraversalKey       *int64   `json:"traversal_key"`
	MaxSpeedKMH        *float64 `json:"max_speed_kmh"`
}
