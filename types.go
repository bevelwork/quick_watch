package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	qc "github.com/bevelwork/quick_color"
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
	Duration      int               `json:"duration" yaml:"duration,omitempty"` // For webhook targets: how long to stay "down" in seconds
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
	Name        string         `json:"name" yaml:"name"`
	Type        string         `json:"type" yaml:"type"` // "console" or "slack"
	Enabled     bool           `json:"enabled" yaml:"enabled"`
	Settings    map[string]any `json:"settings" yaml:"settings"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
}

// ConsoleNotifierSettings represents console notifier settings
type ConsoleNotifierSettings struct {
	Style string `json:"style" yaml:"style"`                   // "plain" or "stylized"
	Color bool   `json:"color" yaml:"color"`                   // enable/disable colors
	Bold  bool   `json:"bold,omitempty" yaml:"bold,omitempty"` // optional explicit bold toggle
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
	Type      string         `json:"type"`
	Target    string         `json:"target"`
	Message   string         `json:"message"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

// TargetState represents the current state of a target
type TargetState struct {
	Target                 *Target
	IsDown                 bool
	DownSince              *time.Time
	LastCheck              *CheckResult
	CheckStrategy          CheckStrategy
	AlertStrategies        []AlertStrategy
	SizeHistory            []int64 // Track response sizes for change detection
	CurrentAckToken        string  // Current acknowledgement token for active alert
	AcknowledgedBy         string  // Who acknowledged (from request metadata)
	AcknowledgedAt         *time.Time
	AcknowledgementNote    string      // Optional note from acknowledger
	AcknowledgementContact string      // Contact information (Slack, Zoom, phone, etc.)
	RecoveryTimer          *time.Timer // Timer for auto-recovery (webhook targets with duration)
	RecoveryTime           *time.Time  // When auto-recovery is scheduled
	FailureCount           int         // Number of consecutive failures
	LastAlertTime          *time.Time  // Time of the last alert sent
}

// TargetEngine represents the core targeting engine
// HookState tracks the state of a hook acknowledgement
type HookState struct {
	HookName               string
	Message                string
	TriggeredAt            time.Time
	AckToken               string
	AcknowledgedAt         *time.Time
	AcknowledgedBy         string
	AcknowledgementNote    string
	AcknowledgementContact string
}

type TargetEngine struct {
	targets                []*TargetState
	config                 *TargetConfig
	checkStrategies        map[string]CheckStrategy
	alertStrategies        map[string]AlertStrategy
	notificationStrategies map[string]NotificationStrategy
	ackTokenMap            map[string]*TargetState // Maps acknowledgement tokens to target states
	hookAckTokenMap        map[string]*HookState   // Maps acknowledgement tokens to hook states
	ackMutex               sync.RWMutex            // Protects ackTokenMap and hookAckTokenMap
	serverAddress          string                  // Server address for generating acknowledgement URLs
	acksEnabled            bool                    // Whether acknowledgements are enabled
}

// NewTargetEngine creates a new targeting engine
func NewTargetEngine(config *TargetConfig, stateManager *StateManager) *TargetEngine {
	engine := &TargetEngine{
		config:                 config,
		checkStrategies:        make(map[string]CheckStrategy),
		alertStrategies:        make(map[string]AlertStrategy),
		notificationStrategies: make(map[string]NotificationStrategy),
		ackTokenMap:            make(map[string]*TargetState),
		hookAckTokenMap:        make(map[string]*HookState),
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
	e.checkStrategies["webhook"] = NewWebhookCheckStrategy()

	// Alert strategies - register default console (stylized + color)
	e.alertStrategies["console"] = NewConsoleAlertStrategy()

	// Register notifier-based strategies if stateManager is provided
	if stateManager != nil {
		notifiers := stateManager.GetAlerts()
		for name, notifier := range notifiers {
			if notifier.Enabled {
				switch notifier.Type {
				case "slack":
					if webhookURL, ok := notifier.Settings["webhook_url"].(string); ok && webhookURL != "" {
						debug := false
						if d, ok := notifier.Settings["debug"].(bool); ok {
							debug = d
						}
						e.alertStrategies[name] = NewSlackAlertStrategyWithDebug(webhookURL, debug)
						// Register a notification strategy with the same name for hooks
						e.notificationStrategies[name] = NewSlackNotificationStrategy(webhookURL)
					}
				case "email":
					// expected settings: smtp_host, smtp_port, username, password_env, to, debug (optional)
					host, _ := notifier.Settings["smtp_host"].(string)
					to, _ := notifier.Settings["to"].(string)
					username, _ := notifier.Settings["username"].(string)
					passwordEnv, _ := notifier.Settings["password_env"].(string)
					debug := false
					if d, ok := notifier.Settings["debug"].(bool); ok {
						debug = d
					}
					var port int
					if v, ok := notifier.Settings["smtp_port"].(int); ok {
						port = v
					} else if vf, ok := notifier.Settings["smtp_port"].(float64); ok {
						port = int(vf)
					}
					if strings.TrimSpace(host) != "" && port > 0 && strings.TrimSpace(username) != "" && strings.TrimSpace(to) != "" && strings.TrimSpace(passwordEnv) != "" {
						pwd := os.Getenv(passwordEnv)
						if strings.TrimSpace(pwd) == "" {
							fmt.Printf("%s email notifier '%s' requires env %s to be set\n", qc.Colorize("❌ Error:", qc.ColorRed), name, passwordEnv)
							os.Exit(1)
						}
						e.alertStrategies[name] = NewEmailAlertStrategyWithDebug(host, port, username, pwd, to, debug)
						e.notificationStrategies[name] = NewEmailNotificationStrategy(host, port, username, pwd, to)
					}
				case "file":
					// expected settings: file_path (string), debug (optional bool), max_size_before_compress (optional int/float in MB)
					filePath, _ := notifier.Settings["file_path"].(string)
					debug := false
					if d, ok := notifier.Settings["debug"].(bool); ok {
						debug = d
					}

					// Read max_size_before_compress (in MB)
					var maxSizeMB int64 = 0
					if maxSize, ok := notifier.Settings["max_size_before_compress"]; ok {
						switch v := maxSize.(type) {
						case int:
							maxSizeMB = int64(v)
						case int64:
							maxSizeMB = v
						case float64:
							maxSizeMB = int64(v)
						}
					}

					if strings.TrimSpace(filePath) != "" {
						if maxSizeMB > 0 {
							e.alertStrategies[name] = NewFileAlertStrategyWithRotation(filePath, debug, maxSizeMB)
						} else {
							e.alertStrategies[name] = NewFileAlertStrategyWithDebug(filePath, debug)
						}
					}
				case "console":
					// Respect console notifier settings (style/color)
					style := "stylized"
					color := true
					if s, ok := notifier.Settings["style"].(string); ok && s != "" {
						style = s
					}
					if c, ok := notifier.Settings["color"].(bool); ok {
						color = c
					}
					e.alertStrategies[name] = NewConsoleAlertStrategyWithSettings(style, color)
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

	// Notification strategies
	e.notificationStrategies["console"] = NewConsoleNotificationStrategy()
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
		// Just went down - send initial alert
		now := time.Now()
		state.DownSince = &now
		state.FailureCount = 1
		state.LastAlertTime = &now

		// Set alert count in result for display
		result.AlertCount = state.FailureCount

		// Generate acknowledgement token if enabled and not already acknowledged
		var ackURL string
		if e.acksEnabled && state.AcknowledgedAt == nil {
			token := e.GenerateAckToken(state)
			ackURL = e.GetAcknowledgementURL(token)
		}

		for _, strat := range state.AlertStrategies {
			if ackSender, ok := strat.(AcknowledgementAwareAlert); ok && ackURL != "" {
				ackSender.SendAlertWithAck(ctx, state.Target, result, ackURL)
			} else {
				strat.SendAlert(ctx, state.Target, result)
			}
		}
	} else if result.Success && wasDown {
		// Just came back up - clear acknowledgement and reset counters
		e.ClearAcknowledgement(state)
		state.DownSince = nil
		state.FailureCount = 0
		state.LastAlertTime = nil
		for _, strat := range state.AlertStrategies {
			strat.SendAllClear(ctx, state.Target, result)
		}
	} else if !result.Success && wasDown {
		// Still down - check if we should send another alert (only if not acknowledged)
		if state.AcknowledgedAt == nil {
			// Calculate exponential backoff based on how many alerts we've already sent
			// Formula: 5 * 2^(FailureCount-1) seconds
			// FailureCount=1 -> 5s, FailureCount=2 -> 10s, FailureCount=3 -> 20s, etc.
			backoffSeconds := 5 * (1 << uint(state.FailureCount-1))
			backoffDuration := time.Duration(backoffSeconds) * time.Second

			// Check if enough time has passed since last alert
			if state.LastAlertTime != nil && time.Since(*state.LastAlertTime) >= backoffDuration {
				// Time to send another alert
				now := time.Now()
				state.LastAlertTime = &now
				state.FailureCount++ // Increment only when we actually send an alert

				// Set alert count in result for display
				result.AlertCount = state.FailureCount

				// Generate or reuse acknowledgement token
				var ackURL string
				if e.acksEnabled {
					if state.CurrentAckToken == "" {
						token := e.GenerateAckToken(state)
						ackURL = e.GetAcknowledgementURL(token)
					} else {
						ackURL = e.GetAcknowledgementURL(state.CurrentAckToken)
					}
				}

				for _, strat := range state.AlertStrategies {
					if ackSender, ok := strat.(AcknowledgementAwareAlert); ok && ackURL != "" {
						ackSender.SendAlertWithAck(ctx, state.Target, result, ackURL)
					} else {
						strat.SendAlert(ctx, state.Target, result)
					}
				}
			}
		}
		// If acknowledged, don't send any more alerts until service recovers
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

// SetAcknowledgementConfig configures acknowledgement settings
func (e *TargetEngine) SetAcknowledgementConfig(serverAddress string, enabled bool) {
	e.serverAddress = serverAddress
	e.acksEnabled = enabled
}

// GetAcknowledgementURL returns the full acknowledgement URL for a token
func (e *TargetEngine) GetAcknowledgementURL(token string) string {
	if e.serverAddress == "" {
		return fmt.Sprintf("/api/acknowledge/%s", token)
	}
	return fmt.Sprintf("%s/api/acknowledge/%s", e.serverAddress, token)
}

// GenerateAckToken generates and stores an acknowledgement token for a target
func (e *TargetEngine) GenerateAckToken(state *TargetState) string {
	e.ackMutex.Lock()
	defer e.ackMutex.Unlock()

	// Generate a simple token based on target URL and timestamp
	token := fmt.Sprintf("%x", time.Now().UnixNano())

	// Store the mapping
	e.ackTokenMap[token] = state
	state.CurrentAckToken = token

	return token
}

// AcknowledgeAlert acknowledges an alert by token
func (e *TargetEngine) AcknowledgeAlert(token, acknowledgedBy, note, contact string) (*TargetState, error) {
	e.ackMutex.Lock()
	defer e.ackMutex.Unlock()

	state, exists := e.ackTokenMap[token]
	if !exists {
		return nil, fmt.Errorf("invalid or expired acknowledgement token")
	}

	// Mark as acknowledged (or update existing acknowledgement)
	now := time.Now()
	if state.AcknowledgedAt == nil {
		state.AcknowledgedAt = &now
	}

	// Update fields (allow updates to acknowledgement info)
	if acknowledgedBy != "" {
		state.AcknowledgedBy = acknowledgedBy
	}
	if note != "" {
		state.AcknowledgementNote = note
	}
	if contact != "" {
		state.AcknowledgementContact = contact
	}

	// Keep token in map so we can detect duplicate acknowledgements
	// Token will be cleared when alert is resolved

	return state, nil
}

// ClearAcknowledgement clears acknowledgement when alert is resolved
func (e *TargetEngine) ClearAcknowledgement(state *TargetState) {
	e.ackMutex.Lock()
	defer e.ackMutex.Unlock()

	// Remove token from map if it exists
	if state.CurrentAckToken != "" {
		delete(e.ackTokenMap, state.CurrentAckToken)
		state.CurrentAckToken = ""
	}

	// Clear acknowledgement info
	state.AcknowledgedAt = nil
	state.AcknowledgedBy = ""
	state.AcknowledgementNote = ""
	state.AcknowledgementContact = ""
}

// TriggerWebhookTarget triggers a webhook target to go "down" and optionally auto-recover
func (e *TargetEngine) TriggerWebhookTarget(targetName string, message string, duration int) (*TargetState, error) {
	// Find the target by name
	var state *TargetState
	for _, s := range e.targets {
		if s.Target.Name == targetName || s.Target.URL == targetName {
			state = s
			break
		}
	}

	if state == nil {
		return nil, fmt.Errorf("target not found: %s", targetName)
	}

	// Verify it's a webhook target
	if state.Target.CheckStrategy != "webhook" {
		return nil, fmt.Errorf("target %s is not a webhook target (check_strategy must be 'webhook')", targetName)
	}

	// Cancel any existing recovery timer
	if state.RecoveryTimer != nil {
		state.RecoveryTimer.Stop()
		state.RecoveryTimer = nil
		state.RecoveryTime = nil
	}

	// Mark as down
	now := time.Now()
	state.IsDown = true
	state.DownSince = &now
	state.FailureCount = 1
	state.LastAlertTime = &now

	// Create check result for the triggered alert
	state.LastCheck = &CheckResult{
		Success:      false,
		Error:        message,
		ResponseTime: 0,
		Timestamp:    now,
	}

	// Use duration from trigger, or fall back to target's duration
	actualDuration := duration
	if actualDuration == 0 && state.Target.Duration > 0 {
		actualDuration = state.Target.Duration
	}

	// Set up auto-recovery if duration is specified
	if actualDuration > 0 {
		recoveryTime := now.Add(time.Duration(actualDuration) * time.Second)
		state.RecoveryTime = &recoveryTime

		state.RecoveryTimer = time.AfterFunc(time.Duration(actualDuration)*time.Second, func() {
			e.RecoverWebhookTarget(state)
		})
	}

	// Generate acknowledgement token if enabled and not already acknowledged
	ctx := context.Background()
	var ackURL string
	if e.acksEnabled && state.AcknowledgedAt == nil {
		token := e.GenerateAckToken(state)
		ackURL = e.GetAcknowledgementURL(token)
	}

	// Send alerts
	for _, strat := range state.AlertStrategies {
		if ackSender, ok := strat.(AcknowledgementAwareAlert); ok && ackURL != "" {
			ackSender.SendAlertWithAck(ctx, state.Target, state.LastCheck, ackURL)
		} else {
			strat.SendAlert(ctx, state.Target, state.LastCheck)
		}
	}

	return state, nil
}

// RecoverWebhookTarget recovers a webhook target from "down" state
func (e *TargetEngine) RecoverWebhookTarget(state *TargetState) {
	if !state.IsDown {
		return
	}

	// Clear acknowledgement
	e.ClearAcknowledgement(state)

	// Mark as up
	state.IsDown = false
	state.DownSince = nil
	state.RecoveryTimer = nil
	state.RecoveryTime = nil
	state.FailureCount = 0
	state.LastAlertTime = nil

	// Create recovery check result
	state.LastCheck = &CheckResult{
		Success:      true,
		StatusCode:   200,
		ResponseTime: 0,
		Timestamp:    time.Now(),
	}

	// Send all-clear notifications
	ctx := context.Background()
	for _, strat := range state.AlertStrategies {
		strat.SendAllClear(ctx, state.Target, state.LastCheck)
	}
}

// GetTargetByName finds a target by name or URL
func (e *TargetEngine) GetTargetByName(name string) *TargetState {
	for _, state := range e.targets {
		if state.Target.Name == name || state.Target.URL == name {
			return state
		}
	}
	return nil
}
