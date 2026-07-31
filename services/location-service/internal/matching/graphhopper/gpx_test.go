package graphhopper

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
)

func TestEncodeGPX(t *testing.T) {
	firstTime := time.Date(
		2026,
		time.July,
		31,
		11,
		0,
		0,
		123000000,
		time.FixedZone("UTC+7", 7*60*60),
	)
	secondTime := firstTime.Add(time.Second)
	input := trace.Trace{
		DriverID: "driver-1",
		Points: []gps.CanonicalEvent{
			{
				DriverID:   "driver-1",
				Lat:        21.02810,
				Lng:        105.83420,
				RecordedAt: firstTime,
			},
			{
				DriverID:   "driver-1",
				Lat:        21.02820,
				Lng:        105.83430,
				RecordedAt: secondTime,
			},
		},
	}

	encoded, err := encodeGPX(input)
	if err != nil {
		t.Fatalf("encodeGPX() error = %v, want nil", err)
	}

	if !strings.HasPrefix(string(encoded), xml.Header) {
		t.Fatalf("encodeGPX() output does not start with XML header")
	}

	var document gpxDocument
	if err := xml.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode encoded GPX: %v", err)
	}

	if document.XMLName.Local != "gpx" {
		t.Errorf("root element = %q, want gpx", document.XMLName.Local)
	}
	if document.Version != "1.1" {
		t.Errorf("Version = %q, want 1.1", document.Version)
	}
	if document.Creator != "traffic-system" {
		t.Errorf("Creator = %q, want traffic-system", document.Creator)
	}
	if document.Track.Name != input.DriverID {
		t.Errorf(
			"track name = %q, want %q",
			document.Track.Name,
			input.DriverID,
		)
	}

	points := document.Track.Segment.Points
	if len(points) != 2 {
		t.Fatalf("encoded point count = %d, want 2", len(points))
	}
	if points[0].Lat != input.Points[0].Lat ||
		points[0].Lon != input.Points[0].Lng {
		t.Errorf(
			"first encoded coordinate = (%f, %f), want (%f, %f)",
			points[0].Lat,
			points[0].Lon,
			input.Points[0].Lat,
			input.Points[0].Lng,
		)
	}
	if points[0].Time != firstTime.UTC().Format(time.RFC3339Nano) {
		t.Errorf(
			"first encoded time = %q, want %q",
			points[0].Time,
			firstTime.UTC().Format(time.RFC3339Nano),
		)
	}
}

func TestEncodeGPXRejectsTooFewPoints(t *testing.T) {
	input := trace.Trace{
		Points: []gps.CanonicalEvent{
			{RecordedAt: time.Now()},
		},
	}

	encoded, err := encodeGPX(input)

	if err == nil {
		t.Fatal("encodeGPX() error = nil, want point count error")
	}
	if err.Error() != "trace requires at least two points" {
		t.Fatalf("encodeGPX() error = %q, want point count error", err)
	}
	if encoded != nil {
		t.Fatalf("encodeGPX() output = %q, want nil", encoded)
	}
}

func TestEncodeGPXRejectsMissingRecordedAt(t *testing.T) {
	input := trace.Trace{
		Points: []gps.CanonicalEvent{
			{RecordedAt: time.Now()},
			{},
		},
	}

	encoded, err := encodeGPX(input)

	if err == nil {
		t.Fatal("encodeGPX() error = nil, want recorded_at error")
	}
	if err.Error() != "trace point 1 recorded_at is required" {
		t.Fatalf("encodeGPX() error = %q, want recorded_at error", err)
	}
	if encoded != nil {
		t.Fatalf("encodeGPX() output = %q, want nil", encoded)
	}
}
