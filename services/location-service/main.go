package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/httpapi"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/mapview"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching/graphhopper"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/pipeline"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/traffic"
)

type graphEdgeReader interface {
	ReadDistance(
		bounds mapview.Bounds,
		distanceKM float64,
	) (mapview.FeatureCollection, error)
}

type demoCongestionCalculator interface {
	Calculate(
		ctx context.Context,
		request mapview.CongestionRequest,
	) (mapview.CongestionResponse, error)
}

type serviceConfig struct {
	port                string
	tile38Address       string
	edgeCollections     []string
	graphHopperURL      string
	carProfile          string
	motorcycleProfile   string
	graphVersion        string
	graphHopperTimeout  time.Duration
	graphHopperAccuracy float64
	traceConfig         trace.BufferConfig
	trafficWindow       time.Duration
	minimumSamples      int
	minimumDrivers      int
	fakeGPSPath         string
}

type vehicleMatcher struct {
	car        matching.Strategy
	motorcycle matching.Strategy
}

func (matcher vehicleMatcher) Match(
	ctx context.Context,
	input trace.Trace,
) ([]matching.MatchedObservation, error) {
	if len(input.Points) == 0 {
		return nil, fmt.Errorf("trace points are required")
	}

	vehicleType := input.Points[0].VehicleType
	for index, point := range input.Points {
		if point.VehicleType != vehicleType {
			return nil, fmt.Errorf(
				"trace point %d changes vehicle type from %q to %q",
				index,
				vehicleType,
				point.VehicleType,
			)
		}
	}

	switch vehicleType {
	case "car":
		return matcher.car.Match(ctx, input)
	case "bike":
		return matcher.motorcycle.Match(ctx, input)
	default:
		return nil, fmt.Errorf("unsupported vehicle type %q", vehicleType)
	}
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

	carMatcher, err := graphhopper.NewMatcher(graphhopper.Config{
		BaseURL:      config.graphHopperURL,
		Profile:      config.carProfile,
		GraphVersion: config.graphVersion,
		GPSAccuracy:  config.graphHopperAccuracy,
		Timeout:      config.graphHopperTimeout,
	})
	if err != nil {
		return fmt.Errorf("create car matcher: %w", err)
	}
	motorcycleMatcher, err := graphhopper.NewMatcher(graphhopper.Config{
		BaseURL:      config.graphHopperURL,
		Profile:      config.motorcycleProfile,
		GraphVersion: config.graphVersion,
		GPSAccuracy:  config.graphHopperAccuracy,
		Timeout:      config.graphHopperTimeout,
	})
	if err != nil {
		return fmt.Errorf("create motorcycle matcher: %w", err)
	}

	buffer, err := trace.NewBuffer(config.traceConfig)
	if err != nil {
		return err
	}
	referenceStore := traffic.NewMaxSpeedReferenceStore()
	processor, err := traffic.NewProcessor(
		config.trafficWindow,
		referenceStore,
		traffic.CalculatorConfig{
			MinSamples: config.minimumSamples,
			MinDrivers: config.minimumDrivers,
		},
	)
	if err != nil {
		return err
	}
	matcherRouter := vehicleMatcher{car: carMatcher, motorcycle: motorcycleMatcher}
	locationPipeline, err := pipeline.New(
		buffer,
		matcherRouter,
		processor,
	)
	if err != nil {
		return err
	}
	demoCalculator, err := mapview.NewDemoCongestionCalculator(
		config.fakeGPSPath,
		matcherRouter,
	)
	if err != nil {
		return err
	}
	locationHandler, err := httpapi.NewHandler(locationPipeline)
	if err != nil {
		return err
	}

	edgeReader := mapview.NewTile38EdgeReader(
		config.tile38Address,
		config.edgeCollections,
	)
	server := &http.Server{
		Addr:              ":" + config.port,
		Handler:           newHandler(edgeReader, locationHandler, demoCalculator),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server stopped: %w", err)
		}
	}

	shutdownContext, cancelShutdown := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancelShutdown()
	return server.Shutdown(shutdownContext)
}

func newHandler(
	edgeReader graphEdgeReader,
	locationHandler http.Handler,
	demoCalculators ...demoCongestionCalculator,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(
		responseWriter http.ResponseWriter,
		_ *http.Request,
	) {
		writeJSON(responseWriter, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "location-service",
		})
	})
	if locationHandler != nil {
		mux.Handle("POST /locations", locationHandler)
		mux.Handle("GET /healthz", locationHandler)
		mux.HandleFunc("POST /v1/gps-events", func(
			responseWriter http.ResponseWriter,
			request *http.Request,
		) {
			aliasedRequest := request.Clone(request.Context())
			aliasedRequest.URL.Path = "/locations"
			locationHandler.ServeHTTP(responseWriter, aliasedRequest)
		})
	}
	mux.HandleFunc("GET /v1/graph-edges", func(
		responseWriter http.ResponseWriter,
		request *http.Request,
	) {
		bounds, err := parseBounds(request.URL.Query().Get("bbox"))
		if err != nil {
			writeAPIError(
				responseWriter,
				http.StatusBadRequest,
				"invalid_bbox",
				err.Error(),
			)
			return
		}
		distanceKM, err := parseDistanceKM(
			request.URL.Query().Get("distance_km"),
		)
		if err != nil {
			writeAPIError(
				responseWriter,
				http.StatusBadRequest,
				"invalid_distance_km",
				err.Error(),
			)
			return
		}

		features, err := edgeReader.ReadDistance(bounds, distanceKM)
		if err != nil {
			writeAPIError(
				responseWriter,
				http.StatusServiceUnavailable,
				"graph_edges_unavailable",
				"could not read graph edges",
			)
			return
		}
		writeJSON(responseWriter, http.StatusOK, features)
	})
	if len(demoCalculators) > 0 && demoCalculators[0] != nil {
		demoCalculator := demoCalculators[0]
		mux.HandleFunc("POST /v1/demo/congestion", func(
			responseWriter http.ResponseWriter,
			request *http.Request,
		) {
			request.Body = http.MaxBytesReader(responseWriter, request.Body, 1<<20)
			var input mapview.CongestionRequest
			decoder := json.NewDecoder(request.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil {
				writeAPIError(responseWriter, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			result, err := demoCalculator.Calculate(request.Context(), input)
			if err != nil {
				writeAPIError(
					responseWriter,
					http.StatusUnprocessableEntity,
					"congestion_calculation_failed",
					err.Error(),
				)
				return
			}
			writeJSON(responseWriter, http.StatusOK, result)
		})
	}
	mux.HandleFunc("GET /map", func(
		responseWriter http.ResponseWriter,
		_ *http.Request,
	) {
		responseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
		responseWriter.WriteHeader(http.StatusOK)
		_, _ = responseWriter.Write(mapview.Page())
	})
	mux.HandleFunc("GET /", func(
		responseWriter http.ResponseWriter,
		request *http.Request,
	) {
		if request.URL.Path != "/" {
			http.NotFound(responseWriter, request)
			return
		}
		http.Redirect(responseWriter, request, "/map", http.StatusTemporaryRedirect)
	})
	return mux
}

func parseBounds(value string) (mapview.Bounds, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return mapview.Bounds{}, fmt.Errorf(
			"bbox must contain minLon,minLat,maxLon,maxLat",
		)
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
		return mapview.Bounds{}, fmt.Errorf(
			"bbox is outside valid coordinate ranges or has reversed bounds",
		)
	}
	return bounds, nil
}

func parseDistanceKM(value string) (float64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("distance_km is required")
	}
	distanceKM, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(distanceKM) || math.IsInf(distanceKM, 0) {
		return 0, fmt.Errorf("distance_km must be a number")
	}
	if distanceKM < 0.1 || distanceKM > 1000 {
		return 0, fmt.Errorf("distance_km must be from 0.1 to 1000")
	}
	return distanceKM, nil
}

func loadConfig() (serviceConfig, error) {
	config := serviceConfig{
		port:              envOrDefault("PORT", "8080"),
		tile38Address:     envOrDefault("TILE38_ADDRESS", "tile38:9851"),
		graphHopperURL:    envOrDefault("GRAPHHOPPER_URL", "http://graphhopper:8989"),
		carProfile:        envOrDefault("GRAPHHOPPER_CAR_PROFILE", "car"),
		motorcycleProfile: envOrDefault("GRAPHHOPPER_MOTORCYCLE_PROFILE", "motorcycle"),
		graphVersion:      envOrDefault("GRAPH_VERSION", "vietnam-20260730-motorcycle-v1"),
		fakeGPSPath:       envOrDefault("FAKE_GPS_PATH", "/data/gps-fake/fake_gps.csv"),
		edgeCollections: splitNonEmpty(envOrDefault(
			"GPS_EDGE_COLLECTIONS",
			"hanoi_graph_edges_vietnam_20260730_motorcycle_v1,"+
				"hochiminh_graph_edges_vietnam_20260730_motorcycle_v1",
		)),
	}

	var err error
	config.graphHopperTimeout, err = durationFromEnv(
		"GRAPHHOPPER_TIMEOUT",
		5*time.Second,
	)
	if err != nil {
		return serviceConfig{}, err
	}
	config.graphHopperAccuracy, err = floatFromEnv(
		"GRAPHHOPPER_GPS_ACCURACY",
		10,
	)
	if err != nil || config.graphHopperAccuracy <= 0 {
		return serviceConfig{}, fmt.Errorf("GRAPHHOPPER_GPS_ACCURACY must be positive")
	}
	config.traceConfig.MaxPoints, err = intFromEnv("TRACE_POINTS", 5)
	if err != nil {
		return serviceConfig{}, err
	}
	config.traceConfig.MinPoints, err = intFromEnv("TRACE_MIN_POINTS", 2)
	if err != nil {
		return serviceConfig{}, err
	}
	config.traceConfig.OverlapPoints, err = intFromEnv("TRACE_OVERLAP_POINTS", 2)
	if err != nil {
		return serviceConfig{}, err
	}
	config.traceConfig.MaxDuration, err = durationFromEnv(
		"TRACE_MAX_DURATION",
		30*time.Second,
	)
	if err != nil {
		return serviceConfig{}, err
	}
	config.trafficWindow, err = durationFromEnv("TRAFFIC_WINDOW", 10*time.Second)
	if err != nil {
		return serviceConfig{}, err
	}
	config.minimumSamples, err = intFromEnv("TRAFFIC_MIN_SAMPLES", 1)
	if err != nil || config.minimumSamples < 1 {
		return serviceConfig{}, fmt.Errorf("TRAFFIC_MIN_SAMPLES must be positive")
	}
	config.minimumDrivers, err = intFromEnv("TRAFFIC_MIN_DRIVERS", 1)
	if err != nil || config.minimumDrivers < 1 {
		return serviceConfig{}, fmt.Errorf("TRAFFIC_MIN_DRIVERS must be positive")
	}
	if len(config.edgeCollections) == 0 {
		return serviceConfig{}, fmt.Errorf(
			"GPS_EDGE_COLLECTIONS must contain at least one collection",
		)
	}
	if err := config.traceConfig.Validate(); err != nil {
		return serviceConfig{}, fmt.Errorf("invalid trace config: %w", err)
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
	value := envOrDefault(
		name,
		strconv.FormatFloat(fallback, 'f', -1, 64),
	)
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%s must be a finite number", name)
	}
	return number, nil
}

func durationFromEnv(
	name string,
	fallback time.Duration,
) (time.Duration, error) {
	value := envOrDefault(name, fallback.String())
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf(
			"%s must be a Go duration such as 5s or 2m: %w",
			name,
			err,
		)
	}
	return duration, nil
}

func writeAPIError(
	responseWriter http.ResponseWriter,
	status int,
	code string,
	message string,
) {
	writeJSON(responseWriter, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func writeJSON(
	responseWriter http.ResponseWriter,
	status int,
	value any,
) {
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(status)
	_ = json.NewEncoder(responseWriter).Encode(value)
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
