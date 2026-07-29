package matching

type MatchedTrace struct {
	TraceID              string              `json:"trace_id"`
	DriverID             string              `json:"driver_id"`
	StartedAtMS          int64               `json:"started_at_ms"`
	EndedAtMS            int64               `json:"ended_at_ms"`
	ObservedDurationMS   int64               `json:"observed_duration_ms"`
	OriginalDistanceM    float64             `json:"original_distance_m"`
	MatchedDistanceM     float64             `json:"matched_distance_m"`
	BaselineTimeMS       int64               `json:"baseline_time_ms"`
	TraversalKeys        []int64             `json:"traversal_keys"`
	GraphVersion         string              `json:"graph_version"`
	RoadDataTimestamp    string              `json:"road_data_timestamp"`
	GraphHopperTookMS    int64               `json:"graphhopper_took_ms"`
	MatchedAtMS          int64               `json:"matched_at_ms"`
	MatchedFragmentCount int                 `json:"matched_fragment_count"`
	DroppedFragmentCount int                 `json:"dropped_fragment_count"`
	GPSPoints            []MatchedGPSPoint   `json:"gps_points"`
	Fragments            []TraversalFragment `json:"fragments"`
}

type MatchedGPSPoint struct {
	PointIndex  int     `json:"point_index"`
	TimestampMS int64   `json:"timestamp_ms"`
	Time        string  `json:"time"`
	TTimestamp  string  `json:"t_timestamp"`
	Speed       float64 `json:"speed"`
	SpeedAcc    float64 `json:"speed_acc"`
	Status      string  `json:"status"`
}

type TraversalFragment struct {
	TransitionIndex      int     `json:"transition_index"`
	FragmentIndex        int     `json:"fragment_index"`
	FromPointIndex       int     `json:"from_point_index"`
	ToPointIndex         int     `json:"to_point_index"`
	FromTimestampMS      int64   `json:"from_timestamp_ms"`
	ToTimestampMS        int64   `json:"to_timestamp_ms"`
	TransitionDurationMS int64   `json:"transition_duration_ms"`
	TraversalKey         int64   `json:"traversal_key"`
	EdgeID               int64   `json:"edge_id"`
	Forward              bool    `json:"forward"`
	TrafficSegmentID     string  `json:"traffic_segment_id"`
	MatchedDistanceM     float64 `json:"matched_distance_m"`
	RoutingDurationMS    int64   `json:"routing_duration_ms"`
}
