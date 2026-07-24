// Hiểu gps nó là gig
// Check xem dữ liệu gps có hợp lệ không
package gps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

// Dữ liệu đã qua validation và chuẩn hóa, sẵn sàng để encode thành JSON response
type CanonicalEvent struct {
	EventID    string   `json:"event_id"`
	VehicleID  string   `json:"vehicle_id"`
	RecordedAt string   `json:"recorded_at"`
	Longitude  float64  `json:"longitude"`
	Latitude   float64  `json:"latitude"`
	SpeedKMH   float64  `json:"speed_kmh"`
	HeadingDeg *float64 `json:"heading_deg"`
}

// Đại diện dữ liệu vừa đọc trong JSON
type requestPayload struct {
	EventID    string          `json:"event_id"`
	VehicleID  string          `json:"vehicle_id"`
	RecordedAt string          `json:"recorded_at"`
	Longitude  *float64        `json:"longitude"`
	Latitude   *float64        `json:"latitude"`
	SpeedKMH   *float64        `json:"speed_kmh"`
	HeadingDeg json.RawMessage `json:"heading_deg"`
}

// JSON request đi vào requestPayload tạm, sau validation được chuyển thành CanonicalEvent.
// HTTP handler encode CanonicalEvent thành JSON response.

func DecodeCanonicalEvent(input io.Reader) (CanonicalEvent, error) {
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	// Đọc JSON vào requestPayload, kiểm tra các trường rồi trả về CanonicalEvent đã chuẩn hóa.
	var value requestPayload
	if err := decoder.Decode(&value); err != nil {
		return CanonicalEvent{}, fmt.Errorf("decode JSON: %w", err)
	}
	if err := ensureEnd(decoder); err != nil {
		return CanonicalEvent{}, err
	}

	eventID := strings.TrimSpace(value.EventID)
	if eventID == "" {
		return CanonicalEvent{}, errors.New("event_id is required")
	}
	vehicleID := strings.TrimSpace(value.VehicleID)
	if vehicleID == "" {
		return CanonicalEvent{}, errors.New("vehicle_id is required")
	}

	recordedAt, err := time.Parse(time.RFC3339Nano, value.RecordedAt)
	if err != nil {
		return CanonicalEvent{}, errors.New("recorded_at must be an RFC3339 timestamp with timezone")
	}

	if value.Longitude == nil || !finite(*value.Longitude) || *value.Longitude < -180 || *value.Longitude > 180 {
		return CanonicalEvent{}, errors.New("longitude must be a finite number from -180 to 180")
	}
	if value.Latitude == nil || !finite(*value.Latitude) || *value.Latitude < -90 || *value.Latitude > 90 {
		return CanonicalEvent{}, errors.New("latitude must be a finite number from -90 to 90")
	}
	if value.SpeedKMH == nil || !finite(*value.SpeedKMH) || *value.SpeedKMH < 0 {
		return CanonicalEvent{}, errors.New("speed_kmh must be a finite number greater than or equal to zero")
	}

	heading, err := decodeHeading(value.HeadingDeg)
	if err != nil {
		return CanonicalEvent{}, err
	}

	return CanonicalEvent{
		EventID:    eventID,
		VehicleID:  vehicleID,
		RecordedAt: recordedAt.UTC().Format(time.RFC3339Nano),
		Longitude:  *value.Longitude,
		Latitude:   *value.Latitude,
		SpeedKMH:   *value.SpeedKMH,
		HeadingDeg: heading,
	}, nil
}

func ensureEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return errors.New("request body must contain exactly one JSON object")
}

func decodeHeading(raw json.RawMessage) (*float64, error) {
	if len(raw) == 0 {
		return nil, errors.New("heading_deg is required and may be null")
	}
	if string(raw) == "null" {
		return nil, nil
	}

	var heading float64
	if err := json.Unmarshal(raw, &heading); err != nil || !finite(heading) || heading < 0 || heading >= 360 {
		return nil, errors.New("heading_deg must be null or a finite number from 0 up to but not including 360")
	}
	return &heading, nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
