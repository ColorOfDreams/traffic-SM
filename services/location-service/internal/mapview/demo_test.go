package mapview

import (
	"math"
	"testing"
	"time"
)

func TestNewDemoFlowKeepsZeroSpeedInTripObservation(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	flow := NewDemoFlow(now)

	if flow.TraversalKey != 11916856 {
		t.Fatalf("unexpected traversal key %d", flow.TraversalKey)
	}
	if flow.Aggregation.EligibleDriverCount != 1 || flow.Aggregation.TotalDriverCount != 1 {
		t.Fatalf("unexpected driver counts %#v", flow.Aggregation)
	}
	if flow.Aggregation.CurrentSpeedMPS != 0 {
		t.Fatalf("expected median speed 0 m/s, got %v", flow.Aggregation.CurrentSpeedMPS)
	}
	if math.Abs(flow.Aggregation.SpeedRatio) > 1e-9 ||
		math.Abs(flow.Aggregation.CongestionScore-1) > 1e-9 {
		t.Fatalf("unexpected congestion calculation %#v", flow.Aggregation)
	}
	if !flow.Observations[0].Eligible || flow.Observations[0].SpeedMPS != 0 {
		t.Fatalf("zero-speed IN_TRIP observation must be included: %#v", flow.Observations[0])
	}
	if flow.Observations[1].Eligible || flow.Observations[2].Eligible {
		t.Fatalf("service-road observations must be excluded: %#v", flow.Observations)
	}
	if flow.Aggregation.Level != "CONGESTED" {
		t.Fatalf("level = %q, want CONGESTED", flow.Aggregation.Level)
	}
}
