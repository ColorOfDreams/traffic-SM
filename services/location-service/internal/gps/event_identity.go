package gps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// eventFingerprint tạo khóa ổn định từ toàn bộ 19 field của GPS đã validate.
// Cùng một payload luôn tạo cùng một khóa, nên Kafka retry không cần event_id
// từ thiết bị. Khóa này chỉ tồn tại trong state nội bộ.
func eventFingerprint(event CanonicalEvent) (string, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("encode GPS fingerprint: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:16]), nil
}

// sameEventIdentity so sánh danh tính tối thiểu của hai GPS trong cùng state.
// Hàm này được dùng để bảo vệ overlap khỏi bị xóa nhầm sau một trace thất bại.
func sameEventIdentity(left CanonicalEvent, right CanonicalEvent) bool {
	return left.DriverID == right.DriverID &&
		left.Time == right.Time &&
		left.TTimestamp == right.TTimestamp &&
		left.Lat == right.Lat &&
		left.Lng == right.Lng
}
