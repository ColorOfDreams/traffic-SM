package traffic

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

type ReferenceSpeed struct {
	SpeedMPS float64
	Source   string
}

type ReferenceSpeedStore interface {
	Get(
		ctx context.Context,
		graphVersion string,
		traversalKey int64,
		vehicleType string,
	) (reference ReferenceSpeed, found bool, err error)
}

type ReferenceSpeedRecorder interface {
	Record(input matching.MatchedObservation) error
}

type referenceSpeedKey struct {
	graphVersion string
	traversalKey int64
	vehicleType  string
}

type MaxSpeedReferenceStore struct {
	mu     sync.RWMutex
	speeds map[referenceSpeedKey]ReferenceSpeed
}

func NewMaxSpeedReferenceStore() *MaxSpeedReferenceStore {
	return &MaxSpeedReferenceStore{
		speeds: make(map[referenceSpeedKey]ReferenceSpeed),
	}
}

func (store *MaxSpeedReferenceStore) Record(
	input matching.MatchedObservation,
) error {
	if store == nil {
		return fmt.Errorf("max speed reference store is not initialized")
	}
	if input.TraversalKey == nil || input.MaxSpeedMPS == nil {
		return nil
	}
	if input.GraphVersion == "" || *input.TraversalKey < 0 {
		return fmt.Errorf("matched observation has invalid reference key")
	}
	if *input.MaxSpeedMPS <= 0 || math.IsNaN(*input.MaxSpeedMPS) || math.IsInf(*input.MaxSpeedMPS, 0) {
		return fmt.Errorf("max speed must be finite and positive")
	}

	key := referenceSpeedKey{
		graphVersion: input.GraphVersion,
		traversalKey: *input.TraversalKey,
		vehicleType:  normalizeVehicleType(input.VehicleType),
	}
	store.mu.Lock()
	store.speeds[key] = ReferenceSpeed{
		SpeedMPS: *input.MaxSpeedMPS,
		Source:   "graphhopper_max_speed",
	}
	store.mu.Unlock()
	return nil
}

func (store *MaxSpeedReferenceStore) Get(
	_ context.Context,
	graphVersion string,
	traversalKey int64,
	vehicleType string,
) (ReferenceSpeed, bool, error) {
	if store == nil {
		return ReferenceSpeed{}, false, fmt.Errorf("max speed reference store is not initialized")
	}
	key := referenceSpeedKey{
		graphVersion: graphVersion,
		traversalKey: traversalKey,
		vehicleType:  normalizeVehicleType(vehicleType),
	}
	store.mu.RLock()
	reference, found := store.speeds[key]
	store.mu.RUnlock()
	return reference, found, nil
}
