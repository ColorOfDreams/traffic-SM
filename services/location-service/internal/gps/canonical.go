package gps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const csvTimeLayout = "1/2/2006 15:04"

var gpsTimeZone = time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)

// CanonicalEvent giữ nguyên schema 19 cột của dữ liệu GPS nguồn.
type CanonicalEvent struct {
	// DriverID là mã định danh tài xế. Field này bắt buộc, không được rỗng
	// và được dùng làm Kafka message key để giữ thứ tự dữ liệu theo tài xế.
	DriverID string `json:"driver_id"`

	// TTimestamp là thời gian rút gọn của bản ghi GPS, có dạng MM:SS.s.
	// Field này bổ sung phần phút và giây chi tiết cho Time.
	TTimestamp string `json:"t_timestamp"`

	// Lat là vĩ độ dạng độ thập phân, bắt buộc nằm trong đoạn [-90, 90].
	Lat float64 `json:"lat"`

	// Lng là kinh độ dạng độ thập phân, bắt buộc nằm trong đoạn [-180, 180].
	Lng float64 `json:"lng"`

	// Bearing là hướng di chuyển do nguồn GPS cung cấp, tính bằng độ.
	// Giá trị -1 biểu thị không có dữ liệu; giá trị thực phải thuộc [0, 360).
	Bearing float64 `json:"bearing"`

	// BearingAcc là sai số ước tính của Bearing.
	// Giá trị 0 có thể biểu thị thiết bị không cung cấp độ chính xác.
	BearingAcc float64 `json:"bearing_acc"`

	// HorizontalAcc là độ chính xác theo phương ngang của tọa độ GPS, thường tính bằng mét.
	HorizontalAcc float64 `json:"horizontal_acc"`

	// Speed là tốc độ tại điểm GPS. Đơn vị chưa được dữ liệu nguồn xác định.
	Speed float64 `json:"speed"`

	// SpeedAcc là sai số hoặc độ chính xác ước tính của Speed, thường được hiểu theo m/s.
	SpeedAcc float64 `json:"speed_acc"`

	// TimeDelta là khoảng thời gian dự kiến giữa hai lần lấy GPS, tính bằng giây.
	TimeDelta float64 `json:"time_delta"`

	// VerticalAcc là độ chính xác theo phương thẳng đứng của Altitude, thường tính bằng mét.
	VerticalAcc float64 `json:"vertical_acc"`

	// Status là trạng thái hoạt động của tài xế tại thời điểm GPS được ghi nhận.
	Status string `json:"status"`

	// VehicleType là loại phương tiện gắn với tài xế hoặc bản ghi GPS.
	VehicleType string `json:"vehicle_type"`

	// Time là ngày, giờ và phút của bản ghi theo định dạng M/D/YYYY HH:MM.
	// Field này đã được làm tròn đến phút và được diễn giải theo múi giờ UTC+7.
	Time string `json:"time"`

	// Timestamp là Unix timestamp dạng millisecond do dữ liệu nguồn cung cấp.
	// CSV có thể biểu diễn field này bằng ký pháp khoa học và làm mất phần chi tiết,
	// vì vậy thời gian chuẩn của pipeline được tạo từ Time + TTimestamp.
	Timestamp float64 `json:"timestamp"`

	// Altitude là độ cao so với mực nước biển, thường tính bằng mét.
	Altitude float64 `json:"altitude"`

	// Side là phía của đường hoặc phía tương đối sau xử lý.
	// Cột có thể rỗng nên dùng con trỏ để phân biệt thiếu giá trị với chuỗi có nội dung.
	Side *string `json:"side"`

	// DeltaTime là thời gian thực tế từ điểm GPS trước đến điểm hiện tại, tính bằng giây.
	// Cột này có thể rỗng.
	DeltaTime *float64 `json:"delta_time"`

	// Distance là khoảng cách từ điểm GPS trước đến điểm hiện tại.
	// Cột này có thể rỗng; tài liệu nguồn chưa xác định đơn vị.
	Distance *float64 `json:"distance"`
}

type requestPayload struct {
	DriverID      string   `json:"driver_id"`
	TTimestamp    string   `json:"t_timestamp"`
	Lat           *float64 `json:"lat"`
	Lng           *float64 `json:"lng"`
	Bearing       *float64 `json:"bearing"`
	BearingAcc    *float64 `json:"bearing_acc"`
	HorizontalAcc *float64 `json:"horizontal_acc"`
	Speed         *float64 `json:"speed"`
	SpeedAcc      *float64 `json:"speed_acc"`
	TimeDelta     *float64 `json:"time_delta"`
	VerticalAcc   *float64 `json:"vertical_acc"`
	Status        string   `json:"status"`
	VehicleType   string   `json:"vehicle_type"`
	Time          string   `json:"time"`
	Timestamp     *float64 `json:"timestamp"`
	Altitude      *float64 `json:"altitude"`
	Side          *string  `json:"side"`
	DeltaTime     *float64 `json:"delta_time"`
	Distance      *float64 `json:"distance"`
}

func DecodeCanonicalEvent(input io.Reader) (CanonicalEvent, error) {
	payload, err := decodeRequestPayload(input)
	if err != nil {
		return CanonicalEvent{}, err
	}
	return canonicalize(payload)
}

func (event *CanonicalEvent) UnmarshalJSON(data []byte) error {
	payload, err := decodeRequestPayload(bytes.NewReader(data))
	if err != nil {
		return err
	}
	decoded, err := canonicalize(payload)
	if err != nil {
		return err
	}
	*event = decoded
	return nil
}

func decodeRequestPayload(input io.Reader) (requestPayload, error) {
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()

	var value requestPayload
	if err := decoder.Decode(&value); err != nil {
		return requestPayload{}, fmt.Errorf("decode JSON: %w", err)
	}
	if err := ensureEnd(decoder); err != nil {
		return requestPayload{}, err
	}
	return value, nil
}

func canonicalize(value requestPayload) (CanonicalEvent, error) {
	driverID := strings.TrimSpace(value.DriverID)
	if driverID == "" {
		return CanonicalEvent{}, errors.New("driver_id is required")
	}

	traceTimestamp := strings.TrimSpace(value.TTimestamp)
	if traceTimestamp == "" {
		return CanonicalEvent{}, errors.New("t_timestamp is required")
	}
	dateTime := strings.TrimSpace(value.Time)
	if dateTime == "" {
		return CanonicalEvent{}, errors.New("time is required")
	}

	status := strings.TrimSpace(value.Status)
	if status == "" {
		return CanonicalEvent{}, errors.New("status is required")
	}
	vehicleType := strings.TrimSpace(value.VehicleType)
	if vehicleType == "" {
		return CanonicalEvent{}, errors.New("vehicle_type is required")
	}

	if err := validateCoordinates(value.Lat, value.Lng); err != nil {
		return CanonicalEvent{}, err
	}
	if err := validateBearing(value.Bearing); err != nil {
		return CanonicalEvent{}, err
	}

	requiredNumbers := []struct {
		value *float64
		field string
	}{
		{value.BearingAcc, "bearing_acc"},
		{value.HorizontalAcc, "horizontal_acc"},
		{value.Speed, "speed"},
		{value.SpeedAcc, "speed_acc"},
		{value.TimeDelta, "time_delta"},
		{value.VerticalAcc, "vertical_acc"},
		{value.Timestamp, "timestamp"},
		{value.Altitude, "altitude"},
	}
	for _, item := range requiredNumbers {
		if err := requiredFinite(item.value, item.field); err != nil {
			return CanonicalEvent{}, err
		}
	}
	if *value.Timestamp <= 0 {
		return CanonicalEvent{}, errors.New("timestamp must be positive")
	}
	if *value.TimeDelta < 0 {
		return CanonicalEvent{}, errors.New("time_delta must not be negative")
	}

	optionalNumbers := []struct {
		value *float64
		field string
	}{
		{value.DeltaTime, "delta_time"},
		{value.Distance, "distance"},
	}
	for _, item := range optionalNumbers {
		if err := optionalFinite(item.value, item.field); err != nil {
			return CanonicalEvent{}, err
		}
	}
	if value.DeltaTime != nil && *value.DeltaTime < 0 {
		return CanonicalEvent{}, errors.New("delta_time must not be negative")
	}
	if value.Distance != nil && *value.Distance < 0 {
		return CanonicalEvent{}, errors.New("distance must not be negative")
	}

	if _, err := parseRecordedAt(dateTime, traceTimestamp); err != nil {
		return CanonicalEvent{}, err
	}

	return CanonicalEvent{
		DriverID:      driverID,
		TTimestamp:    traceTimestamp,
		Lat:           *value.Lat,
		Lng:           *value.Lng,
		Bearing:       *value.Bearing,
		BearingAcc:    *value.BearingAcc,
		HorizontalAcc: *value.HorizontalAcc,
		Speed:         *value.Speed,
		SpeedAcc:      *value.SpeedAcc,
		TimeDelta:     *value.TimeDelta,
		VerticalAcc:   *value.VerticalAcc,
		Status:        status,
		VehicleType:   vehicleType,
		Time:          dateTime,
		Timestamp:     *value.Timestamp,
		Altitude:      *value.Altitude,
		Side:          trimOptionalString(value.Side),
		DeltaTime:     value.DeltaTime,
		Distance:      value.Distance,
	}, nil
}

func validateCoordinates(latitude *float64, longitude *float64) error {
	if latitude == nil || !finite(*latitude) || *latitude < -90 || *latitude > 90 {
		return errors.New("lat must be a finite number from -90 to 90")
	}
	if longitude == nil || !finite(*longitude) || *longitude < -180 || *longitude > 180 {
		return errors.New("lng must be a finite number from -180 to 180")
	}
	return nil
}

func validateBearing(value *float64) error {
	if value == nil || !finite(*value) ||
		(*value != -1 && (*value < 0 || *value >= 360)) {
		return errors.New("bearing must be -1 or from 0 up to but not including 360")
	}
	return nil
}

// RecordedAt chuyển Time + TTimestamp thành thời điểm UTC chính xác của GPS.
func (event CanonicalEvent) RecordedAt() (time.Time, error) {
	return parseRecordedAt(event.Time, event.TTimestamp)
}

func parseRecordedAt(dateTime string, traceTimestamp string) (time.Time, error) {
	base, err := time.ParseInLocation(csvTimeLayout, dateTime, gpsTimeZone)
	if err != nil {
		return time.Time{}, fmt.Errorf("time %q is invalid: %w", dateTime, err)
	}

	parts := strings.Split(traceTimestamp, ":")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("t_timestamp %q must use MM:SS.s format", traceTimestamp)
	}
	minute, err := strconv.Atoi(parts[0])
	if err != nil || minute < 0 || minute >= 60 {
		return time.Time{}, fmt.Errorf("t_timestamp %q contains an invalid minute", traceTimestamp)
	}
	if minute != base.Minute() {
		return time.Time{}, fmt.Errorf(
			"t_timestamp minute %d does not match time minute %d",
			minute,
			base.Minute(),
		)
	}

	seconds, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || !finite(seconds) || seconds < 0 || seconds >= 60 {
		return time.Time{}, fmt.Errorf("t_timestamp %q contains invalid seconds", traceTimestamp)
	}

	return base.Add(time.Duration(math.Round(seconds*1000)) * time.Millisecond).UTC(), nil
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalFinite(value *float64, field string) error {
	if value != nil && !finite(*value) {
		return fmt.Errorf("%s must be a finite number", field)
	}
	return nil
}

func requiredFinite(value *float64, field string) error {
	if value == nil {
		return fmt.Errorf("%s is required", field)
	}
	if !finite(*value) {
		return fmt.Errorf("%s must be a finite number", field)
	}
	return nil
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
