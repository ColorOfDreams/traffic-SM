package trace

// Tạo buffer, buffer gps tài xế theo id, tạo trace cho chuỗi gps của tài xế
import (
	"fmt"
	"sync"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
)

// config quy tắc đóng trace
type BufferConfig struct {
	MaxPoints     int
	MaxDuration   time.Duration
	MinPoints     int
	OverlapPoints int
}

func (c BufferConfig) Validate() error {
	if c.MaxPoints < 2 {
		return fmt.Errorf("max_points must be at least 2")
	}

	if c.MaxDuration <= 0 {
		return fmt.Errorf("max_duration must be positive")
	}

	if c.MinPoints < 2 {
		return fmt.Errorf("min_points must be at least 2")
	}

	if c.MinPoints > c.MaxPoints {
		return fmt.Errorf("min_points must not exceed max_points")
	}

	if c.OverlapPoints < 0 {
		return fmt.Errorf("overlap_points must not be negative")
	}

	if c.OverlapPoints >= c.MaxPoints {
		return fmt.Errorf("overlap_points must be less than max_points")
	}

	return nil
}

type Buffer struct {
	mu             sync.Mutex
	config         BufferConfig
	pointsByDriver map[string][]gps.CanonicalEvent
}

func DefaultBufferConfig() BufferConfig {
	return BufferConfig{
		MaxPoints:     10,               // Số point max của 1 buffer
		MaxDuration:   30 * time.Second, // Diều kiện thời gian cách nhau để  nhận 1 gps
		MinPoints:     2,                // Số point nhỏ nhất của buffer
		OverlapPoints: 2,                // giữ lại 2 point cuỗi
	}
}

func NewBuffer(config BufferConfig) (*Buffer, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid buffer config: %w", err)
	}

	return &Buffer{
		config:         config,
		pointsByDriver: make(map[string][]gps.CanonicalEvent),
	}, nil
}

func (b *Buffer) Add(event gps.CanonicalEvent) (*Trace, error) {
	b.mu.Lock()         // Khóa lại buffer chỉ cho 1 goroutine sử dụng
	defer b.mu.Unlock() // UNlock khi hàm kết thúc

	if event.DriverID == "" {
		return nil, fmt.Errorf("driver_id is required")
	}

	points := b.pointsByDriver[event.DriverID]
	if len(points) > 0 {
		lastPoint := points[len(points)-1]

		if !event.RecordedAt.After(lastPoint.RecordedAt) {
			return nil, fmt.Errorf("recorded_at must be newer than the last point")
		}
	}

	//Thêm gps vào buffer
	points = append(points, event)
	b.pointsByDriver[event.DriverID] = points

	// Check xem đủ point chưa
	if !b.shouldEmit(points) {
		return nil, nil
	}
	// Tạo trace
	trace := b.buildTrace(event.DriverID, points)

	// Xác định point cần giữ lại
	overlapStart := len(points) - b.config.OverlapPoints
	overlap := make([]gps.CanonicalEvent, b.config.OverlapPoints)
	copy(overlap, points[overlapStart:])

	b.pointsByDriver[event.DriverID] = overlap

	return &trace, nil
}

func (b *Buffer) shouldEmit(points []gps.CanonicalEvent) bool {
	// Check đóng mở Trace
	if len(points) < b.config.MinPoints {
		return false
	}

	if len(points) >= b.config.MaxPoints {
		return true
	}

	startedAt := points[0].RecordedAt
	endedAt := points[len(points)-1].RecordedAt

	return endedAt.Sub(startedAt) >= b.config.MaxDuration
}

func (b *Buffer) buildTrace(driverID string, points []gps.CanonicalEvent) Trace {
	// Tạo Trace
	tracePoints := make([]gps.CanonicalEvent, len(points))
	copy(tracePoints, points)

	return Trace{
		DriverID:  driverID,
		Points:    tracePoints,
		StartedAt: tracePoints[0].RecordedAt,
		EndedAt:   tracePoints[len(tracePoints)-1].RecordedAt,
	}
}
