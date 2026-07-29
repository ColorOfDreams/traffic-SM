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

// NewConsumerState kiểm tra config và tạo state rỗng cho một Kafka partition.
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

// Add nhận một GPS đã validate, cập nhật state của tài xế và trả về Trace khi
// buffer đã đủ điểm và đạt độ dịch chuyển tối thiểu. Kết quả nil nghĩa là state
// đã nhận GPS nhưng chưa có trace sẵn sàng.
//
// Add, CompleteMatch và EvictInactive của cùng ConsumerState phải chạy tuần tự
// trên goroutine sở hữu Kafka partition.
func (state *ConsumerState) Add(event CanonicalEvent, receivedAt time.Time) (*Trace, error) {
	if receivedAt.IsZero() {
		return nil, ErrReceivedAtMissing
	}
	receivedAt = receivedAt.UTC()

	recordedAt, err := event.RecordedAt()
	if err != nil {
		return nil, fmt.Errorf("derive GPS time: %w", err)
	}
	eventKey, err := eventFingerprint(event)
	if err != nil {
		return nil, err
	}

	driver := state.drivers[event.DriverID]
	if driver == nil {
		driver = &DriverState{
			Points: make([]CanonicalEvent, 0, state.config.TracePoints),
		}
		state.drivers[event.DriverID] = driver
	}

	// Kafka at-least-once thường phát lại message ngay sát message gốc.
	if eventKey == driver.LastEventKey {
		return nil, nil
	}

	if !driver.LastRecordedAt.IsZero() {
		// DriverID là Kafka key nên event hợp lệ phải đến đúng thứ tự.
		if !recordedAt.After(driver.LastRecordedAt) {
			return nil, nil
		}

		gap := recordedAt.Sub(driver.LastRecordedAt)
		if state.config.MaxPointGap > 0 && gap > state.config.MaxPointGap {
			// Chỉ bỏ overlap/pending của hành trình cũ. Trace đang chạy được giữ
			// riêng trong inFlight nên không xóa nhầm GPS của hành trình mới.
			driver.Points = driver.Points[:0]
		} else if state.config.MaxImpliedSpeedMPS > 0 {
			distanceM := haversineMeters(
				driver.LastLat,
				driver.LastLng,
				event.Lat,
				event.Lng,
			)
			impliedSpeedMPS := distanceM / gap.Seconds()
			if impliedSpeedMPS > state.config.MaxImpliedSpeedMPS {
				// Điểm GPS có bước nhảy bất thường; không đưa hai hành trình cách
				// xa nhau vào cùng một yêu cầu GraphHopper.
				driver.Points = driver.Points[:0]
			}
		}
	}

	if len(driver.Points) >= state.config.MaxBufferedPoints {
		// Caller phải tạo backpressure thay vì tiếp tục tăng bộ nhớ vô hạn.
		return nil, ErrDriverBufferFull
	}

	driver.Points = append(driver.Points, event)
	driver.LastEventKey = eventKey
	driver.LastRecordedAt = recordedAt
	driver.LastLng = event.Lng
	driver.LastLat = event.Lat
	driver.LastSeenAt = receivedAt

	trace, err := state.startTrace(event.DriverID, driver)
	if err != nil {
		return nil, err
	}
	if trace == nil &&
		driver.inFlight == nil &&
		len(driver.Points) >= state.config.TracePoints {
		// Xe đứng yên: giữ cửa sổ trượt thay vì tích lũy vô hạn hoặc gửi một
		// trace không có chuyển động cho GraphHopper.
		keep := state.config.TracePoints - 1
		driver.Points = append([]CanonicalEvent(nil), driver.Points[len(driver.Points)-keep:]...)
	}
	return trace, nil
}

// CompleteMatch hoàn tất trace đang xử lý nếu driverID và traceID cùng khớp.
// Kết quả trùng hoặc cũ không được phép làm thay đổi state hiện tại.
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

		completion := MatchCompletion{FailedTrace: &failed}
		nextTrace, err := state.startTrace(driverID, driver)
		if err != nil {
			return MatchCompletion{}, err
		}
		completion.NextTrace = nextTrace
		return completion, nil
	}

	driver.inFlight = nil
	nextTrace, err := state.startTrace(driverID, driver)
	if err != nil {
		return MatchCompletion{}, err
	}
	return MatchCompletion{NextTrace: nextTrace}, nil
}

// EvictInactive xóa state của tài xế đã ngừng gửi GPS và không có trace đang xử lý.
// Output là số tài xế đã bị xóa khỏi state.
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

// startTrace lấy TracePoints GPS đầu buffer, kiểm tra độ dịch chuyển, tạo Trace
// và giữ lại OverlapPoints cho cửa sổ kế tiếp.
func (state *ConsumerState) startTrace(
	driverID string,
	driver *DriverState,
) (*Trace, error) {
	if driver.inFlight != nil || len(driver.Points) < state.config.TracePoints {
		return nil, nil
	}

	candidate := driver.Points[:state.config.TracePoints]
	if !hasMinimumDisplacement(candidate, state.config.MinDisplacementM) {
		return nil, nil
	}

	points := append([]CanonicalEvent(nil), candidate...)
	startedAt, err := points[0].RecordedAt()
	if err != nil {
		return nil, fmt.Errorf("derive trace start time: %w", err)
	}
	endedAt, err := points[len(points)-1].RecordedAt()
	if err != nil {
		return nil, fmt.Errorf("derive trace end time: %w", err)
	}
	trace := Trace{
		TraceID: fmt.Sprintf(
			"%s:%d:%d",
			driverID,
			startedAt.UnixMilli(),
			endedAt.UnixMilli(),
		),
		DriverID:  driverID,
		StartedAt: startedAt,
		EndedAt:   endedAt,
		Points:    points,
	}

	consumed := state.config.TracePoints - state.config.OverlapPoints
	driver.Points = append([]CanonicalEvent(nil), driver.Points[consumed:]...)
	driver.inFlight = &inFlightMatch{
		trace:    trace,
		attempts: 1,
	}

	result := cloneTrace(trace)
	return &result, nil
}

// dropFailedOverlap bỏ phần overlap thuộc trace đã thất bại, nhưng chỉ khi
// từng event khớp để không xóa nhầm GPS của trace mới.
func (state *ConsumerState) dropFailedOverlap(driver *DriverState, failed Trace) {
	overlap := state.config.OverlapPoints
	if overlap == 0 || len(driver.Points) < overlap || len(failed.Points) < overlap {
		return
	}

	failedSuffix := failed.Points[len(failed.Points)-overlap:]
	for index := 0; index < overlap; index++ {
		if !sameEventIdentity(driver.Points[index], failedSuffix[index]) {
			return
		}
	}
	driver.Points = append([]CanonicalEvent(nil), driver.Points[overlap:]...)
}

// cloneTrace sao chép slice Points để caller không sửa chung backing array với state.
func cloneTrace(trace Trace) Trace {
	trace.Points = append([]CanonicalEvent(nil), trace.Points...)
	return trace
}
