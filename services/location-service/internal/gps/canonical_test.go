package gps

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

const validGPSJSON = `{
	"driver_id":"43",
	"t_timestamp":"55:04.6",
	"lat":20.988913,
	"lng":105.941475,
	"bearing":320.5792,
	"bearing_acc":-1,
	"horizontal_acc":2.6855512,
	"speed":-1,
	"speed_acc":5.612715,
	"time_delta":5,
	"vertical_acc":14.870662,
	"status":"OFFLINE",
	"vehicle_type":"car",
	"time":"7/23/2026 7:55",
	"timestamp":1.78e12,
	"altitude":81.4825,
	"side":null,
	"delta_time":5.03,
	"distance":0
}`

func TestDecodeCanonicalEvent(t *testing.T) {
	event, err := DecodeCanonicalEvent(strings.NewReader(validGPSJSON))
	if err != nil {
		t.Fatalf("decode valid GPS event: %v", err)
	}

	if event.DriverID != "43" {
		t.Fatalf("expected driver 43, got %q", event.DriverID)
	}
	if event.Lat != 20.988913 || event.Lng != 105.941475 {
		t.Fatalf("unexpected coordinates lat=%f lng=%f", event.Lat, event.Lng)
	}
}

func TestCanonicalEventJSONRoundTrip(t *testing.T) {
	original, err := DecodeCanonicalEvent(strings.NewReader(validGPSJSON))
	if err != nil {
		t.Fatalf("decode valid GPS event: %v", err)
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var decoded CanonicalEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("event changed after JSON round trip")
	}
}

func TestDecodeCanonicalEventRejectsMissingRequiredNumber(t *testing.T) {
	input := strings.Replace(validGPSJSON, `"lat":20.988913,`, "", 1)
	_, err := DecodeCanonicalEvent(strings.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "lat") {
		t.Fatalf("expected missing lat error, got %v", err)
	}
}

func TestDecodeCanonicalEventRejectsMismatchedTraceMinute(t *testing.T) {
	input := strings.Replace(validGPSJSON, `"t_timestamp":"55:04.6"`, `"t_timestamp":"54:04.6"`, 1)
	_, err := DecodeCanonicalEvent(strings.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatched minute error, got %v", err)
	}
}

func TestCanonicalEventRecordedAtCombinesTimeAndTTimestamp(t *testing.T) {
	event, err := DecodeCanonicalEvent(strings.NewReader(validGPSJSON))
	if err != nil {
		t.Fatalf("decode valid GPS event: %v", err)
	}

	recordedAt, err := event.RecordedAt()
	if err != nil {
		t.Fatalf("derive recorded time: %v", err)
	}
	expected := time.Date(2026, time.July, 23, 0, 55, 4, 600_000_000, time.UTC)
	if !recordedAt.Equal(expected) {
		t.Fatalf("expected %s, got %s", expected, recordedAt)
	}
}

func TestDecodeCanonicalEventRejectsNegativeIntervalsAndDistance(t *testing.T) {
	tests := []struct {
		name       string
		oldValue   string
		newValue   string
		errorField string
	}{
		{
			name:       "time delta",
			oldValue:   `"time_delta":5`,
			newValue:   `"time_delta":-1`,
			errorField: "time_delta",
		},
		{
			name:       "delta time",
			oldValue:   `"delta_time":5.03`,
			newValue:   `"delta_time":-1`,
			errorField: "delta_time",
		},
		{
			name:       "distance",
			oldValue:   `"distance":0`,
			newValue:   `"distance":-1`,
			errorField: "distance",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := strings.Replace(validGPSJSON, test.oldValue, test.newValue, 1)
			_, err := DecodeCanonicalEvent(strings.NewReader(input))
			if err == nil || !strings.Contains(err.Error(), test.errorField) {
				t.Fatalf("expected %s validation error, got %v", test.errorField, err)
			}
		})
	}
}
