package main

import (
	"context"
	"testing"
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
