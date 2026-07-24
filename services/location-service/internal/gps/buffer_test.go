package gps

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBufferManagerGroupsByVehicleAndSortsByRecordedAt(t *testing.T) {
	manager := NewBufferManager()

	for _, event := range []CanonicalEvent{
		testEvent("event-003", "vehicle-001", "2026-07-23T04:00:10Z"),
		testEvent("event-002", "vehicle-002", "2026-07-23T04:00:05Z"),
		testEvent("event-001", "vehicle-001", "2026-07-23T04:00:00Z"),
	} {
		if _, err := manager.Add(event); err != nil {
			t.Fatal(err)
		}
	}

	vehicleOne := manager.Events("vehicle-001")
	if len(vehicleOne) != 2 {
		t.Fatalf("expected 2 events for vehicle-001, got %d", len(vehicleOne))
	}
	if vehicleOne[0].EventID != "event-001" || vehicleOne[1].EventID != "event-003" {
		t.Fatalf("events are not ordered by recorded_at: %#v", vehicleOne)
	}

	vehicleTwo := manager.Events("vehicle-002")
	if len(vehicleTwo) != 1 || vehicleTwo[0].EventID != "event-002" {
		t.Fatalf("unexpected vehicle-002 events: %#v", vehicleTwo)
	}
}

func TestBufferManagerTreatsSameEventAsDuplicate(t *testing.T) {
	manager := NewBufferManager()
	event := testEvent("event-001", "vehicle-001", "2026-07-23T04:00:00Z")

	first, err := manager.Add(event)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Add(event)
	if err != nil {
		t.Fatal(err)
	}

	if !first.Added || first.Duplicate {
		t.Fatalf("unexpected first result: %#v", first)
	}
	if second.Added || !second.Duplicate || second.VehicleEventCount != 1 {
		t.Fatalf("unexpected duplicate result: %#v", second)
	}
	if len(manager.Events("vehicle-001")) != 1 {
		t.Fatal("duplicate event was added to the vehicle buffer")
	}
}

func TestBufferManagerRejectsConflictingEventID(t *testing.T) {
	manager := NewBufferManager()
	original := testEvent("event-001", "vehicle-001", "2026-07-23T04:00:00Z")
	conflict := original
	conflict.Longitude = 106

	if _, err := manager.Add(original); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Add(conflict); !errors.Is(err, ErrEventIDConflict) {
		t.Fatalf("expected ErrEventIDConflict, got %v", err)
	}
}

func TestBufferManagerSupportsConcurrentAdds(t *testing.T) {
	manager := NewBufferManager()
	startedAt := time.Date(2026, 7, 23, 4, 0, 0, 0, time.UTC)
	const eventCount = 100

	var waitGroup sync.WaitGroup
	errs := make(chan error, eventCount)
	for index := eventCount - 1; index >= 0; index-- {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			event := testEvent(
				fmt.Sprintf("event-%03d", index),
				"vehicle-001",
				startedAt.Add(time.Duration(index)*time.Second).Format(time.RFC3339Nano),
			)
			_, err := manager.Add(event)
			errs <- err
		}(index)
	}

	waitGroup.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	events := manager.Events("vehicle-001")
	if len(events) != eventCount {
		t.Fatalf("expected %d events, got %d", eventCount, len(events))
	}
	for index, event := range events {
		expected := fmt.Sprintf("event-%03d", index)
		if event.EventID != expected {
			t.Fatalf("expected event %q at index %d, got %q", expected, index, event.EventID)
		}
	}
}

func TestBufferManagerStoresLatestReceivedAt(t *testing.T) {
	manager := NewBufferManager()
	latest := time.Date(2026, 7, 23, 4, 0, 10, 0, time.UTC)
	earlier := latest.Add(-5 * time.Second)

	if _, err := manager.AddAt(
		testEvent("event-001", "vehicle-001", "2026-07-23T04:00:00Z"),
		latest,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AddAt(
		testEvent("event-002", "vehicle-001", "2026-07-23T04:00:05Z"),
		earlier,
	); err != nil {
		t.Fatal(err)
	}

	if receivedAt := manager.vehicles["vehicle-001"].lastReceivedAt; !receivedAt.Equal(latest) {
		t.Fatalf("expected latest received time %v, got %v", latest, receivedAt)
	}
}

func TestBufferManagerDuplicateDoesNotRefreshReceivedAt(t *testing.T) {
	manager := NewBufferManager()
	event := testEvent("event-001", "vehicle-001", "2026-07-23T04:00:00Z")
	receivedAt := time.Date(2026, 7, 23, 4, 0, 1, 0, time.UTC)

	if _, err := manager.AddAt(event, receivedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.AddAt(event, receivedAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	actual := manager.vehicles["vehicle-001"].lastReceivedAt
	if !actual.Equal(receivedAt) {
		t.Fatalf("duplicate refreshed received time: expected %v, got %v", receivedAt, actual)
	}
}

func testEvent(eventID string, vehicleID string, recordedAt string) CanonicalEvent {
	return CanonicalEvent{
		EventID:    eventID,
		VehicleID:  vehicleID,
		RecordedAt: recordedAt,
		Longitude:  105.8542,
		Latitude:   21.0285,
		SpeedKMH:   32.4,
	}
}
