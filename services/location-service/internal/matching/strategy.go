package matching

import (
	"context"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
)

type Strategy interface {
	Match(
		ctx context.Context, // Quản lí timeout , xử lí hoặc kết thúc request
		input trace.Trace, // Trace gps cần map matching
	) ([]MatchedObservation, error)
}
