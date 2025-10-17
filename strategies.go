package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	qc "github.com/bevelwork/quick_color"
)

// CheckResult represents the result of a health check
type CheckResult struct {
	Success      bool          `json:"success"`
	StatusCode   int           `json:"status_code,omitempty"`
	ResponseTime time.Duration `json:"response_time"`
	ResponseSize int64         `json:"response_size,omitempty"`
	Error        string        `json:"error,omitempty"`
	Timestamp    time.Time     `json:"timestamp"`
}

// CheckStrategy defines the interface for health check strategies
type CheckStrategy interface {
	Check(ctx context.Context, target *Target) (*CheckResult, error)
	Name() string
}

// AlertStrategy defines the interface for alert strategies
type AlertStrategy interface {
	SendAlert(ctx context.Context, target *Target, result *CheckResult) error
	SendAllClear(ctx context.Context, target *Target, result *CheckResult) error
	Name() string
}

// NotificationStrategy defines the interface for handling incoming notifications
type NotificationStrategy interface {
	HandleNotification(ctx context.Context, notification *WebhookNotification) error
	Name() string
}

// HTTPCheckStrategy implements HTTP health checks
type HTTPCheckStrategy struct {
	client *http.Client
}

// NewHTTPCheckStrategy creates a new HTTP check strategy
func NewHTTPCheckStrategy() *HTTPCheckStrategy {
	return &HTTPCheckStrategy{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// isStatusCodeAllowed checks if a status code matches any of the allowed patterns
func isStatusCodeAllowed(statusCode int, allowedCodes []string) bool {
	// If no status codes specified, default to "*" (all codes)
	if len(allowedCodes) == 0 {
		allowedCodes = []string{"*"}
	}

	statusStr := fmt.Sprintf("%d", statusCode)

	for _, pattern := range allowedCodes {
		// Handle wildcard "*" - matches all status codes
		if pattern == "*" {
			return true
		}

		// Handle exact match
		if pattern == statusStr {
			return true
		}

		// Handle wildcard patterns like "2**", "3**", "4**", "5**"
		if len(pattern) == 4 && pattern[1:] == "**" {
			prefix := pattern[0]
			statusStr := fmt.Sprintf("%d", statusCode)
			if len(statusStr) >= 1 && statusStr[0] == prefix {
				return true
			}
		}

		// Handle range patterns like "200-299"
		if strings.Contains(pattern, "-") {
			parts := strings.Split(pattern, "-")
			if len(parts) == 2 {
				min, err1 := strconv.Atoi(parts[0])
				max, err2 := strconv.Atoi(parts[1])
				if err1 == nil && err2 == nil && statusCode >= min && statusCode <= max {
					return true
				}
			}
		}
	}

	return false
}

// checkSizeChange detects significant changes in response size
func checkSizeChange(state *TargetState, newSize int64) bool {
	if !state.Target.SizeAlerts.Enabled {
		return false
	}

	// Add new size to history
	state.SizeHistory = append(state.SizeHistory, newSize)

	// Keep only the last N responses
	historySize := state.Target.SizeAlerts.HistorySize
	if len(state.SizeHistory) > historySize {
		state.SizeHistory = state.SizeHistory[len(state.SizeHistory)-historySize:]
	}

	// Need at least 2 responses to detect change
	if len(state.SizeHistory) < 2 {
		return false
	}

	// Calculate average of previous responses (excluding the current one)
	previousResponses := state.SizeHistory[:len(state.SizeHistory)-1]
	var sum int64
	for _, size := range previousResponses {
		sum += size
	}
	avgSize := float64(sum) / float64(len(previousResponses))

	// Calculate percentage change
	change := math.Abs(float64(newSize)-avgSize) / avgSize

	// Check if change exceeds threshold
	return change >= state.Target.SizeAlerts.Threshold
}

// Check performs an HTTP health check
func (h *HTTPCheckStrategy) Check(ctx context.Context, target *Target) (*CheckResult, error) {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, target.Method, target.URL, nil)
	if err != nil {
		return &CheckResult{
			Success:   false,
			Error:     fmt.Sprintf("Failed to create request: %v", err),
			Timestamp: start,
		}, nil
	}

	// Add headers
	for key, value := range target.Headers {
		req.Header.Set(key, value)
	}

	resp, err := h.client.Do(req)
	responseTime := time.Since(start)

	if err != nil {
		return &CheckResult{
			Success:      false,
			Error:        fmt.Sprintf("Request failed: %v", err),
			ResponseTime: responseTime,
			Timestamp:    start,
		}, nil
	}
	defer resp.Body.Close()

	// Read response body to get size
	var responseSize int64
	if resp.Body != nil {
		// We'll estimate size from Content-Length header
		responseSize = resp.ContentLength
		if responseSize < 0 {
			responseSize = 0 // Unknown size
		}
	}

	// Check if status code matches allowed status codes
	success := isStatusCodeAllowed(resp.StatusCode, target.StatusCodes)

	return &CheckResult{
		Success:      success,
		StatusCode:   resp.StatusCode,
		ResponseTime: responseTime,
		ResponseSize: responseSize,
		Timestamp:    start,
	}, nil
}

// Name returns the strategy name
func (h *HTTPCheckStrategy) Name() string {
	return "http"
}

// ConsoleAlertStrategy implements console-based alerting
type ConsoleAlertStrategy struct {
	style string // "plain" or "stylized"
	color bool   // enable/disable color output
}

// NewConsoleAlertStrategy creates a new console alert strategy
func NewConsoleAlertStrategy() *ConsoleAlertStrategy {
	return &ConsoleAlertStrategy{style: "stylized", color: true}
}

// NewConsoleAlertStrategyWithSettings constructs a console alert strategy honoring settings
func NewConsoleAlertStrategyWithSettings(style string, color bool) *ConsoleAlertStrategy {
	if strings.TrimSpace(style) == "" {
		style = "stylized"
	}
	return &ConsoleAlertStrategy{style: style, color: color}
}

func (c *ConsoleAlertStrategy) format(text string, colorCode string, bold bool) string {
	s := text
	if c.color && colorCode != "" {
		s = qc.Colorize(s, colorCode)
	}
	if bold && strings.EqualFold(c.style, "stylized") {
		// Enable bold without resetting color; 22 disables bold only
		s = "\x1b[1m" + s + "\x1b[22m"
	}
	return s
}

// SendAlert sends an alert to the console
func (c *ConsoleAlertStrategy) SendAlert(ctx context.Context, target *Target, result *CheckResult) error {
	timestamp := result.Timestamp.Format("2006-01-02 15:04:05")
	title := c.format("🚨 ALERT:", qc.ColorRed, true)
	name := c.format(target.Name, qc.ColorRed, true)
	fmt.Printf("%s %s is DOWN - %s (Status: %d, Time: %v)\n",
		title,
		name,
		target.URL,
		result.StatusCode,
		result.ResponseTime)
	fmt.Printf("   %s %s\n", c.format("Target:", qc.ColorCyan, true), target.Name)
	fmt.Printf("   %s %s\n", c.format("URL:", qc.ColorCyan, true), target.URL)
	fmt.Printf("   %s %s\n", c.format("Time:", qc.ColorCyan, true), timestamp)
	fmt.Printf("   %s %v\n", c.format("Response Time:", qc.ColorCyan, true), result.ResponseTime)
	if result.ResponseSize > 0 {
		fmt.Printf("   %s %d bytes\n", c.format("Response Size:", qc.ColorCyan, true), result.ResponseSize)
	}
	fmt.Println()
	return nil
}

// SendAllClear sends an all-clear notification to the console
func (c *ConsoleAlertStrategy) SendAllClear(ctx context.Context, target *Target, result *CheckResult) error {
	timestamp := result.Timestamp.Format("2006-01-02 15:04:05")
	title := c.format("✅ ALL CLEAR:", qc.ColorGreen, true)
	name := c.format(target.Name, qc.ColorGreen, true)
	fmt.Printf("%s %s is UP - %s (Status: %d, Time: %v)\n",
		title,
		name,
		target.URL,
		result.StatusCode,
		result.ResponseTime)
	fmt.Printf("   %s %s\n", c.format("Target:", qc.ColorCyan, true), target.Name)
	fmt.Printf("   %s %s\n", c.format("URL:", qc.ColorCyan, true), target.URL)
	fmt.Printf("   %s %s\n", c.format("Time:", qc.ColorCyan, true), timestamp)
	fmt.Printf("   %s %v\n", c.format("Response Time:", qc.ColorCyan, true), result.ResponseTime)
	if result.ResponseSize > 0 {
		fmt.Printf("   %s %d bytes\n", c.format("Response Size:", qc.ColorCyan, true), result.ResponseSize)
	}
	fmt.Println()
	return nil
}

// SendSizeChangeAlert sends a size change alert to the console
func (c *ConsoleAlertStrategy) SendSizeChangeAlert(ctx context.Context, target *Target, result *CheckResult, avgSize float64, changePercent float64) error {
	timestamp := result.Timestamp.Format("2006-01-02 15:04:05")
	changeDirection := "increased"
	if float64(result.ResponseSize) < avgSize {
		changeDirection = "decreased"
	}

	fmt.Printf("%s %s response size %s significantly - %s (Size: %d bytes, Avg: %.0f bytes, Change: %.1f%%)\n",
		c.format("📏 SIZE ALERT:", qc.ColorYellow, true),
		c.format(target.Name, qc.ColorYellow, true),
		changeDirection,
		target.URL,
		result.ResponseSize,
		avgSize,
		changePercent*100)
	fmt.Printf("   %s %s\n", c.format("Target:", qc.ColorCyan, true), target.Name)
	fmt.Printf("   %s %s\n", c.format("URL:", qc.ColorCyan, true), target.URL)
	fmt.Printf("   %s %s\n", c.format("Time:", qc.ColorCyan, true), timestamp)
	fmt.Printf("   %s %d bytes\n", c.format("Current Size:", qc.ColorCyan, true), result.ResponseSize)
	fmt.Printf("   %s %.0f bytes\n", c.format("Average Size:", qc.ColorCyan, true), avgSize)
	fmt.Printf("   %s %.1f%%\n", c.format("Change:", qc.ColorCyan, true), changePercent*100)
	fmt.Println()
	return nil
}

// Name returns the strategy name
func (c *ConsoleAlertStrategy) Name() string {
	return "console"
}

// SendStartupMessage prints a stylized startup line to the console
func (c *ConsoleAlertStrategy) SendStartupMessage(version string, targetCount int) {
	title := c.format("🚀 Quick Watch", qc.ColorCyan, true)
	v := c.format(version, qc.ColorWhite, true)
	t := c.format(fmt.Sprintf("%d", targetCount), qc.ColorWhite, true)
	fmt.Printf("%s started - Version: %s, Targets: %s\n", title, v, t)
}

// WebhookAlertStrategy implements webhook-based alerting
type WebhookAlertStrategy struct {
	webhookURL string
	client     *http.Client
}

// NewWebhookAlertStrategy creates a new webhook alert strategy
func NewWebhookAlertStrategy(webhookURL string) *WebhookAlertStrategy {
	return &WebhookAlertStrategy{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendAlert sends an alert via webhook
func (w *WebhookAlertStrategy) SendAlert(ctx context.Context, target *Target, result *CheckResult) error {
	payload := map[string]interface{}{
		"type":          "alert",
		"target":        target.Name,
		"url":           target.URL,
		"status":        "down",
		"timestamp":     result.Timestamp,
		"error":         result.Error,
		"status_code":   result.StatusCode,
		"response_time": result.ResponseTime.String(),
	}
	return w.sendWebhook(ctx, payload)
}

// SendAllClear sends an all-clear notification via webhook
func (w *WebhookAlertStrategy) SendAllClear(ctx context.Context, target *Target, result *CheckResult) error {
	payload := map[string]interface{}{
		"type":          "all_clear",
		"target":        target.Name,
		"url":           target.URL,
		"status":        "up",
		"timestamp":     result.Timestamp,
		"status_code":   result.StatusCode,
		"response_time": result.ResponseTime.String(),
	}
	return w.sendWebhook(ctx, payload)
}

// sendWebhook sends a webhook notification
func (w *WebhookAlertStrategy) sendWebhook(ctx context.Context, payload map[string]interface{}) error {
	// This is a simplified implementation
	// In a real implementation, you'd marshal the payload to JSON and send it
	fmt.Printf("%s Sending notification to %s\n", qc.Colorize("📡 WEBHOOK:", qc.ColorBlue), w.webhookURL)
	fmt.Printf("   Payload: %+v\n", payload)
	return nil
}

// Name returns the strategy name
func (w *WebhookAlertStrategy) Name() string {
	return "webhook"
}

// SlackAlertStrategy implements Slack-based alerting
type SlackAlertStrategy struct {
	webhookURL string
	client     *http.Client
	debug      bool
}

// NewSlackAlertStrategy creates a new Slack alert strategy
func NewSlackAlertStrategy(webhookURL string) *SlackAlertStrategy {
	return &SlackAlertStrategy{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		debug: false,
	}
}

// NewSlackAlertStrategyWithDebug creates a new Slack alert strategy with debug option
func NewSlackAlertStrategyWithDebug(webhookURL string, debug bool) *SlackAlertStrategy {
	return &SlackAlertStrategy{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		debug: debug,
	}
}

// SendAlert sends an alert to Slack
func (s *SlackAlertStrategy) SendAlert(ctx context.Context, target *Target, result *CheckResult) error {
	message := fmt.Sprintf("🚨 *%s* is DOWN\n• URL: %s\n• Status: %d\n• Time: %v\n• Error: %s",
		target.Name, target.URL, result.StatusCode, result.ResponseTime, result.Error)

	payload := map[string]interface{}{
		"text":   message,
		"mrkdwn": true,
		"attachments": []map[string]interface{}{
			{
				"color":     "danger",
				"mrkdwn_in": []string{"fields"},
				"fields": []map[string]interface{}{
					{
						"title": "Target",
						"value": fmt.Sprintf("*%s*", target.Name),
						"short": true,
					},
					{
						"title": "URL",
						"value": fmt.Sprintf("<%s|%s>", target.URL, target.URL),
						"short": true,
					},
					{
						"title": "Status Code",
						"value": fmt.Sprintf("`%d`", result.StatusCode),
						"short": true,
					},
					{
						"title": "Response Time",
						"value": fmt.Sprintf("`%s`", result.ResponseTime.String()),
						"short": true,
					},
					{
						"title": "Timestamp",
						"value": fmt.Sprintf("<!date^%d^{date} {time}|%s>",
							result.Timestamp.Unix(),
							result.Timestamp.Format("2006-01-02 15:04:05")),
						"short": false,
					},
				},
			},
		},
	}

	return s.sendSlackWebhook(ctx, payload)
}

// SendAllClear sends an all-clear notification to Slack
func (s *SlackAlertStrategy) SendAllClear(ctx context.Context, target *Target, result *CheckResult) error {
	message := fmt.Sprintf("✅ *%s* is UP\n• URL: %s\n• Status: %d\n• Time: %v",
		target.Name, target.URL, result.StatusCode, result.ResponseTime)

	payload := map[string]interface{}{
		"text":   message,
		"mrkdwn": true,
		"attachments": []map[string]interface{}{
			{
				"color":     "good",
				"mrkdwn_in": []string{"fields"},
				"fields": []map[string]interface{}{
					{
						"title": "Target",
						"value": fmt.Sprintf("*%s*", target.Name),
						"short": true,
					},
					{
						"title": "URL",
						"value": fmt.Sprintf("<%s|%s>", target.URL, target.URL),
						"short": true,
					},
					{
						"title": "Status Code",
						"value": fmt.Sprintf("`%d`", result.StatusCode),
						"short": true,
					},
					{
						"title": "Response Time",
						"value": fmt.Sprintf("`%s`", result.ResponseTime.String()),
						"short": true,
					},
					{
						"title": "Timestamp",
						"value": fmt.Sprintf("<!date^%d^{date} {time}|%s>",
							result.Timestamp.Unix(),
							result.Timestamp.Format("2006-01-02 15:04:05")),
						"short": false,
					},
				},
			},
		},
	}

	return s.sendSlackWebhook(ctx, payload)
}

// sendSlackWebhook sends a notification to Slack
func (s *SlackAlertStrategy) sendSlackWebhook(ctx context.Context, payload map[string]interface{}) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack payload: %v", err)
	}

	if s.debug {
		fmt.Printf("🐛 SLACK DEBUG: Sending to %s\n", sanitizeSlackWebhookURL(s.webhookURL))
		fmt.Printf("🐛 SLACK DEBUG: Payload: %s\n", string(jsonData))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create Slack request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if s.debug {
		fmt.Printf("🐛 SLACK DEBUG: Request headers: %+v\n", req.Header)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		if s.debug {
			fmt.Printf("🐛 SLACK DEBUG: Request failed: %v\n", err)
		}
		return fmt.Errorf("failed to send Slack webhook: %v", err)
	}
	defer resp.Body.Close()

	if s.debug {
		fmt.Printf("🐛 SLACK DEBUG: Response status: %d\n", resp.StatusCode)
		fmt.Printf("🐛 SLACK DEBUG: Response headers: %+v\n", resp.Header)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	fmt.Printf("📡 SLACK: Sent notification to %s\n", sanitizeSlackWebhookURL(s.webhookURL))
	return nil
}

// SendStartupMessage sends a startup notification to Slack
func (s *SlackAlertStrategy) SendStartupMessage(ctx context.Context, version string, targetCount int) error {
	message := fmt.Sprintf("🚀 *Quick Watch* started successfully\n• Version: %s\n• Targets: %d\n• Timestamp: %s",
		version, targetCount, time.Now().Format("2006-01-02 15:04:05"))

	payload := map[string]interface{}{
		"text":   message,
		"mrkdwn": true,
		"attachments": []map[string]interface{}{
			{
				"color":     "good",
				"mrkdwn_in": []string{"fields"},
				"fields": []map[string]interface{}{
					{
						"title": "Service",
						"value": "*Quick Watch*",
						"short": true,
					},
					{
						"title": "Version",
						"value": fmt.Sprintf("`%s`", version),
						"short": true,
					},
					{
						"title": "Targets",
						"value": fmt.Sprintf("`%d`", targetCount),
						"short": true,
					},
					{
						"title": "Status",
						"value": "*Running*",
						"short": true,
					},
					{
						"title": "Startup Time",
						"value": fmt.Sprintf("<!date^%d^{date} {time}|%s>",
							time.Now().Unix(),
							time.Now().Format("2006-01-02 15:04:05")),
						"short": false,
					},
				},
			},
		},
	}

	return s.sendSlackWebhook(ctx, payload)
}

// Name returns the strategy name
func (s *SlackAlertStrategy) Name() string {
	return "slack"
}

// ConsoleNotificationStrategy implements console-based notification handling
type ConsoleNotificationStrategy struct{}

// NewConsoleNotificationStrategy creates a new console notification strategy
func NewConsoleNotificationStrategy() *ConsoleNotificationStrategy {
	return &ConsoleNotificationStrategy{}
}

// HandleNotification handles incoming notifications by printing to console
func (c *ConsoleNotificationStrategy) HandleNotification(ctx context.Context, notification *WebhookNotification) error {
	fmt.Printf("📨 NOTIFICATION: %s\n", notification.Type)
	fmt.Printf("   Target: %s\n", notification.Target)
	fmt.Printf("   Message: %s\n", notification.Message)
	fmt.Printf("   Timestamp: %s\n", notification.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Println()
	return nil
}

// Name returns the strategy name
func (c *ConsoleNotificationStrategy) Name() string {
	return "console"
}

// SlackNotificationStrategy sends generic notifications to a Slack webhook
type SlackNotificationStrategy struct {
	webhookURL string
	client     *http.Client
}

// NewSlackNotificationStrategy constructs a SlackNotificationStrategy
func NewSlackNotificationStrategy(webhookURL string) *SlackNotificationStrategy {
	return &SlackNotificationStrategy{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// HandleNotification posts a generic message to Slack
func (s *SlackNotificationStrategy) HandleNotification(ctx context.Context, notification *WebhookNotification) error {
	title := "Notification"
	if notification.Type != "" {
		title = notification.Type
	}
	message := fmt.Sprintf("%s: %s", title, notification.Message)
	if notification.Target != "" {
		message = fmt.Sprintf("%s — %s", notification.Target, message)
	}
	payload := map[string]any{
		"text":   message,
		"mrkdwn": true,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack payload: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create Slack request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Slack webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}
	fmt.Printf("📡 SLACK: Sent generic notification to %s\n", sanitizeSlackWebhookURL(s.webhookURL))
	return nil
}

// Name returns the strategy name
func (s *SlackNotificationStrategy) Name() string {
	return "slack"
}

// EmailNotificationStrategy implements email-based notification handling
type EmailNotificationStrategy struct {
	smtpHost string
	smtpPort int
	username string
	password string
	to       string
}

// NewEmailNotificationStrategy creates a new email notification strategy
func NewEmailNotificationStrategy(smtpHost string, smtpPort int, username, password, to string) *EmailNotificationStrategy {
	return &EmailNotificationStrategy{
		smtpHost: smtpHost,
		smtpPort: smtpPort,
		username: username,
		password: password,
		to:       to,
	}
}

// HandleNotification handles incoming notifications by sending email
func (e *EmailNotificationStrategy) HandleNotification(ctx context.Context, notification *WebhookNotification) error {
	// Build a simple HTML email
	subject := fmt.Sprintf("%s — %s", safeNonEmpty(notification.Type, "Notification"), notification.Target)
	body := fmt.Sprintf(
		"<html><body><h3>%s</h3><p><strong>Target:</strong> %s</p><p><strong>Message:</strong> %s</p><p><strong>Timestamp:</strong> %s</p></body></html>",
		safeNonEmpty(notification.Type, "Notification"),
		notification.Target,
		notification.Message,
		notification.Timestamp.Format("2006-01-02 15:04:05"),
	)
	// EmailNotificationStrategy doesn't have debug flag, use false
	return sendSMTPHTML(e.smtpHost, e.smtpPort, e.username, e.password, e.username, e.to, subject, body, false)
}

// Name returns the strategy name
func (e *EmailNotificationStrategy) Name() string {
	return "email"
}

// EmailAlertStrategy implements email-based alerting for target up/down
type EmailAlertStrategy struct {
	smtpHost string
	smtpPort int
	username string
	password string
	to       string
	debug    bool
}

// NewEmailAlertStrategy creates a new email alert strategy
func NewEmailAlertStrategy(smtpHost string, smtpPort int, username, password, to string) *EmailAlertStrategy {
	return &EmailAlertStrategy{
		smtpHost: smtpHost,
		smtpPort: smtpPort,
		username: username,
		password: password,
		to:       to,
		debug:    false,
	}
}

// NewEmailAlertStrategyWithDebug creates a new email alert strategy with debug option
func NewEmailAlertStrategyWithDebug(smtpHost string, smtpPort int, username, password, to string, debug bool) *EmailAlertStrategy {
	return &EmailAlertStrategy{
		smtpHost: smtpHost,
		smtpPort: smtpPort,
		username: username,
		password: password,
		to:       to,
		debug:    debug,
	}
}

// SendAlert sends a DOWN alert via email with a simple HTML body
func (e *EmailAlertStrategy) SendAlert(ctx context.Context, target *Target, result *CheckResult) error {
	subject := fmt.Sprintf("🚨 %s is DOWN", target.Name)
	body := fmt.Sprintf(
		"<html><body>"+
			"<h2 style=\"color:#c62828\">%s is DOWN</h2>"+
			"<ul>"+
			"<li><strong>URL:</strong> %s</li>"+
			"<li><strong>Status:</strong> %d</li>"+
			"<li><strong>Response Time:</strong> %s</li>"+
			"<li><strong>Error:</strong> %s</li>"+
			"<li><strong>Timestamp:</strong> %s</li>"+
			"</ul>"+
			"</body></html>",
		target.Name,
		target.URL,
		result.StatusCode,
		result.ResponseTime.String(),
		result.Error,
		result.Timestamp.Format("2006-01-02 15:04:05"),
	)
	return sendSMTPHTML(e.smtpHost, e.smtpPort, e.username, e.password, e.username, e.to, subject, body, e.debug)
}

// SendAllClear sends an UP notification via email with a simple HTML body
func (e *EmailAlertStrategy) SendAllClear(ctx context.Context, target *Target, result *CheckResult) error {
	subject := fmt.Sprintf("✅ %s is UP", target.Name)
	body := fmt.Sprintf(
		"<html><body>"+
			"<h2 style=\"color:#2e7d32\">%s is UP</h2>"+
			"<ul>"+
			"<li><strong>URL:</strong> %s</li>"+
			"<li><strong>Status:</strong> %d</li>"+
			"<li><strong>Response Time:</strong> %s</li>"+
			"<li><strong>Timestamp:</strong> %s</li>"+
			"</ul>"+
			"</body></html>",
		target.Name,
		target.URL,
		result.StatusCode,
		result.ResponseTime.String(),
		result.Timestamp.Format("2006-01-02 15:04:05"),
	)
	return sendSMTPHTML(e.smtpHost, e.smtpPort, e.username, e.password, e.username, e.to, subject, body, e.debug)
}

// Name returns the strategy name
func (e *EmailAlertStrategy) Name() string {
	return "email"
}

// SendStartupMessage sends a startup notification via email
func (e *EmailAlertStrategy) SendStartupMessage(ctx context.Context, version string, targetCount int) error {
	subject := "🚀 Quick Watch Started"
	body := fmt.Sprintf(
		"<html><body>"+
			"<h2 style=\"color:#1976d2\">🚀 Quick Watch Started</h2>"+
			"<ul>"+
			"<li><strong>Version:</strong> %s</li>"+
			"<li><strong>Targets:</strong> %d</li>"+
			"<li><strong>Timestamp:</strong> %s</li>"+
			"</ul>"+
			"<p>Quick Watch monitoring service has started successfully and is now monitoring your configured targets.</p>"+
			"</body></html>",
		version,
		targetCount,
		time.Now().Format("2006-01-02 15:04:05"),
	)
	err := sendSMTPHTML(e.smtpHost, e.smtpPort, e.username, e.password, e.username, e.to, subject, body, e.debug)
	if err != nil {
		return err
	}
	fmt.Printf("📧 EMAIL: Startup notification sent to %s\n", e.to)
	return nil
}

// sendSMTPHTML sends an HTML email using net/smtp with minimal dependencies
func sendSMTPHTML(host string, port int, username, password, from, to, subject, htmlBody string, debug bool) error {
	addr := fmt.Sprintf("%s:%d", host, port)

	if debug {
		fmt.Printf("🐛 EMAIL DEBUG: Connecting to SMTP server %s:%d\n", host, port)
		fmt.Printf("🐛 EMAIL DEBUG: From: %s, To: %s\n", from, to)
		fmt.Printf("🐛 EMAIL DEBUG: Subject: %s\n", subject)
	}

	// Build headers and body per RFC 5322
	headers := map[string]string{
		"From":         from,
		"To":           to,
		"Subject":      subject,
		"MIME-Version": "1.0",
		"Content-Type": "text/html; charset=\"UTF-8\"",
	}
	var msgBuilder strings.Builder
	for k, v := range headers {
		msgBuilder.WriteString(k)
		msgBuilder.WriteString(": ")
		msgBuilder.WriteString(v)
		msgBuilder.WriteString("\r\n")
	}
	msgBuilder.WriteString("\r\n")
	msgBuilder.WriteString(htmlBody)

	if debug {
		fmt.Printf("🐛 EMAIL DEBUG: Message size: %d bytes\n", msgBuilder.Len())
		fmt.Printf("🐛 EMAIL DEBUG: Authenticating as %s\n", username)
	}

	auth := smtp.PlainAuth("", username, password, host)
	if err := smtp.SendMail(addr, auth, from, []string{to}, []byte(msgBuilder.String())); err != nil {
		if debug {
			fmt.Printf("🐛 EMAIL DEBUG: Send failed: %v\n", err)
		}
		return fmt.Errorf("failed to send email via smtp: %w", err)
	}

	if debug {
		fmt.Printf("🐛 EMAIL DEBUG: Email sent successfully\n")
	}

	fmt.Printf("📧 EMAIL sent to %s (subject: %s)\n", to, subject)
	return nil
}

// safeNonEmpty returns fallback when s is empty
func safeNonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// sanitizeSlackWebhookURL hides the middle portion of a Slack webhook URL, keeping
// the first three characters after /services/ and the last three characters of the URL
func sanitizeSlackWebhookURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.Host != "hooks.slack.com" {
		return raw
	}
	if !strings.HasPrefix(parsed.Path, "/services/") {
		return raw
	}
	// take first 3 chars of the first segment after /services/
	rest := strings.TrimPrefix(parsed.Path, "/services/")
	rest = strings.TrimLeft(rest, "/")
	firstSeg := rest
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		firstSeg = rest[:idx]
	}
	first3 := firstSeg
	if len(first3) > 3 {
		first3 = first3[:3]
	}
	// last 3 chars of the entire raw URL (to match provided example)
	last3 := ""
	trimmed := strings.TrimRight(raw, "/")
	if len(trimmed) >= 3 {
		last3 = trimmed[len(trimmed)-3:]
	} else {
		last3 = trimmed
	}
	return parsed.Scheme + "://" + parsed.Host + "/services/" + first3 + "***" + last3
}
