package traffic

import (
	"context"
	"testing"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

func TestMaxSpeedReferenceStoreNormalizesBike(t *testing.T) {
	store := NewMaxSpeedReferenceStore()
	traversalKey := int64(11916856)
	maxSpeedMPS := 50.0 / 3.6

	err := store.Record(matching.MatchedObservation{
		GraphVersion: "graph-v1",
		TraversalKey: &traversalKey,
		VehicleType:  "bike",
		MaxSpeedMPS:  &maxSpeedMPS,
	})
	if err != nil {
		t.Fatal(err)
	}

	reference, found, err := store.Get(
		context.Background(),
		"graph-v1",
		traversalKey,
		"motorcycle",
	)
	if err != nil || !found {
		t.Fatalf("Get() found=%v error=%v", found, err)
	}
	if reference.SpeedMPS != maxSpeedMPS || reference.Source != "graphhopper_max_speed" {
		t.Fatalf("unexpected reference %#v", reference)
	}
}

func TestMaxSpeedReferenceStoreIgnoresMissingMaxSpeed(t *testing.T) {
	store := NewMaxSpeedReferenceStore()
	traversalKey := int64(10)

	if err := store.Record(matching.MatchedObservation{
		GraphVersion: "graph-v1",
		TraversalKey: &traversalKey,
		VehicleType:  "car",
	}); err != nil {
		t.Fatal(err)
	}

	_, found, err := store.Get(context.Background(), "graph-v1", traversalKey, "car")
	if err != nil || found {
		t.Fatalf("Get() found=%v error=%v, want missing reference", found, err)
	}
}
