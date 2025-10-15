package main

import (
	"context"
	"encoding/json"
	"time"
)

// Monitor represents a monitoring target
type Monitor struct {
	Name          string            `json:"name"`
	URL           string            `json:"url"`
	Method        string            `json:"method"`
	Headers       map[string]string `json:"headers"`
	Threshold     int               `json:"threshold"`    // seconds
	StatusCodes   []string          `json:"status_codes"` // List of acceptable status codes (e.g., ["2**", "302"])
	CheckStrategy string            `json:"check_strategy"`
	AlertStrategy string            `json:"alert_strategy"`
}

// MonitorConfig represents the configuration for monitors
type MonitorConfig struct {
	Monitors   []Monitor      `json:"monitors"`
	Webhook    WebhookConfig  `json:"webhook,omitempty"`
	Strategies StrategyConfig `json:"strategies,omitempty"`
}

// WebhookConfig represents webhook server configuration
type WebhookConfig struct {
	Port int    `json:"port"`
	Path string `json:"path"`
}

// StrategyConfig represents strategy configuration
type StrategyConfig struct {
	Check        map[string]json.RawMessage `json:"check,omitempty"`
	Alert        map[string]json.RawMessage `json:"alert,omitempty"`
	Notification map[string]json.RawMessage `json:"notification,omitempty"`
}

// WebhookNotification represents an incoming webhook notification
type WebhookNotification struct {
	Type      string                 `json:"type"`
	Monitor   string                 `json:"monitor"`
	Message   string                 `json:"message"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// MonitorState represents the current state of a monitor
type MonitorState struct {
	Monitor       *Monitor
	IsDown        bool
	DownSince     *time.Time
	LastCheck     *CheckResult
	CheckStrategy CheckStrategy
	AlertStrategy AlertStrategy
}

// MonitoringEngine represents the core monitoring engine
type MonitoringEngine struct {
	monitors               []*MonitorState
	config                 *MonitorConfig
	checkStrategies        map[string]CheckStrategy
	alertStrategies        map[string]AlertStrategy
	notificationStrategies map[string]NotificationStrategy
}

// NewMonitoringEngine creates a new monitoring engine
func NewMonitoringEngine(config *MonitorConfig) *MonitoringEngine {
	engine := &MonitoringEngine{
		config:                 config,
		checkStrategies:        make(map[string]CheckStrategy),
		alertStrategies:        make(map[string]AlertStrategy),
		notificationStrategies: make(map[string]NotificationStrategy),
	}

	// Register default strategies
	engine.registerDefaultStrategies()

	// Initialize monitors
	engine.initializeMonitors()

	return engine
}

// registerDefaultStrategies registers the default strategies
func (e *MonitoringEngine) registerDefaultStrategies() {
	// Check strategies
	e.checkStrategies["http"] = NewHTTPCheckStrategy()

	// Alert strategies
	e.alertStrategies["console"] = NewConsoleAlertStrategy()

	// Notification strategies
	e.notificationStrategies["console"] = NewConsoleNotificationStrategy()
}

// initializeMonitors initializes monitors from configuration
func (e *MonitoringEngine) initializeMonitors() {
	for _, monitor := range e.config.Monitors {
		state := &MonitorState{
			Monitor: &monitor,
			IsDown:  false,
		}

		// Set check strategy
		if strategy, exists := e.checkStrategies[monitor.CheckStrategy]; exists {
			state.CheckStrategy = strategy
		} else {
			state.CheckStrategy = e.checkStrategies["http"] // default
		}

		// Set alert strategy
		if strategy, exists := e.alertStrategies[monitor.AlertStrategy]; exists {
			state.AlertStrategy = strategy
		} else {
			state.AlertStrategy = e.alertStrategies["console"] // default
		}

		e.monitors = append(e.monitors, state)
	}
}

// Start begins monitoring all configured monitors
func (e *MonitoringEngine) Start(ctx context.Context) error {
	// Start monitoring loop for each monitor
	for _, state := range e.monitors {
		go e.monitorLoop(ctx, state)
	}

	return nil
}

// monitorLoop runs the monitoring loop for a single monitor
func (e *MonitoringEngine) monitorLoop(ctx context.Context, state *MonitorState) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.checkMonitor(ctx, state)
		}
	}
}

// checkMonitor performs a single check for a monitor
func (e *MonitoringEngine) checkMonitor(ctx context.Context, state *MonitorState) {
	result, err := state.CheckStrategy.Check(ctx, state.Monitor)
	if err != nil {
		// Handle check error
		result = &CheckResult{
			Success:   false,
			Error:     err.Error(),
			Timestamp: time.Now(),
		}
	}

	state.LastCheck = result

	// Update state based on result
	wasDown := state.IsDown
	state.IsDown = !result.Success

	if !result.Success && !wasDown {
		// Just went down
		now := time.Now()
		state.DownSince = &now
		state.AlertStrategy.SendAlert(ctx, state.Monitor, result)
	} else if result.Success && wasDown {
		// Just came back up
		state.DownSince = nil
		state.AlertStrategy.SendAllClear(ctx, state.Monitor, result)
	} else if !result.Success && wasDown {
		// Still down - check if we should send another alert
		if state.DownSince != nil {
			downDuration := time.Since(*state.DownSince)
			threshold := time.Duration(state.Monitor.Threshold) * time.Second
			if downDuration >= threshold {
				// Send another alert if we've been down for the threshold
				state.AlertStrategy.SendAlert(ctx, state.Monitor, result)
			}
		}
	}
}

// HandleWebhookNotification handles incoming webhook notifications
func (e *MonitoringEngine) HandleWebhookNotification(ctx context.Context, notification *WebhookNotification) error {
	// Find the appropriate notification strategy
	// For now, use console strategy
	if strategy, exists := e.notificationStrategies["console"]; exists {
		return strategy.HandleNotification(ctx, notification)
	}

	return nil
}

// GetMonitorStatus returns the current status of all monitors
func (e *MonitoringEngine) GetMonitorStatus() []*MonitorState {
	return e.monitors
}
