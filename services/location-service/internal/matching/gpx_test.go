package matching

import (
	"strings"
	"testing"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
)

func TestEncodeGPXUsesCanonicalCoordinatesAndTime(t *testing.T) {
	trace := gps.Trace{
		DriverID: "43",
		Points: []gps.CanonicalEvent{
			{
				DriverID:   "43",
				Time:       "7/23/2026 7:55",
				TTimestamp: "55:04.6",
				Lat:        20.988913,
				Lng:        105.941475,
			},
			{
				DriverID:   "43",
				Time:       "7/23/2026 7:55",
				TTimestamp: "55:09.6",
				Lat:        20.989013,
				Lng:        105.941575,
			},
		},
	}

	body, err := encodeGPX(trace)
	if err != nil {
		t.Fatalf("encode GPX: %v", err)
	}
	output := string(body)

	expectedParts := []string{
		`<trkpt lat="20.988913" lon="105.941475">`,
		`<time>2026-07-23T00:55:04.6Z</time>`,
		`<trkpt lat="20.989013" lon="105.941575">`,
		`<time>2026-07-23T00:55:09.6Z</time>`,
	}
	for _, expected := range expectedParts {
		if !strings.Contains(output, expected) {
			t.Fatalf("GPX does not contain %q: %s", expected, output)
		}
	}
}
