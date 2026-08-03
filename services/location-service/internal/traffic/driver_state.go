package traffic

// CHủ yếu để check và lưu lại để cho vô aggregator, tránh trường hợp input cũ hơn input mới
import (
	"fmt"
	"sync"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
)

type DriverState struct {
	LastStatus       string
	LastGraphVersion string
	LastTraversalKey *int64
	LastTimestamp    time.Time
	LastSeenAt       time.Time
}

type DriverStateBuffer struct {
	mu     sync.Mutex
	states map[string]DriverState
}

func NewDriverStateBuffer() *DriverStateBuffer {
	return &DriverStateBuffer{
		states: make(map[string]DriverState),
	}
}

func (buffer *DriverStateBuffer) Add(
	input matching.MatchedObservation,
	now time.Time,
) (matching.MatchedObservation, bool, error) {
	// Kiểm tra tồn tại của input và thời gian hiện tại
	if !IsValid(input) {
		return matching.MatchedObservation{}, false, fmt.Errorf("invalid matched observation")
	}
	if now.IsZero() {
		return matching.MatchedObservation{}, false, fmt.Errorf("processing time is required")
	}

	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	// Check qua các dữ kiện xem có hợp lệ không
	state, exists := buffer.states[input.DriverID]
	if exists {
		if input.RecordedAt.Equal(state.LastTimestamp) {
			return matching.MatchedObservation{}, false, nil
		}
		if input.RecordedAt.Before(state.LastTimestamp) {
			return matching.MatchedObservation{}, false, fmt.Errorf(
				"recorded_at for driver %q is older than the last observation",
				input.DriverID,
			)
		}
	}
	// Check xong nếu ổn thêm thì trả về giá trị hợp lệ để thêm vào aggregator
	state.LastStatus = input.Status
	state.LastTimestamp = input.RecordedAt
	state.LastSeenAt = now
	if CanAggregate(input) {
		traversalKey := *input.TraversalKey
		state.LastGraphVersion = input.GraphVersion
		state.LastTraversalKey = &traversalKey
	}
	buffer.states[input.DriverID] = state

	if !CanAggregate(input) {
		return matching.MatchedObservation{}, false, nil
	}
	// Trả ra input, true, nil nếu hợp lệ
	return input, true, nil
}
