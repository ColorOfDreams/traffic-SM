package gps

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestCanonicalizeValidInput(t *testing.T) {
	input := validRequestPayload()

	event, err := Canonicalize(input)
	if err != nil {
		t.Fatalf("Canonicalize returned error: %v", err)
	}

	if event.DriverID != "driver-001" {
		t.Errorf("DriverID = %q, want %q", event.DriverID, "driver-001")
	}
	if event.Status != "IN TRIP" {
		t.Errorf("Status = %q, want %q", event.Status, "IN TRIP")
	}
	if event.VehicleType != "bike" {
		t.Errorf("VehicleType = %q, want %q", event.VehicleType, "bike")
	}
	if event.Side == nil {
		t.Fatal("Side is nil, want left")
	}
	if *event.Side != "left" {
		t.Errorf("Side = %q, want %q", *event.Side, "left")
	}
	if event.Lat != 10.7783785 {
		t.Errorf("Lat = %v, want %v", event.Lat, 10.7783785)
	}
	if event.Lng != 106.70171 {
		t.Errorf("Lng = %v, want %v", event.Lng, 106.70171)
	}
	if event.Speed != 9 {
		t.Errorf("Speed = %v, want %v", event.Speed, 9.0)
	}

	expectedTime := time.Date(
		2026,
		time.July,
		23,
		22,
		0,
		4,
		400_000_000,
		time.UTC,
	)
	if !event.RecordedAt.Equal(expectedTime) {
		t.Errorf(
			"RecordedAt = %s, want %s",
			event.RecordedAt,
			expectedTime,
		)
	}
}

func TestCanonicalizeRejectsInvalidStrings(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(*RequestPayload)
		wantError string
	}{
		{
			name: "missing driver ID",
			modify: func(input *RequestPayload) {
				input.DriverID = " "
			},
			wantError: "driver_id is required",
		},
		{
			name: "missing trace timestamp",
			modify: func(input *RequestPayload) {
				input.TTimestamp = ""
			},
			wantError: "t_timestamp is required",
		},
		{
			name: "missing time",
			modify: func(input *RequestPayload) {
				input.Time = ""
			},
			wantError: "time is required",
		},
		{
			name: "unsupported status",
			modify: func(input *RequestPayload) {
				input.Status = "BUSY"
			},
			wantError: `status must be "IN TRIP", "ONLINE", or "OFFLINE"`,
		},
		{
			name: "unsupported vehicle type",
			modify: func(input *RequestPayload) {
				input.VehicleType = "truck"
			},
			wantError: `vehicle_type must be "car" or "bike"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validRequestPayload()
			test.modify(&input)

			_, err := Canonicalize(input)
			if err == nil {
				t.Fatalf(
					"Canonicalize returned nil error, want %q",
					test.wantError,
				)
			}
			if err.Error() != test.wantError {
				t.Errorf("error = %q, want %q", err.Error(), test.wantError)
			}
		})
	}
}

func TestCanonicalizeRejectsMissingRequiredNumbers(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(*RequestPayload)
		wantError string
	}{
		{
			name: "missing latitude",
			modify: func(input *RequestPayload) {
				input.Lat = nil
			},
			wantError: "lat is required",
		},
		{
			name: "missing longitude",
			modify: func(input *RequestPayload) {
				input.Lng = nil
			},
			wantError: "lng is required",
		},
		{
			name: "missing bearing",
			modify: func(input *RequestPayload) {
				input.Bearing = nil
			},
			wantError: "bearing is required",
		},
		{
			name: "missing bearing accuracy",
			modify: func(input *RequestPayload) {
				input.BearingAcc = nil
			},
			wantError: "bearing_acc is required",
		},
		{
			name: "missing horizontal accuracy",
			modify: func(input *RequestPayload) {
				input.HorizontalAcc = nil
			},
			wantError: "horizontal_acc is required",
		},
		{
			name: "missing speed",
			modify: func(input *RequestPayload) {
				input.Speed = nil
			},
			wantError: "speed is required",
		},
		{
			name: "missing speed accuracy",
			modify: func(input *RequestPayload) {
				input.SpeedAcc = nil
			},
			wantError: "speed_acc is required",
		},
		{
			name: "missing time delta",
			modify: func(input *RequestPayload) {
				input.TimeDelta = nil
			},
			wantError: "time_delta is required",
		},
		{
			name: "missing vertical accuracy",
			modify: func(input *RequestPayload) {
				input.VerticalAcc = nil
			},
			wantError: "vertical_acc is required",
		},
		{
			name: "missing timestamp",
			modify: func(input *RequestPayload) {
				input.Timestamp = nil
			},
			wantError: "timestamp is required",
		},
		{
			name: "missing altitude",
			modify: func(input *RequestPayload) {
				input.Altitude = nil
			},
			wantError: "altitude is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validRequestPayload()
			test.modify(&input)
			assertCanonicalizeError(t, input, test.wantError)
		})
	}
}

func TestCanonicalizeRejectsNonFiniteNumbers(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(*RequestPayload)
		wantError string
	}{
		{
			name: "NaN latitude",
			modify: func(input *RequestPayload) {
				input.Lat = floatPointer(math.NaN())
			},
			wantError: "lat must be a finite number",
		},
		{
			name: "infinite speed",
			modify: func(input *RequestPayload) {
				input.Speed = floatPointer(math.Inf(1))
			},
			wantError: "speed must be a finite number",
		},
		{
			name: "NaN delta time",
			modify: func(input *RequestPayload) {
				input.DeltaTime = floatPointer(math.NaN())
			},
			wantError: "delta_time must be a finite number",
		},
		{
			name: "infinite distance",
			modify: func(input *RequestPayload) {
				input.Distance = floatPointer(math.Inf(-1))
			},
			wantError: "distance must be a finite number",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validRequestPayload()
			test.modify(&input)
			assertCanonicalizeError(t, input, test.wantError)
		})
	}
}

func TestCanonicalizeRejectsOutOfRangeNumbers(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(*RequestPayload)
		wantError string
	}{
		{
			name: "latitude below minimum",
			modify: func(input *RequestPayload) {
				input.Lat = floatPointer(-90.1)
			},
			wantError: "lat must be from -90 to 90",
		},
		{
			name: "latitude above maximum",
			modify: func(input *RequestPayload) {
				input.Lat = floatPointer(90.1)
			},
			wantError: "lat must be from -90 to 90",
		},
		{
			name: "longitude below minimum",
			modify: func(input *RequestPayload) {
				input.Lng = floatPointer(-180.1)
			},
			wantError: "lng must be from -180 to 180",
		},
		{
			name: "longitude above maximum",
			modify: func(input *RequestPayload) {
				input.Lng = floatPointer(180.1)
			},
			wantError: "lng must be from -180 to 180",
		},
		{
			name: "bearing below sentinel",
			modify: func(input *RequestPayload) {
				input.Bearing = floatPointer(-2)
			},
			wantError: "bearing must be -1 or from 0 up to but not including 360",
		},
		{
			name: "bearing reaches 360",
			modify: func(input *RequestPayload) {
				input.Bearing = floatPointer(360)
			},
			wantError: "bearing must be -1 or from 0 up to but not including 360",
		},
		{
			name: "negative bearing accuracy",
			modify: func(input *RequestPayload) {
				input.BearingAcc = floatPointer(-1)
			},
			wantError: "bearing_acc must not be negative",
		},
		{
			name: "negative horizontal accuracy",
			modify: func(input *RequestPayload) {
				input.HorizontalAcc = floatPointer(-1)
			},
			wantError: "horizontal_acc must not be negative",
		},
		{
			name: "negative speed",
			modify: func(input *RequestPayload) {
				input.Speed = floatPointer(-1)
			},
			wantError: "speed must not be negative",
		},
		{
			name: "negative speed accuracy",
			modify: func(input *RequestPayload) {
				input.SpeedAcc = floatPointer(-1)
			},
			wantError: "speed_acc must not be negative",
		},
		{
			name: "negative time delta",
			modify: func(input *RequestPayload) {
				input.TimeDelta = floatPointer(-1)
			},
			wantError: "time_delta must not be negative",
		},
		{
			name: "negative vertical accuracy",
			modify: func(input *RequestPayload) {
				input.VerticalAcc = floatPointer(-1)
			},
			wantError: "vertical_acc must not be negative",
		},
		{
			name: "zero timestamp",
			modify: func(input *RequestPayload) {
				input.Timestamp = floatPointer(0)
			},
			wantError: "timestamp must be positive",
		},
		{
			name: "negative delta time",
			modify: func(input *RequestPayload) {
				input.DeltaTime = floatPointer(-1)
			},
			wantError: "delta_time must not be negative",
		},
		{
			name: "negative distance",
			modify: func(input *RequestPayload) {
				input.Distance = floatPointer(-1)
			},
			wantError: "distance must not be negative",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validRequestPayload()
			test.modify(&input)
			assertCanonicalizeError(t, input, test.wantError)
		})
	}
}

func TestCanonicalizeRejectsInvalidRecordedAt(t *testing.T) {
	tests := []struct {
		name          string
		modify        func(*RequestPayload)
		wantSubstring string
	}{
		{
			name: "invalid date time",
			modify: func(input *RequestPayload) {
				input.Time = "not-a-time"
			},
			wantSubstring: `time "not-a-time" is invalid`,
		},
		{
			name: "invalid trace timestamp format",
			modify: func(input *RequestPayload) {
				input.TTimestamp = "bad"
			},
			wantSubstring: `must use MM:SS.s format`,
		},
		{
			name: "trace minute does not match time",
			modify: func(input *RequestPayload) {
				input.TTimestamp = "01:04.4"
			},
			wantSubstring: "does not match time minute",
		},
		{
			name: "negative trace seconds",
			modify: func(input *RequestPayload) {
				input.TTimestamp = "00:-1"
			},
			wantSubstring: "contains invalid seconds",
		},
		{
			name: "trace seconds reaches 60",
			modify: func(input *RequestPayload) {
				input.TTimestamp = "00:60"
			},
			wantSubstring: "contains invalid seconds",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validRequestPayload()
			test.modify(&input)

			_, err := Canonicalize(input)
			if err == nil {
				t.Fatalf(
					"Canonicalize returned nil error containing %q",
					test.wantSubstring,
				)
			}
			if !strings.Contains(err.Error(), test.wantSubstring) {
				t.Errorf(
					"error = %q, want substring %q",
					err.Error(),
					test.wantSubstring,
				)
			}
		})
	}
}

func TestCanonicalizeAcceptsBoundaryAndOptionalValues(t *testing.T) {
	input := validRequestPayload()
	input.Bearing = floatPointer(-1)
	input.Speed = floatPointer(0)
	input.Altitude = floatPointer(-10)
	input.Side = stringPointer(" ")
	input.DeltaTime = nil
	input.Distance = nil

	event, err := Canonicalize(input)
	if err != nil {
		t.Fatalf("Canonicalize returned error: %v", err)
	}
	if event.Bearing != -1 {
		t.Errorf("Bearing = %v, want -1", event.Bearing)
	}
	if event.Speed != 0 {
		t.Errorf("Speed = %v, want 0", event.Speed)
	}
	if event.Altitude != -10 {
		t.Errorf("Altitude = %v, want -10", event.Altitude)
	}
	if event.Side != nil {
		t.Errorf("Side = %q, want nil", *event.Side)
	}
	if event.DeltaTime != nil {
		t.Errorf("DeltaTime = %v, want nil", *event.DeltaTime)
	}
	if event.Distance != nil {
		t.Errorf("Distance = %v, want nil", *event.Distance)
	}
}

func assertCanonicalizeError(
	t *testing.T,
	input RequestPayload,
	wantError string,
) {
	t.Helper()

	_, err := Canonicalize(input)
	if err == nil {
		t.Fatalf("Canonicalize returned nil error, want %q", wantError)
	}
	if err.Error() != wantError {
		t.Errorf("error = %q, want %q", err.Error(), wantError)
	}
}

func validRequestPayload() RequestPayload {
	return RequestPayload{
		DriverID:      " driver-001 ",
		TTimestamp:    "00:04.4",
		Lat:           floatPointer(10.7783785),
		Lng:           floatPointer(106.70171),
		Bearing:       floatPointer(90),
		BearingAcc:    floatPointer(0),
		HorizontalAcc: floatPointer(3),
		Speed:         floatPointer(9),
		SpeedAcc:      floatPointer(1.5),
		TimeDelta:     floatPointer(5),
		VerticalAcc:   floatPointer(1),
		Status:        " in trip ",
		VehicleType:   " Bike ",
		Time:          "7/24/2026 5:00",
		Timestamp:     floatPointer(1784872804400),
		Altitude:      floatPointer(58.7),
		Side:          stringPointer(" left "),
		DeltaTime:     floatPointer(5.004),
		Distance:      floatPointer(45),
	}
}

func floatPointer(value float64) *float64 {
	return &value
}

func stringPointer(value string) *string {
	return &value
}
