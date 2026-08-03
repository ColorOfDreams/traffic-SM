package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/pipeline"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/traffic"
)

type fakeLocationProcessor struct {
	result pipeline.Result
	err    error
	event  gps.CanonicalEvent
	now    time.Time
	calls  int
}

func (processor *fakeLocationProcessor) Process(
	_ context.Context,
	event gps.CanonicalEvent,
	now time.Time,
) (pipeline.Result, error) {
	processor.calls++
	processor.event = event
	processor.now = now
	return processor.result, processor.err
}

func TestHealth(t *testing.T) {
	handler := newTestHandler(t, &fakeLocationProcessor{})
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if response.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("body = %q", response.Body.String())
	}
}

func TestProcessLocationRejectsInvalidJSON(t *testing.T) {
	processor := &fakeLocationProcessor{}
	handler := newTestHandler(t, processor)
	request := httptest.NewRequest(
		http.MethodPost,
		"/locations",
		strings.NewReader("{"),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if processor.calls != 0 {
		t.Fatalf("processor calls = %d, want 0", processor.calls)
	}
}

func TestProcessLocationRejectsInvalidGPS(t *testing.T) {
	processor := &fakeLocationProcessor{}
	handler := newTestHandler(t, processor)
	request := httptest.NewRequest(
		http.MethodPost,
		"/locations",
		strings.NewReader("{}"),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.Code)
	}
	if processor.calls != 0 {
		t.Fatalf("processor calls = %d, want 0", processor.calls)
	}
}

func TestProcessLocationReturnsPipelineResult(t *testing.T) {
	windowStart := fixedNow.Add(-10 * time.Second)
	processor := &fakeLocationProcessor{result: pipeline.Result{
		TraceEmitted:            true,
		MatchedObservationCount: 2,
		CongestionStates: []traffic.CongestionState{{
			GraphVersion:    "vietnam-20260730-car-v1",
			TraversalKey:    12,
			VehicleType:     "car",
			WindowStart:     windowStart,
			CurrentSpeedMPS: 2,
			Level:           traffic.LevelCongested,
			UpdatedAt:       fixedNow,
		}},
	}}
	handler := newTestHandler(t, processor)
	request := httptest.NewRequest(
		http.MethodPost,
		"/locations",
		strings.NewReader(validPayloadJSON()),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if processor.calls != 1 {
		t.Fatalf("processor calls = %d, want 1", processor.calls)
	}
	if processor.event.DriverID != "driver-1" ||
		processor.event.RecordedAt.IsZero() {
		t.Fatalf("unexpected canonical event: %#v", processor.event)
	}
	if !processor.now.Equal(fixedNow) {
		t.Fatalf("processing time = %s, want %s", processor.now, fixedNow)
	}

	var body locationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Accepted || !body.TraceEmitted ||
		body.MatchedObservationCount != 2 || len(body.CongestionStates) != 1 {
		t.Fatalf("unexpected response: %#v", body)
	}
	if body.CongestionStates[0].Level != traffic.LevelCongested {
		t.Fatalf("unexpected state: %#v", body.CongestionStates[0])
	}
}

func TestProcessLocationReturnsPipelineError(t *testing.T) {
	processor := &fakeLocationProcessor{err: errors.New("GraphHopper unavailable")}
	handler := newTestHandler(t, processor)
	request := httptest.NewRequest(
		http.MethodPost,
		"/locations",
		strings.NewReader(validPayloadJSON()),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
}

var fixedNow = time.Date(2026, time.August, 3, 8, 30, 0, 0, time.UTC)

func newTestHandler(
	t *testing.T,
	processor LocationProcessor,
) http.Handler {
	t.Helper()
	handler, err := newHandler(processor, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("newHandler() error = %v", err)
	}
	return handler
}

func validPayloadJSON() string {
	return `{
		"driver_id":"driver-1",
		"t_timestamp":"30:01.000",
		"lat":21.0285,
		"lng":105.8542,
		"bearing":0,
		"bearing_acc":1,
		"horizontal_acc":5,
		"speed":2,
		"speed_acc":1,
		"time_delta":1,
		"vertical_acc":1,
		"status":"IN TRIP",
		"vehicle_type":"car",
		"time":"8/3/2026 08:30",
		"timestamp":1785720601,
		"altitude":10
	}`
}
