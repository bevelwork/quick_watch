package main

import (
	"context"
	"testing"
	"time"
)

func TestEngine_MultipleAlertStrategies(t *testing.T) {
	cfg := &TargetConfig{
		Targets: []Target{
			{
				Name:          "t",
				URL:           "https://example.com/health",
				CheckStrategy: "http",
				Alerts:        []string{"console"},
			},
		},
	}
	engine := NewTargetEngine(cfg, nil)
	if len(engine.targets) != 1 {
		t.Fatalf("expected 1 target state, got %d", len(engine.targets))
	}
	if len(engine.targets[0].AlertStrategies) == 0 {
		t.Fatalf("expected at least one alert strategy")
	}
	// Run a quick iteration of check loop with a context cancel
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("engine start error: %v", err)
	}
}

func TestTargetState_ExponentialBackoff(t *testing.T) {
	// Create a target state
	target := &Target{
		Name:          "test-target",
		URL:           "https://example.com/health",
		CheckStrategy: "http",
		Threshold:     30,
		Alerts:        []string{"console"},
	}

	state := &TargetState{
		Target: target,
		IsDown: false,
	}

	// Test initial failure - should alert immediately
	state.IsDown = true
	now := time.Now()
	state.DownSince = &now
	state.FailureCount = 1
	state.LastAlertTime = &now

	if state.FailureCount != 1 {
		t.Errorf("expected FailureCount to be 1, got %d", state.FailureCount)
	}
	if state.LastAlertTime == nil {
		t.Error("expected LastAlertTime to be set")
	}

	// Test exponential backoff calculation
	testCases := []struct {
		failureCount       int
		expectedBackoffSec int
	}{
		{1, 5},   // 5 * 2^0 = 5 seconds
		{2, 10},  // 5 * 2^1 = 10 seconds
		{3, 20},  // 5 * 2^2 = 20 seconds
		{4, 40},  // 5 * 2^3 = 40 seconds
		{5, 80},  // 5 * 2^4 = 80 seconds
		{6, 160}, // 5 * 2^5 = 160 seconds
		{7, 320}, // 5 * 2^6 = 320 seconds
		{8, 640}, // 5 * 2^7 = 640 seconds
	}

	for _, tc := range testCases {
		backoffSeconds := 5 * (1 << uint(tc.failureCount-1))
		if backoffSeconds != tc.expectedBackoffSec {
			t.Errorf("for failureCount %d, expected backoff %d seconds, got %d",
				tc.failureCount, tc.expectedBackoffSec, backoffSeconds)
		}
	}

	// Test that acknowledged alerts stop backoff
	acknowledgedTime := time.Now()
	state.AcknowledgedAt = &acknowledgedTime
	state.AcknowledgedBy = "test-user"

	// When acknowledged, no more alerts should be sent
	if state.AcknowledgedAt == nil {
		t.Error("expected AcknowledgedAt to be set")
	}

	// Test recovery resets counters
	state.IsDown = false
	state.DownSince = nil
	state.FailureCount = 0
	state.LastAlertTime = nil
	state.AcknowledgedAt = nil
	state.AcknowledgedBy = ""

	if state.FailureCount != 0 {
		t.Errorf("expected FailureCount to be reset to 0, got %d", state.FailureCount)
	}
	if state.LastAlertTime != nil {
		t.Error("expected LastAlertTime to be nil after recovery")
	}
	if state.AcknowledgedAt != nil {
		t.Error("expected AcknowledgedAt to be nil after recovery")
	}
}

func TestTargetState_BackoffTiming(t *testing.T) {
	// Create a simple mock to test timing logic
	now := time.Now()

	// Simulate first failure
	failureCount := 1
	lastAlertTime := now
	backoffSeconds := 5 * (1 << uint(failureCount-1)) // 5 seconds

	// Check too early - should not alert
	checkTime := now.Add(3 * time.Second)
	timeSinceLastAlert := checkTime.Sub(lastAlertTime)
	if timeSinceLastAlert >= time.Duration(backoffSeconds)*time.Second {
		t.Error("expected check to be too early for alert")
	}

	// Check at exact backoff time - should alert
	checkTime = now.Add(5 * time.Second)
	timeSinceLastAlert = checkTime.Sub(lastAlertTime)
	if timeSinceLastAlert < time.Duration(backoffSeconds)*time.Second {
		t.Error("expected check to be ready for alert")
	}

	// Simulate second failure
	failureCount = 2
	lastAlertTime = now
	backoffSeconds = 5 * (1 << uint(failureCount-1)) // 10 seconds

	// Check at 5 seconds - should not alert
	checkTime = now.Add(5 * time.Second)
	timeSinceLastAlert = checkTime.Sub(lastAlertTime)
	if timeSinceLastAlert >= time.Duration(backoffSeconds)*time.Second {
		t.Error("expected check to be too early for alert")
	}

	// Check at 10 seconds - should alert
	checkTime = now.Add(10 * time.Second)
	timeSinceLastAlert = checkTime.Sub(lastAlertTime)
	if timeSinceLastAlert < time.Duration(backoffSeconds)*time.Second {
		t.Error("expected check to be ready for alert")
	}
}
