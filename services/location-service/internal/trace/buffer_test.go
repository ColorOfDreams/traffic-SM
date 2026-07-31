package trace

import (
	"strings"
	"testing"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
)

func newTestBuffer(t *testing.T) *Buffer {
	t.Helper()

	buffer, err := NewBuffer(DefaultBufferConfig())
	if err != nil {
		t.Fatalf("NewBuffer() error = %v, want nil", err)
	}

	return buffer
}

func TestDefaultBufferConfig(t *testing.T) {
	config := DefaultBufferConfig()

	if config.MaxPoints != 10 {
		t.Errorf("MaxPoints = %d, want 10", config.MaxPoints)
	}

	if config.MaxDuration != 30*time.Second {
		t.Errorf("MaxDuration = %s, want 30s", config.MaxDuration)
	}

	if config.MinPoints != 2 {
		t.Errorf("MinPoints = %d, want 2", config.MinPoints)
	}

	if config.OverlapPoints != 2 {
		t.Errorf("OverlapPoints = %d, want 2", config.OverlapPoints)
	}
}

func TestBufferConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*BufferConfig)
		wantError string
	}{
		{
			name:      "default config",
			configure: func(*BufferConfig) {},
		},
		{
			name: "max points below two",
			configure: func(config *BufferConfig) {
				config.MaxPoints = 1
			},
			wantError: "max_points must be at least 2",
		},
		{
			name: "zero max duration",
			configure: func(config *BufferConfig) {
				config.MaxDuration = 0
			},
			wantError: "max_duration must be positive",
		},
		{
			name: "negative max duration",
			configure: func(config *BufferConfig) {
				config.MaxDuration = -time.Second
			},
			wantError: "max_duration must be positive",
		},
		{
			name: "min points below two",
			configure: func(config *BufferConfig) {
				config.MinPoints = 1
			},
			wantError: "min_points must be at least 2",
		},
		{
			name: "min points exceeds max points",
			configure: func(config *BufferConfig) {
				config.MinPoints = config.MaxPoints + 1
			},
			wantError: "min_points must not exceed max_points",
		},
		{
			name: "negative overlap",
			configure: func(config *BufferConfig) {
				config.OverlapPoints = -1
			},
			wantError: "overlap_points must not be negative",
		},
		{
			name: "overlap equals max points",
			configure: func(config *BufferConfig) {
				config.OverlapPoints = config.MaxPoints
			},
			wantError: "overlap_points must be less than max_points",
		},
		{
			name: "overlap exceeds max points",
			configure: func(config *BufferConfig) {
				config.OverlapPoints = config.MaxPoints + 1
			},
			wantError: "overlap_points must be less than max_points",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultBufferConfig()
			test.configure(&config)

			err := config.Validate()

			if test.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("Validate() error = nil, want %q", test.wantError)
			}

			if err.Error() != test.wantError {
				t.Fatalf(
					"Validate() error = %q, want %q",
					err,
					test.wantError,
				)
			}
		})
	}
}

func TestBufferAddRejectsEmptyDriverID(t *testing.T) {
	buffer := newTestBuffer(t)
	event := newCanonicalEvent("", time.Now())

	trace, err := buffer.Add(event)

	if err == nil {
		t.Fatal("Add() error = nil, want driver_id error")
	}

	if !strings.Contains(err.Error(), "driver_id is required") {
		t.Fatalf("Add() error = %q, want driver_id is required", err)
	}

	if trace != nil {
		t.Fatalf("Add() trace = %#v, want nil", trace)
	}

	if len(buffer.pointsByDriver) != 0 {
		t.Fatalf(
			"pointsByDriver contains %d drivers after rejected event, want 0",
			len(buffer.pointsByDriver),
		)
	}
}

func TestBufferAddRejectsNonIncreasingRecordedAt(t *testing.T) {
	baseTime := time.Date(2026, time.July, 31, 4, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		recordedAt time.Time
	}{
		{
			name:       "same time",
			recordedAt: baseTime,
		},
		{
			name:       "older time",
			recordedAt: baseTime.Add(-time.Second),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buffer := newTestBuffer(t)

			if _, err := buffer.Add(newCanonicalEvent("driver-1", baseTime)); err != nil {
				t.Fatalf("first Add() error = %v, want nil", err)
			}

			trace, err := buffer.Add(
				newCanonicalEvent("driver-1", test.recordedAt),
			)

			if err == nil {
				t.Fatal("second Add() error = nil, want recorded_at error")
			}

			if !strings.Contains(
				err.Error(),
				"recorded_at must be newer than the last point",
			) {
				t.Fatalf(
					"second Add() error = %q, want recorded_at ordering error",
					err,
				)
			}

			if trace != nil {
				t.Fatalf("second Add() trace = %#v, want nil", trace)
			}

			points := buffer.pointsByDriver["driver-1"]
			if len(points) != 1 {
				t.Fatalf(
					"driver buffer contains %d points after rejection, want 1",
					len(points),
				)
			}
		})
	}
}

func TestBufferAddDoesNotEmitBeforeThreshold(t *testing.T) {
	buffer := newTestBuffer(t)
	baseTime := time.Date(2026, time.July, 31, 4, 0, 0, 0, time.UTC)

	for index := range 9 {
		trace, err := buffer.Add(
			newCanonicalEvent(
				"driver-1",
				baseTime.Add(time.Duration(index)*time.Second),
			),
		)

		if err != nil {
			t.Fatalf("Add(point %d) error = %v, want nil", index+1, err)
		}

		if trace != nil {
			t.Fatalf(
				"Add(point %d) trace = %#v, want nil before threshold",
				index+1,
				trace,
			)
		}
	}

	if got := len(buffer.pointsByDriver["driver-1"]); got != 9 {
		t.Fatalf("driver buffer contains %d points, want 9", got)
	}
}

func TestBufferAddEmitsAtMaxPointsAndKeepsOverlap(t *testing.T) {
	buffer := newTestBuffer(t)
	baseTime := time.Date(2026, time.July, 31, 4, 0, 0, 0, time.UTC)

	firstTrace := addEvents(
		t,
		buffer,
		"driver-1",
		baseTime,
		0,
		10,
	)

	assertTrace(
		t,
		firstTrace,
		"driver-1",
		10,
		baseTime,
		baseTime.Add(9*time.Second),
	)
	assertBufferedTimes(
		t,
		buffer,
		"driver-1",
		baseTime.Add(8*time.Second),
		baseTime.Add(9*time.Second),
	)

	secondTrace := addEvents(
		t,
		buffer,
		"driver-1",
		baseTime,
		10,
		18,
	)

	assertTrace(
		t,
		secondTrace,
		"driver-1",
		10,
		baseTime.Add(8*time.Second),
		baseTime.Add(17*time.Second),
	)
	assertBufferedTimes(
		t,
		buffer,
		"driver-1",
		baseTime.Add(16*time.Second),
		baseTime.Add(17*time.Second),
	)
}

func TestBufferAddEmitsAtMaxDuration(t *testing.T) {
	buffer := newTestBuffer(t)
	baseTime := time.Date(2026, time.July, 31, 4, 0, 0, 0, time.UTC)

	firstTrace, err := buffer.Add(newCanonicalEvent("driver-1", baseTime))
	if err != nil {
		t.Fatalf("first Add() error = %v, want nil", err)
	}
	if firstTrace != nil {
		t.Fatalf("first Add() trace = %#v, want nil", firstTrace)
	}

	emittedTrace, err := buffer.Add(
		newCanonicalEvent("driver-1", baseTime.Add(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("second Add() error = %v, want nil", err)
	}

	assertTrace(
		t,
		emittedTrace,
		"driver-1",
		2,
		baseTime,
		baseTime.Add(30*time.Second),
	)
	assertBufferedTimes(
		t,
		buffer,
		"driver-1",
		baseTime,
		baseTime.Add(30*time.Second),
	)
}

func TestBufferAddDoesNotEmitBeforeMaxDuration(t *testing.T) {
	buffer := newTestBuffer(t)
	baseTime := time.Date(2026, time.July, 31, 4, 0, 0, 0, time.UTC)

	if _, err := buffer.Add(newCanonicalEvent("driver-1", baseTime)); err != nil {
		t.Fatalf("first Add() error = %v, want nil", err)
	}

	trace, err := buffer.Add(
		newCanonicalEvent(
			"driver-1",
			baseTime.Add(30*time.Second-time.Nanosecond),
		),
	)
	if err != nil {
		t.Fatalf("second Add() error = %v, want nil", err)
	}

	if trace != nil {
		t.Fatalf("second Add() trace = %#v, want nil before 30s", trace)
	}
}

func TestBufferKeepsDriversIndependent(t *testing.T) {
	buffer := newTestBuffer(t)
	baseTime := time.Date(2026, time.July, 31, 4, 0, 0, 0, time.UTC)

	for index := range 9 {
		recordedAt := baseTime.Add(time.Duration(index) * time.Second)

		for _, driverID := range []string{"driver-1", "driver-2"} {
			trace, err := buffer.Add(
				newCanonicalEvent(driverID, recordedAt),
			)
			if err != nil {
				t.Fatalf(
					"Add(%s point %d) error = %v, want nil",
					driverID,
					index+1,
					err,
				)
			}
			if trace != nil {
				t.Fatalf(
					"Add(%s point %d) emitted unexpectedly",
					driverID,
					index+1,
				)
			}
		}
	}

	trace, err := buffer.Add(
		newCanonicalEvent("driver-1", baseTime.Add(9*time.Second)),
	)
	if err != nil {
		t.Fatalf("Add(driver-1 point 10) error = %v, want nil", err)
	}

	assertTrace(
		t,
		trace,
		"driver-1",
		10,
		baseTime,
		baseTime.Add(9*time.Second),
	)

	if got := len(buffer.pointsByDriver["driver-1"]); got != 2 {
		t.Errorf("driver-1 buffer contains %d points, want 2", got)
	}
	if got := len(buffer.pointsByDriver["driver-2"]); got != 9 {
		t.Errorf("driver-2 buffer contains %d points, want 9", got)
	}
}

func TestEmittedTraceDoesNotSharePointsWithBuffer(t *testing.T) {
	buffer := newTestBuffer(t)
	baseTime := time.Date(2026, time.July, 31, 4, 0, 0, 0, time.UTC)

	trace := addEvents(
		t,
		buffer,
		"driver-1",
		baseTime,
		0,
		10,
	)

	trace.Points[8].Lat = 99
	trace.Points[9].Lat = 100

	bufferedPoints := buffer.pointsByDriver["driver-1"]
	if bufferedPoints[0].Lat == 99 {
		t.Error("first overlap point changed after mutating emitted trace")
	}
	if bufferedPoints[1].Lat == 100 {
		t.Error("second overlap point changed after mutating emitted trace")
	}
}

func addEvents(
	t *testing.T,
	buffer *Buffer,
	driverID string,
	baseTime time.Time,
	start int,
	end int,
) *Trace {
	t.Helper()

	var emittedTrace *Trace

	for index := start; index < end; index++ {
		trace, err := buffer.Add(
			newCanonicalEvent(
				driverID,
				baseTime.Add(time.Duration(index)*time.Second),
			),
		)
		if err != nil {
			t.Fatalf("Add(point %d) error = %v, want nil", index+1, err)
		}

		if index < end-1 && trace != nil {
			t.Fatalf("Add(point %d) emitted unexpectedly", index+1)
		}

		emittedTrace = trace
	}

	if emittedTrace == nil {
		t.Fatalf("Add(point %d) trace = nil, want emitted trace", end)
	}

	return emittedTrace
}

func assertTrace(
	t *testing.T,
	trace *Trace,
	driverID string,
	pointCount int,
	startedAt time.Time,
	endedAt time.Time,
) {
	t.Helper()

	if trace == nil {
		t.Fatal("trace = nil, want emitted trace")
	}

	if trace.DriverID != driverID {
		t.Errorf("DriverID = %q, want %q", trace.DriverID, driverID)
	}

	if len(trace.Points) != pointCount {
		t.Errorf("len(Points) = %d, want %d", len(trace.Points), pointCount)
	}

	if !trace.StartedAt.Equal(startedAt) {
		t.Errorf("StartedAt = %s, want %s", trace.StartedAt, startedAt)
	}

	if !trace.EndedAt.Equal(endedAt) {
		t.Errorf("EndedAt = %s, want %s", trace.EndedAt, endedAt)
	}
}

func assertBufferedTimes(
	t *testing.T,
	buffer *Buffer,
	driverID string,
	want ...time.Time,
) {
	t.Helper()

	points := buffer.pointsByDriver[driverID]

	if len(points) != len(want) {
		t.Fatalf(
			"%s buffer contains %d points, want %d",
			driverID,
			len(points),
			len(want),
		)
	}

	for index := range want {
		if !points[index].RecordedAt.Equal(want[index]) {
			t.Errorf(
				"%s point %d RecordedAt = %s, want %s",
				driverID,
				index,
				points[index].RecordedAt,
				want[index],
			)
		}
	}
}

func newCanonicalEvent(
	driverID string,
	recordedAt time.Time,
) gps.CanonicalEvent {
	return gps.CanonicalEvent{
		DriverID:   driverID,
		Lat:        float64(recordedAt.UnixNano()),
		RecordedAt: recordedAt,
	}
}

func TestNewBufferRejectsInvalidConfig(t *testing.T) {
	config := DefaultBufferConfig()
	config.OverlapPoints = config.MaxPoints

	buffer, err := NewBuffer(config)

	if err == nil {
		t.Fatal("NewBuffer() error = nil, want invalid config error")
	}

	if buffer != nil {
		t.Fatalf("NewBuffer() buffer = %#v, want nil", buffer)
	}

	if !strings.Contains(err.Error(), "invalid buffer config") {
		t.Fatalf(
			"NewBuffer() error = %q, want invalid buffer config",
			err,
		)
	}
}
