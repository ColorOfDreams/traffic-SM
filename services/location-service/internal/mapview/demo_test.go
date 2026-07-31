package mapview

import (
	"testing"
	"time"
)

func TestNewDemoFlowExcludesBusinessStop(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	flow := NewDemoFlow(now)

	if flow.TraversalKey != 366218 {
		t.Fatalf("unexpected traversal key %d", flow.TraversalKey)
	}
	if flow.Aggregation.EligibleDriverCount != 2 || flow.Aggregation.TotalDriverCount != 3 {
		t.Fatalf("unexpected driver counts %#v", flow.Aggregation)
	}
	if flow.Aggregation.CurrentSpeedMPS != 2.6 {
		t.Fatalf("expected median speed 2.6 m/s, got %v", flow.Aggregation.CurrentSpeedMPS)
	}
	if flow.Aggregation.SpeedRatio != 0.26 || flow.Aggregation.CongestionScore != 0.74 {
		t.Fatalf("unexpected congestion calculation %#v", flow.Aggregation)
	}
	if flow.Observations[2].Eligible || flow.Observations[2].BehaviorState != "BUSINESS_STOP" {
		t.Fatalf("business stop must be excluded: %#v", flow.Observations[2])
	}
}
