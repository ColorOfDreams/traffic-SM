// Kiểm tra và chuẩn hóa dữ liệu gps nhận vào
package gps

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const sourceTimeLayout = "1/2/2006 15:04"

var sourceTimeZone = time.FixedZone(
	"Asia/Ho_Chi_Minh",
	7*60*60,
)

type CanonicalEvent struct {
	DriverID      string    `json:"driver_id"`
	TTimestamp    string    `json:"t_timestamp"`    // Thời gian rút gọn trong bản ghi/chuyến
	Lat           float64   `json:"lat"`            // Vĩ độ
	Lng           float64   `json:"lng"`            // Kinh độ
	Bearing       float64   `json:"bearing"`        // Hướng 0 - 360 độ
	BearingAcc    float64   `json:"bearing_acc"`    // Sai số hương
	HorizontalAcc float64   `json:"horizontal_acc"` // Độ chính xác theo phương ngang
	Speed         float64   `json:"speed"`          // Tốc độ xe tại thời điểm nhận gps m/s
	SpeedAcc      float64   `json:"speed_acc"`      // Sai số tốc độ
	TimeDelta     float64   `json:"time_delta"`     // Thời gian dự kiến giữa 2 lần lấy gps
	VerticalAcc   float64   `json:"vertical_acc"`   // Độ chính xác theo phương thẳng đứng
	Status        string    `json:"status"`         // Trạng thái của xe : IN TRIP, ONLINE, OFFLINE
	VehicleType   string    `json:"vehicle_type"`   // Loại phương tịện
	Time          string    `json:"time"`           // Thòi gian theo ngày tháng năm
	Timestamp     float64   `json:"timestamp"`      // Thòi gian dạng UNIX số ??
	Altitude      float64   `json:"altitude"`       // Độ cao so với mực nước biển
	Side          *string   `json:"side"`           // ??
	DeltaTime     *float64  `json:"delta_time"`     // Thời gian từ gps trước đến gsps này
	Distance      *float64  `json:"distance"`       // ??
	RecordedAt    time.Time `json:"recorded_at"`
}

type RequestPayload struct {
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

func Canonicalize(input RequestPayload) (CanonicalEvent, error) {
	driverID := strings.TrimSpace(input.DriverID)
	if driverID == "" {
		return CanonicalEvent{}, errors.New("driver_id is required")
	}
	traceTimestamp := strings.TrimSpace(input.TTimestamp)
	if traceTimestamp == "" {
		return CanonicalEvent{}, errors.New("t_timestamp is required")
	}
	dateTime := strings.TrimSpace(input.Time)
	if dateTime == "" {
		return CanonicalEvent{}, errors.New("time is required")
	}
	status := strings.ToUpper(strings.TrimSpace(input.Status))
	switch status {
	case "IN TRIP", "ONLINE", "OFFLINE":
		// hợp lệ
	default:
		return CanonicalEvent{}, errors.New(
			`status must be "IN TRIP", "ONLINE", or "OFFLINE"`,
		)
	}
	vehicleType := strings.ToLower(strings.TrimSpace(input.VehicleType))

	switch vehicleType {
	case "car", "bike":
		// hợp lệ
	default:
		return CanonicalEvent{}, errors.New(
			`vehicle_type must be "car" or "bike"`,
		)
	}
	requiredNumbers := []struct {
		value *float64
		field string
	}{
		{input.Lat, "lat"},
		{input.Lng, "lng"},
		{input.Bearing, "bearing"},
		{input.BearingAcc, "bearing_acc"},
		{input.HorizontalAcc, "horizontal_acc"},
		{input.Speed, "speed"},
		{input.SpeedAcc, "speed_acc"},
		{input.TimeDelta, "time_delta"},
		{input.VerticalAcc, "vertical_acc"},
		{input.Timestamp, "timestamp"},
		{input.Altitude, "altitude"},
	}
	for _, number := range requiredNumbers {
		if err := requiredFinite(number.value, number.field); err != nil {
			return CanonicalEvent{}, err
		}
	}
	// Kiểm tra phạm vi tọa độ
	if *input.Lat < -90 || *input.Lat > 90 {
		return CanonicalEvent{}, errors.New(
			"lat must be from -90 to 90",
		)
	}

	if *input.Lng < -180 || *input.Lng > 180 {
		return CanonicalEvent{}, errors.New(
			"lng must be from -180 to 180",
		)
	}
	// Kiểm tra hướng -1 ko cung cấp hướng hoặc ko xác định được, [0, 360) hợp lệ
	bearing := *input.Bearing

	if bearing != -1 && (bearing < 0 || bearing >= 360) {
		return CanonicalEvent{}, errors.New(
			"bearing must be -1 or from 0 up to but not including 360",
		)
	}
	nonNegativeNumbers := []struct {
		value float64
		field string
	}{
		{*input.BearingAcc, "bearing_acc"},
		{*input.HorizontalAcc, "horizontal_acc"},
		{*input.Speed, "speed"},
		{*input.SpeedAcc, "speed_acc"},
		{*input.TimeDelta, "time_delta"},
		{*input.VerticalAcc, "vertical_acc"},
	}
	for _, number := range nonNegativeNumbers {
		if number.value < 0 {
			return CanonicalEvent{}, fmt.Errorf(
				"%s must not be negative",
				number.field,
			)
		}
	}
	if *input.Timestamp <= 0 {
		return CanonicalEvent{}, errors.New(
			"timestamp must be positive",
		)
	}

	optionalNumbers := []struct {
		value *float64
		field string
	}{
		{input.DeltaTime, "delta_time"},
		{input.Distance, "distance"},
	}
	for _, number := range optionalNumbers {
		if number.value == nil {
			continue
		}

		if math.IsNaN(*number.value) || math.IsInf(*number.value, 0) {
			return CanonicalEvent{}, fmt.Errorf(
				"%s must be a finite number",
				number.field,
			)
		}

		if *number.value < 0 {
			return CanonicalEvent{}, fmt.Errorf(
				"%s must not be negative",
				number.field,
			)
		}
	}

	var side *string

	if input.Side != nil {
		trimmedSide := strings.TrimSpace(*input.Side)

		if trimmedSide != "" {
			side = &trimmedSide
		}
	}

	recordedAt, err := parseRecordedAt(
		dateTime,
		traceTimestamp,
	)
	if err != nil {
		return CanonicalEvent{}, err
	}
	return CanonicalEvent{
		DriverID:      driverID,
		TTimestamp:    traceTimestamp,
		Lat:           *input.Lat,
		Lng:           *input.Lng,
		Bearing:       bearing,
		BearingAcc:    *input.BearingAcc,
		HorizontalAcc: *input.HorizontalAcc,
		Speed:         *input.Speed,
		SpeedAcc:      *input.SpeedAcc,
		TimeDelta:     *input.TimeDelta,
		VerticalAcc:   *input.VerticalAcc,
		Status:        status,
		VehicleType:   vehicleType,
		Time:          dateTime,
		Timestamp:     *input.Timestamp,
		Altitude:      *input.Altitude,
		Side:          side,
		DeltaTime:     input.DeltaTime,
		Distance:      input.Distance,
		RecordedAt:    recordedAt}, nil
}

//helper

// Kiểm tra vô hạn của số
func requiredFinite(value *float64, field string) error {
	if value == nil {
		return fmt.Errorf("%s is required", field)
	}

	if math.IsNaN(*value) || math.IsInf(*value, 0) {
		return fmt.Errorf("%s must be a finite number", field)
	}

	return nil
}

// Recoreded At
func parseRecordedAt(
	dateTime string,
	traceTimestamp string,
) (time.Time, error) {
	base, err := time.ParseInLocation(
		sourceTimeLayout,
		dateTime,
		sourceTimeZone,
	)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"time %q is invalid: %w",
			dateTime,
			err,
		)
	}

	parts := strings.Split(traceTimestamp, ":")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf(
			"t_timestamp %q must use MM:SS.s format",
			traceTimestamp,
		)
	}

	minute, err := strconv.Atoi(parts[0])
	if err != nil || minute < 0 || minute >= 60 {
		return time.Time{}, fmt.Errorf(
			"t_timestamp %q contains an invalid minute",
			traceTimestamp,
		)
	}

	if minute != base.Minute() {
		return time.Time{}, fmt.Errorf(
			"t_timestamp minute %d does not match time minute %d",
			minute,
			base.Minute(),
		)
	}

	seconds, err := strconv.ParseFloat(parts[1], 64)
	if err != nil ||
		math.IsNaN(seconds) ||
		math.IsInf(seconds, 0) ||
		seconds < 0 ||
		seconds >= 60 {
		return time.Time{}, fmt.Errorf(
			"t_timestamp %q contains invalid seconds",
			traceTimestamp,
		)
	}

	milliseconds := math.Round(seconds * 1000)

	return base.
		Add(time.Duration(milliseconds) * time.Millisecond).
		UTC(), nil
}
