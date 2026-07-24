package gps

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// schema
// trace struct
type Trace struct {
	TraceID   string           // Trace id của mỗi thằng trace
	VehicleID string           // id xe
	StartedAt time.Time        // thời gian ghi nhận gps đầu tiên
	EndedAt   time.Time        // Thời gian ghi nhận gps cuối cùng
	Points    []CanonicalEvent // Danh sách GPS của trace
}

// Trace config
type TraceConfig struct {
	Window            time.Duration // Độ dài của một trace
	MinimumPoints     int           // Số gps tối thiểu để gửi matching
	InactivityTimeout time.Duration // Khoảng thời gian server không nhận GPS mới từ xe trước khi đóng trace
	OverlapPoints     int           // Số điểm trace cuối trước khi giữ lại cho trace tiếp theo
}

// Trace builder : buffer nguồn FPS được chia theo xe và sắp xếp, config quy tắc xác định khi nào tạo trace
type TraceBuilder struct {
	buffer *BufferManager
	config TraceConfig
}

func NewTraceBuilder(buffer *BufferManager, config TraceConfig) (*TraceBuilder, error) {
	// Check tồn tại và giá trị
	if buffer == nil {
		return nil, errors.New("buffer is required")
	}
	if config.Window <= 0 {
		return nil, errors.New("trace window must be greater than zero")
	}
	if config.MinimumPoints < 2 {
		return nil, errors.New("minimum points must be at least two")
	}
	if config.InactivityTimeout <= 0 {
		return nil, errors.New("inactivity timeout must be greater than zero")
	}
	if config.OverlapPoints < 0 {
		return nil, errors.New("overlap points must not be negative")
	}
	if config.OverlapPoints >= config.MinimumPoints {
		return nil, errors.New("overlap points must be less than minimum points")
	}

	return &TraceBuilder{
		buffer: buffer,
		config: config,
	}, nil
}

func (builder *TraceBuilder) CollectReady(now time.Time) []Trace {
	now = now.UTC()

	builder.buffer.mu.Lock()
	defer builder.buffer.mu.Unlock()

	vehicleIDs := make([]string, 0, len(builder.buffer.vehicles))
	for vehicleID := range builder.buffer.vehicles {
		vehicleIDs = append(vehicleIDs, vehicleID)
	}
	sort.Strings(vehicleIDs)

	var traces []Trace
	for _, vehicleID := range vehicleIDs {
		vehicle := builder.buffer.vehicles[vehicleID]

		for {
			endIndex := builder.windowEndIndex(vehicle.events)
			if endIndex < 0 {
				break
			}

			traces = append(traces, buildTrace(vehicleID, vehicle.events[:endIndex+1]))
			keepFrom := endIndex + 1 - builder.config.OverlapPoints
			vehicle.events = append([]bufferedEvent(nil), vehicle.events[keepFrom:]...)
		}

		inactive := !vehicle.lastReceivedAt.IsZero() &&
			now.Sub(vehicle.lastReceivedAt) >= builder.config.InactivityTimeout
		if !inactive {
			continue
		}

		if len(vehicle.events) >= builder.config.MinimumPoints {
			traces = append(traces, buildTrace(vehicleID, vehicle.events))
		}
		delete(builder.buffer.vehicles, vehicleID)
	}

	return traces
}

func (builder *TraceBuilder) windowEndIndex(events []bufferedEvent) int {
	if len(events) < builder.config.MinimumPoints {
		return -1
	}

	windowEndsAt := events[0].recordedAt.Add(builder.config.Window)
	endIndex := sort.Search(len(events), func(index int) bool {
		return !events[index].recordedAt.Before(windowEndsAt)
	})
	if endIndex == len(events) {
		return -1
	}
	if endIndex < builder.config.MinimumPoints-1 {
		return builder.config.MinimumPoints - 1
	}
	return endIndex
}

func buildTrace(vehicleID string, events []bufferedEvent) Trace {
	points := make([]CanonicalEvent, len(events))
	for index := range events {
		points[index] = cloneCanonicalEvent(events[index].event)
	}

	first := events[0]
	last := events[len(events)-1]
	return Trace{
		TraceID:   fmt.Sprintf("%s:%s:%s", vehicleID, first.event.EventID, last.event.EventID),
		VehicleID: vehicleID,
		StartedAt: first.recordedAt,
		EndedAt:   last.recordedAt,
		Points:    points,
	}
}
