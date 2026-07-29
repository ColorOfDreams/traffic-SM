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
		key     any
		wantErr bool
	}{
		{name: "forward", id: "v1_e7_f", forward: true, key: float64(14)},
		{name: "reverse", id: "v1_e7_r", forward: false, key: float64(15)},
		{name: "mismatched suffix", id: "v1_e7_r", forward: true, key: float64(14), wantErr: true},
		{name: "invalid direction type", id: "v1_e7_f", forward: "true", key: float64(14), wantErr: true},
		{name: "mismatched traversal key", id: "v1_e7_f", forward: true, key: float64(15), wantErr: true},
		{name: "missing traversal key", id: "v1_e7_f", forward: true, wantErr: true},
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
					"traversal_key": test.key,
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
