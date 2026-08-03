package mapview

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/traffic"
)

const (
	demoTracePoints     = 5
	demoTraceStep       = 3
	demoMaxTraces       = 12
	demoPointsPerDriver = 60
	demoTrafficWindow   = 10 * time.Second
)

type CongestionRequest struct {
	TraversalKeys []int64 `json:"traversal_keys"`
	Bounds        Bounds  `json:"bounds"`
	VehicleType   string  `json:"vehicle_type"`
}

type CongestionResponse struct {
	ScannedRows         int                       `json:"scanned_rows"`
	CandidatePoints     int                       `json:"candidate_points"`
	AttemptedTraces     int                       `json:"attempted_traces"`
	MatchedTraces       int                       `json:"matched_traces"`
	MatchedObservations int                       `json:"matched_observations"`
	States              []traffic.CongestionState `json:"states"`
}

type DemoCongestionCalculator struct {
	gpsPath string
	matcher matching.Strategy
}

func NewDemoCongestionCalculator(
	gpsPath string,
	matcher matching.Strategy,
) (*DemoCongestionCalculator, error) {
	if strings.TrimSpace(gpsPath) == "" {
		return nil, fmt.Errorf("fake GPS path is required")
	}
	if matcher == nil {
		return nil, fmt.Errorf("matcher is required")
	}
	return &DemoCongestionCalculator{
		gpsPath: strings.TrimSpace(gpsPath),
		matcher: matcher,
	}, nil
}

func (calculator *DemoCongestionCalculator) Calculate(
	ctx context.Context,
	request CongestionRequest,
) (CongestionResponse, error) {
	if calculator == nil || calculator.matcher == nil {
		return CongestionResponse{}, fmt.Errorf("demo congestion calculator is not initialized")
	}
	if err := validateCongestionRequest(request); err != nil {
		return CongestionResponse{}, err
	}

	allowed := make(map[int64]struct{}, len(request.TraversalKeys))
	for _, traversalKey := range request.TraversalKeys {
		allowed[traversalKey] = struct{}{}
	}
	eventsByDriver, scannedRows, candidatePoints, err := readGPSCandidates(
		calculator.gpsPath,
		request.Bounds,
		request.VehicleType,
	)
	if err != nil {
		return CongestionResponse{}, err
	}

	references := traffic.NewMaxSpeedReferenceStore()
	processor, err := traffic.NewProcessor(
		demoTrafficWindow,
		references,
		traffic.CalculatorConfig{MinSamples: 1, MinDrivers: 1},
	)
	if err != nil {
		return CongestionResponse{}, err
	}

	response := CongestionResponse{
		ScannedRows:     scannedRows,
		CandidatePoints: candidatePoints,
		States:          make([]traffic.CongestionState, 0),
	}
	latest := time.Time{}

	driverIDs := make([]string, 0, len(eventsByDriver))
	for driverID := range eventsByDriver {
		driverIDs = append(driverIDs, driverID)
	}
	sort.Strings(driverIDs)

	for _, driverID := range driverIDs {
		events := eventsByDriver[driverID]
		sort.Slice(events, func(i, j int) bool {
			return events[i].RecordedAt.Before(events[j].RecordedAt)
		})
		for start := 0; start+demoTracePoints <= len(events); start += demoTraceStep {
			if response.AttemptedTraces >= demoMaxTraces {
				break
			}
			points := append([]gps.CanonicalEvent(nil), events[start:start+demoTracePoints]...)
			input := trace.Trace{
				DriverID:  driverID,
				Points:    points,
				StartedAt: points[0].RecordedAt,
				EndedAt:   points[len(points)-1].RecordedAt,
			}
			response.AttemptedTraces++
			observations, matchErr := calculator.matcher.Match(ctx, input)
			if matchErr != nil {
				continue
			}
			response.MatchedTraces++
			for _, observation := range observations {
				if !observation.Matched || observation.TraversalKey == nil {
					continue
				}
				if _, ok := allowed[*observation.TraversalKey]; !ok {
					continue
				}
				response.MatchedObservations++
				if err := processor.Add(observation, observation.RecordedAt); err != nil {
					return CongestionResponse{}, err
				}
				if observation.RecordedAt.After(latest) {
					latest = observation.RecordedAt
				}
			}
		}
		if response.AttemptedTraces >= demoMaxTraces {
			break
		}
	}

	if !latest.IsZero() {
		states, err := processor.Flush(ctx, latest.Add(demoTrafficWindow))
		if err != nil {
			return CongestionResponse{}, err
		}
		response.States = states
	}
	return response, nil
}

func validateCongestionRequest(request CongestionRequest) error {
	if len(request.TraversalKeys) == 0 {
		return fmt.Errorf("traversal_keys are required")
	}
	for _, traversalKey := range request.TraversalKeys {
		if traversalKey < 0 {
			return fmt.Errorf("traversal_keys must be non-negative")
		}
	}
	if request.Bounds.MinLongitude >= request.Bounds.MaxLongitude ||
		request.Bounds.MinLatitude >= request.Bounds.MaxLatitude {
		return fmt.Errorf("bounds are invalid")
	}
	if request.VehicleType != "car" && request.VehicleType != "motorcycle" {
		return fmt.Errorf("vehicle_type must be car or motorcycle")
	}
	return nil
}

func readGPSCandidates(
	path string,
	bounds Bounds,
	vehicleType string,
) (map[string][]gps.CanonicalEvent, int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("open fake GPS: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read fake GPS header: %w", err)
	}
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[strings.TrimSpace(name)] = index
	}

	eventsByDriver := make(map[string][]gps.CanonicalEvent)
	scannedRows := 0
	candidatePoints := 0
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, scannedRows, candidatePoints, fmt.Errorf("read fake GPS row: %w", err)
		}
		scannedRows++
		lat, latOK := rowFloat(row, columns, "lat")
		lng, lngOK := rowFloat(row, columns, "lng")
		if !latOK || !lngOK || lat < bounds.MinLatitude || lat > bounds.MaxLatitude ||
			lng < bounds.MinLongitude || lng > bounds.MaxLongitude {
			continue
		}
		payload, err := csvPayload(row, columns)
		if err != nil {
			continue
		}
		event, err := gps.Canonicalize(payload)
		if err != nil {
			continue
		}
		if normalizeDemoVehicle(event.VehicleType) != vehicleType {
			continue
		}
		if len(eventsByDriver[event.DriverID]) >= demoPointsPerDriver {
			continue
		}
		eventsByDriver[event.DriverID] = append(eventsByDriver[event.DriverID], event)
		candidatePoints++
	}
	return eventsByDriver, scannedRows, candidatePoints, nil
}

func csvPayload(row []string, columns map[string]int) (gps.RequestPayload, error) {
	required := []string{
		"lat", "lng", "bearing", "bearing_acc", "horizontal_acc", "speed",
		"speed_acc", "time_delta", "vertical_acc", "timestamp", "altitude",
	}
	values := make(map[string]*float64, len(required))
	for _, name := range required {
		value, ok := rowFloat(row, columns, name)
		if !ok {
			return gps.RequestPayload{}, fmt.Errorf("invalid %s", name)
		}
		valueCopy := value
		values[name] = &valueCopy
	}

	return gps.RequestPayload{
		DriverID:      rowText(row, columns, "driver_id"),
		TTimestamp:    rowText(row, columns, "t_timestamp"),
		Lat:           values["lat"],
		Lng:           values["lng"],
		Bearing:       values["bearing"],
		BearingAcc:    values["bearing_acc"],
		HorizontalAcc: values["horizontal_acc"],
		Speed:         values["speed"],
		SpeedAcc:      values["speed_acc"],
		TimeDelta:     values["time_delta"],
		VerticalAcc:   values["vertical_acc"],
		Status:        rowText(row, columns, "status"),
		VehicleType:   rowText(row, columns, "vehicle_type"),
		Time:          rowText(row, columns, "time"),
		Timestamp:     values["timestamp"],
		Altitude:      values["altitude"],
	}, nil
}

func rowText(row []string, columns map[string]int, name string) string {
	index, ok := columns[name]
	if !ok || index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func rowFloat(row []string, columns map[string]int, name string) (float64, bool) {
	text := rowText(row, columns, name)
	if text == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(text, 64)
	return value, err == nil
}

func normalizeDemoVehicle(vehicleType string) string {
	if vehicleType == "bike" {
		return "motorcycle"
	}
	return vehicleType
}
