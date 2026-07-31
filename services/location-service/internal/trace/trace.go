package trace

import (
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
)

type Trace struct {
	DriverID  string
	Points    []gps.CanonicalEvent
	StartedAt time.Time
	EndedAt   time.Time
}
