package matching

import "time"

type MatchedObservation struct {
	GraphVersion    string
	DriverID        string
	PointIndex      int
	RecordedAt      time.Time
	Speed           float64
	Status          string
	VehicleType     string
	Matched         bool
	TrafficEligible bool
	TraversalKey    *int64
	MaxSpeedMPS     *float64
	SnappedLat      *float64
	SnappedLng      *float64
	SnapDistanceM   *float64
}
