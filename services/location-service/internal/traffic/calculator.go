package traffic

// Tính toán congestion và trả về congestion state cho một segment window. Nếu không đủ dữ liệu, trả về LevelUnknown.
import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type CongestionLevel string

const (
	LevelUnknown   CongestionLevel = "UNKNOWN"
	LevelFree      CongestionLevel = "FREE"
	LevelSlow      CongestionLevel = "SLOW"
	LevelCongested CongestionLevel = "CONGESTED"
)

type CongestionState struct {
	GraphVersion        string
	TraversalKey        int64
	VehicleType         string
	WindowStart         time.Time
	CurrentSpeedMPS     float64
	ReferenceSpeedMPS   float64
	SpeedRatio          float64
	CongestionScore     float64
	SampleCount         int
	DistinctDriverCount int
	Confidence          float64
	Level               CongestionLevel
	UpdatedAt           time.Time
}

type CalculatorConfig struct {
	MinSamples int
	MinDrivers int
}

type Calculator struct {
	references ReferenceSpeedStore
	config     CalculatorConfig
}

func NewCalculator(
	references ReferenceSpeedStore,
	config CalculatorConfig,
) (*Calculator, error) {
	if references == nil {
		return nil, fmt.Errorf("reference speed store is required")
	}
	if config.MinSamples < 1 {
		return nil, fmt.Errorf("minimum samples must be at least one")
	}
	if config.MinDrivers < 1 {
		return nil, fmt.Errorf("minimum drivers must be at least one")
	}

	return &Calculator{
		references: references,
		config:     config,
	}, nil
}

// Lấy median speed
func medianSpeed(speeds []float64) (float64, error) {
	if len(speeds) == 0 {
		return 0, fmt.Errorf("speed samples are required")
	}

	values := append([]float64(nil), speeds...)

	for _, speed := range values {
		if speed < 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
			return 0, fmt.Errorf("speed samples must be finite and non-negative")
		}
	}

	sort.Float64s(values)

	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle], nil
	}

	return (values[middle-1] + values[middle]) / 2, nil
}

func clampRatio(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func classifyLevel(speedRatio float64) CongestionLevel {
	if speedRatio >= 0.8 {
		return LevelFree
	}
	if speedRatio >= 0.5 {
		return LevelSlow
	}
	return LevelCongested
}

func calculateConfidence(
	sampleCount int,
	driverCount int,
	minSamples int,
	minDrivers int,
) float64 {
	if sampleCount < minSamples || driverCount < minDrivers {
		return 0
	}

	sampleConfidence := math.Min(
		float64(sampleCount)/float64(2*minSamples),
		1,
	)
	driverConfidence := math.Min(
		float64(driverCount)/float64(2*minDrivers),
		1,
	)
	return math.Min(sampleConfidence, driverConfidence)
}

// Tính toán và trả về congestion state cho một segment window. Nếu không đủ dữ liệu, trả về LevelUnknown.
func (calculator *Calculator) Calculate(
	ctx context.Context,
	window SegmentWindow,
	updatedAt time.Time,
) (CongestionState, error) {
	if calculator == nil {
		return CongestionState{}, fmt.Errorf("calculator is not initialized")
	}
	if strings.TrimSpace(window.Key.GraphVersion) == "" {
		return CongestionState{}, fmt.Errorf("graph version is required")
	}
	if window.Key.TraversalKey < 0 {
		return CongestionState{}, fmt.Errorf("traversal key must be non-negative")
	}
	if window.Key.VehicleType != "car" &&
		window.Key.VehicleType != "motorcycle" {
		return CongestionState{}, fmt.Errorf("unsupported vehicle type")
	}
	if window.Key.WindowStart.IsZero() {
		return CongestionState{}, fmt.Errorf("window start is required")
	}
	if updatedAt.IsZero() {
		return CongestionState{}, fmt.Errorf("updated time is required")
	}

	currentSpeed, err := medianSpeed(window.Speeds)
	if err != nil {
		return CongestionState{}, err
	}

	state := CongestionState{
		GraphVersion:        window.Key.GraphVersion,
		TraversalKey:        window.Key.TraversalKey,
		VehicleType:         window.Key.VehicleType,
		WindowStart:         window.Key.WindowStart,
		CurrentSpeedMPS:     currentSpeed,
		SampleCount:         len(window.Speeds),
		DistinctDriverCount: len(window.DriverIDs),
		Level:               LevelUnknown,
		UpdatedAt:           updatedAt,
	}

	if state.SampleCount < calculator.config.MinSamples ||
		state.DistinctDriverCount < calculator.config.MinDrivers {
		return state, nil
	}

	reference, found, err := calculator.references.Get(
		ctx,
		window.Key.GraphVersion,
		window.Key.TraversalKey,
		window.Key.VehicleType,
	)
	if err != nil {
		return CongestionState{}, fmt.Errorf(
			"get reference speed: %w",
			err,
		)
	}
	if !found {
		return state, nil
	}

	if reference.SpeedMPS <= 0 ||
		math.IsNaN(reference.SpeedMPS) ||
		math.IsInf(reference.SpeedMPS, 0) {
		return CongestionState{}, fmt.Errorf(
			"reference speed must be finite and positive",
		)
	}
	state.ReferenceSpeedMPS = reference.SpeedMPS
	state.SpeedRatio = clampRatio(
		state.CurrentSpeedMPS / state.ReferenceSpeedMPS,
	)
	state.CongestionScore = 1 - state.SpeedRatio
	state.Level = classifyLevel(state.SpeedRatio)
	state.Confidence = calculateConfidence(
		state.SampleCount,
		state.DistinctDriverCount,
		calculator.config.MinSamples,
		calculator.config.MinDrivers,
	)

	return state, nil
}
