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
