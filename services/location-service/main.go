// Biến kết quả thành HTTP response
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/mapview"
)

const maxGPSRequestBytes = 64 * 1024
const maxMapFeatures = 5000

type graphEdgeReader interface {
	ReadBounds(bounds mapview.Bounds, limit int) (mapview.FeatureCollection, error)
}

// ServiceConfig giữ cấu hình của location service, được đọc từ biến môi trường.
type serviceConfig struct {
	port            string
	tile38Address   string
	edgeCollections []string
}

func main() {
	// khởi động locaion service
	// Đọc biến môi trường, tạo graph edge reader, handler và server. Nếu có lỗi, in ra stderr và thoát với mã lỗi.
	config, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		checkHealth(config.port)
		return
	}
	// Tạo graph edge reader phục vụ endpoint hiển thị dữ liệu từ Tile38.
	edgeReader := mapview.NewTile38EdgeReader(config.tile38Address, config.edgeCollections)
	handler := newHandler(edgeReader)

	server := &http.Server{
		Addr:              ":" + config.port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newHandler(edgeReader graphEdgeReader) http.Handler {
	mux := http.NewServeMux()

	// POST /v1/gps-events ánh xạ kết quả:
	// 200: GPS hợp lệ theo canonical schema.
	// 400: JSON hoặc GPS schema không hợp lệ.
	// 415: Content-Type không phải application/json.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "location-service",
		})
	})
	// reqest mẫu :
	// 	{
	//   "event_id": "gps-001",
	//   "vehicle_id": "vehicle-001",
	//   "recorded_at": "2026-07-23T11:00:05+07:00",
	//   "longitude": 105.8542,
	//   "latitude": 21.0285,
	//   "speed_kmh": 32.4,
	//   "heading_deg": 275
	// }
	// response mẫu :
	// {
	//   "status": "valid",
	//   "event": {
	//     "event_id": "gps-001",
	//     "vehicle_id": "vehicle-001",
	//     "recorded_at": "2026-07-23T04:00:05Z",
	//     "longitude": 105.8542,
	//     "latitude": 21.0285,
	//     "speed_kmh": 32.4,
	//     "heading_deg": 275
	//   }
	// }
	mux.HandleFunc("POST /v1/gps-events", func(w http.ResponseWriter, request *http.Request) {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
			return
		}

		request.Body = http.MaxBytesReader(w, request.Body, maxGPSRequestBytes)
		event, err := gps.DecodeCanonicalEvent(request.Body)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_gps_event", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status": "valid",
			"event":  event,
		})
	})
	mux.HandleFunc("GET /v1/graph-edges", func(w http.ResponseWriter, request *http.Request) {
		bounds, err := parseBounds(request.URL.Query().Get("bbox"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_bbox", err.Error())
			return
		}

		features, err := edgeReader.ReadBounds(bounds, maxMapFeatures)
		if err != nil {
			writeAPIError(w, http.StatusServiceUnavailable, "graph_edges_unavailable", "could not read graph edges")
			return
		}
		writeJSON(w, http.StatusOK, features)
	})
	return mux
}

func parseBounds(value string) (mapview.Bounds, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return mapview.Bounds{}, fmt.Errorf("bbox must contain minLon,minLat,maxLon,maxLat")
	}

	coordinates := make([]float64, 4)
	for index, part := range parts {
		number, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return mapview.Bounds{}, fmt.Errorf("bbox contains an invalid number")
		}
		coordinates[index] = number
	}

	bounds := mapview.Bounds{
		MinLongitude: coordinates[0],
		MinLatitude:  coordinates[1],
		MaxLongitude: coordinates[2],
		MaxLatitude:  coordinates[3],
	}
	if bounds.MinLongitude < -180 || bounds.MaxLongitude > 180 ||
		bounds.MinLatitude < -90 || bounds.MaxLatitude > 90 ||
		bounds.MinLongitude >= bounds.MaxLongitude ||
		bounds.MinLatitude >= bounds.MaxLatitude {
		return mapview.Bounds{}, fmt.Errorf("bbox is outside valid coordinate ranges or has reversed bounds")
	}
	return bounds, nil
}

// load config kiểm tra
func loadConfig() (serviceConfig, error) {
	config := serviceConfig{
		port:          envOrDefault("PORT", "8080"),
		tile38Address: envOrDefault("TILE38_ADDRESS", "tile38:9851"),
		edgeCollections: splitNonEmpty(
			envOrDefault(
				"GPS_EDGE_COLLECTIONS",
				"hanoi_graph_edges_vietnam_20260722,hochiminh_graph_edges_vietnam_20260722",
			),
		),
	}

	if len(config.edgeCollections) == 0 {
		return serviceConfig{}, fmt.Errorf("GPS_EDGE_COLLECTIONS must contain at least one collection")
	}
	return config, nil
}

func envOrDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func splitNonEmpty(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func writeAPIError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func checkHealth(port string) {
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + port + "/health")
	if err != nil {
		os.Exit(1)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
