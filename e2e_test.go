package main

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestEndToEnd simulates a user flow end-to-end:
// - start with no config file
// - create a new alert
// - create a new target
// - validate
// - run alerts and targets editors with no edits
// - validate again and ensure no-op edits didn't change state
func TestEndToEnd(t *testing.T) {
	// Use a temporary state file (does not exist initially)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "watch-state.yml")

	// Ensure editor invocations are no-ops
	t.Setenv("EDITOR", "/bin/true")

	// Initialize state (file does not exist, Load will create it)
	sm := NewStateManager(statePath)
	if err := sm.Load(); err != nil {
		t.Fatalf("initial load failed: %v", err)
	}

	// Create a new alert (console already exists by default in edit flow; here we add a valid slack alert)
	alerts := map[string]NotifierConfig{
		"console": {
			Name:     "console",
			Type:     "console",
			Enabled:  true,
			Settings: map[string]interface{}{"style": "stylized", "color": true},
		},
		"slack-alerts": {
			Name:     "slack-alerts",
			Type:     "slack",
			Enabled:  true,
			Settings: map[string]interface{}{"webhook_url": "https://hooks.slack.com/services/T000/B000/XXXX"},
		},
	}
	if err := sm.UpdateAlerts(alerts); err != nil {
		t.Fatalf("update alerts failed: %v", err)
	}

	// Create a new target
	target := Target{
		Name:          "API",
		URL:           "https://api.example.com/health",
		Method:        "GET",
		Threshold:     30,
		CheckStrategy: "http",
		Alerts:        []string{"console", "slack-alerts"},
	}
	if err := sm.AddTarget(target); err != nil {
		t.Fatalf("add target failed: %v", err)
	}

	// Validate targets and alerts using internal validators
	if err := sm.Load(); err != nil {
		t.Fatalf("reload after add failed: %v", err)
	}
	cfg := sm.GetTargetConfig()
	if err := validateTargets(mapFromSlice(cfg.Targets), sm); err != nil {
		t.Fatalf("validate targets failed: %v", err)
	}
	if err := validateAlerts(sm.GetAlerts()); err != nil {
		t.Fatalf("validate alerts failed: %v", err)
	}

	// Capture state snapshot (excluding Updated timestamps)
	beforeTargets := sm.ListTargets()
	beforeAlerts := sm.GetAlerts()
	beforeUpdated := sm.GetStateInfo()["updated"].(time.Time)

	// Run alerts editor with no changes
	editAlerts(sm)

	// Run targets editor with no changes
	handleEditTargets(statePath)

	// Re-validate
	if err := sm.Load(); err != nil {
		t.Fatalf("reload after edits failed: %v", err)
	}
	cfg = sm.GetTargetConfig()
	if err := validateTargets(mapFromSlice(cfg.Targets), sm); err != nil {
		t.Fatalf("validate targets (post-edit) failed: %v", err)
	}
	if err := validateAlerts(sm.GetAlerts()); err != nil {
		t.Fatalf("validate alerts (post-edit) failed: %v", err)
	}

	// Confirm that no-op edits did not modify implementation (state content)
	afterTargets := sm.ListTargets()
	afterAlerts := sm.GetAlerts()
	// Targets and Alerts should be deeply equal
	if !reflect.DeepEqual(beforeTargets, afterTargets) {
		t.Fatalf("targets changed after no-op edits:\nBEFORE=%v\nAFTER =%v", beforeTargets, afterTargets)
	}
	if !reflect.DeepEqual(beforeAlerts, afterAlerts) {
		t.Fatalf("alerts changed after no-op edits:\nBEFORE=%v\nAFTER =%v", beforeAlerts, afterAlerts)
	}

	// Updated timestamp may advance; ensure it didn't go backwards
	afterUpdated := sm.GetStateInfo()["updated"].(time.Time)
	if afterUpdated.Before(beforeUpdated) {
		t.Fatalf("state Updated timestamp moved backwards")
	}
}

func mapFromSlice(items []Target) map[string]Target {
	res := make(map[string]Target, len(items))
	for _, it := range items {
		res[it.URL] = it
	}
	return res
}

// TestTargetsEditor_PreservesAlertsOnNoop reproduces the reported alerts loss scenario
// and ensures that opening/closing the targets editor without changes does not
// modify the Alerts list for a target.
func TestTargetsEditor_PreservesAlertsOnNoop(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "watch-state.yml")
	t.Setenv("EDITOR", "/bin/true")

	sm := NewStateManager(statePath)
	if err := sm.Load(); err != nil {
		t.Fatalf("initial load failed: %v", err)
	}

	// 1) create alert
	alerts := map[string]NotifierConfig{
		"slack": {
			Name:     "slack",
			Type:     "slack",
			Enabled:  true,
			Settings: map[string]interface{}{"webhook_url": "https://hooks.slack.com/services/T000/B000/XXXX"},
		},
	}
	if err := sm.UpdateAlerts(alerts); err != nil {
		t.Fatalf("update alerts failed: %v", err)
	}

	// 2) create target (no alerts yet)
	base := Target{
		Name:          "secondary",
		URL:           "https://bevel.work",
		Method:        "GET",
		Threshold:     30,
		CheckStrategy: "http",
	}
	if err := sm.AddTarget(base); err != nil {
		t.Fatalf("add target failed: %v", err)
	}

	// 3) append new alert to target via 'targets' simplified editor flow
	edited := []byte(
		"secondary:\n" +
			"  url: https://bevel.work\n" +
			"  alerts: [slack]\n",
	)
	parsed, parsedFields, err := parseTargetsFromYAML(edited)
	if err != nil {
		t.Fatalf("parseTargetsFromYAML error: %v", err)
	}
	// Simulate handleEditTargets save loop with merge semantics
	for url, tgt := range parsed {
		existing, ok := sm.GetTarget(url)
		if ok {
			f := parsedFields[url]
			if f == nil {
				f = &TargetFields{}
			}
			if !f.Method && tgt.Method == "" {
				tgt.Method = existing.Method
			}
			if !f.Headers && len(tgt.Headers) == 0 && existing.Headers != nil {
				tgt.Headers = existing.Headers
			}
			if !f.Threshold && tgt.Threshold == 0 {
				tgt.Threshold = existing.Threshold
			}
			if !f.StatusCodes && len(tgt.StatusCodes) == 0 && len(existing.StatusCodes) > 0 {
				tgt.StatusCodes = existing.StatusCodes
			}
			if !f.SizeAlerts && (tgt.SizeAlerts == (SizeAlertConfig{})) {
				tgt.SizeAlerts = existing.SizeAlerts
			}
			if !f.CheckStrategy && tgt.CheckStrategy == "" {
				tgt.CheckStrategy = existing.CheckStrategy
			}
			if !f.Alerts && len(tgt.Alerts) == 0 {
				if len(existing.Alerts) > 0 {
					tgt.Alerts = existing.Alerts
				}
				if len(tgt.Alerts) == 0 && existing.AlertStrategy != "" {
					tgt.Alerts = []string{existing.AlertStrategy}
				}
			}
			if tgt.Name == "" {
				tgt.Name = existing.Name
			}
		}
		if err := sm.AddTarget(tgt); err != nil {
			t.Fatalf("save merged target failed: %v", err)
		}
	}

	// Ensure alert was appended
	if got, ok := sm.GetTarget("https://bevel.work"); !ok {
		t.Fatalf("target not found after edit")
	} else if len(got.Alerts) != 1 || got.Alerts[0] != "slack" {
		t.Fatalf("expected Alerts [slack], got %v", got.Alerts)
	}

	// 4) open and close targets with no changes
	handleEditTargets(statePath)

	// 5) confirm alerts list unchanged
	if got, ok := sm.GetTarget("https://bevel.work"); !ok {
		t.Fatalf("target missing after noop edit")
	} else if len(got.Alerts) != 1 || got.Alerts[0] != "slack" {
		t.Fatalf("alerts changed after noop edit, got %v", got.Alerts)
	}
}
