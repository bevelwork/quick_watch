package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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
type ConsoleAlertStrategy struct{}

// NewConsoleAlertStrategy creates a new console alert strategy
func NewConsoleAlertStrategy() *ConsoleAlertStrategy {
	return &ConsoleAlertStrategy{}
}

// SendAlert sends an alert to the console
func (c *ConsoleAlertStrategy) SendAlert(ctx context.Context, target *Target, result *CheckResult) error {
	timestamp := result.Timestamp.Format("2006-01-02 15:04:05")
	fmt.Printf("%s %s is DOWN - %s (Status: %d, Time: %v)\n",
		qc.Colorize("🚨 ALERT:", qc.ColorRed),
		qc.Colorize(target.Name, qc.ColorRed),
		target.URL,
		result.StatusCode,
		result.ResponseTime)
	fmt.Printf("   %s %s\n", qc.Colorize("Target:", qc.ColorCyan), target.Name)
	fmt.Printf("   %s %s\n", qc.Colorize("URL:", qc.ColorCyan), target.URL)
	fmt.Printf("   %s %s\n", qc.Colorize("Time:", qc.ColorCyan), timestamp)
	fmt.Printf("   %s %v\n", qc.Colorize("Response Time:", qc.ColorCyan), result.ResponseTime)
	if result.ResponseSize > 0 {
		fmt.Printf("   %s %d bytes\n", qc.Colorize("Response Size:", qc.ColorCyan), result.ResponseSize)
	}
	fmt.Println()
	return nil
}

// SendAllClear sends an all-clear notification to the console
func (c *ConsoleAlertStrategy) SendAllClear(ctx context.Context, target *Target, result *CheckResult) error {
	timestamp := result.Timestamp.Format("2006-01-02 15:04:05")
	fmt.Printf("%s %s is UP - %s (Status: %d, Time: %v)\n",
		qc.Colorize("✅ ALL CLEAR:", qc.ColorGreen),
		qc.Colorize(target.Name, qc.ColorGreen),
		target.URL,
		result.StatusCode,
		result.ResponseTime)
	fmt.Printf("   %s %s\n", qc.Colorize("Target:", qc.ColorCyan), target.Name)
	fmt.Printf("   %s %s\n", qc.Colorize("URL:", qc.ColorCyan), target.URL)
	fmt.Printf("   %s %s\n", qc.Colorize("Time:", qc.ColorCyan), timestamp)
	fmt.Printf("   %s %v\n", qc.Colorize("Response Time:", qc.ColorCyan), result.ResponseTime)
	if result.ResponseSize > 0 {
		fmt.Printf("   %s %d bytes\n", qc.Colorize("Response Size:", qc.ColorCyan), result.ResponseSize)
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
		qc.Colorize("📏 SIZE ALERT:", qc.ColorYellow),
		qc.Colorize(target.Name, qc.ColorYellow),
		changeDirection,
		target.URL,
		result.ResponseSize,
		avgSize,
		changePercent*100)
	fmt.Printf("   %s %s\n", qc.Colorize("Target:", qc.ColorCyan), target.Name)
	fmt.Printf("   %s %s\n", qc.Colorize("URL:", qc.ColorCyan), target.URL)
	fmt.Printf("   %s %s\n", qc.Colorize("Time:", qc.ColorCyan), timestamp)
	fmt.Printf("   %s %d bytes\n", qc.Colorize("Current Size:", qc.ColorCyan), result.ResponseSize)
	fmt.Printf("   %s %.0f bytes\n", qc.Colorize("Average Size:", qc.ColorCyan), avgSize)
	fmt.Printf("   %s %.1f%%\n", qc.Colorize("Change:", qc.ColorCyan), changePercent*100)
	fmt.Println()
	return nil
}

// Name returns the strategy name
func (c *ConsoleAlertStrategy) Name() string {
	return "console"
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
		"target":       target.Name,
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
		"target":       target.Name,
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
	fmt.Printf("📡 WEBHOOK: Sending notification to %s\n", w.webhookURL)
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
}

// NewSlackAlertStrategy creates a new Slack alert strategy
func NewSlackAlertStrategy(webhookURL string) *SlackAlertStrategy {
	return &SlackAlertStrategy{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
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
		return fmt.Errorf("Slack webhook returned status %d", resp.StatusCode)
	}

	fmt.Printf("📡 SLACK: Sent notification to %s\n", s.webhookURL)
	return nil
}

// SendStartupMessage sends a startup notification to Slack
func (s *SlackAlertStrategy) SendStartupMessage(ctx context.Context, version string, targetCount int) error {
	message := fmt.Sprintf("🚀 *Quick Watch* started successfully\n• Version: %s\n• Targets: %d\n• Timestamp: %s",
		version, targetCount, time.Now().Format("2006-01-02 15:04:05"))
	
	payload := map[string]interface{}{
		"text": message,
		"mrkdwn": true,
		"attachments": []map[string]interface{}{
			{
				"color": "good",
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
	// This is a simplified implementation
	// In a real implementation, you'd use an SMTP client to send emails
	fmt.Printf("📧 EMAIL: Sending notification to %s\n", e.to)
	fmt.Printf("   Subject: %s - %s\n", notification.Type, notification.Target)
	fmt.Printf("   Message: %s\n", notification.Message)
	return nil
}

// Name returns the strategy name
func (e *EmailNotificationStrategy) Name() string {
	return "email"
}
