package gps

import "math"

// hasMinimumDisplacement kiểm tra có ít nhất một điểm cách điểm đầu của trace
// một khoảng minimumMeters hay không.
func hasMinimumDisplacement(points []CanonicalEvent, minimumMeters float64) bool {
	if minimumMeters <= 0 {
		return true
	}

	first := points[0]
	for _, point := range points[1:] {
		if haversineMeters(
			first.Lat,
			first.Lng,
			point.Lat,
			point.Lng,
		) >= minimumMeters {
			return true
		}
	}
	return false
}

// haversineMeters trả về khoảng cách đường chim bay theo mét giữa hai tọa độ
// latitude/longitude biểu diễn bằng độ thập phân.
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
