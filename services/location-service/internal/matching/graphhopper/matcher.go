package graphhopper

// Thực hiện luồng encode gọi /match chuyển thành decode json
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
)

type Config struct {
	BaseURL     string
	Profile     string
	GPSAccuracy float64
	Timeout     time.Duration
}

type Matcher struct {
	endpoint    *url.URL
	profile     string
	gpsAccuracy float64
	client      *http.Client
}

var _ matching.Strategy = (*Matcher)(nil)

func NewMatcher(config Config) (*Matcher, error) {
	baseURL := strings.TrimRight(
		strings.TrimSpace(config.BaseURL),
		"/",
	)

	if baseURL == "" {
		return nil, fmt.Errorf("GraphHopper base URL is required")
	}

	if strings.TrimSpace(config.Profile) == "" {
		return nil, fmt.Errorf("GraphHopper profile is required")
	}

	if config.GPSAccuracy <= 0 {
		return nil, fmt.Errorf("GraphHopper GPS accuracy must be positive")
	}

	if config.Timeout <= 0 {
		return nil, fmt.Errorf("GraphHopper timeout must be positive")
	}

	endpoint, err := url.Parse(baseURL + "/match")
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf(
			"invalid GraphHopper base URL %q",
			config.BaseURL,
		)
	}

	return &Matcher{
		endpoint:    endpoint,
		profile:     strings.TrimSpace(config.Profile),
		gpsAccuracy: config.GPSAccuracy,
		client: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

func (m *Matcher) Match(
	ctx context.Context,
	input trace.Trace,
) ([]matching.MatchedObservation, error) {
	// Encode
	gpxBody, err := encodeGPX(input)
	if err != nil {
		return nil, err
	}

	endpoint := *m.endpoint
	query := endpoint.Query()
	query.Set("type", "json")
	query.Set("profile", m.profile)
	query.Set("traversal_keys", "true")
	query.Set("gps_accuracy", strconv.FormatFloat(
		m.gpsAccuracy,
		'f',
		-1,
		64,
	))
	query.Set("instructions", "false")
	query.Set("calc_points", "false")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint.String(),
		bytes.NewReader(gpxBody),
	)
	if err != nil {
		return nil, fmt.Errorf("create GraphHopper request: %w", err)
	}

	request.Header.Set("Content-Type", "application/gpx+xml")
	request.Header.Set("Accept", "application/json")

	response, err := m.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call GraphHopper: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))

		return nil, fmt.Errorf(
			"GraphHopper returned status %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	//Decode
	var decoded matchResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode GraphHopper response: %w", err)
	}

	return adaptResponse(input, decoded)
}
