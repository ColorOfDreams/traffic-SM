package matching

import (
	"encoding/xml"
	"errors"
	"fmt"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
)

// gpxDocument là root document GPX 1.1 được gửi tới GraphHopper /match.
type gpxDocument struct {
	XMLName xml.Name `xml:"gpx"`
	Version string   `xml:"version,attr"`
	Creator string   `xml:"creator,attr"`
	XMLNS   string   `xml:"xmlns,attr"`
	Track   gpxTrack `xml:"trk"`
}

// gpxTrack chứa một segment liên tục của cùng tài xế.
type gpxTrack struct {
	Segment gpxSegment `xml:"trkseg"`
}

// gpxSegment chứa các GPS point theo thứ tự thời gian.
type gpxSegment struct {
	Points []gpxPoint `xml:"trkpt"`
}

// gpxPoint là schema tối thiểu GraphHopper cần cho map matching.
type gpxPoint struct {
	Latitude  float64 `xml:"lat,attr"`
	Longitude float64 `xml:"lon,attr"`
	Time      string  `xml:"time"`
}

// encodeGPX chuyển một GPS trace thành GPX 1.1.
// Input cần ít nhất hai point; output là XML hoàn chỉnh có XML header.
func encodeGPX(trace gps.Trace) ([]byte, error) {
	if len(trace.Points) < 2 {
		return nil, errors.New("map matching requires at least two GPS points")
	}

	points := make([]gpxPoint, len(trace.Points))
	for index, point := range trace.Points {
		recordedAt, err := point.RecordedAt()
		if err != nil {
			return nil, fmt.Errorf("derive GPX point %d time: %w", index, err)
		}
		points[index] = gpxPoint{
			Latitude:  point.Lat,
			Longitude: point.Lng,
			Time:      recordedAt.Format(time.RFC3339Nano),
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
