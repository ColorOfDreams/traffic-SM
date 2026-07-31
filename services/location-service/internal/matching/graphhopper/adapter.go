package graphhopper

import (
	"fmt"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/matching"
	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/trace"
)

// Kiểm tra dữ liệu graphhopper trả về và ghép dữ liệu gốc với map matching, cuối cùng trả về dữ liệu cần
func adaptResponse(input trace.Trace, response matchResponse) ([]matching.MatchedObservation, error) {
	// Kiểm tra
	if len(response.MatchedPoints) != len(input.Points) {
		return nil, fmt.Errorf(
			"matched point count %d does not match trace point count %d",
			len(response.MatchedPoints),
			len(input.Points),
		)
	}
	// Tạo slice kết quả có số phần tử bằng gps đầu vào
	observations := make(
		[]matching.MatchedObservation,
		len(input.Points),
	)

	// Tạo một mảng dùng để check xem pointIndex có trong response chưa
	// output nó dạng [false, false, true, false, true]
	seen := make([]bool, len(input.Points))

	for _, matchedPoint := range response.MatchedPoints {
		index := matchedPoint.PointIndex

		if index < 0 || index >= len(input.Points) {
			return nil, fmt.Errorf(
				"point_index %d is outside trace",
				index,
			)
		}

		// Kiểm tra xem có index nào bị lặp
		if seen[index] {
			return nil, fmt.Errorf(
				"point_index %d appears more than once",
				index,
			)
		}
		// Đánh dấu index đã xuất hiện
		seen[index] = true

		// Lấy GPS gốc
		originalPoint := input.Points[index]
		if originalPoint.DriverID != input.DriverID {
			return nil, fmt.Errorf(
				"trace point %d belongs to driver %q instead of %q",
				index,
				originalPoint.DriverID,
				input.DriverID,
			)
		}

		observation := matching.MatchedObservation{
			DriverID:    originalPoint.DriverID,
			PointIndex:  index,
			RecordedAt:  originalPoint.RecordedAt,
			Speed:       originalPoint.Speed,
			Status:      originalPoint.Status,
			VehicleType: originalPoint.VehicleType,
			Matched:     matchedPoint.Matched,
		}

		if matchedPoint.Matched {
			if matchedPoint.TraversalKey == nil ||
				matchedPoint.SnappedLat == nil ||
				matchedPoint.SnappedLon == nil ||
				matchedPoint.SnapDistanceM == nil {

				return nil, fmt.Errorf(
					"matched point %d is missing matching fields",
					index,
				)
			}

			traversalKey := *matchedPoint.TraversalKey
			snappedLat := *matchedPoint.SnappedLat
			snappedLng := *matchedPoint.SnappedLon
			snapDistanceM := *matchedPoint.SnapDistanceM

			observation.TraversalKey = &traversalKey
			observation.SnappedLat = &snappedLat
			observation.SnappedLng = &snappedLng
			observation.SnapDistanceM = &snapDistanceM
		}

		observations[index] = observation
	}

	return observations, nil
}
