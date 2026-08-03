package httpapi

// Giaor tiếp HTTP API cho location service, nhận các request từ client và trả về response tương ứng.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/pipeline"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/traffic"
)

const maxRequestBodyBytes = 1 << 20

type LocationProcessor interface {
	Process(
		ctx context.Context,
		event gps.CanonicalEvent,
		now time.Time,
	) (pipeline.Result, error)
}

type handler struct {
	processor LocationProcessor
	now       func() time.Time
}

type locationResponse struct {
	Accepted                bool                      `json:"accepted"`
	TraceEmitted            bool                      `json:"trace_emitted"`
	MatchedObservationCount int                       `json:"matched_observation_count"`
	CongestionStates        []congestionStateResponse `json:"congestion_states"`
}

type congestionStateResponse struct {
	GraphVersion        string                  `json:"graph_version"`
	TraversalKey        int64                   `json:"traversal_key"`
	VehicleType         string                  `json:"vehicle_type"`
	WindowStart         time.Time               `json:"window_start"`
	CurrentSpeedMPS     float64                 `json:"current_speed_mps"`
	ReferenceSpeedMPS   float64                 `json:"reference_speed_mps"`
	SpeedRatio          float64                 `json:"speed_ratio"`
	CongestionScore     float64                 `json:"congestion_score"`
	SampleCount         int                     `json:"sample_count"`
	DistinctDriverCount int                     `json:"distinct_driver_count"`
	Confidence          float64                 `json:"confidence"`
	Level               traffic.CongestionLevel `json:"level"`
	UpdatedAt           time.Time               `json:"updated_at"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHandler(processor LocationProcessor) (http.Handler, error) {
	return newHandler(processor, time.Now)
}

func newHandler(
	processor LocationProcessor,
	now func() time.Time,
) (http.Handler, error) {
	if processor == nil {
		return nil, fmt.Errorf("location processor is required")
	}
	if now == nil {
		return nil, fmt.Errorf("clock is required")
	}

	handler := &handler{processor: processor, now: now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.health)
	mux.HandleFunc("POST /locations", handler.processLocation)
	return mux, nil
}

func (handler *handler) health(
	responseWriter http.ResponseWriter,
	_ *http.Request,
) {
	writeJSON(responseWriter, http.StatusOK, map[string]string{"status": "ok"})
}

// Kiểm tra observation khi qua driverstate buffer có hợ lệ ko mới đưa vô aggregator
func (handler *handler) processLocation(
	responseWriter http.ResponseWriter,
	request *http.Request,
) {
	request.Body = http.MaxBytesReader(
		responseWriter,
		request.Body,
		maxRequestBodyBytes,
	)
	// decode JSON body
	var payload gps.RequestPayload
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(responseWriter, http.StatusBadRequest, errorResponse{
			Error: fmt.Sprintf("invalid JSON body: %v", err),
		})
		return
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeJSON(responseWriter, http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}
	// canonicalize payload sang event
	event, err := gps.Canonicalize(payload)
	if err != nil {
		writeJSON(responseWriter, http.StatusUnprocessableEntity, errorResponse{
			Error: err.Error(),
		})
		return
	}

	result, err := handler.processor.Process(
		request.Context(),
		event,
		handler.now().UTC(),
	)
	if err != nil {
		writeJSON(responseWriter, http.StatusInternalServerError, errorResponse{
			Error: err.Error(),
		})
		return
	}

	writeJSON(responseWriter, http.StatusOK, locationResponse{
		Accepted:                true,
		TraceEmitted:            result.TraceEmitted,
		MatchedObservationCount: result.MatchedObservationCount,
		CongestionStates:        congestionResponses(result.CongestionStates),
	})
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON body must contain exactly one object")
		}
		return fmt.Errorf("invalid JSON body: %v", err)
	}
	return nil
}

// Response nhận sau khi congestion
func congestionResponses(
	states []traffic.CongestionState,
) []congestionStateResponse {
	responses := make([]congestionStateResponse, len(states))
	for index, state := range states {
		responses[index] = congestionStateResponse{
			GraphVersion:        state.GraphVersion,
			TraversalKey:        state.TraversalKey,
			VehicleType:         state.VehicleType,
			WindowStart:         state.WindowStart,
			CurrentSpeedMPS:     state.CurrentSpeedMPS,
			ReferenceSpeedMPS:   state.ReferenceSpeedMPS,
			SpeedRatio:          state.SpeedRatio,
			CongestionScore:     state.CongestionScore,
			SampleCount:         state.SampleCount,
			DistinctDriverCount: state.DistinctDriverCount,
			Confidence:          state.Confidence,
			Level:               state.Level,
			UpdatedAt:           state.UpdatedAt,
		}
	}
	return responses
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
