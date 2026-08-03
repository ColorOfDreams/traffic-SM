package graphhopper

import (
	"strings"
	"testing"
	"time"
)

func TestNewMatcherRequiresGraphVersion(t *testing.T) {
	_, err := NewMatcher(Config{
		BaseURL:     "http://graphhopper:8989",
		Profile:     "car",
		GPSAccuracy: 10,
		Timeout:     time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "graph version is required") {
		t.Fatalf("NewMatcher() error = %v, want graph version error", err)
	}
}

func TestNewMatcherStoresTrimmedGraphVersion(t *testing.T) {
	matcher, err := NewMatcher(Config{
		BaseURL:      "http://graphhopper:8989",
		Profile:      "car",
		GraphVersion: " vietnam-20260730-motorcycle-v1 ",
		GPSAccuracy:  10,
		Timeout:      time.Second,
	})
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}
	if matcher.graphVersion != testGraphVersion {
		t.Fatalf("graph version = %q, want %q", matcher.graphVersion, testGraphVersion)
	}
}
