package graphhopper

// encode dữ liệu thành gpx
import (
	"encoding/xml"
	"fmt"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
)

const gpxNamespace = "http://www.topografix.com/GPX/1/1"

type gpxDocument struct {
	XMLName xml.Name `xml:"gpx"`
	XMLNS   string   `xml:"xmlns,attr"`
	Version string   `xml:"version,attr"`
	Creator string   `xml:"creator,attr"`
	Track   gpxTrack `xml:"trk"`
}

type gpxTrack struct {
	Name    string          `xml:"name"`
	Segment gpxTrackSegment `xml:"trkseg"`
}

type gpxTrackSegment struct {
	Points []gpxTrackPoint `xml:"trkpt"`
}

type gpxTrackPoint struct {
	Lat  float64 `xml:"lat,attr"`
	Lon  float64 `xml:"lon,attr"`
	Time string  `xml:"time"`
}

func encodeGPX(input trace.Trace) ([]byte, error) {
	if len(input.Points) < 2 {
		return nil, fmt.Errorf("trace requires at least two points")
	}

	points := make([]gpxTrackPoint, len(input.Points))

	for index, point := range input.Points {
		if point.RecordedAt.IsZero() {
			return nil, fmt.Errorf(
				"trace point %d recorded_at is required",
				index,
			)
		}
		points[index] = gpxTrackPoint{
			Lat:  point.Lat,
			Lon:  point.Lng,
			Time: point.RecordedAt.UTC().Format(time.RFC3339Nano),
		}
	}

	document := gpxDocument{
		XMLNS:   gpxNamespace,
		Version: "1.1",
		Creator: "traffic-system",
		Track: gpxTrack{
			Name: input.DriverID,
			Segment: gpxTrackSegment{
				Points: points,
			},
		},
	}
	body, err := xml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode GPX: %w", err)
	}

	return append([]byte(xml.Header), body...), nil

}
