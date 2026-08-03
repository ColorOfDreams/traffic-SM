package traffic

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
)

const calculatorGraphVersion = "vietnam-20260730-motorcycle-v1"

type fakeReferenceSpeedStore struct {
	reference ReferenceSpeed
	found     bool
	err       error
	calls     int
	graph     string
	key       int64
	vehicle   string
}

func (store *fakeReferenceSpeedStore) Get(
	_ context.Context,
	graphVersion string,
	traversalKey int64,
	vehicleType string,
) (ReferenceSpeed, bool, error) {
	store.calls++
	store.graph = graphVersion
	store.key = traversalKey
	store.vehicle = vehicleType
	return store.reference, store.found, store.err
}

func TestMedianSpeed(t *testing.T) {
	tests := []struct {
		name   string
		speeds []float64
		want   float64
	}{
		{name: "odd", speeds: []float64{10, 0, 5}, want: 5},
		{name: "even", speeds: []float64{10, 0, 5, 0}, want: 2.5},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := append([]float64(nil), test.speeds...)
			got, err := medianSpeed(test.speeds)
			if err != nil {
				t.Fatalf("medianSpeed() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("medianSpeed() = %v, want %v", got, test.want)
			}
			if !slices.Equal(test.speeds, original) {
				t.Fatal("medianSpeed() changed the input slice")
			}
		})
	}
}

func TestCalculatorReturnsUnknownWithoutEnoughEvidence(t *testing.T) {
	store := &fakeReferenceSpeedStore{
		reference: ReferenceSpeed{SpeedMPS: 10},
		found:     true,
	}
	calculator := newTestCalculator(t, store, CalculatorConfig{
		MinSamples: 2,
		MinDrivers: 2,
	})
	window := calculatorWindow([]float64{0}, "driver-1")

	state, err := calculator.Calculate(context.Background(), window, testTime(10))
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if state.Level != LevelUnknown {
		t.Fatalf("level = %q, want UNKNOWN", state.Level)
	}
	if store.calls != 0 {
		t.Fatalf("reference store calls = %d, want 0", store.calls)
	}
}

func TestCalculatorReturnsUnknownWithoutReference(t *testing.T) {
	store := &fakeReferenceSpeedStore{}
	calculator := newTestCalculator(t, store, CalculatorConfig{
		MinSamples: 2,
		MinDrivers: 2,
	})

	state, err := calculator.Calculate(
		context.Background(),
		calculatorWindow([]float64{0, 10}, "driver-1", "driver-2"),
		testTime(10),
	)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if state.Level != LevelUnknown || state.ReferenceSpeedMPS != 0 {
		t.Fatalf("state = %#v, want UNKNOWN without reference speed", state)
	}
}

func TestCalculatorComputesRatioAndScore(t *testing.T) {
	store := &fakeReferenceSpeedStore{
		reference: ReferenceSpeed{SpeedMPS: 10, Source: "edge_fallback"},
		found:     true,
	}
	calculator := newTestCalculator(t, store, CalculatorConfig{
		MinSamples: 4,
		MinDrivers: 2,
	})
	window := calculatorWindow([]float64{0, 0, 5, 10}, "driver-1", "driver-2")

	state, err := calculator.Calculate(context.Background(), window, testTime(10))
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if state.CurrentSpeedMPS != 2.5 {
		t.Fatalf("current speed = %v, want 2.5", state.CurrentSpeedMPS)
	}
	if state.ReferenceSpeedMPS != 10 || state.SpeedRatio != 0.25 || state.CongestionScore != 0.75 {
		t.Fatalf("unexpected calculated state: %#v", state)
	}
	if state.Level != LevelCongested || state.Confidence != 0.5 {
		t.Fatalf("level=%q confidence=%v, want CONGESTED and 0.5", state.Level, state.Confidence)
	}
	if store.graph != calculatorGraphVersion || store.key != 12 || store.vehicle != "motorcycle" {
		t.Fatalf("reference lookup = (%q, %d, %q)", store.graph, store.key, store.vehicle)
	}
}

func TestCalculatorClampsSpeedAboveReference(t *testing.T) {
	store := &fakeReferenceSpeedStore{
		reference: ReferenceSpeed{SpeedMPS: 10},
		found:     true,
	}
	calculator := newTestCalculator(t, store, CalculatorConfig{
		MinSamples: 2,
		MinDrivers: 1,
	})

	state, err := calculator.Calculate(
		context.Background(),
		calculatorWindow([]float64{15, 20}, "driver-1"),
		testTime(10),
	)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if state.SpeedRatio != 1 || state.CongestionScore != 0 {
		t.Fatalf("ratio=%v score=%v, want 1 and 0", state.SpeedRatio, state.CongestionScore)
	}
	if state.Level != LevelFree || state.Confidence != 0.5 {
		t.Fatalf("level=%q confidence=%v, want FREE and 0.5", state.Level, state.Confidence)
	}
}

func TestClassifyLevelBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		ratio float64
		want  CongestionLevel
	}{
		{name: "free boundary", ratio: 0.8, want: LevelFree},
		{name: "slow upper range", ratio: 0.799, want: LevelSlow},
		{name: "slow boundary", ratio: 0.5, want: LevelSlow},
		{name: "congested", ratio: 0.499, want: LevelCongested},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyLevel(test.ratio); got != test.want {
				t.Fatalf("classifyLevel(%v) = %q, want %q", test.ratio, got, test.want)
			}
		})
	}
}

func TestCalculateConfidence(t *testing.T) {
	tests := []struct {
		name          string
		sampleCount   int
		driverCount   int
		want          float64
	}{
		{name: "not enough samples", sampleCount: 4, driverCount: 2, want: 0},
		{name: "not enough drivers", sampleCount: 5, driverCount: 1, want: 0},
		{name: "minimum evidence", sampleCount: 5, driverCount: 2, want: 0.5},
		{name: "double evidence", sampleCount: 10, driverCount: 4, want: 1},
		{name: "drivers limit confidence", sampleCount: 20, driverCount: 2, want: 0.5},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := calculateConfidence(test.sampleCount, test.driverCount, 5, 2)
			if got != test.want {
				t.Fatalf("calculateConfidence() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCalculatorReturnsReferenceStoreError(t *testing.T) {
	store := &fakeReferenceSpeedStore{err: errors.New("store unavailable")}
	calculator := newTestCalculator(t, store, CalculatorConfig{
		MinSamples: 1,
		MinDrivers: 1,
	})

	_, err := calculator.Calculate(
		context.Background(),
		calculatorWindow([]float64{5}, "driver-1"),
		testTime(10),
	)
	if err == nil {
		t.Fatal("expected reference store error")
	}
}

func TestCalculatorRejectsInvalidReferenceSpeed(t *testing.T) {
	tests := []float64{0, -1, math.NaN(), math.Inf(1)}
	for _, speed := range tests {
		store := &fakeReferenceSpeedStore{
			reference: ReferenceSpeed{SpeedMPS: speed},
			found:     true,
		}
		calculator := newTestCalculator(t, store, CalculatorConfig{
			MinSamples: 1,
			MinDrivers: 1,
		})
		_, err := calculator.Calculate(
			context.Background(),
			calculatorWindow([]float64{5}, "driver-1"),
			testTime(10),
		)
		if err == nil {
			t.Fatalf("reference speed %v: expected error", speed)
		}
	}
}

func newTestCalculator(
	t *testing.T,
	store ReferenceSpeedStore,
	config CalculatorConfig,
) *Calculator {
	t.Helper()
	calculator, err := NewCalculator(store, config)
	if err != nil {
		t.Fatalf("NewCalculator() error = %v", err)
	}
	return calculator
}

func calculatorWindow(speeds []float64, drivers ...string) SegmentWindow {
	driverIDs := make(map[string]struct{}, len(drivers))
	for _, driverID := range drivers {
		driverIDs[driverID] = struct{}{}
	}
	return SegmentWindow{
		Key: WindowKey{
			GraphVersion: calculatorGraphVersion,
			TraversalKey: 12,
			VehicleType:  "motorcycle",
			WindowStart:  testTime(0),
		},
		Speeds:    append([]float64(nil), speeds...),
		DriverIDs: driverIDs,
	}
}
