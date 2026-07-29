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
