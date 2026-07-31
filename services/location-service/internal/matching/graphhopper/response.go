package graphhopper

type matchResponse struct {
	MatchedPoints []matchedPointResponse `json:"matched_points"`
}

type matchedPointResponse struct {
	PointIndex    int      `json:"point_index"`
	Matched       bool     `json:"matched"`
	TraversalKey  *int64   `json:"traversal_key"`
	SnappedLat    *float64 `json:"snapped_lat"`
	SnappedLon    *float64 `json:"snapped_lon"`
	SnapDistanceM *float64 `json:"snap_distance_m"`
}
