package gps

import (
	"strings"
	"testing"
)

func TestDecodeCanonicalEventValid(t *testing.T) {
	event, err := DecodeCanonicalEvent(strings.NewReader(`{
		"event_id":"gps-001",
		"vehicle_id":"vehicle-001",
		"recorded_at":"2026-07-23T11:00:05+07:00",
		"longitude":105.8542,
		"latitude":21.0285,
		"speed_kmh":32.4,
		"heading_deg":275
	}`))
	if err != nil {
		t.Fatal(err)
	}

	if event.RecordedAt != "2026-07-23T04:00:05Z" {
		t.Fatalf("unexpected canonical timestamp %q", event.RecordedAt)
	}
	if event.HeadingDeg == nil || *event.HeadingDeg != 275 {
		t.Fatalf("unexpected heading %#v", event.HeadingDeg)
	}
}

func TestDecodeCanonicalEventAllowsNullHeading(t *testing.T) {
	event, err := DecodeCanonicalEvent(strings.NewReader(`{
		"event_id":"gps-002",
		"vehicle_id":"vehicle-001",
		"recorded_at":"2026-07-23T04:00:10Z",
		"longitude":105.8542,
		"latitude":21.0285,
		"speed_kmh":0,
		"heading_deg":null
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.HeadingDeg != nil {
		t.Fatalf("expected nil heading, got %#v", event.HeadingDeg)
	}
}

func TestDecodeCanonicalEventRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name: "missing latitude",
			payload: `{"event_id":"e","vehicle_id":"v","recorded_at":"2026-07-23T04:00:10Z",
				"longitude":105.8,"speed_kmh":1,"heading_deg":0}`,
		},
		{
			name: "negative speed",
			payload: `{"event_id":"e","vehicle_id":"v","recorded_at":"2026-07-23T04:00:10Z",
				"longitude":105.8,"latitude":21.0,"speed_kmh":-1,"heading_deg":0}`,
		},
		{
			name: "invalid heading",
			payload: `{"event_id":"e","vehicle_id":"v","recorded_at":"2026-07-23T04:00:10Z",
				"longitude":105.8,"latitude":21.0,"speed_kmh":1,"heading_deg":360}`,
		},
		{
			name: "unknown field",
			payload: `{"event_id":"e","vehicle_id":"v","recorded_at":"2026-07-23T04:00:10Z",
				"longitude":105.8,"latitude":21.0,"speed_kmh":1,"heading_deg":0,"extra":true}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeCanonicalEvent(strings.NewReader(test.payload)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
