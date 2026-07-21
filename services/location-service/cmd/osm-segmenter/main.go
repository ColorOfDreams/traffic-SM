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
	"sort"
	"strconv"
	"strings"
)

const maxFeatureSize = 16 * 1024 * 1024

type feature struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Geometry   geometry       `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type geometry struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"`
}

type segmentRange struct {
	StartIndex   int
	EndIndex     int
	LengthMeters float64
}

type analysisStats struct {
	Ways                 int
	UniqueNodes          int
	JunctionNodes        int
	JunctionSegments     int
	EstimatedSegments    int
	LengthSplits         int
	LongSegments         int
	OverOneKilometer     int
	OverTwoKilometers    int
	OverThreeKilometers  int
	OverFiveKilometers   int
	TotalLengthMeters    float64
	SegmentLengthsMeters []float64
}

func main() {
	inputPath := flag.String("input", "", "path to GeoJSONSeq containing OSM way_nodes")
	outputPath := flag.String("output", "", "optional path for segmented GeoJSONSeq output")
	overwrite := flag.Bool("overwrite", false, "overwrite the output file if it already exists")
	longSegmentThresholdMeters := flag.Float64("long-segment-threshold-meters", 3000, "split only junction-to-junction segments longer than this")
	targetSegmentLengthMeters := flag.Float64("target-segment-length-meters", 1000, "target length for parts of a long segment")
	flag.Parse()

	if *inputPath == "" {
		fmt.Fprintln(os.Stderr, "missing required -input argument")
		os.Exit(2)
	}
	if *longSegmentThresholdMeters <= 0 {
		fmt.Fprintln(os.Stderr, "-long-segment-threshold-meters must be greater than zero")
		os.Exit(2)
	}
	if *targetSegmentLengthMeters <= 0 {
		fmt.Fprintln(os.Stderr, "-target-segment-length-meters must be greater than zero")
		os.Exit(2)
	}
	if *longSegmentThresholdMeters <= *targetSegmentLengthMeters {
		fmt.Fprintln(os.Stderr, "-long-segment-threshold-meters must be greater than -target-segment-length-meters")
		os.Exit(2)
	}
	// cách cắt ví dụ có a-b-c, thì a canh b, b cạnh c và a, c lại cạnh b, nếu có d cạnh b được coi có 3 nhanh được xem là giao lộ
	nodeNeighbors := make(map[int64]map[int64]struct{})
	wayCount, err := walkFeatures(*inputPath, func(_ feature, nodes []int64) error {
		for index := 1; index < len(nodes); index++ {
			fromNode := nodes[index-1]
			toNode := nodes[index]
			if fromNode == toNode {
				continue
			}
			if nodeNeighbors[fromNode] == nil {
				nodeNeighbors[fromNode] = make(map[int64]struct{})
			}
			if nodeNeighbors[toNode] == nil {
				nodeNeighbors[toNode] = make(map[int64]struct{})
			}
			nodeNeighbors[fromNode][toNode] = struct{}{}
			nodeNeighbors[toNode][fromNode] = struct{}{}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "build node connectivity: %v\n", err)
		os.Exit(1)
	}

	stats := analysisStats{Ways: wayCount, UniqueNodes: len(nodeNeighbors)}
	for _, neighbors := range nodeNeighbors {
		if len(neighbors) >= 3 {
			stats.JunctionNodes++
		}
	}

	outputFile, outputWriter, err := openOutput(*outputPath, *overwrite)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open output: %v\n", err)
		os.Exit(1)
	}
	writtenSegments := 0

	_, err = walkFeatures(*inputPath, func(item feature, nodes []int64) error {
		wayID, parseErr := strconv.ParseInt(strings.TrimPrefix(item.ID, "w"), 10, 64)
		if parseErr != nil {
			return fmt.Errorf("parse OSM way ID: %w", parseErr)
		}
		segmentIndex := 0
		segmentStartIndex := 0
		// chưa tới đoạn cuối hoặc chưa tới giao lộ thì cộng dồn chiều dài, tới đoạn cuối hoặc tới giao lộ thì tính toán thống kê
		segmentLength := 0.0
		for index := 1; index < len(nodes); index++ {
			distance, distanceErr := distanceMeters(
				item.Geometry.Coordinates[index-1],
				item.Geometry.Coordinates[index],
			)
			if distanceErr != nil {
				return fmt.Errorf("coordinate %d: %w", index, distanceErr)
			}

			segmentLength += distance
			stats.TotalLengthMeters += distance

			isEnd := index == len(nodes)-1
			isJunction := !isEnd && len(nodeNeighbors[nodes[index]]) >= 3
			if !isEnd && !isJunction {
				continue
			}
			// điều kiện cắt
			stats.JunctionSegments++
			stats.SegmentLengthsMeters = append(stats.SegmentLengthsMeters, segmentLength)
			if segmentLength > 1000 {
				stats.OverOneKilometer++
			}
			if segmentLength > 2000 {
				stats.OverTwoKilometers++
			}
			if segmentLength > 3000 {
				stats.OverThreeKilometers++
			}
			if segmentLength > 5000 {
				stats.OverFiveKilometers++
			}
			// gọi hàm chia đoạn tại mỗi ranh giới giao lộ
			ranges, splitErr := splitSegmentRange(
				item.Geometry.Coordinates,
				segmentStartIndex,
				index,
				segmentLength,
				*longSegmentThresholdMeters,
				*targetSegmentLengthMeters,
			)
			if splitErr != nil {
				return splitErr
			}
			if len(ranges) > 1 {
				stats.LongSegments++
				stats.LengthSplits += len(ranges) - 1
			}
			stats.EstimatedSegments += len(ranges)

			for _, segment := range ranges {
				if outputWriter != nil {
					if writeErr := writeSegment(outputWriter, item, nodes, wayID, segmentIndex, segment); writeErr != nil {
						return writeErr
					}
					writtenSegments++
				}
				segmentIndex++
			}

			segmentStartIndex = index
			segmentLength = 0
		}
		return nil
	})
	if err != nil {
		_ = closeOutput(outputFile, outputWriter)
		fmt.Fprintf(os.Stderr, "count segments: %v\n", err)
		os.Exit(1)
	}
	if err := closeOutput(outputFile, outputWriter); err != nil {
		fmt.Fprintf(os.Stderr, "close output: %v\n", err)
		os.Exit(1)
	}

	sort.Float64s(stats.SegmentLengthsMeters)
	fmt.Printf("ways=%d unique_nodes=%d junction_nodes=%d total_length_km=%.2f\n",
		stats.Ways, stats.UniqueNodes, stats.JunctionNodes, stats.TotalLengthMeters/1000)
	fmt.Printf("junction_segments=%d p50_m=%.2f p75_m=%.2f p90_m=%.2f p95_m=%.2f p99_m=%.2f max_m=%.2f\n",
		stats.JunctionSegments,
		percentile(stats.SegmentLengthsMeters, 0.50),
		percentile(stats.SegmentLengthsMeters, 0.75),
		percentile(stats.SegmentLengthsMeters, 0.90),
		percentile(stats.SegmentLengthsMeters, 0.95),
		percentile(stats.SegmentLengthsMeters, 0.99),
		percentile(stats.SegmentLengthsMeters, 1.00))
	fmt.Printf("over_1km=%d over_2km=%d over_3km=%d over_5km=%d\n",
		stats.OverOneKilometer, stats.OverTwoKilometers, stats.OverThreeKilometers, stats.OverFiveKilometers)
	fmt.Printf("threshold_m=%.0f target_m=%.0f long_segments=%d added_splits=%d estimated_segments=%d\n",
		*longSegmentThresholdMeters,
		*targetSegmentLengthMeters,
		stats.LongSegments,
		stats.LengthSplits,
		stats.EstimatedSegments)
	if outputWriter != nil {
		fmt.Printf("output=%s written_segments=%d\n", *outputPath, writtenSegments)
	}
}

func openOutput(path string, overwrite bool) (*os.File, *bufio.Writer, error) {
	if path == "" {
		return nil, nil, nil
	}

	flags := os.O_WRONLY | os.O_CREATE
	if overwrite {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}

	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return file, bufio.NewWriterSize(file, 1024*1024), nil
}

func closeOutput(file *os.File, writer *bufio.Writer) error {
	if file == nil {
		return nil
	}
	if writer != nil {
		if err := writer.Flush(); err != nil {
			_ = file.Close()
			return err
		}
	}
	return file.Close()
}

// Hàm chia đoạn theo độ dài, nếu đoạn dài hơn ngưỡng thì chia thành nhiều đoạn nhỏ hơn
func splitSegmentRange(
	coordinates [][]float64,
	startIndex int,
	endIndex int,
	totalLengthMeters float64,
	longSegmentThresholdMeters float64,
	targetSegmentLengthMeters float64,
) ([]segmentRange, error) {
	if startIndex < 0 || endIndex >= len(coordinates) || startIndex >= endIndex {
		return nil, fmt.Errorf("invalid segment coordinate range: start=%d end=%d", startIndex, endIndex)
	}
	if totalLengthMeters <= longSegmentThresholdMeters {
		return []segmentRange{{
			StartIndex:   startIndex,
			EndIndex:     endIndex,
			LengthMeters: totalLengthMeters,
		}}, nil
	}

	edgeCount := endIndex - startIndex
	parts := int(math.Round(totalLengthMeters / targetSegmentLengthMeters))
	if parts < 2 {
		parts = 2
	}
	if parts > edgeCount {
		parts = edgeCount
	}

	cumulativeLengths := make([]float64, edgeCount+1)
	for localIndex := 1; localIndex <= edgeCount; localIndex++ {
		globalIndex := startIndex + localIndex
		distance, err := distanceMeters(coordinates[globalIndex-1], coordinates[globalIndex])
		if err != nil {
			return nil, fmt.Errorf("split coordinate %d: %w", globalIndex, err)
		}
		cumulativeLengths[localIndex] = cumulativeLengths[localIndex-1] + distance
	}

	boundaries := make([]int, 0, parts+1)
	boundaries = append(boundaries, 0)
	previousBoundary := 0
	measuredLength := cumulativeLengths[edgeCount]
	for partIndex := 1; partIndex < parts; partIndex++ {
		desiredLength := measuredLength * float64(partIndex) / float64(parts)
		candidate := sort.SearchFloat64s(cumulativeLengths, desiredLength)
		if candidate > 0 && candidate < len(cumulativeLengths) {
			beforeDifference := math.Abs(cumulativeLengths[candidate-1] - desiredLength)
			afterDifference := math.Abs(cumulativeLengths[candidate] - desiredLength)
			if beforeDifference < afterDifference {
				candidate--
			}
		}

		minimumBoundary := previousBoundary + 1
		maximumBoundary := edgeCount - (parts - partIndex)
		if candidate < minimumBoundary {
			candidate = minimumBoundary
		}
		if candidate > maximumBoundary {
			candidate = maximumBoundary
		}
		boundaries = append(boundaries, candidate)
		previousBoundary = candidate
	}
	boundaries = append(boundaries, edgeCount)

	ranges := make([]segmentRange, 0, parts)
	for partIndex := 0; partIndex < len(boundaries)-1; partIndex++ {
		localStart := boundaries[partIndex]
		localEnd := boundaries[partIndex+1]
		ranges = append(ranges, segmentRange{
			StartIndex:   startIndex + localStart,
			EndIndex:     startIndex + localEnd,
			LengthMeters: cumulativeLengths[localEnd] - cumulativeLengths[localStart],
		})
	}
	return ranges, nil
}

// Tạo ID, geometry, properties cho từng đoạn và ghi ra output
func writeSegment(
	writer io.Writer,
	item feature,
	nodes []int64,
	wayID int64,
	segmentIndex int,
	segment segmentRange,
) error {
	properties := make(map[string]any, len(item.Properties)+7)
	for key, value := range item.Properties {
		properties[key] = value
	}
	properties["@way_nodes"] = append([]int64(nil), nodes[segment.StartIndex:segment.EndIndex+1]...)
	properties["osm_way_id"] = wayID
	properties["segment_index"] = segmentIndex
	properties["start_node_id"] = nodes[segment.StartIndex]
	properties["end_node_id"] = nodes[segment.EndIndex]
	properties["length_m"] = segment.LengthMeters
	if _, exists := properties["road_class"]; !exists {
		properties["road_class"] = properties["highway"]
	}

	output := feature{
		Type: "Feature",
		ID:   fmt.Sprintf("%s_s%03d", item.ID, segmentIndex),
		Geometry: geometry{
			Type:        "LineString",
			Coordinates: item.Geometry.Coordinates[segment.StartIndex : segment.EndIndex+1],
		},
		Properties: properties,
	}
	if err := json.NewEncoder(writer).Encode(output); err != nil {
		return fmt.Errorf("write segment %s: %w", output.ID, err)
	}
	return nil
}

func percentile(sortedValues []float64, quantile float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}
	index := int(math.Ceil(quantile*float64(len(sortedValues)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sortedValues) {
		index = len(sortedValues) - 1
	}
	return sortedValues[index]
}

func distanceMeters(from []float64, to []float64) (float64, error) {
	// cách tính chiều dài bằng haversine, từ 2 điểm có kinh độ và vĩ độ, tính ra khoảng cách theo mét
	if len(from) < 2 || len(to) < 2 {
		return 0, errors.New("coordinate must contain longitude and latitude")
	}

	const earthRadiusMeters = 6371008.8
	toRadians := math.Pi / 180
	fromLongitude := from[0] * toRadians
	fromLatitude := from[1] * toRadians
	toLongitude := to[0] * toRadians
	toLatitude := to[1] * toRadians
	deltaLongitude := toLongitude - fromLongitude
	deltaLatitude := toLatitude - fromLatitude

	haversine := math.Sin(deltaLatitude/2)*math.Sin(deltaLatitude/2) +
		math.Cos(fromLatitude)*math.Cos(toLatitude)*
			math.Sin(deltaLongitude/2)*math.Sin(deltaLongitude/2)
	if haversine < 0 {
		haversine = 0
	}
	if haversine > 1 {
		haversine = 1
	}

	return 2 * earthRadiusMeters * math.Atan2(
		math.Sqrt(haversine),
		math.Sqrt(1-haversine),
	), nil
}

func walkFeatures(path string, visit func(feature, []int64) error) (int, error) {
	input, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open input: %w", err)
	}
	defer input.Close()

	return scanFeatures(input, visit)
}

func scanFeatures(input io.Reader, visit func(feature, []int64) error) (int, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxFeatureSize)

	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var item feature
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return count, fmt.Errorf("feature %d: decode JSON: %w", count+1, err)
		}

		nodes, err := validateAndReadNodes(item)
		if err != nil {
			return count, fmt.Errorf("feature %s: %w", item.ID, err)
		}
		if err := visit(item, nodes); err != nil {
			return count, fmt.Errorf("feature %s: %w", item.ID, err)
		}
		count++
	}

	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("scan GeoJSONSeq: %w", err)
	}
	return count, nil
}

func validateAndReadNodes(item feature) ([]int64, error) {
	if item.Type != "Feature" {
		return nil, errors.New("object is not a GeoJSON Feature")
	}
	if !strings.HasPrefix(item.ID, "w") || len(item.ID) == 1 {
		return nil, errors.New("feature ID is not an OSM way ID")
	}
	if item.Geometry.Type != "LineString" {
		return nil, errors.New("geometry is not a LineString")
	}

	rawNodes, exists := item.Properties["@way_nodes"]
	if !exists {
		return nil, errors.New("feature has no @way_nodes attribute")
	}
	nodeValues, ok := rawNodes.([]any)
	if !ok {
		return nil, errors.New("@way_nodes is not an array")
	}

	nodes := make([]int64, len(nodeValues))
	for index, value := range nodeValues {
		number, ok := value.(float64)
		if !ok || number < 0 || number != float64(int64(number)) {
			return nil, fmt.Errorf("invalid node ID at index %d", index)
		}
		nodes[index] = int64(number)
	}

	if len(nodes) < 2 {
		return nil, errors.New("way has fewer than two nodes")
	}
	if len(nodes) != len(item.Geometry.Coordinates) {
		return nil, fmt.Errorf("node/coordinate count mismatch: nodes=%d coordinates=%d", len(nodes), len(item.Geometry.Coordinates))
	}
	return nodes, nil
}
