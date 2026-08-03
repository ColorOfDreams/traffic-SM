package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/mapview"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
)

type fakeGraphEdgeReader struct {
	features mapview.FeatureCollection
	err      error
}

func (reader fakeGraphEdgeReader) ReadDistance(
	mapview.Bounds,
	float64,
) (mapview.FeatureCollection, error) {
	return reader.features, reader.err
}

type fakeStrategy struct {
	calls int
}

func (strategy *fakeStrategy) Match(
	_ context.Context,
	_ trace.Trace,
) ([]matching.MatchedObservation, error) {
	strategy.calls++
	return nil, nil
}

func TestGPSEventAliasUsesLocationHandler(t *testing.T) {
	var receivedPath string
	locationHandler := http.HandlerFunc(func(
		responseWriter http.ResponseWriter,
		request *http.Request,
	) {
		receivedPath = request.URL.Path
		writeJSON(responseWriter, http.StatusOK, map[string]bool{"accepted": true})
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/gps-events",
		strings.NewReader(`{"driver_id":"1"}`),
	)
	response := httptest.NewRecorder()

	newHandler(fakeGraphEdgeReader{}, locationHandler).
		ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if receivedPath != "/locations" {
		t.Fatalf("aliased path = %q, want /locations", receivedPath)
	}
}

func TestVehicleMatcherSelectsProfile(t *testing.T) {
	car := &fakeStrategy{}
	motorcycle := &fakeStrategy{}
	router := vehicleMatcher{car: car, motorcycle: motorcycle}

	for _, vehicleType := range []string{"car", "bike"} {
		_, err := router.Match(context.Background(), trace.Trace{
			Points: []gps.CanonicalEvent{{VehicleType: vehicleType}},
		})
		if err != nil {
			t.Fatalf("Match(%s) error = %v", vehicleType, err)
		}
	}
	if car.calls != 1 || motorcycle.calls != 1 {
		t.Fatalf("car calls=%d motorcycle calls=%d, want 1 each", car.calls, motorcycle.calls)
	}
}

func TestGraphEdgesReturned(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/graph-edges?bbox=105.75,20.8,106.1,21.25&distance_km=1000",
		nil,
	)
	response := httptest.NewRecorder()
	reader := fakeGraphEdgeReader{
		features: mapview.FeatureCollection{
			Type: "FeatureCollection",
			Features: []mapview.Feature{
				{ID: "edge-1", Properties: map[string]any{"edge_id": 1, "distance_m": 12000.0}},
			},
		},
	}

	newHandler(reader, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"type":"FeatureCollection"`) {
		t.Fatalf("unexpected response %s", response.Body.String())
	}
}

func TestGraphEdgesRequireDistance(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/graph-edges?bbox=105.75,20.8,106.1,21.25",
		nil,
	)
	response := httptest.NewRecorder()

	newHandler(fakeGraphEdgeReader{}, nil).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestMapViewReturned(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/map", nil)
	response := httptest.NewRecorder()

	newHandler(fakeGraphEdgeReader{}, nil).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type %q", contentType)
	}
	if !strings.Contains(response.Body.String(), "function styleCasing") ||
		!strings.Contains(response.Body.String(), `id="distance-km"`) ||
		!strings.Contains(response.Body.String(), `id="congestion-button"`) ||
		!strings.Contains(response.Body.String(), "function calculateCongestion") ||
		!strings.Contains(response.Body.String(), `value="1000"`) ||
		!strings.Contains(response.Body.String(), "edgeRequestController") ||
		!strings.Contains(response.Body.String(), "function annotateDirections") ||
		!strings.Contains(response.Body.String(), "properties.road_direction") {
		t.Fatal("map page does not contain the new traffic overlay")
	}
}
