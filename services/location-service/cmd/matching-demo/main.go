package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching/graphhopper"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
)

func main() {
	matcher, err := graphhopper.NewMatcher(graphhopper.Config{
		BaseURL:      "http://localhost:8989",
		Profile:      "car",
		GraphVersion: "vietnam-20260730-motorcycle-v1",
		GPSAccuracy:  20,
		Timeout:      10 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}

	startedAt := time.Now().UTC()

	points := []gps.CanonicalEvent{
		demoPoint(21.028511, 105.854193, 8.2, startedAt),
		demoPoint(21.028224, 105.854304, 8.5, startedAt.Add(time.Second)),
		demoPoint(21.027939, 105.854410, 8.7, startedAt.Add(2*time.Second)),
		demoPoint(21.027650, 105.854520, 8.4, startedAt.Add(3*time.Second)),
	}

	input := trace.Trace{
		DriverID:  "demo-driver",
		Points:    points,
		StartedAt: points[0].RecordedAt,
		EndedAt:   points[len(points)-1].RecordedAt,
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	observations, err := matcher.Match(ctx, input)
	if err != nil {
		log.Fatal(err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(observations); err != nil {
		log.Fatal(err)
	}
}

func demoPoint(
	lat float64,
	lng float64,
	speed float64,
	recordedAt time.Time,
) gps.CanonicalEvent {
	return gps.CanonicalEvent{
		DriverID:    "demo-driver",
		Lat:         lat,
		Lng:         lng,
		Speed:       speed,
		Status:      "IN TRIP",
		VehicleType: "car",
		RecordedAt:  recordedAt,
	}
}
