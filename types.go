package main

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

// Target represents a targeting target
type Target struct {
	Name          string            `json:"name" yaml:"name"`
	URL           string            `json:"url" yaml:"url"`
	Method        string            `json:"method" yaml:"method,omitempty"`
	Headers       map[string]string `json:"headers" yaml:"headers,omitempty"`
	Threshold     int               `json:"threshold" yaml:"threshold,omitempty"`       // seconds (default: 30s)
	StatusCodes   []string          `json:"status_codes" yaml:"status_codes,omitempty"` // List of acceptable status codes (e.g., ["2**", "302"])
	SizeAlerts    SizeAlertConfig   `json:"size_alerts" yaml:"size_alerts,omitempty"`   // Page size change detection
	CheckStrategy string            `json:"check_strategy" yaml:"check_strategy,omitempty"`
	// Preferred field supporting multiple alert strategies
	Alerts []string `json:"alerts" yaml:"alerts,omitempty"`
	// Legacy single alert strategy name (kept for backward compatibility)
	AlertStrategy string `json:"alert_strategy,omitempty" yaml:"alert_strategy,omitempty"`
}

// SizeAlertConfig represents configuration for page size change detection
type SizeAlertConfig struct {
	Enabled     bool    `json:"enabled" yaml:"enabled"`           // Enable size change detection (default: true)
	HistorySize int     `json:"history_size" yaml:"history_size"` // Number of responses to track (default: 100)
	Threshold   float64 `json:"threshold" yaml:"threshold"`       // Percentage change threshold (default: 0.5 = 50%)
}

// TargetConfig represents the configuration for targets
type TargetConfig struct {
	Targets    []Target       `json:"targets"`
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

// Hook represents a named incoming HTTP hook route that can trigger notifications
type Hook struct {
	Name     string            `json:"name" yaml:"name"`
	Path     string            `json:"path" yaml:"path"`
	Methods  []string          `json:"methods" yaml:"methods,omitempty"`
	Alerts   []string          `json:"alerts" yaml:"alerts,omitempty"` // notifier names (e.g., slack, console)
	Auth     HookAuth          `json:"auth" yaml:"auth,omitempty"`
	Message  string            `json:"message" yaml:"message,omitempty"`
	Metadata map[string]string `json:"metadata" yaml:"metadata,omitempty"`
}

// HookAuth defines optional authentication for a hook route
type HookAuth struct {
	// If set, require Authorization: Bearer <Token>
	BearerToken string `json:"bearer_token" yaml:"bearer_token,omitempty"`
	// If set, require HTTP Basic Auth
	Username string `json:"username" yaml:"username,omitempty"`
	Password string `json:"password" yaml:"password,omitempty"`
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
	Target    string                 `json:"target"`
	Message   string                 `json:"message"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// TargetState represents the current state of a target
type TargetState struct {
	Target          *Target
	IsDown          bool
	DownSince       *time.Time
	LastCheck       *CheckResult
	CheckStrategy   CheckStrategy
	AlertStrategies []AlertStrategy
	SizeHistory     []int64 // Track response sizes for change detection
}

// TargetEngine represents the core targeting engine
type TargetEngine struct {
	targets                []*TargetState
	config                 *TargetConfig
	checkStrategies        map[string]CheckStrategy
	alertStrategies        map[string]AlertStrategy
	notificationStrategies map[string]NotificationStrategy
}

// NewTargetEngine creates a new targeting engine
func NewTargetEngine(config *TargetConfig, stateManager *StateManager) *TargetEngine {
	engine := &TargetEngine{
		config:                 config,
		checkStrategies:        make(map[string]CheckStrategy),
		alertStrategies:        make(map[string]AlertStrategy),
		notificationStrategies: make(map[string]NotificationStrategy),
	}

	// Register default strategies
	engine.registerDefaultStrategies(stateManager)

	// Initialize targets
	engine.initializeTargets()

	return engine
}

// registerDefaultStrategies registers the default strategies
func (e *TargetEngine) registerDefaultStrategies(stateManager *StateManager) {
	// Check strategies
	e.checkStrategies["http"] = NewHTTPCheckStrategy()

	// Alert strategies - register default console
	e.alertStrategies["console"] = NewConsoleAlertStrategy()

	// Register notifier-based strategies if stateManager is provided
	if stateManager != nil {
		notifiers := stateManager.GetAlerts()
		for name, notifier := range notifiers {
			if notifier.Enabled {
				switch notifier.Type {
				case "slack":
					if webhookURL, ok := notifier.Settings["webhook_url"].(string); ok && webhookURL != "" {
						e.alertStrategies[name] = NewSlackAlertStrategy(webhookURL)
						// Register a notification strategy with the same name for hooks
						e.notificationStrategies[name] = NewSlackNotificationStrategy(webhookURL)
					}
				case "console":
					// Console is already registered as default
					if name != "console" {
						e.alertStrategies[name] = NewConsoleAlertStrategy()
					}
					e.notificationStrategies[name] = NewConsoleNotificationStrategy()
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

	// Ensure default console notification exists
	if _, ok := e.notificationStrategies["console"]; !ok {
		e.notificationStrategies["console"] = NewConsoleNotificationStrategy()
	}
}

// initializeTargets initializes targets from configuration
func (e *TargetEngine) initializeTargets() {
	for _, target := range e.config.Targets {
		state := &TargetState{
			Target: &target,
			IsDown: false,
		}

		// Set check strategy
		if strategy, exists := e.checkStrategies[target.CheckStrategy]; exists {
			state.CheckStrategy = strategy
		} else {
			state.CheckStrategy = e.checkStrategies["http"] // default
		}

		// Set alert strategies (supports multiple). Prefer new Alerts slice, fallback to legacy AlertStrategy.
		strategyNames := target.Alerts
		if len(strategyNames) == 0 {
			if target.AlertStrategy != "" {
				strategyNames = []string{target.AlertStrategy}
			} else {
				strategyNames = []string{"console"}
			}
		}
		for _, name := range strategyNames {
			if strategy, exists := e.alertStrategies[name]; exists {
				state.AlertStrategies = append(state.AlertStrategies, strategy)
			}
		}

		e.targets = append(e.targets, state)
	}
}

// Start begins targeting all configured targets
func (e *TargetEngine) Start(ctx context.Context) error {
	// Start targeting loop for each target
	for _, state := range e.targets {
		go e.targetLoop(ctx, state)
	}

	return nil
}

// targetLoop runs the targeting loop for a single target
func (e *TargetEngine) targetLoop(ctx context.Context, state *TargetState) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.checkTarget(ctx, state)
		}
	}
}

// checkTarget performs a single check for a target
func (e *TargetEngine) checkTarget(ctx context.Context, state *TargetState) {
	result, err := state.CheckStrategy.Check(ctx, state.Target)
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

			// Send size change alert to console strategies
			for _, strat := range state.AlertStrategies {
				if consoleAlert, ok := strat.(*ConsoleAlertStrategy); ok {
					consoleAlert.SendSizeChangeAlert(ctx, state.Target, result, avgSize, changePercent)
				}
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
		for _, strat := range state.AlertStrategies {
			strat.SendAlert(ctx, state.Target, result)
		}
	} else if result.Success && wasDown {
		// Just came back up
		state.DownSince = nil
		for _, strat := range state.AlertStrategies {
			strat.SendAllClear(ctx, state.Target, result)
		}
	} else if !result.Success && wasDown {
		// Still down - check if we should send another alert
		if state.DownSince != nil {
			downDuration := time.Since(*state.DownSince)
			threshold := time.Duration(state.Target.Threshold) * time.Second
			if downDuration >= threshold {
				// Send another alert if we've been down for the threshold
				for _, strat := range state.AlertStrategies {
					strat.SendAlert(ctx, state.Target, result)
				}
			}
		}
	}
}

// HandleWebhookNotification handles incoming webhook notifications
func (e *TargetEngine) HandleWebhookNotification(ctx context.Context, notification *WebhookNotification) error {
	// Find the appropriate notification strategy
	// For now, use console strategy
	if strategy, exists := e.notificationStrategies["console"]; exists {
		return strategy.HandleNotification(ctx, notification)
	}

	return nil
}

// GetTargetStatus returns the current status of all targets
func (e *TargetEngine) GetTargetStatus() []*TargetState {
	return e.targets
}
