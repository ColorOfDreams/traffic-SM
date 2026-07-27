package gps

import (
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	ErrDriverBufferFull  = errors.New("driver GPS buffer is full")
	ErrMatchResultStale  = errors.New("match result does not belong to the in-flight trace")
	ErrReceivedAtMissing = errors.New("received_at is required")
)

type Trace struct {
	TraceID   string           `json:"trace_id"`
	DriverID  string           `json:"driver_id"`
	StartedAt time.Time        `json:"started_at"`
	EndedAt   time.Time        `json:"ended_at"`
	Points    []CanonicalEvent `json:"points"`
}

type ConsumerStateConfig struct {
	TracePoints        int
	OverlapPoints      int
	MaxPointGap        time.Duration
	InactivityTimeout  time.Duration
	MaxBufferedPoints  int
	MaxMatchAttempts   int
	MinDisplacementM   float64
	MaxImpliedSpeedMPS float64
}

type inFlightMatch struct {
	trace    Trace
	attempts int
}

type DriverState struct {
	// Points chỉ chứa overlap và các GPS đang chờ trace tiếp theo.
	// Các điểm GraphHopper đang xử lý nằm riêng trong inFlight.
	Points           []CanonicalEvent
	LastEventID      string
	LastRecordedAtMS int64
	LastLongitude    float64
	LastLatitude     float64
	LastSeenAt       time.Time
	inFlight         *inFlightMatch
}

type ConsumerState struct {
	config  ConsumerStateConfig
	drivers map[string]*DriverState
}

// MatchCompletion mô tả công việc tiếp theo sau khi GraphHopper trả kết quả.
// RetryTrace và NextTrace loại trừ lẫn nhau. FailedTrace cần được ghi log hoặc
// đưa vào dead-letter topic khi đã dùng hết số lần thử.
type MatchCompletion struct {
	RetryTrace  *Trace
	NextTrace   *Trace
	FailedTrace *Trace
}

func NewConsumerState(config ConsumerStateConfig) (*ConsumerState, error) {
	if config.TracePoints < 2 {
		return nil, errors.New("trace points must be at least two")
	}
	if config.OverlapPoints < 0 || config.OverlapPoints >= config.TracePoints {
		return nil, errors.New("overlap points must be smaller than trace points")
	}
	if config.MaxPointGap < 0 {
		return nil, errors.New("max point gap must not be negative")
	}
	if config.InactivityTimeout <= 0 {
		return nil, errors.New("inactivity timeout must be positive")
	}
	if math.IsNaN(config.MinDisplacementM) ||
		math.IsInf(config.MinDisplacementM, 0) ||
		config.MinDisplacementM < 0 {
		return nil, errors.New("minimum displacement must be a finite non-negative number")
	}
	if math.IsNaN(config.MaxImpliedSpeedMPS) ||
		math.IsInf(config.MaxImpliedSpeedMPS, 0) ||
		config.MaxImpliedSpeedMPS < 0 {
		return nil, errors.New("maximum implied speed must be a finite non-negative number")
	}

	if config.MaxBufferedPoints == 0 {
		config.MaxBufferedPoints = config.TracePoints * 4
	}
	if config.MaxBufferedPoints < config.TracePoints {
		return nil, errors.New("max buffered points must be at least trace points")
	}

	if config.MaxMatchAttempts == 0 {
		config.MaxMatchAttempts = 3
	}
	if config.MaxMatchAttempts < 1 {
		return nil, errors.New("max match attempts must be at least one")
	}

	return &ConsumerState{
		config:  config,
		drivers: make(map[string]*DriverState),
	}, nil
}

// Add phải được gọi bởi goroutine sở hữu Kafka partition. CompleteMatch và
// EvictInactive của cùng ConsumerState cũng phải chạy trên chính goroutine đó.
func (state *ConsumerState) Add(event CanonicalEvent, receivedAt time.Time) (*Trace, error) {
	if receivedAt.IsZero() {
		return nil, ErrReceivedAtMissing
	}
	receivedAt = receivedAt.UTC()

	driver := state.drivers[event.DriverID]
	if driver == nil {
		driver = &DriverState{
			Points: make([]CanonicalEvent, 0, state.config.TracePoints),
		}
		state.drivers[event.DriverID] = driver
	}

	// Kafka at-least-once thường phát lại message ngay sát message gốc.
	if event.EventID == driver.LastEventID {
		return nil, nil
	}

	if driver.LastRecordedAtMS > 0 {
		// driver_id là Kafka key nên event hợp lệ phải đến đúng thứ tự.
		if event.RecordedAtMS <= driver.LastRecordedAtMS {
			return nil, nil
		}

		elapsedMS := event.RecordedAtMS - driver.LastRecordedAtMS
		gap := time.Duration(elapsedMS) * time.Millisecond
		if state.config.MaxPointGap > 0 && gap > state.config.MaxPointGap {
			// Chỉ bỏ overlap/pending của hành trình cũ. Trace đang chạy được giữ
			// riêng trong inFlight nên kết quả cũ không thể xóa GPS hành trình mới.
			driver.Points = driver.Points[:0]
		} else if state.config.MaxImpliedSpeedMPS > 0 {
			distanceM := haversineMeters(
				driver.LastLatitude,
				driver.LastLongitude,
				event.Latitude,
				event.Longitude,
			)
			impliedSpeedMPS := distanceM / (float64(elapsedMS) / 1000)
			if impliedSpeedMPS > state.config.MaxImpliedSpeedMPS {
				// Thiết bị vừa teleport hoặc đổi khu vực hoạt động. Không đưa hai
				// hành trình cách xa nhau vào cùng một yêu cầu GraphHopper.
				driver.Points = driver.Points[:0]
			}
		}
	}

	if len(driver.Points) >= state.config.MaxBufferedPoints {
		// Consumer phải pause partition hoặc ngừng poll/commit để tạo backpressure.
		return nil, ErrDriverBufferFull
	}

	driver.Points = append(driver.Points, event)
	driver.LastEventID = event.EventID
	driver.LastRecordedAtMS = event.RecordedAtMS
	driver.LastLongitude = event.Longitude
	driver.LastLatitude = event.Latitude
	driver.LastSeenAt = receivedAt

	trace := state.startTrace(event.DriverID, driver)
	if trace == nil &&
		driver.inFlight == nil &&
		len(driver.Points) >= state.config.TracePoints {
		// Xe đứng yên: dùng cửa sổ trượt thay vì tích lũy vô hạn hoặc gửi một
		// trace không có chuyển động cho GraphHopper.
		keep := state.config.TracePoints - 1
		driver.Points = append([]CanonicalEvent(nil), driver.Points[len(driver.Points)-keep:]...)
	}
	return trace, nil
}

// CompleteMatch chỉ thay đổi state khi driver_id và trace_id cùng khớp với
// trace đang chạy. Nhờ vậy kết quả trùng hoặc kết quả cũ không xóa nhầm GPS.
func (state *ConsumerState) CompleteMatch(
	driverID string,
	traceID string,
	success bool,
) (MatchCompletion, error) {
	driver := state.drivers[driverID]
	if driver == nil || driver.inFlight == nil || driver.inFlight.trace.TraceID != traceID {
		return MatchCompletion{}, ErrMatchResultStale
	}

	if !success {
		if driver.inFlight.attempts < state.config.MaxMatchAttempts {
			driver.inFlight.attempts++
			retry := cloneTrace(driver.inFlight.trace)
			return MatchCompletion{RetryTrace: &retry}, nil
		}

		failed := cloneTrace(driver.inFlight.trace)
		driver.inFlight = nil
		state.dropFailedOverlap(driver, failed)

		return MatchCompletion{
			FailedTrace: &failed,
			NextTrace:   state.startTrace(driverID, driver),
		}, nil
	}

	driver.inFlight = nil
	return MatchCompletion{
		NextTrace: state.startTrace(driverID, driver),
	}, nil
}

// EvictInactive chỉ dọn driver không có match đang chạy. Timeout GraphHopper
// phải được worker chuyển thành CompleteMatch(..., false).
func (state *ConsumerState) EvictInactive(now time.Time) int {
	now = now.UTC()
	removed := 0

	for driverID, driver := range state.drivers {
		if driver.inFlight != nil {
			continue
		}

		if !driver.LastSeenAt.IsZero() &&
			now.Sub(driver.LastSeenAt) >= state.config.InactivityTimeout {
			delete(state.drivers, driverID)
			removed++
		}
	}

	return removed
}

func (state *ConsumerState) startTrace(driverID string, driver *DriverState) *Trace {
	if driver.inFlight != nil || len(driver.Points) < state.config.TracePoints {
		return nil
	}

	candidate := driver.Points[:state.config.TracePoints]
	if !hasMinimumDisplacement(candidate, state.config.MinDisplacementM) {
		return nil
	}

	points := append(
		[]CanonicalEvent(nil),
		candidate...,
	)
	trace := Trace{
		TraceID: fmt.Sprintf(
			"%s:%s:%s",
			driverID,
			points[0].EventID,
			points[len(points)-1].EventID,
		),
		DriverID:  driverID,
		StartedAt: time.UnixMilli(points[0].RecordedAtMS).UTC(),
		EndedAt:   time.UnixMilli(points[len(points)-1].RecordedAtMS).UTC(),
		Points:    points,
	}

	// Di chuyển trace sang inFlight ngay khi tạo. Points chỉ giữ overlap và
	// những GPS đến sau trace này, nên completion không cần xóa theo số lượng.
	consumed := state.config.TracePoints - state.config.OverlapPoints
	driver.Points = append([]CanonicalEvent(nil), driver.Points[consumed:]...)
	driver.inFlight = &inFlightMatch{
		trace:    trace,
		attempts: 1,
	}

	result := cloneTrace(trace)
	return &result
}

func (state *ConsumerState) dropFailedOverlap(driver *DriverState, failed Trace) {
	overlap := state.config.OverlapPoints
	if overlap == 0 || len(driver.Points) < overlap || len(failed.Points) < overlap {
		return
	}

	failedSuffix := failed.Points[len(failed.Points)-overlap:]
	for index := 0; index < overlap; index++ {
		if driver.Points[index].EventID != failedSuffix[index].EventID {
			return
		}
	}
	driver.Points = append([]CanonicalEvent(nil), driver.Points[overlap:]...)
}

func cloneTrace(trace Trace) Trace {
	trace.Points = append([]CanonicalEvent(nil), trace.Points...)
	return trace
}

func hasMinimumDisplacement(points []CanonicalEvent, minimumMeters float64) bool {
	if minimumMeters <= 0 {
		return true
	}

	first := points[0]
	for _, point := range points[1:] {
		if haversineMeters(first.Latitude, first.Longitude, point.Latitude, point.Longitude) >= minimumMeters {
			return true
		}
	}
	return false
}

func haversineMeters(lat1 float64, lon1 float64, lat2 float64, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0

	lat1Radians := lat1 * math.Pi / 180
	lat2Radians := lat2 * math.Pi / 180
	deltaLatitude := (lat2 - lat1) * math.Pi / 180
	deltaLongitude := (lon2 - lon1) * math.Pi / 180

	sinLatitude := math.Sin(deltaLatitude / 2)
	sinLongitude := math.Sin(deltaLongitude / 2)
	a := sinLatitude*sinLatitude +
		math.Cos(lat1Radians)*math.Cos(lat2Radians)*sinLongitude*sinLongitude
	a = math.Min(1, math.Max(0, a))
	return earthRadiusMeters * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
