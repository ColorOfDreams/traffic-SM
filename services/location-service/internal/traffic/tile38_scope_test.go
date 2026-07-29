package traffic

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

type fakeCommandClient struct {
	responses map[string]string
	errors    map[string]error
	calls     []string
}

func (client *fakeCommandClient) Do(arguments ...string) (string, error) {
	key := fmt.Sprintf("%s/%s", arguments[1], arguments[2])
	client.calls = append(client.calls, key)
	if err := client.errors[key]; err != nil {
		return "", err
	}
	return client.responses[key], nil
}

func (client *fakeCommandClient) Close() error {
	return nil
}

func TestTile38ScopeFiltersAndCachesSegmentLookups(t *testing.T) {
	client := &fakeCommandClient{
		responses: map[string]string{
			"hanoi/graph_e1_f": "0",
			"hcm/graph_e1_f":   "1",
			"hanoi/graph_e2_r": "0",
			"hcm/graph_e2_r":   "0",
		},
		errors: map[string]error{},
	}
	scope := &Tile38Scope{
		collections: []string{"hanoi", "hcm"},
		dial: func() (commandClient, error) {
			return client, nil
		},
	}
	fragments := []matching.TraversalFragment{
		{TrafficSegmentID: "graph_e1_f", TraversalKey: 2},
		{TrafficSegmentID: "graph_e1_f", TraversalKey: 2},
		{TrafficSegmentID: "graph_e2_r", TraversalKey: 5},
	}

	filtered, dropped, err := scope.Filter(context.Background(), fragments)
	if err != nil {
		t.Fatalf("filter fragments: %v", err)
	}
	if len(filtered) != 2 || dropped != 1 {
		t.Fatalf("expected 2 retained and 1 dropped, got retained=%d dropped=%d", len(filtered), dropped)
	}
	wantCalls := []string{
		"hanoi/graph_e1_f",
		"hcm/graph_e1_f",
		"hanoi/graph_e2_r",
		"hcm/graph_e2_r",
	}
	if !reflect.DeepEqual(client.calls, wantCalls) {
		t.Fatalf("unexpected Tile38 calls: %v", client.calls)
	}
}

func TestTile38ScopeReturnsLookupErrors(t *testing.T) {
	client := &fakeCommandClient{
		responses: map[string]string{},
		errors: map[string]error{
			"hanoi/graph_e1_f": errors.New("Tile38 unavailable"),
		},
	}
	scope := &Tile38Scope{
		collections: []string{"hanoi"},
		dial: func() (commandClient, error) {
			return client, nil
		},
	}

	_, _, err := scope.Filter(context.Background(), []matching.TraversalFragment{
		{TrafficSegmentID: "graph_e1_f"},
	})
	if err == nil || !errors.Is(err, client.errors["hanoi/graph_e1_f"]) {
		t.Fatalf("expected Tile38 lookup error, got %v", err)
	}
}
