package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/mapview"
)

type fakeGraphEdgeReader struct {
	features mapview.FeatureCollection
	err      error
}

func (reader fakeGraphEdgeReader) ReadBounds(mapview.Bounds, int) (mapview.FeatureCollection, error) {
	return reader.features, reader.err
}

func TestGPSEventAccepted(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/gps-events",
		strings.NewReader(`{
			"event_id":"gps-001",
			"vehicle_id":"vehicle-001",
			"recorded_at":"2026-07-23T11:00:05+07:00",
			"longitude":105.8542,
			"latitude":21.0285,
			"speed_kmh":32.4,
			"heading_deg":275
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newHandler(fakeGraphEdgeReader{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status":"valid"`) {
		t.Fatalf("unexpected response %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"coverage"`) {
		t.Fatalf("GPS response must not contain coverage: %s", response.Body.String())
	}
}

func TestGraphEdgesReturned(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/graph-edges?bbox=105.84,21.02,105.87,21.04",
		nil,
	)
	response := httptest.NewRecorder()
	reader := fakeGraphEdgeReader{
		features: mapview.FeatureCollection{
			Type:     "FeatureCollection",
			Features: []mapview.Feature{},
		},
	}

	newHandler(reader).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"type":"FeatureCollection"`) {
		t.Fatalf("unexpected response %s", response.Body.String())
	}
}

func TestGraphEdgesRejectInvalidBounds(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/graph-edges?bbox=105.87,21.04,105.84,21.02",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(fakeGraphEdgeReader{}).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", response.Code, response.Body.String())
	}
}
