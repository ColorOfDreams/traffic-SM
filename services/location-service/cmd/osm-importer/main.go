package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/tile38"
)

// maxFeatureSize nâng giới hạn 64 KB mặc định của bufio.Scanner. Một OSM way
// dài có thể tạo ra một dòng GeoJSONSeq lớn do chứa nhiều cặp tọa độ.
const maxFeatureSize = 16 * 1024 * 1024

// feature biểu diễn một GeoJSON Feature đọc từ file GeoJSONSeq.
// Các điều kiện LineString và highway được kiểm tra trong validateFeature.
type feature struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Geometry   geometry       `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

// geometry giữ loại geometry và tọa độ JSON thô để tránh decode rồi encode
// lại toàn bộ tọa độ của đường.
type geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// importStats lưu số liệu của quá trình đọc và xử lý file đầu vào.
type importStats struct {
	Read      int // Số dòng JSON không rỗng đã đọc.
	Valid     int // Số Feature vượt qua validateFeature.
	Skipped   int // Số Feature bị bỏ qua do không đạt validation.
	Processed int // Số Feature đã chạy callback process thành công.
}

func main() {
	// Dry-run mặc định được bật để tránh ghi dữ liệu ngoài ý muốn. Limit bằng
	// 0 nghĩa là xử lý toàn bộ file.
	inputPath := flag.String("input", "", "path to a GeoJSONSeq file")
	tile38Address := flag.String("tile38-address", "tile38:9851", "Tile38 TCP address")
	collection := flag.String("collection", "hanoi_roads", "Tile38 collection name")
	dryRun := flag.Bool("dry-run", true, "validate input without writing to Tile38")
	limit := flag.Int("limit", 0, "maximum number of valid features to process; zero means unlimited")
	flag.Parse()

	// Bắt buộc người dùng cung cấp đường dẫn file đầu vào.
	if *inputPath == "" {
		fmt.Fprintln(os.Stderr, "missing required -input argument")
		os.Exit(2)
	}

	// Mở file GeoJSONSeq ở chế độ chỉ đọc; dừng ngay nếu không mở được file.
	input, err := os.Open(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open input: %v\n", err)
		os.Exit(1)
	}
	defer input.Close()

	// Chỉ kết nối Tile38 khi import thật. Dry-run chỉ đọc và validate file.
	var client *tile38.Client
	if !*dryRun {
		client, err = tile38.Dial(*tile38Address)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer client.Close()
	}

	stats, err := processFeatures(input, *limit, func(item feature) error {
		// Callback giúp dùng chung một pipeline đọc/validate cho cả dry-run và
		// import thật. Dry-run không tạo side effect với Tile38.
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
		"read=%d valid=%d skipped=%d processed=%d imported=%d dry_run=%t\n",
		stats.Read,
		stats.Valid,
		stats.Skipped,
		stats.Processed,
		imported,
		*dryRun,
	)
}

func processFeatures(input io.Reader, limit int, process func(feature) error) (importStats, error) {
	// Đọc từng Feature theo dòng để không phải nạp toàn bộ file vào RAM. Một
	// LineString dài có thể vượt giới hạn 64 KB mặc định của Scanner.
	var stats importStats
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxFeatureSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		stats.Read++

		// JSON lỗi làm dừng cả quá trình để tránh import một phần file mà không
		// biết chính xác dữ liệu đã hỏng tại đâu.
		var item feature
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return stats, fmt.Errorf("feature %d: decode JSON: %w", stats.Read, err)
		}

		if err := validateFeature(item); err != nil {
			// Feature không đạt schema được bỏ qua có chủ đích. Lỗi JSON ở trên
			// vẫn làm dừng chương trình vì khi đó không thể tin cậy cấu trúc file.
			stats.Skipped++
			continue
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

func importFeature(client *tile38.Client, collection string, item feature) error {
	// Encode geometry trước khi tạo lệnh để lỗi được trả về có kiểm soát.
	encodedGeometry, err := marshalGeometry(item.Geometry)
	if err != nil {
		return err
	}

	// Tile38 nhận một lệnh SET duy nhất cho mỗi đường. Cách này bảo đảm object,
	// field tĩnh và trạng thái traffic ban đầu được ghi cùng nhau.
	arguments := []string{
		"SET",
		collection,
		item.ID,
		"FIELD",
		"osm_way_id",
		// GeoJSONSeq dùng ID dạng w9656730; field osm_way_id chỉ giữ phần số
		// để khớp trực tiếp với encoded value GraphHopper trả về.
		propertyText(item.Properties["osm_way_id"]),
	}
	// Ánh xạ các tag tĩnh của OSM sang field trong Tile38., bổ sung lấy osm way từ properties để lưu vào Tile38
	// THêm metadata từ properties của OSM way vào Tile38 để phục vụ cho việc truy vấn sau này.
	segmentIndex, _ := integerProperty(item.Properties, "segment_index")
	arguments = append(arguments, "FIELD", "segment_index", fmt.Sprintf("s%03d", segmentIndex))
	arguments = appendPropertyField(arguments, item.Properties, "start_node_id", "start_node_id")
	arguments = appendPropertyField(arguments, item.Properties, "end_node_id", "end_node_id")
	arguments = appendPropertyField(arguments, item.Properties, "length_m", "length_m")
	arguments = appendPropertyField(arguments, item.Properties, "road_class", "road_class")
	arguments = appendPropertyField(arguments, item.Properties, "name", "name")
	arguments = appendPropertyField(arguments, item.Properties, "ref", "ref")
	arguments = appendPropertyField(arguments, item.Properties, "maxspeed", "max_speed")
	arguments = appendPropertyField(arguments, item.Properties, "oneway", "oneway")
	arguments = appendPropertyField(arguments, item.Properties, "lanes", "lanes")

	// Tile38 không trả các field số có giá trị 0 trong WITHFIELDS. Dùng một
	// trạng thái chuỗi để phân biệt rõ "chưa có dữ liệu" với tốc độ thực bằng
	// 0. Worker sẽ thêm avg_speed, vehicle_count, congestion_level và
	// updated_at khi đã xử lý được một batch GPS.
	arguments = append(arguments,
		"FIELD", "traffic_status", "unknown",
		"OBJECT", encodedGeometry,
	)

	_, err = client.Do(arguments...)
	return err
}

func appendPropertyField(arguments []string, properties map[string]any, property string, field string) []string {
	// Chỉ thêm field khi property tương ứng tồn tại và không rỗng.
	// Không phải OSM way nào cũng có name, maxspeed, lanes hoặc ref.
	value, ok := properties[property]
	if !ok {
		return arguments
	}

	text := propertyText(value)
	if text == "" {
		return arguments
	}

	return append(arguments, "FIELD", field, text)
}

func propertyText(value any) string {
	switch number := value.(type) {
	case float64:
		return strconv.FormatFloat(number, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(number), 'f', -1, 32)
	default:
		return fmt.Sprint(value)
	}
}

func marshalGeometry(value geometry) (string, error) {
	// Tile38 SET ... OBJECT cần một chuỗi GeoJSON, trong khi importer đang giữ
	// geometry dưới dạng struct Go và coordinates dưới dạng JSON thô.
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode geometry: %w", err)
	}
	return string(encoded), nil
}

// Check schema geometry của Feature trước khi import vào Tile38. Nếu Feature không hợp lệ thì
func validateFeature(item feature) error {
	// Collection hanoi_roads chỉ dành cho đường. Point như đèn giao thông và
	// Polygon không được phép đi vào cùng collection này.
	if item.Type != "Feature" {
		return errors.New("object is not a GeoJSON Feature")
	}
	if !strings.HasPrefix(item.ID, "w") || len(item.ID) == 1 {
		return errors.New("feature ID is not an OSM segment ID")
	}
	wayID, segmentIndex, err := parseSegmentID(item.ID)
	if err != nil {
		return err
	}
	propertyWayID, err := integerProperty(item.Properties, "osm_way_id")
	if err != nil {
		return err
	}
	propertySegmentIndex, err := integerProperty(item.Properties, "segment_index")
	if err != nil {
		return err
	}
	if propertyWayID != wayID || propertySegmentIndex != int64(segmentIndex) {
		return errors.New("segment ID does not match osm_way_id or segment_index")
	}
	if _, err := integerProperty(item.Properties, "start_node_id"); err != nil {
		return err
	}
	if _, err := integerProperty(item.Properties, "end_node_id"); err != nil {
		return err
	}
	lengthMeters, err := numberProperty(item.Properties, "length_m")
	if err != nil || lengthMeters <= 0 {
		return errors.New("feature has invalid length_m")
	}
	if item.Geometry.Type != "LineString" {
		return errors.New("geometry is not a LineString")
	}
	if len(item.Geometry.Coordinates) == 0 || string(item.Geometry.Coordinates) == "null" {
		return errors.New("geometry has no coordinates")
	}
	var coordinates [][]float64
	if err := json.Unmarshal(item.Geometry.Coordinates, &coordinates); err != nil || len(coordinates) < 2 {
		return errors.New("geometry must contain at least two coordinates")
	}
	if roadClass, ok := item.Properties["road_class"].(string); !ok || roadClass == "" {
		return errors.New("feature has no road_class")
	}

	return nil
}

// tách và kiểm tra id
func parseSegmentID(id string) (int64, int, error) {
	separator := strings.LastIndex(id, "_s")
	if separator <= 1 || separator+2 >= len(id) {
		return 0, 0, errors.New("feature ID must match w<way_id>_s<segment_index>")
	}
	wayID, err := strconv.ParseInt(id[1:separator], 10, 64)
	if err != nil || wayID <= 0 {
		return 0, 0, errors.New("feature ID has invalid OSM way ID")
	}
	segmentIndex, err := strconv.Atoi(id[separator+2:])
	if err != nil || segmentIndex < 0 {
		return 0, 0, errors.New("feature ID has invalid segment index")
	}
	return wayID, segmentIndex, nil
}

func integerProperty(properties map[string]any, name string) (int64, error) {
	value, ok := properties[name]
	if !ok {
		return 0, fmt.Errorf("feature has no %s", name)
	}
	number, ok := value.(float64)
	if !ok || number <= 0 || number != float64(int64(number)) {
		if name == "segment_index" && ok && number == 0 {
			return 0, nil
		}
		return 0, fmt.Errorf("feature has invalid %s", name)
	}
	return int64(number), nil
}

func numberProperty(properties map[string]any, name string) (float64, error) {
	value, ok := properties[name]
	if !ok {
		return 0, fmt.Errorf("feature has no %s", name)
	}
	number, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("feature has invalid %s", name)
	}
	return number, nil
}
