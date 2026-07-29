package eventstream

import (
	"context"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

// Matcher là hợp đồng giữa pipeline Kafka và map-matching provider.
type Matcher interface {
	Match(context.Context, gps.Trace) (matching.MatchedTrace, error)
}
