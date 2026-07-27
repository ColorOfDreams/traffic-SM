package gps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

// CanonicalEvent là payload GPS tối thiểu được ghi vào Kafka.
// DriverID đồng thời được dùng làm Kafka message key để giữ thứ tự theo tài xế.
// SpeedMPS, HeadingDeg và AccuracyM dùng -1 khi thiết bị không cung cấp giá trị.
// Phiên bản chính thức nhận được
type CanonicalEvent struct {
	EventID      string  `json:"event_id"`
	DriverID     string  `json:"driver_id"`
	RecordedAtMS int64   `json:"recorded_at_ms"`
	Longitude    float64 `json:"longitude"`
	Latitude     float64 `json:"latitude"`
	SpeedMPS     float64 `json:"speed_mps"`
	HeadingDeg   float64 `json:"heading_deg"`
	AccuracyM    float64 `json:"accuracy_m"`
}

// Struct trung gian để phát hiện field bị thiếu
type requestPayload struct {
	EventID      string   `json:"event_id"`
	DriverID     string   `json:"driver_id"`
	RecordedAtMS *int64   `json:"recorded_at_ms"`
	Longitude    *float64 `json:"longitude"`
	Latitude     *float64 `json:"latitude"`
	SpeedMPS     *float64 `json:"speed_mps"`
	HeadingDeg   *float64 `json:"heading_deg"`
	AccuracyM    *float64 `json:"accuracy_m"`
}

// Decode chuyển sang chuẩn file caniconal và validate dữ liệu
// Input nhận vào là 1 lần nhận dữ liệu
func DecodeCanonicalEvent(input io.Reader) (CanonicalEvent, error) {
	// Khai báo con trỏ
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	// Tạo biến request và thực hiện check nếu đúng thì trả về canonical, nếu vấn đề thì trả về lỗi
	var value requestPayload
	if err := decoder.Decode(&value); err != nil {
		return CanonicalEvent{}, fmt.Errorf("decode JSON: %w", err)
	}
	if err := ensureEnd(decoder); err != nil {
		return CanonicalEvent{}, err
	}
	// Validate
	eventID := strings.TrimSpace(value.EventID)
	if eventID == "" {
		return CanonicalEvent{}, errors.New("event_id is required")
	}
	driverID := strings.TrimSpace(value.DriverID)
	if driverID == "" {
		return CanonicalEvent{}, errors.New("driver_id is required")
	}
	if value.RecordedAtMS == nil || *value.RecordedAtMS <= 0 {
		return CanonicalEvent{}, errors.New("recorded_at_ms must be a positive Unix timestamp in milliseconds")
	}

	if value.Longitude == nil || !finite(*value.Longitude) || *value.Longitude < -180 || *value.Longitude > 180 {
		return CanonicalEvent{}, errors.New("longitude must be a finite number from -180 to 180")
	}
	if value.Latitude == nil || !finite(*value.Latitude) || *value.Latitude < -90 || *value.Latitude > 90 {
		return CanonicalEvent{}, errors.New("latitude must be a finite number from -90 to 90")
	}
	if value.SpeedMPS == nil || !validOptionalNonNegative(*value.SpeedMPS) {
		return CanonicalEvent{}, errors.New("speed_mps must be -1 or a finite number greater than or equal to zero")
	}
	if value.HeadingDeg == nil || !finite(*value.HeadingDeg) ||
		(*value.HeadingDeg != -1 && (*value.HeadingDeg < 0 || *value.HeadingDeg >= 360)) {
		return CanonicalEvent{}, errors.New("heading_deg must be -1 or a finite number from 0 up to but not including 360")
	}
	if value.AccuracyM == nil || !validOptionalNonNegative(*value.AccuracyM) {
		return CanonicalEvent{}, errors.New("accuracy_m must be -1 or a finite number greater than or equal to zero")
	}

	return CanonicalEvent{
		EventID:      eventID,
		DriverID:     driverID,
		RecordedAtMS: *value.RecordedAtMS,
		Longitude:    *value.Longitude,
		Latitude:     *value.Latitude,
		SpeedMPS:     *value.SpeedMPS,
		HeadingDeg:   *value.HeadingDeg,
		AccuracyM:    *value.AccuracyM,
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

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validOptionalNonNegative(value float64) bool {
	return finite(value) && (value == -1 || value >= 0)
}
