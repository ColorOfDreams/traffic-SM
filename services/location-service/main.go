// Biến kết quả thành HTTP response
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/eventstream"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/mapview"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/traffic"
)

const maxGPSRequestBytes = 64 * 1024
const maxMapFeatures = 5000

type graphEdgeReader interface {
	ReadBounds(bounds mapview.Bounds, limit int) (mapview.FeatureCollection, error)
}

type gpsPublisher interface {
	PublishGPS(context.Context, gps.CanonicalEvent) error
}

type serviceConfig struct {
	port                         string
	tile38Address                string
	edgeCollections              []string
	kafkaBrokers                 []string
	kafkaRawTopic                string
	kafkaTraceTopic              string
	kafkaMatchedTopic            string
	kafkaDeadTopic               string
	kafkaTraceGroup              string
	kafkaMatcherGroup            string
	kafkaCommitEvery             time.Duration
	graphHopperURL               string
	graphHopperCarProfile        string
	graphHopperMotorcycleProfile string
	graphVersion                 string
	graphHopperTimeout           time.Duration
	graphHopperAccuracy          float64
	graphHopperWorkers           int
	matchQueueSize               int
	stateConfig                  gps.ConsumerStateConfig
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	config, err := loadConfig()
	if err != nil {
		return err
	}

	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		checkHealth(config.port)
		return nil
	}

	stream, err := eventstream.New(eventstream.Config{
		Brokers:           config.kafkaBrokers,
		RawTopic:          config.kafkaRawTopic,
		TraceTopic:        config.kafkaTraceTopic,
		MatchedTopic:      config.kafkaMatchedTopic,
		DeadLetterTopic:   config.kafkaDeadTopic,
		TraceBuilderGroup: config.kafkaTraceGroup,
		MatcherGroup:      config.kafkaMatcherGroup,
		Workers:           config.graphHopperWorkers,
		JobQueueSize:      config.matchQueueSize,
		MaxMatchAttempts:  config.stateConfig.MaxMatchAttempts,
		CommitInterval:    config.kafkaCommitEvery,
		State:             config.stateConfig,
	})
	if err != nil {
		return err
	}
	defer stream.Close()

	pingContext, cancelPing := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelPing()
	if err := stream.Ping(pingContext); err != nil {
		return err
	}

	fragmentScope, err := traffic.NewTile38Scope(
		config.tile38Address,
		config.edgeCollections,
	)
	if err != nil {
		return err
	}

	matcher, err := matching.NewClient(matching.Config{
		BaseURL:           config.graphHopperURL,
		CarProfile:        config.graphHopperCarProfile,
		MotorcycleProfile: config.graphHopperMotorcycleProfile,
		GraphVersion:      config.graphVersion,
		GPSAccuracy:       config.graphHopperAccuracy,
		Timeout:           config.graphHopperTimeout,
		FragmentScope:     fragmentScope,
	})
	if err != nil {
		return err
	}

	edgeReader := mapview.NewTile38EdgeReader(config.tile38Address, config.edgeCollections)
	handler := newHandlerWithPublisher(edgeReader, stream)

	server := &http.Server{
		Addr:              ":" + config.port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	streamErrors := make(chan error, 1)
	go func() {
		streamErrors <- stream.Run(ctx, matcher)
	}()

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-streamErrors:
		if err != nil {
			runErr = fmt.Errorf("Kafka consumer stopped: %w", err)
		}
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("HTTP server stopped: %w", err)
		}
	}

	stop()
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil && runErr == nil {
		runErr = fmt.Errorf("shutdown HTTP server: %w", err)
	}
	return runErr
}

func newHandler(edgeReader graphEdgeReader) http.Handler {
	return newHandlerWithPublisher(edgeReader, nil)
}

func newHandlerWithPublisher(edgeReader graphEdgeReader, publisher gpsPublisher) http.Handler {
	mux := http.NewServeMux()

	// POST /v1/gps-events ánh xạ kết quả:
	// 202: GPS hợp lệ và đã được Kafka xác nhận.
	// 400: JSON hoặc GPS schema không hợp lệ.
	// 415: Content-Type không phải application/json.
	// 503: Kafka tạm thời không nhận được GPS.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "location-service",
		})
	})
	// reqest mẫu :
	// 	{
	//   "event_id": "gps-001",
	//   "driver_id": "1",
	//   "recorded_at_ms": 1784872804400,
	//   "longitude": 105.8542,
	//   "latitude": 21.0285,
	//   "speed_mps": 9.0,
	//   "heading_deg": 275,
	//   "accuracy_m": 3
	// }
	// response mẫu :
	// {
	//   "status": "valid",
	//   "event": {
	//     "event_id": "gps-001",
	//     "driver_id": "1",
	//     "recorded_at_ms": 1784872804400,
	//     "longitude": 105.8542,
	//     "latitude": 21.0285,
	//     "speed_mps": 9.0,
	//     "heading_deg": 275,
	//     "accuracy_m": 3
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

		// nil chỉ được dùng bởi handler cô lập trong các kiểm tra cũ.
		if publisher != nil {
			if err := publisher.PublishGPS(request.Context(), event); err != nil {
				writeAPIError(w, http.StatusServiceUnavailable, "gps_queue_unavailable", "could not queue GPS event")
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]string{
				"status":    "queued",
				"driver_id": event.DriverID,
			})
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
	mux.HandleFunc("GET /v1/demo/congestion-flow", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, mapview.NewDemoFlow(time.Now()))
	})
	mux.HandleFunc("GET /map", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(mapview.Page())
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
		port:                         envOrDefault("PORT", "8080"),
		tile38Address:                envOrDefault("TILE38_ADDRESS", "tile38:9851"),
		kafkaBrokers:                 splitNonEmpty(envOrDefault("KAFKA_BROKERS", "kafka:29092")),
		kafkaRawTopic:                envOrDefault("KAFKA_RAW_TOPIC", "gps.raw"),
		kafkaTraceTopic:              envOrDefault("KAFKA_TRACE_TOPIC", "gps.traces"),
		kafkaMatchedTopic:            envOrDefault("KAFKA_MATCHED_TOPIC", "gps.matched"),
		kafkaDeadTopic:               envOrDefault("KAFKA_DEAD_LETTER_TOPIC", "gps.dead-letter"),
		kafkaTraceGroup:              envOrDefault("KAFKA_TRACE_BUILDER_GROUP", "location-trace-builder-v1"),
		kafkaMatcherGroup:            envOrDefault("KAFKA_MATCHER_GROUP", "location-map-matching-v2"),
		graphHopperURL:               envOrDefault("GRAPHHOPPER_URL", "http://graphhopper:8989"),
		graphHopperCarProfile:        envOrDefault("GRAPHHOPPER_CAR_PROFILE", "car"),
		graphHopperMotorcycleProfile: envOrDefault("GRAPHHOPPER_MOTORCYCLE_PROFILE", "motorcycle"),
		graphVersion:                 envOrDefault("GRAPH_VERSION", "vietnam-20260730-motorcycle-v1"),
		edgeCollections: splitNonEmpty(
			envOrDefault(
				"GPS_EDGE_COLLECTIONS",
				"hanoi_graph_edges_vietnam_20260730_motorcycle_v1,hochiminh_graph_edges_vietnam_20260730_motorcycle_v1",
			),
		),
	}
	var err error

	config.kafkaCommitEvery, err = durationFromEnv("KAFKA_COMMIT_INTERVAL", time.Second)
	if err != nil {
		return serviceConfig{}, err
	}
	config.graphHopperTimeout, err = durationFromEnv("GRAPHHOPPER_TIMEOUT", 5*time.Second)
	if err != nil {
		return serviceConfig{}, err
	}
	config.graphHopperAccuracy, err = floatFromEnv("GRAPHHOPPER_GPS_ACCURACY", 10)
	if err != nil {
		return serviceConfig{}, err
	}
	config.graphHopperWorkers, err = intFromEnv("GRAPHHOPPER_WORKERS", 4)
	if err != nil {
		return serviceConfig{}, err
	}
	config.matchQueueSize, err = intFromEnv("MATCH_QUEUE_SIZE", 64)
	if err != nil {
		return serviceConfig{}, err
	}

	config.stateConfig.TracePoints, err = intFromEnv("TRACE_POINTS", 5)
	if err != nil {
		return serviceConfig{}, err
	}
	config.stateConfig.OverlapPoints, err = intFromEnv("TRACE_OVERLAP_POINTS", 2)
	if err != nil {
		return serviceConfig{}, err
	}
	config.stateConfig.MaxPointGap, err = durationFromEnv("TRACE_MAX_POINT_GAP", 20*time.Second)
	if err != nil {
		return serviceConfig{}, err
	}
	config.stateConfig.InactivityTimeout, err = durationFromEnv("DRIVER_INACTIVITY_TIMEOUT", 2*time.Minute)
	if err != nil {
		return serviceConfig{}, err
	}
	config.stateConfig.MaxBufferedPoints, err = intFromEnv("DRIVER_MAX_BUFFERED_POINTS", 20)
	if err != nil {
		return serviceConfig{}, err
	}
	config.stateConfig.MaxMatchAttempts, err = intFromEnv("MATCH_MAX_ATTEMPTS", 3)
	if err != nil {
		return serviceConfig{}, err
	}
	config.stateConfig.MinDisplacementM, err = floatFromEnv("TRACE_MIN_DISPLACEMENT_METERS", 10)
	if err != nil {
		return serviceConfig{}, err
	}
	config.stateConfig.MaxImpliedSpeedMPS, err = floatFromEnv("TRACE_MAX_IMPLIED_SPEED_MPS", 70)
	if err != nil {
		return serviceConfig{}, err
	}

	if len(config.edgeCollections) == 0 {
		return serviceConfig{}, fmt.Errorf("GPS_EDGE_COLLECTIONS must contain at least one collection")
	}
	if len(config.kafkaBrokers) == 0 {
		return serviceConfig{}, fmt.Errorf("KAFKA_BROKERS must contain at least one broker")
	}
	if _, err := gps.NewConsumerState(config.stateConfig); err != nil {
		return serviceConfig{}, fmt.Errorf("invalid consumer state config: %w", err)
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

func intFromEnv(name string, fallback int) (int, error) {
	value := envOrDefault(name, strconv.Itoa(fallback))
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return number, nil
}

func floatFromEnv(name string, fallback float64) (float64, error) {
	value := envOrDefault(name, strconv.FormatFloat(fallback, 'f', -1, 64))
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%s must be a finite number", name)
	}
	return number, nil
}

func durationFromEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := envOrDefault(name, fallback.String())
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a Go duration such as 5s or 2m: %w", name, err)
	}
	return duration, nil
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
