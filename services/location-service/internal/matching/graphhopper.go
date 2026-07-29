package matching

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
)

const maxGraphHopperResponseBytes = 4 * 1024 * 1024

type Config struct {
	BaseURL       string
	Profile       string
	GraphVersion  string
	GPSAccuracy   float64
	Timeout       time.Duration
	FragmentScope FragmentScope
}

type Client struct {
	matchURL      string
	graphVersion  string
	httpClient    *http.Client
	fragmentScope FragmentScope
}

type FragmentScope interface {
	Filter(context.Context, []TraversalFragment) ([]TraversalFragment, int, error)
}

func NewClient(config Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("GraphHopper base URL is required")
	}
	if config.Profile == "" {
		return nil, errors.New("GraphHopper profile is required")
	}
	graphVersion := strings.TrimSpace(config.GraphVersion)
	if graphVersion == "" {
		return nil, errors.New("GraphHopper graph version is required")
	}
	if config.GPSAccuracy <= 0 {
		return nil, errors.New("GraphHopper GPS accuracy must be positive")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("GraphHopper timeout must be positive")
	}
	if config.FragmentScope == nil {
		return nil, errors.New("traffic fragment scope is required")
	}

	matchURL, err := url.Parse(baseURL + "/match")
	if err != nil {
		return nil, fmt.Errorf("parse GraphHopper URL: %w", err)
	}
	query := matchURL.Query()
	query.Set("profile", config.Profile)
	query.Set("type", "json")
	query.Set("instructions", "false")
	query.Set("calc_points", "false")
	query.Set("traversal_keys", "true")
	query.Set("gps_accuracy", strconv.FormatFloat(config.GPSAccuracy, 'f', -1, 64))
	matchURL.RawQuery = query.Encode()

	return &Client{
		matchURL:     matchURL.String(),
		graphVersion: graphVersion,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		fragmentScope: config.FragmentScope,
	}, nil
}

func (client *Client) Match(ctx context.Context, trace gps.Trace) (MatchedTrace, error) {
	body, err := encodeGPX(trace)
	if err != nil {
		return MatchedTrace{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.matchURL, bytes.NewReader(body))
	if err != nil {
		return MatchedTrace{}, fmt.Errorf("create GraphHopper request: %w", err)
	}
	request.Header.Set("Content-Type", "application/gpx+xml")
	request.Header.Set("Accept", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return MatchedTrace{}, fmt.Errorf("call GraphHopper: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxGraphHopperResponseBytes+1))
	if err != nil {
		return MatchedTrace{}, fmt.Errorf("read GraphHopper response: %w", err)
	}
	if len(responseBody) > maxGraphHopperResponseBytes {
		return MatchedTrace{}, errors.New("GraphHopper response exceeds size limit")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return MatchedTrace{}, fmt.Errorf(
			"GraphHopper returned HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}
	if !json.Valid(responseBody) {
		return MatchedTrace{}, errors.New("GraphHopper returned invalid JSON")
	}

	matchedTrace, err := adaptGraphHopperResponse(
		trace,
		client.graphVersion,
		time.Now().UTC(),
		responseBody,
	)
	if err != nil {
		return MatchedTrace{}, err
	}

	matchedTrace.MatchedFragmentCount = len(matchedTrace.Fragments)
	filtered, dropped, err := client.fragmentScope.Filter(ctx, matchedTrace.Fragments)
	if err != nil {
		return MatchedTrace{}, fmt.Errorf("filter matched fragments by traffic scope: %w", err)
	}
	matchedTrace.Fragments = filtered
	matchedTrace.DroppedFragmentCount = dropped
	return matchedTrace, nil
}
