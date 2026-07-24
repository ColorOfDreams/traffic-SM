package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/tile38"
)

const maxFeatureSize = 16 * 1024 * 1024

var graphVersionPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

var includedRoadClasses = map[string]struct{}{
	"motorway":       {},
	"motorway_link":  {},
	"trunk":          {},
	"trunk_link":     {},
	"primary":        {},
	"primary_link":   {},
	"secondary":      {},
	"secondary_link": {},
	"tertiary":       {},
	"tertiary_link":  {},
}

var allowedProperties = map[string]struct{}{
	"graph_version": {},
	"edge_id":       {},
	"forward":       {},
	"osm_way_id":    {},
	"distance_m":    {},
	"road_class":    {},
	"name":          {},
}

// Khai báo kiểu dữ liệu
// INput phải là 1 dòng geojsonSeq, ví dụ:
type feature struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`         // Khóa trong tile38
	Geometry   geometry       `json:"geometry"`   // Hình học edge
	Properties map[string]any `json:"properties"` // metadata của graohhopper
}

type geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"` // giải mã sau khi đọc ra []float64
}

type importStats struct {
	Read      int
	Valid     int
	Skipped   int
	Processed int
}

// Nhận tham số từ command line, đọc file geojsonSeq, validate từng feature, và import vào tile38 nếu không phải dry-run
func main() {
	inputPath := flag.String("input", "", "path to a GraphHopper edge GeoJSONSeq file")
	graphVersion := flag.String("graph-version", "", "expected GraphHopper graph version")
	tile38Address := flag.String("tile38-address", "tile38:9851", "Tile38 TCP address")
	collection := flag.String("collection", "", "Tile38 collection name")
	dryRun := flag.Bool("dry-run", true, "validate input without writing to Tile38")
	limit := flag.Int("limit", 0, "maximum number of valid features to process; zero means unlimited")
	flag.Parse()

	if *inputPath == "" {
		exitUsage("missing required -input argument")
	}
	if *graphVersion == "" || !graphVersionPattern.MatchString(*graphVersion) {
		exitUsage("missing or invalid -graph-version argument")
	}
	if !*dryRun && *collection == "" {
		exitUsage("missing required -collection argument for import")
	}
	if *limit < 0 {
		exitUsage("limit must be zero or greater")
	}

	input, err := os.Open(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open input: %v\n", err)
		os.Exit(1)
	}
	defer input.Close()

	var client *tile38.Client
	if !*dryRun {
		client, err = tile38.Dial(*tile38Address)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer client.Close()
	}

	stats, err := processFeatures(input, *graphVersion, *limit, func(item feature) error {
		if *dryRun {
			return nil
		}
		return importFeature(client, *collection, item)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "process input: %v\n", err)
		os.Exit(1)
	}

	imported := 0
	if !*dryRun {
		imported = stats.Processed
	}

	fmt.Printf(
		"graph_version=%s read=%d valid=%d skipped=%d processed=%d imported=%d dry_run=%t\n",
		*graphVersion,
		stats.Read,
		stats.Valid,
		stats.Skipped,
		stats.Processed,
		imported,
		*dryRun,
	)
}

func exitUsage(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(2)
}

// đọc File
func processFeatures(input io.Reader, graphVersion string, limit int, process func(feature) error) (importStats, error) {
	var stats importStats
	// Dùng scanner để đọc từng dòng của file geojsonSeq, mỗi dòng là 1 feature
	// iput là 1 file geojsonSeq, mỗi dòng là 1 feature, ví dụ:
	// output là 1 struct importStats, ví dụ:
	// importStats{
	// 	Read:      1000,
	// 	Valid:     900,
	// 	Skipped:   100,
	// 	Processed: 800,
	// }
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxFeatureSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		stats.Read++
		var item feature
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return stats, fmt.Errorf("feature %d: decode JSON: %w", stats.Read, err)
		}

		if err := validateFeature(item, graphVersion); err != nil {
			return stats, fmt.Errorf("feature %d (%s): %w", stats.Read, item.ID, err)
		}

		stats.Valid++
		if err := process(item); err != nil {
			return stats, fmt.Errorf("feature %s: %w", item.ID, err)
		}
		stats.Processed++

		if limit > 0 && stats.Processed >= limit {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return stats, fmt.Errorf("scan GeoJSONSeq: %w", err)
	}

	return stats, nil
}

// validate kiểm tra tung feature có hợp lệ hay không, dựa trên các thuộc tính và định dạng của nó. Nếu feature không hợp lệ, trả về lỗi.
func validateFeature(item feature, expectedGraphVersion string) error {
	if item.Type != "Feature" {
		return errors.New("object is not a GeoJSON Feature")
	}
	for name := range item.Properties {
		if _, ok := allowedProperties[name]; !ok {
			return fmt.Errorf("feature has unsupported property %s", name)
		}
	}

	graphVersion, err := stringProperty(item.Properties, "graph_version")
	if err != nil || graphVersion != expectedGraphVersion {
		return errors.New("graph_version does not match expected version")
	}

	edgeID, err := nonNegativeIntegerProperty(item.Properties, "edge_id")
	if err != nil {
		return err
	}
	forward, err := boolProperty(item.Properties, "forward")
	if err != nil {
		return err
	}
	expectedDirectionSuffix := "r"
	if forward {
		expectedDirectionSuffix = "f"
	}
	if item.ID != fmt.Sprintf("%s_e%d_%s", graphVersion, edgeID, expectedDirectionSuffix) {
		return errors.New("feature ID does not match graph version, edge ID, or direction")
	}

	if _, err := positiveIntegerProperty(item.Properties, "osm_way_id"); err != nil {
		return err
	}
	distanceMeters, err := numberProperty(item.Properties, "distance_m")
	if err != nil || distanceMeters <= 0 {
		return errors.New("feature has invalid distance_m")
	}

	roadClass, err := stringProperty(item.Properties, "road_class")
	if err != nil {
		return err
	}
	if _, ok := includedRoadClasses[roadClass]; !ok {
		return errors.New("feature has unsupported road_class")
	}
	if _, ok := item.Properties["name"]; ok {
		if _, err := stringProperty(item.Properties, "name"); err != nil {
			return err
		}
	}

	// validate quan trong
	if item.Geometry.Type != "LineString" {
		return errors.New("geometry is not a LineString")
	}
	var coordinates [][]float64
	if err := json.Unmarshal(item.Geometry.Coordinates, &coordinates); err != nil || len(coordinates) < 2 {
		return errors.New("geometry must contain at least two coordinates")
	}
	for index, coordinate := range coordinates {
		if len(coordinate) < 2 || !validLongitude(coordinate[0]) || !validLatitude(coordinate[1]) {
			return fmt.Errorf("geometry has invalid coordinate at index %d", index)
		}
	}

	return nil
}

func importFeature(client *tile38.Client, collection string, item feature) error {
	encodedGeometry, err := marshalGeometry(item.Geometry)
	if err != nil {
		return err
	}

	fieldMappings := [][2]string{
		{"graph_version", "graph_version"},
		{"edge_id", "edge_id"},
		{"forward", "forward"},
		{"osm_way_id", "osm_way_id"},
		{"distance_m", "distance_m"},
		{"road_class", "road_class"},
		{"name", "name"},
	}

	arguments := []string{"SET", collection, item.ID}
	for _, mapping := range fieldMappings {
		value, ok := item.Properties[mapping[0]]
		if !ok || value == nil {
			continue
		}
		arguments = append(arguments, "FIELD", mapping[1], propertyText(value))
	}
	arguments = append(arguments, "FIELD", "traffic_status", "unknown", "OBJECT", encodedGeometry)

	_, err = client.Do(arguments...)
	return err
}

func stringProperty(properties map[string]any, name string) (string, error) {
	value, ok := properties[name]
	if !ok {
		return "", fmt.Errorf("feature has no %s", name)
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return "", fmt.Errorf("feature has invalid %s", name)
	}
	return text, nil
}

func boolProperty(properties map[string]any, name string) (bool, error) {
	value, ok := properties[name]
	if !ok {
		return false, fmt.Errorf("feature has no %s", name)
	}
	boolean, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("feature has invalid %s", name)
	}
	return boolean, nil
}

func nonNegativeIntegerProperty(properties map[string]any, name string) (int64, error) {
	value, ok := properties[name]
	if !ok {
		return 0, fmt.Errorf("feature has no %s", name)
	}
	number, ok := value.(float64)
	if !ok || number < 0 || number != math.Trunc(number) || number > math.MaxInt64 {
		return 0, fmt.Errorf("feature has invalid %s", name)
	}
	return int64(number), nil
}

func positiveIntegerProperty(properties map[string]any, name string) (int64, error) {
	number, err := nonNegativeIntegerProperty(properties, name)
	if err != nil || number == 0 {
		return 0, fmt.Errorf("feature has invalid %s", name)
	}
	return number, nil
}

func numberProperty(properties map[string]any, name string) (float64, error) {
	value, ok := properties[name]
	if !ok {
		return 0, fmt.Errorf("feature has no %s", name)
	}
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("feature has invalid %s", name)
	}
	return number, nil
}

func validLongitude(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= -180 && value <= 180
}

func validLatitude(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= -90 && value <= 90
}

func marshalGeometry(value geometry) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode geometry: %w", err)
	}
	return string(encoded), nil
}

func propertyText(value any) string {
	switch typed := value.(type) {
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(value)
	}
}
