package traffic

import (
	"fmt"
	"sync"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

type WindowKey struct {
	GraphVersion string
	TraversalKey int64
	VehicleType  string
	WindowStart  time.Time
}

type SegmentWindow struct {
	Key WindowKey

	// Speeds giữ mọi sample hợp lệ, kể cả speed=0 và nhiều sample từ cùng driver.
	Speeds []float64

	// DriverIDs chỉ phục vụ distinct-driver count/confidence, không dùng để deduplicate speed.
	DriverIDs map[string]struct{}
}

type Aggregator struct {
	mu             sync.Mutex
	windowDuration time.Duration
	windows        map[WindowKey]*SegmentWindow
}

func NewAggregator(windowDuration time.Duration) (*Aggregator, error) {
	if windowDuration <= 0 {
		return nil, fmt.Errorf("window duration must be positive")
	}

	return &Aggregator{
		windowDuration: windowDuration,
		windows:        make(map[WindowKey]*SegmentWindow),
	}, nil
}

func (aggregator *Aggregator) Add(input matching.MatchedObservation) error {
	if aggregator == nil || aggregator.windowDuration <= 0 || aggregator.windows == nil {
		return fmt.Errorf("aggregator is not initialized")
	}
	if !CanAggregate(input) {
		return fmt.Errorf("observation cannot be aggregated")
	}

	// Cùng directed segment, loại xe và mốc window mới được gom chung một bucket.
	key := WindowKey{
		GraphVersion: input.GraphVersion,
		TraversalKey: *input.TraversalKey,
		VehicleType:  normalizeVehicleType(input.VehicleType),
		WindowStart:  input.RecordedAt.Truncate(aggregator.windowDuration),
	}

	aggregator.mu.Lock()
	defer aggregator.mu.Unlock()

	window, exists := aggregator.windows[key]
	if !exists {
		window = &SegmentWindow{
			Key:       key,
			Speeds:    make([]float64, 0),
			DriverIDs: make(map[string]struct{}),
		}
		aggregator.windows[key] = window
	}

	window.Speeds = append(window.Speeds, input.Speed)
	window.DriverIDs[input.DriverID] = struct{}{}
	return nil
}

func (aggregator *Aggregator) Close(now time.Time) []SegmentWindow {
	if aggregator == nil {
		return nil
	}

	aggregator.mu.Lock()
	defer aggregator.mu.Unlock()

	var closedWindows []SegmentWindow
	for key, window := range aggregator.windows {
		windowEnd := key.WindowStart.Add(aggregator.windowDuration)
		if windowEnd.After(now) {
			continue
		}

		// Sau khi trả window cho calculator, xóa bucket để không giữ lịch sử trong RAM.
		closedWindows = append(closedWindows, *window)
		delete(aggregator.windows, key)
	}

	return closedWindows
}

func normalizeVehicleType(vehicleType string) string {
	if vehicleType == "bike" {
		return "motorcycle"
	}
	return vehicleType
}
