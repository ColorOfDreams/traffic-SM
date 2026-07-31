package mapview

import (
	"encoding/json"
	"testing"
)

func TestDecodeSearchResponsePreservesNumericFields(t *testing.T) {
	response, err := decodeSearchResponse(`{
		"ok":true,
		"fields":["edge_id","forward"],
		"objects":[{
			"id":"vietnam-20260722_e1_f",
			"object":{"type":"LineString","coordinates":[[105,21],[106,22]]},
			"fields":[1,true]
		}],
		"cursor":0
	}`)
	if err != nil {
		t.Fatal(err)
	}

	properties := mapFields(response.Fields, response.Objects[0].Fields)
	edgeID, ok := properties["edge_id"].(json.Number)
	if !ok || edgeID.String() != "1" {
		t.Fatalf("unexpected edge_id %#v", properties["edge_id"])
	}
	forward, ok := properties["forward"].(bool)
	if !ok || !forward {
		t.Fatalf("unexpected forward %#v", properties["forward"])
	}
}

func TestDirectedFeaturesUseTile38ObjectIdentity(t *testing.T) {
	response, err := decodeSearchResponse(`{
		"ok":true,
		"fields":["edge_id","forward","traversal_key"],
		"objects":[
			{
				"id":"graph_e1_f",
				"object":{"type":"LineString","coordinates":[[105,21],[106,22]]},
				"fields":[1,true,2]
			},
			{
				"id":"graph_e1_r",
				"object":{"type":"LineString","coordinates":[[106,22],[105,21]]},
				"fields":[1,false,3]
			}
		],
		"cursor":0
	}`)
	if err != nil {
		t.Fatal(err)
	}

	seenFeatureIDs := make(map[string]struct{})
	features := make([]Feature, 0, len(response.Objects))
	for _, item := range response.Objects {
		if _, exists := seenFeatureIDs[item.ID]; exists {
			continue
		}
		seenFeatureIDs[item.ID] = struct{}{}
		features = append(features, Feature{
			ID:         item.ID,
			Properties: mapFields(response.Fields, item.Fields),
		})
	}

	if len(features) != 2 {
		t.Fatalf("expected both directed features, got %#v", features)
	}
	if features[0].ID != "graph_e1_f" || features[1].ID != "graph_e1_r" {
		t.Fatalf("unexpected directed feature identities %#v", features)
	}
}
