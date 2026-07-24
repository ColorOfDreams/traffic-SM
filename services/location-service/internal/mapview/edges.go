package mapview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/tile38"
)

const searchPageSize = 500

type Bounds struct {
	MinLongitude float64
	MinLatitude  float64
	MaxLongitude float64
	MaxLatitude  float64
}

type FeatureCollection struct {
	Type      string    `json:"type"`
	Features  []Feature `json:"features"`
	Truncated bool      `json:"truncated,omitempty"`
}

type Feature struct {
	Type       string          `json:"type"`
	ID         string          `json:"id"`
	Geometry   json.RawMessage `json:"geometry"`
	Properties map[string]any  `json:"properties"`
}

type Tile38EdgeReader struct {
	address     string
	collections []string
}

type searchResponse struct {
	OK      bool           `json:"ok"`
	Fields  []string       `json:"fields"`
	Objects []searchObject `json:"objects"`
	Cursor  int            `json:"cursor"`
	Error   string         `json:"err"`
}

type searchObject struct {
	ID     string          `json:"id"`
	Object json.RawMessage `json:"object"`
	Fields []any           `json:"fields"`
}

func NewTile38EdgeReader(address string, collections []string) *Tile38EdgeReader {
	return &Tile38EdgeReader{
		address:     address,
		collections: append([]string(nil), collections...),
	}
}

func (reader *Tile38EdgeReader) ReadBounds(bounds Bounds, limit int) (FeatureCollection, error) {
	client, err := tile38.Dial(reader.address)
	if err != nil {
		return FeatureCollection{}, err
	}
	defer client.Close()

	result := FeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]Feature, 0),
	}
	seenEdgeIDs := make(map[string]struct{})

	for _, collection := range reader.collections {
		truncated, err := reader.readCollection(client, collection, bounds, limit, seenEdgeIDs, &result)
		if err != nil {
			return FeatureCollection{}, err
		}
		if truncated {
			result.Truncated = true
			break
		}
	}

	return result, nil
}

func (reader *Tile38EdgeReader) readCollection(
	client *tile38.Client,
	collection string,
	bounds Bounds,
	limit int,
	seenEdgeIDs map[string]struct{},
	result *FeatureCollection,
) (bool, error) {
	cursor := 0
	for {
		responseText, err := client.Do(
			"INTERSECTS",
			collection,
			"CURSOR",
			strconv.Itoa(cursor),
			"LIMIT",
			strconv.Itoa(searchPageSize),
			"OBJECTS",
			"BOUNDS",
			formatFloat(bounds.MinLatitude),
			formatFloat(bounds.MinLongitude),
			formatFloat(bounds.MaxLatitude),
			formatFloat(bounds.MaxLongitude),
		)
		if err != nil {
			return false, fmt.Errorf("query collection %s: %w", collection, err)
		}

		response, err := decodeSearchResponse(responseText)
		if err != nil {
			return false, fmt.Errorf("decode collection %s response: %w", collection, err)
		}
		if !response.OK {
			return false, fmt.Errorf("query collection %s: %s", collection, response.Error)
		}

		for _, item := range response.Objects {
			properties := mapFields(response.Fields, item.Fields)
			edgeID := propertyKey(properties["edge_id"])
			if edgeID == "" {
				edgeID = item.ID
			}
			if _, exists := seenEdgeIDs[edgeID]; exists {
				continue
			}
			if len(result.Features) >= limit {
				return true, nil
			}

			seenEdgeIDs[edgeID] = struct{}{}
			result.Features = append(result.Features, Feature{
				Type:       "Feature",
				ID:         item.ID,
				Geometry:   item.Object,
				Properties: properties,
			})
		}

		if response.Cursor == 0 {
			return false, nil
		}
		cursor = response.Cursor
	}
}

func decodeSearchResponse(responseText string) (searchResponse, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(responseText))
	decoder.UseNumber()
	var response searchResponse
	if err := decoder.Decode(&response); err != nil {
		return searchResponse{}, err
	}
	return response, nil
}

func mapFields(names []string, values []any) map[string]any {
	properties := make(map[string]any, len(names))
	for index, name := range names {
		if index < len(values) {
			properties[name] = values[index]
		}
	}
	return properties
}

func propertyKey(value any) string {
	switch typed := value.(type) {
	case json.Number:
		return typed.String()
	case string:
		return typed
	default:
		return ""
	}
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
