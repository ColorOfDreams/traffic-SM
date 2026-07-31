package matching

import "time"

type MatchedObservation struct {
	DriverID      string
	PointIndex    int
	RecordedAt    time.Time
	Speed         float64
	Status        string
	VehicleType   string
	Matched       bool
	TraversalKey  *int64
	SnappedLat    *float64
	SnappedLng    *float64
	SnapDistanceM *float64
}
