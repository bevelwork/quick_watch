package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseTargets_SimplifiedMap(t *testing.T) {
	yaml := []byte(
		"my-target:\n" +
			"  url: https://example.com/health\n" +
			"  alerts: [console, slack-alerts]\n",
	)
	targets, fields, err := parseTargetsFromYAML(yaml)
	if err != nil {
		t.Fatalf("parseTargetsFromYAML error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	var got Target
	for _, m := range targets {
		got = m
		break
	}
	if got.URL != "https://example.com/health" {
		t.Errorf("unexpected URL: %s", got.URL)
	}
	if len(got.Alerts) != 2 {
		t.Errorf("expected 2 alerts, got %d", len(got.Alerts))
	}
	if len(fields) != 1 {
		t.Errorf("expected fields for 1 target, got %d", len(fields))
	}
}

func TestParseTargets_WrappedTargets(t *testing.T) {
	yaml := []byte(
		"targets:\n" +
			"  api:\n" +
			"    url: https://api.example.com\n" +
			"    alerts: console\n",
	)
	targets, _, err := parseTargetsFromYAML(yaml)
	if err != nil {
		t.Fatalf("parseTargetsFromYAML error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	m := targets["https://api.example.com"]
	if m.Alerts[0] != "console" {
		t.Errorf("expected alerts console, got %s", m.Alerts)
	}
}

func TestParseTargets_ListUnderTargets(t *testing.T) {
	yaml := []byte(
		"targets:\n" +
			"  - name: api\n" +
			"    url: https://api.example.com\n" +
			"    alerts: [console]\n",
	)
	targets, _, err := parseTargetsFromYAML(yaml)
	if err != nil {
		t.Fatalf("parseTargetsFromYAML error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	m := targets["https://api.example.com"]
	if len(m.Alerts) != 1 || m.Alerts[0] != "console" {
		t.Errorf("expected alerts [console], got %v", m.Alerts)
	}
}

func TestEdit_PersistsTargetsToState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "watch-state.yml")
	sm := NewStateManager(statePath)
	if err := sm.Load(); err != nil {
		t.Fatalf("load state error: %v", err)
	}

	// Simulate edited simplified YAML
	edited := []byte(
		"blog:\n" +
			"  url: https://example.com/blog\n" +
			"  alerts: [console]\n",
	)

	targets, _, err := parseTargetsFromYAML(edited)
	if err != nil {
		t.Fatalf("parse edited error: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 target parsed, got %d", len(targets))
	}
	for _, m := range targets {
		if err := sm.AddTarget(m); err != nil {
			t.Fatalf("AddTarget error: %v", err)
		}
	}
	// Verify persisted
	sm2 := NewStateManager(statePath)
	if err := sm2.Load(); err != nil {
		t.Fatalf("reload state error: %v", err)
	}
	got := sm2.ListTargets()
	if len(got) != 1 {
		t.Fatalf("expected 1 persisted target, got %d", len(got))
	}
}

// TestHooksCommand_EmptyStdin_Succeeds verifies that `echo "" | go run . hooks --stdin` succeeds
func TestHooksCommand_EmptyStdin_Succeeds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell-based test on Windows")
	}

	dir := t.TempDir()
	statePath := filepath.Join(dir, "watch-state.yml")

	// Run from the quick_watch package directory; tests execute in this dir
	cmd := exec.Command("/bin/bash", "-lc", "echo \"\" | go run . hooks --stdin --state "+statePath)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hooks command failed: %v\nOutput:\n%s", err, string(out))
	}
}
