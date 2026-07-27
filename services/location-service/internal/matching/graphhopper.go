package matching

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
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
	BaseURL     string
	Profile     string
	GPSAccuracy float64
	Timeout     time.Duration
}

type Result struct {
	ResponseJSON []byte
	TookMS       int64
}

type Client struct {
	matchURL   string
	httpClient *http.Client
}

func NewClient(config Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("GraphHopper base URL is required")
	}
	if config.Profile == "" {
		return nil, errors.New("GraphHopper profile is required")
	}
	if config.GPSAccuracy <= 0 {
		return nil, errors.New("GraphHopper GPS accuracy must be positive")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("GraphHopper timeout must be positive")
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
		matchURL: matchURL.String(),
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

func (client *Client) Match(ctx context.Context, trace gps.Trace) (Result, error) {
	body, err := encodeGPX(trace)
	if err != nil {
		return Result{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.matchURL, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("create GraphHopper request: %w", err)
	}
	request.Header.Set("Content-Type", "application/gpx+xml")
	request.Header.Set("Accept", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("call GraphHopper: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxGraphHopperResponseBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("read GraphHopper response: %w", err)
	}
	if len(responseBody) > maxGraphHopperResponseBytes {
		return Result{}, errors.New("GraphHopper response exceeds size limit")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Result{}, fmt.Errorf(
			"GraphHopper returned HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}
	if !json.Valid(responseBody) {
		return Result{}, errors.New("GraphHopper returned invalid JSON")
	}

	var tookMS int64
	if value := response.Header.Get("X-GH-Took"); value != "" {
		tookMS, _ = strconv.ParseInt(value, 10, 64)
	}

	return Result{
		ResponseJSON: responseBody,
		TookMS:       tookMS,
	}, nil
}

type gpxDocument struct {
	XMLName xml.Name `xml:"gpx"`
	Version string   `xml:"version,attr"`
	Creator string   `xml:"creator,attr"`
	XMLNS   string   `xml:"xmlns,attr"`
	Track   gpxTrack `xml:"trk"`
}

type gpxTrack struct {
	Segment gpxSegment `xml:"trkseg"`
}

type gpxSegment struct {
	Points []gpxPoint `xml:"trkpt"`
}

type gpxPoint struct {
	Latitude  float64 `xml:"lat,attr"`
	Longitude float64 `xml:"lon,attr"`
	Time      string  `xml:"time"`
}

func encodeGPX(trace gps.Trace) ([]byte, error) {
	if len(trace.Points) < 2 {
		return nil, errors.New("map matching requires at least two GPS points")
	}

	points := make([]gpxPoint, len(trace.Points))
	for index, point := range trace.Points {
		points[index] = gpxPoint{
			Latitude:  point.Latitude,
			Longitude: point.Longitude,
			Time:      time.UnixMilli(point.RecordedAtMS).UTC().Format(time.RFC3339Nano),
		}
	}

	document := gpxDocument{
		Version: "1.1",
		Creator: "traffic-location-service",
		XMLNS:   "http://www.topografix.com/GPX/1/1",
		Track: gpxTrack{
			Segment: gpxSegment{Points: points},
		},
	}
	body, err := xml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode GPX: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}
