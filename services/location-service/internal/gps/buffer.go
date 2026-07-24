// Buffer để nhận gps
package gps

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// event_id giống nhau nhưng longitude, latitude, speed hoặc trường khác không giống nhau trả ra lỗi
var ErrEventIDConflict = errors.New("event_id already exists with a different payload")

// Result add
type BufferAddResult struct {
	Added             bool
	Duplicate         bool
	VehicleEventCount int
}

// Buffered Event để ghi nhận mỗi lần nhận canonical event
type bufferedEvent struct {
	event      CanonicalEvent
	recordedAt time.Time
}

// Bổ sung thêm 1 thằng nữa, mỗi xe chi cần giữ 2 loại dữ liệu đó là events danh sách gps ddocjw sắp xếp theo recored ai
// Last receivedAit lần gần nhất server nhận được một GPS mới của xe
type vehicleBuffer struct {
	events         []bufferedEvent
	lastReceivedAt time.Time
}

// Buffer manager lưu buffered event và canonicalevent, ngăn 2 request cùng sửa map, lock cho ghi, rlock đọc
type BufferManager struct {
	mu       sync.RWMutex
	vehicles map[string]*vehicleBuffer
	eventIDs map[string]CanonicalEvent
}

// vehicles:
//Lưu buffer chính, nhóm danh sách bufferedEvent theo vehicle_id.

// eventIDs:
// Chỉ mục toàn cục theo event_id, phục vụ chống trùng và phát hiện xung đột.
func NewBufferManager() *BufferManager {
	return &BufferManager{
		vehicles: make(map[string]*vehicleBuffer),
		eventIDs: make(map[string]CanonicalEvent),
	}
}

// Kiểm tra input của add
func (manager *BufferManager) Add(event CanonicalEvent) (BufferAddResult, error) {
	return manager.AddAt(event, time.Now().UTC())
}

func (manager *BufferManager) AddAt(event CanonicalEvent, receivedAt time.Time) (BufferAddResult, error) {
	// Check xem event id có rỗng hay ko
	if strings.TrimSpace(event.EventID) == "" {
		return BufferAddResult{}, errors.New("event_id is required")
	}
	// Vehice id có rỗng hay không
	if strings.TrimSpace(event.VehicleID) == "" {
		return BufferAddResult{}, errors.New("vehicle_id is required")
	}
	// Recoreded at phải chính xác, đúng định dạng giờ, chuyển thành time, và có thong tin múi giờ
	recordedAt, err := time.Parse(time.RFC3339Nano, event.RecordedAt)
	if err != nil {
		return BufferAddResult{}, fmt.Errorf("recorded_at must be an RFC3339 timestamp with timezone: %w", err)
	}
	if receivedAt.IsZero() {
		return BufferAddResult{}, errors.New("received_at is required")
	}
	//cloneCanonicalEvent:
	//Tạo bản sao độc lập của HeadingDeg vì đây là con trỏ.
	event = cloneCanonicalEvent(event)
	receivedAt = receivedAt.UTC()

	//Từ thời điểm khóa thành công đến khi Add kết thúc,
	// chỉ một goroutine được thay đổi BufferManager.
	manager.mu.Lock()
	defer manager.mu.Unlock()
	// Cùng event_id và toàn bộ canonical payload giống nhau
	// duplicate hợp lệ, không thêm lại và ngược lại
	if existing, found := manager.eventIDs[event.EventID]; found {
		if !sameCanonicalEvent(existing, event) {
			return BufferAddResult{}, ErrEventIDConflict
		}
		vehicle := manager.vehicles[event.VehicleID]
		eventCount := 0
		if vehicle != nil {
			eventCount = len(vehicle.events)
		}
		return BufferAddResult{
			Duplicate:         true,
			VehicleEventCount: eventCount,
		}, nil
	}
	// dùng vehicle id làm khóa để lấy danh sách id tương ứng, nếu chưa có xe thì trả 1 slice rỗng
	vehicle := manager.vehicles[event.VehicleID]
	if vehicle == nil {
		vehicle = &vehicleBuffer{}
		manager.vehicles[event.VehicleID] = vehicle
	}
	events := vehicle.events
	// tìm vị trú cần phải chèn vào
	insertAt := sort.Search(len(events), func(index int) bool {
		if events[index].recordedAt.Equal(recordedAt) {
			return events[index].event.EventID >= event.EventID
		}
		return events[index].recordedAt.After(recordedAt)
	})

	// insert vào trong buffer có vehicle id tương ứng
	events = append(events, bufferedEvent{})
	copy(events[insertAt+1:], events[insertAt:])
	events[insertAt] = bufferedEvent{
		event:      event,
		recordedAt: recordedAt,
	}
	// Ghi lại 2 map
	vehicle.events = events
	if receivedAt.After(vehicle.lastReceivedAt) {
		vehicle.lastReceivedAt = receivedAt
	}
	manager.eventIDs[event.EventID] = event
	// Trả kết quả
	return BufferAddResult{
		Added:             true,
		VehicleEventCount: len(events),
	}, nil
}

// Đọc gps của 1 xe
func (manager *BufferManager) Events(vehicleID string) []CanonicalEvent {
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	vehicle := manager.vehicles[vehicleID]
	if vehicle == nil {
		return nil
	}
	events := make([]CanonicalEvent, len(vehicle.events))
	for index := range vehicle.events {
		events[index] = cloneCanonicalEvent(vehicle.events[index].event)
	}
	return events
}

// so sánh 2 event
func sameCanonicalEvent(left CanonicalEvent, right CanonicalEvent) bool {
	if left.EventID != right.EventID ||
		left.VehicleID != right.VehicleID ||
		left.RecordedAt != right.RecordedAt ||
		left.Longitude != right.Longitude ||
		left.Latitude != right.Latitude ||
		left.SpeedKMH != right.SpeedKMH {
		return false
	}
	if left.HeadingDeg == nil || right.HeadingDeg == nil {
		return left.HeadingDeg == nil && right.HeadingDeg == nil
	}
	return *left.HeadingDeg == *right.HeadingDeg
}

func cloneCanonicalEvent(event CanonicalEvent) CanonicalEvent {
	if event.HeadingDeg != nil {
		heading := *event.HeadingDeg
		event.HeadingDeg = &heading
	}
	return event
}
