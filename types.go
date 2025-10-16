package main

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

// Monitor represents a monitoring target
type Monitor struct {
	Name          string            `json:"name" yaml:"name"`
	URL           string            `json:"url" yaml:"url"`
	Method        string            `json:"method" yaml:"method,omitempty"`
	Headers       map[string]string `json:"headers" yaml:"headers,omitempty"`
	Threshold     int               `json:"threshold" yaml:"threshold,omitempty"`       // seconds
	StatusCodes   []string          `json:"status_codes" yaml:"status_codes,omitempty"` // List of acceptable status codes (e.g., ["2**", "302"])
	SizeAlerts    SizeAlertConfig   `json:"size_alerts" yaml:"size_alerts,omitempty"`   // Page size change detection
	CheckStrategy string            `json:"check_strategy" yaml:"check_strategy,omitempty"`
	AlertStrategy string            `json:"alert_strategy" yaml:"alert_strategy,omitempty"`
}

// SizeAlertConfig represents configuration for page size change detection
type SizeAlertConfig struct {
	Enabled     bool    `json:"enabled" yaml:"enabled"`           // Enable size change detection (default: true)
	HistorySize int     `json:"history_size" yaml:"history_size"` // Number of responses to track (default: 100)
	Threshold   float64 `json:"threshold" yaml:"threshold"`       // Percentage change threshold (default: 0.5 = 50%)
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

// NotifierConfig represents a notification configuration
type NotifierConfig struct {
	Name        string                 `json:"name" yaml:"name"`
	Type        string                 `json:"type" yaml:"type"` // "console" or "slack"
	Enabled     bool                   `json:"enabled" yaml:"enabled"`
	Settings    map[string]interface{} `json:"settings" yaml:"settings"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
}

// ConsoleNotifierSettings represents console notifier settings
type ConsoleNotifierSettings struct {
	Style string `json:"style" yaml:"style"` // "plain" or "stylized"
	Color bool   `json:"color" yaml:"color"` // enable/disable colors
}

// SlackNotifierSettings represents Slack notifier settings
type SlackNotifierSettings struct {
	WebhookURL string `json:"webhook_url" yaml:"webhook_url"`
	Channel    string `json:"channel,omitempty" yaml:"channel,omitempty"`
	Username   string `json:"username,omitempty" yaml:"username,omitempty"`
	IconEmoji  string `json:"icon_emoji,omitempty" yaml:"icon_emoji,omitempty"`
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
	SizeHistory   []int64 // Track response sizes for change detection
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
func NewMonitoringEngine(config *MonitorConfig, stateManager *StateManager) *MonitoringEngine {
	engine := &MonitoringEngine{
		config:                 config,
		checkStrategies:        make(map[string]CheckStrategy),
		alertStrategies:        make(map[string]AlertStrategy),
		notificationStrategies: make(map[string]NotificationStrategy),
	}

	// Register default strategies
	engine.registerDefaultStrategies(stateManager)

	// Initialize monitors
	engine.initializeMonitors()

	return engine
}

// registerDefaultStrategies registers the default strategies
func (e *MonitoringEngine) registerDefaultStrategies(stateManager *StateManager) {
	// Check strategies
	e.checkStrategies["http"] = NewHTTPCheckStrategy()

	// Alert strategies - register default console
	e.alertStrategies["console"] = NewConsoleAlertStrategy()

	// Register notifier-based strategies if stateManager is provided
	if stateManager != nil {
		notifiers := stateManager.GetNotifiers()
		for name, notifier := range notifiers {
			if notifier.Enabled {
				switch notifier.Type {
				case "slack":
					if webhookURL, ok := notifier.Settings["webhook_url"].(string); ok && webhookURL != "" {
						e.alertStrategies[name] = NewSlackAlertStrategy(webhookURL)
					}
				case "console":
					// Console is already registered as default
					if name != "console" {
						e.alertStrategies[name] = NewConsoleAlertStrategy()
					}
				}
			}
		}
	}

	// Legacy Slack strategy registration for backward compatibility
	if e.config.Strategies.Alert != nil {
		if slackConfig, exists := e.config.Strategies.Alert["slack"]; exists {
			var slackData map[string]interface{}
			if err := json.Unmarshal(slackConfig, &slackData); err == nil {
				if webhookURL, ok := slackData["webhook_url"].(string); ok && webhookURL != "" {
					e.alertStrategies["slack"] = NewSlackAlertStrategy(webhookURL)
				}
			}
		}
	}

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

	// Check for size changes if enabled and we have a response size
	if result.Success && result.ResponseSize > 0 {
		if checkSizeChange(state, result.ResponseSize) {
			// Calculate average size for the alert
			previousResponses := state.SizeHistory[:len(state.SizeHistory)-1]
			var sum int64
			for _, size := range previousResponses {
				sum += size
			}
			avgSize := float64(sum) / float64(len(previousResponses))
			changePercent := math.Abs(float64(result.ResponseSize)-avgSize) / avgSize

			// Send size change alert
			if consoleAlert, ok := state.AlertStrategy.(*ConsoleAlertStrategy); ok {
				consoleAlert.SendSizeChangeAlert(ctx, state.Monitor, result, avgSize, changePercent)
			}
		}
	}

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
