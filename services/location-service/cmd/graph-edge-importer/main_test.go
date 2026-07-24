package main

import (
	"encoding/json"
	"testing"
)

func TestValidateFeatureDirection(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		forward any
		wantErr bool
	}{
		{name: "forward", id: "v1_e7_f", forward: true},
		{name: "reverse", id: "v1_e7_r", forward: false},
		{name: "mismatched suffix", id: "v1_e7_r", forward: true, wantErr: true},
		{name: "invalid type", id: "v1_e7_f", forward: "true", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := feature{
				Type: "Feature",
				ID:   test.id,
				Geometry: geometry{
					Type:        "LineString",
					Coordinates: json.RawMessage(`[[105,21],[106,22]]`),
				},
				Properties: map[string]any{
					"graph_version": "v1",
					"edge_id":       float64(7),
					"forward":       test.forward,
					"osm_way_id":    float64(10),
					"distance_m":    float64(100),
					"road_class":    "primary",
				},
			}

			err := validateFeature(item, "v1")
			if (err != nil) != test.wantErr {
				t.Fatalf("validateFeature() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}
