package mapview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/tile38"
)

const searchPageSize = 100

type Bounds struct {
	MinLongitude float64 `json:"min_longitude"`
	MinLatitude  float64 `json:"min_latitude"`
	MaxLongitude float64 `json:"max_longitude"`
	MaxLatitude  float64 `json:"max_latitude"`
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

func (reader *Tile38EdgeReader) ReadAll() (FeatureCollection, error) {
	client, err := tile38.Dial(reader.address)
	if err != nil {
		return FeatureCollection{}, err
	}
	defer client.Close()
	if _, err := client.Do("OUTPUT", "json"); err != nil {
		return FeatureCollection{}, fmt.Errorf("configure Tile38 JSON output: %w", err)
	}

	result := FeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]Feature, 0),
	}
	seenFeatureIDs := make(map[string]struct{})

	for _, collection := range reader.collections {
		if err := reader.readAllCollection(client, collection, seenFeatureIDs, &result); err != nil {
			return FeatureCollection{}, err
		}
	}

	return result, nil
}

func (reader *Tile38EdgeReader) ReadDistance(
	bounds Bounds,
	distanceKM float64,
) (FeatureCollection, error) {
	client, err := tile38.Dial(reader.address)
	if err != nil {
		return FeatureCollection{}, err
	}
	defer client.Close()
	if _, err := client.Do("OUTPUT", "json"); err != nil {
		return FeatureCollection{}, fmt.Errorf("configure Tile38 JSON output: %w", err)
	}

	result := FeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]Feature, 0),
	}
	seenFeatureIDs := make(map[string]struct{})
	seenEdgeIDs := make(map[string]struct{})
	displayedDistanceM := 0.0
	targetDistanceM := distanceKM * 1000

	for _, collection := range reader.collections {
		reached, err := reader.readDistanceCollection(
			client,
			collection,
			bounds,
			targetDistanceM,
			seenFeatureIDs,
			seenEdgeIDs,
			&displayedDistanceM,
			&result,
		)
		if err != nil {
			return FeatureCollection{}, err
		}
		if reached {
			result.Truncated = true
			return result, nil
		}
	}

	return result, nil
}

func (reader *Tile38EdgeReader) readDistanceCollection(
	client *tile38.Client,
	collection string,
	bounds Bounds,
	targetDistanceM float64,
	seenFeatureIDs map[string]struct{},
	seenEdgeIDs map[string]struct{},
	displayedDistanceM *float64,
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
			if _, exists := seenFeatureIDs[item.ID]; exists {
				continue
			}
			seenFeatureIDs[item.ID] = struct{}{}
			properties := mapFields(response.Fields, item.Fields)
			result.Features = append(result.Features, Feature{
				Type:       "Feature",
				ID:         item.ID,
				Geometry:   item.Object,
				Properties: properties,
			})

			edgeID, edgeOK := numericPropertyText(properties["edge_id"])
			distanceM, distanceOK := numericPropertyFloat(properties["distance_m"])
			if !edgeOK || !distanceOK || distanceM <= 0 {
				continue
			}
			if _, exists := seenEdgeIDs[edgeID]; !exists {
				seenEdgeIDs[edgeID] = struct{}{}
				*displayedDistanceM += distanceM
			}
		}

		if *displayedDistanceM >= targetDistanceM {
			return true, nil
		}
		if response.Cursor == 0 {
			return false, nil
		}
		cursor = response.Cursor
	}
}

func (reader *Tile38EdgeReader) readAllCollection(
	client *tile38.Client,
	collection string,
	seenFeatureIDs map[string]struct{},
	result *FeatureCollection,
) error {
	cursor := 0
	for {
		responseText, err := client.Do(
			"SCAN",
			collection,
			"CURSOR",
			strconv.Itoa(cursor),
			"LIMIT",
			strconv.Itoa(searchPageSize),
			"OBJECTS",
		)
		if err != nil {
			return fmt.Errorf("scan collection %s: %w", collection, err)
		}

		response, err := decodeSearchResponse(responseText)
		if err != nil {
			return fmt.Errorf("decode collection %s response: %w", collection, err)
		}
		if !response.OK {
			return fmt.Errorf("scan collection %s: %s", collection, response.Error)
		}

		for _, item := range response.Objects {
			if _, exists := seenFeatureIDs[item.ID]; exists {
				continue
			}
			seenFeatureIDs[item.ID] = struct{}{}
			result.Features = append(result.Features, Feature{
				Type:       "Feature",
				ID:         item.ID,
				Geometry:   item.Object,
				Properties: mapFields(response.Fields, item.Fields),
			})
		}

		if response.Cursor == 0 {
			return nil
		}
		cursor = response.Cursor
	}
}

func (reader *Tile38EdgeReader) ReadBounds(bounds Bounds, limit int) (FeatureCollection, error) {
	client, err := tile38.Dial(reader.address)
	if err != nil {
		return FeatureCollection{}, err
	}
	defer client.Close()
	if _, err := client.Do("OUTPUT", "json"); err != nil {
		return FeatureCollection{}, fmt.Errorf("configure Tile38 JSON output: %w", err)
	}

	result := FeatureCollection{
		Type:     "FeatureCollection",
		Features: make([]Feature, 0),
	}
	seenFeatureIDs := make(map[string]struct{})

	for _, collection := range reader.collections {
		truncated, err := reader.readCollection(client, collection, bounds, limit, seenFeatureIDs, &result)
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
	seenFeatureIDs map[string]struct{},
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
			if _, exists := seenFeatureIDs[item.ID]; exists {
				continue
			}
			if len(result.Features) >= limit {
				return true, nil
			}

			seenFeatureIDs[item.ID] = struct{}{}
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

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func numericPropertyText(value any) (string, bool) {
	switch typed := value.(type) {
	case json.Number:
		return typed.String(), typed.String() != ""
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case int:
		return strconv.Itoa(typed), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	default:
		return "", false
	}
}

func numericPropertyFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}
