package matching

type graphHopperResponse struct {
	Info struct {
		TookMS            int64  `json:"took"`
		RoadDataTimestamp string `json:"road_data_timestamp"`
	} `json:"info"`
	Paths              []graphHopperPath              `json:"paths"`
	MapMatching        *graphHopperMatching           `json:"map_matching"`
	TraversalKeys      []int64                        `json:"traversal_keys"`
	MatchedTransitions []graphHopperMatchedTransition `json:"matched_transitions"`
}

type graphHopperPath struct {
	DistanceM float64 `json:"distance"`
	TimeMS    int64   `json:"time"`
}

type graphHopperMatching struct {
	OriginalDistanceM float64 `json:"original_distance"`
	DistanceM         float64 `json:"distance"`
	TimeMS            int64   `json:"time"`
}

type graphHopperMatchedTransition struct {
	FromPointIndex int                         `json:"from_point_index"`
	ToPointIndex   int                         `json:"to_point_index"`
	Segments       []graphHopperMatchedSegment `json:"segments"`
}

type graphHopperMatchedSegment struct {
	TraversalKey       int64   `json:"traversal_key"`
	MatchedDistanceM   float64 `json:"matched_distance_m"`
	BaselineDurationMS int64   `json:"baseline_duration_ms"`
}
