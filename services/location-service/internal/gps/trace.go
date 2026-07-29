package gps

import "time"

// Trace là một nhóm GPS liên tiếp của cùng tài xế được gửi sang map matching.
// StartedAt và EndedAt mô tả khoảng thời gian của các Points trong trace.
type Trace struct {
	TraceID   string           `json:"trace_id"`
	DriverID  string           `json:"driver_id"`
	StartedAt time.Time        `json:"started_at"`
	EndedAt   time.Time        `json:"ended_at"`
	Points    []CanonicalEvent `json:"points"`
}

// ConsumerStateConfig định nghĩa quy tắc gom GPS thành trace và giới hạn bộ nhớ.
type ConsumerStateConfig struct {
	// TracePoints là số GPS cần có trong một trace.
	TracePoints int

	// OverlapPoints là số GPS cuối của trace trước được giữ lại cho trace tiếp theo.
	OverlapPoints int

	// MaxPointGap tách hành trình khi hai GPS liên tiếp cách nhau quá lâu.
	// Giá trị 0 tắt quy tắc này.
	MaxPointGap time.Duration

	// InactivityTimeout là thời gian giữ state của một tài xế không còn gửi GPS.
	InactivityTimeout time.Duration

	// MaxBufferedPoints giới hạn số GPS chờ xử lý của mỗi tài xế.
	MaxBufferedPoints int

	// MaxMatchAttempts là số lần tối đa một trace được phép thử lại.
	MaxMatchAttempts int

	// MinDisplacementM là độ dịch chuyển tối thiểu để một nhóm GPS trở thành trace.
	MinDisplacementM float64

	// MaxImpliedSpeedMPS tách hành trình khi vận tốc suy ra giữa hai GPS quá lớn.
	// Giá trị 0 tắt quy tắc này.
	MaxImpliedSpeedMPS float64
}

// inFlightMatch giữ trace đang được xử lý cùng số lần đã thử.
// Tên "match" là tên legacy; state hiện được hoàn tất ngay sau khi trace được
// publish sang Kafka chứ không chờ GraphHopper trả kết quả.
type inFlightMatch struct {
	trace    Trace
	attempts int
}

// DriverState là bộ nhớ tạm của một tài xế trong Kafka partition hiện tại.
type DriverState struct {
	// Points chỉ chứa overlap và các GPS đang chờ tạo trace tiếp theo.
	// GPS thuộc trace đang xử lý được giữ riêng trong inFlight.
	Points []CanonicalEvent

	// Các field Last* phục vụ loại trùng, kiểm tra thứ tự và phát hiện teleport.
	// Chúng là state nội bộ, không được ghi vào payload GPS hoặc Kafka.
	LastEventKey   string
	LastRecordedAt time.Time
	LastLng        float64
	LastLat        float64

	// LastSeenAt là thời điểm service nhận GPS gần nhất, dùng để dọn state hết hạn.
	LastSeenAt time.Time

	inFlight *inFlightMatch
}

// ConsumerState giữ state tài xế của một Kafka partition.
// Instance này chỉ được sử dụng tuần tự bởi goroutine sở hữu partition.
type ConsumerState struct {
	config  ConsumerStateConfig
	drivers map[string]*DriverState
}

// MatchCompletion mô tả output sau khi hoàn tất trace đang xử lý.
// RetryTrace và NextTrace loại trừ lẫn nhau. FailedTrace cần được ghi log hoặc
// gửi sang dead-letter khi đã dùng hết số lần thử.
//
// Tên MatchCompletion là tên legacy và sẽ được đổi khi cập nhật state theo
// canonical mới.
type MatchCompletion struct {
	RetryTrace  *Trace
	NextTrace   *Trace
	FailedTrace *Trace
}
