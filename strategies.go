package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	AlertCount   int           `json:"alert_count,omitempty"` // Number of alerts sent for this incident (for exponential backoff display)
	ContentType  string        `json:"content_type,omitempty"`
	ResponseBody string        `json:"response_body,omitempty"` // Response body (limited for JSON)
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
	SendStatusReport(ctx context.Context, report *StatusReportData) error
	Name() string
}

// AcknowledgementAwareAlert is an optional interface for alert strategies that support acknowledgements
type AcknowledgementAwareAlert interface {
	AlertStrategy
	SendAlertWithAck(ctx context.Context, target *Target, result *CheckResult, ackURL string) error
	SendAcknowledgement(ctx context.Context, target *Target, acknowledgedBy, note, contact string) error
}

// NotificationStrategy defines the interface for handling incoming notifications
type NotificationStrategy interface {
	HandleNotification(ctx context.Context, notification *WebhookNotification) error
	Name() string
}

// AcknowledgementAwareNotification is an optional interface for notification strategies that support acknowledgements
type AcknowledgementAwareNotification interface {
	NotificationStrategy
	HandleNotificationWithAck(ctx context.Context, notification *WebhookNotification, ackURL string) error
	SendNotificationAcknowledgement(ctx context.Context, hookName, acknowledgedBy, note, contact string) error
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

	// Get Content-Type header
	contentType := resp.Header.Get("Content-Type")

	// Read response body to get size and capture JSON responses
	var responseSize int64
	var responseBody string
	if resp.Body != nil {
		// Read body (limit to 10KB for JSON responses to avoid memory issues)
		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024))
		if err == nil {
			responseSize = int64(len(bodyBytes))
			// Only capture body for JSON responses
			if strings.Contains(contentType, "application/json") {
				responseBody = string(bodyBytes)
			}
		} else {
			// If we can't read the body, estimate from Content-Length
			responseSize = max(0, resp.ContentLength)
		}
	}

	// Check if status code matches allowed status codes
	success := isStatusCodeAllowed(resp.StatusCode, target.StatusCodes)

	return &CheckResult{
		Success:      success,
		StatusCode:   resp.StatusCode,
		ResponseTime: responseTime,
		ResponseSize: responseSize,
		ContentType:  contentType,
		ResponseBody: responseBody,
		Timestamp:    start,
	}, nil
}

// Name returns the strategy name
func (h *HTTPCheckStrategy) Name() string {
	return "http"
}

// WebhookCheckStrategy implements webhook-triggered manual alerts
// This strategy doesn't actively check anything - targets are triggered via webhooks
type WebhookCheckStrategy struct{}

// NewWebhookCheckStrategy creates a new webhook check strategy
func NewWebhookCheckStrategy() *WebhookCheckStrategy {
	return &WebhookCheckStrategy{}
}

// Check for webhook strategy always returns success (actual state is managed via triggers)
func (w *WebhookCheckStrategy) Check(ctx context.Context, target *Target) (*CheckResult, error) {
	// Webhook targets don't actively check - they're triggered externally
	// Return a successful check result (actual down state is set via TriggerWebhookTarget)
	return &CheckResult{
		Success:      true,
		StatusCode:   200,
		ResponseTime: 0,
		Timestamp:    time.Now(),
	}, nil
}

// Name returns the strategy name
func (w *WebhookCheckStrategy) Name() string {
	return "webhook"
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

// SendAlertWithAck sends an alert to the console with acknowledgement URL
func (c *ConsoleAlertStrategy) SendAlertWithAck(ctx context.Context, target *Target, result *CheckResult, ackURL string) error {
	timestamp := result.Timestamp.Format("2006-01-02 15:04:05")
	title := c.format("🚨 ALERT:", qc.ColorRed, true)
	name := c.format(target.Name, qc.ColorRed, true)

	// Build alert message with count
	alertMsg := fmt.Sprintf("%s %s is DOWN - %s (Status: %d, Time: %v)",
		title, name, target.URL, result.StatusCode, result.ResponseTime)
	if result.AlertCount > 1 {
		alertMsg += fmt.Sprintf(" %s", c.format(fmt.Sprintf("[Alert #%d]", result.AlertCount), qc.ColorYellow, false))
	}
	fmt.Println(alertMsg)

	fmt.Printf("   %s %s\n", c.format("Target:", qc.ColorCyan, true), target.Name)
	fmt.Printf("   %s %s\n", c.format("URL:", qc.ColorCyan, true), target.URL)
	fmt.Printf("   %s %s\n", c.format("Time:", qc.ColorCyan, true), timestamp)
	fmt.Printf("   %s %v\n", c.format("Response Time:", qc.ColorCyan, true), result.ResponseTime)
	if result.AlertCount > 0 {
		fmt.Printf("   %s %d\n", c.format("Alert Count:", qc.ColorCyan, true), result.AlertCount)
	}
	if result.ResponseSize > 0 {
		fmt.Printf("   %s %d bytes\n", c.format("Response Size:", qc.ColorCyan, true), result.ResponseSize)
	}
	fmt.Printf("   %s %s\n", c.format("Acknowledge:", qc.ColorYellow, true), ackURL)
	fmt.Println()
	return nil
}

// SendAcknowledgement sends acknowledgement notification to the console
func (c *ConsoleAlertStrategy) SendAcknowledgement(ctx context.Context, target *Target, acknowledgedBy, note, contact string) error {
	title := c.format("✅ ACKNOWLEDGED:", qc.ColorGreen, true)
	name := c.format(target.Name, qc.ColorGreen, true)
	fmt.Printf("%s Alert for %s has been acknowledged\n", title, name)
	fmt.Printf("   %s %s\n", c.format("Target:", qc.ColorCyan, true), target.Name)
	fmt.Printf("   %s %s\n", c.format("URL:", qc.ColorCyan, true), target.URL)
	fmt.Printf("   %s %s\n", c.format("Acknowledged By:", qc.ColorCyan, true), acknowledgedBy)
	fmt.Printf("   %s %s\n", c.format("Time:", qc.ColorCyan, true), time.Now().Format("2006-01-02 15:04:05 MST"))
	if contact != "" {
		fmt.Printf("   %s %s\n", c.format("Contact:", qc.ColorCyan, true), contact)
	}
	if note != "" {
		fmt.Printf("   %s %s\n", c.format("Note:", qc.ColorCyan, true), note)
	}
	fmt.Println()
	return nil
}

// SendStatusReport sends a status report to the console
func (c *ConsoleAlertStrategy) SendStatusReport(ctx context.Context, report *StatusReportData) error {
	title := c.format("📊 STATUS REPORT", qc.ColorBlue, true)
	period := fmt.Sprintf("%s to %s",
		report.ReportPeriodStart.Format("15:04:05"),
		report.ReportPeriodEnd.Format("15:04:05"))
	fmt.Printf("%s (%s)\n", title, period)
	fmt.Println()

	// Active outages
	if len(report.ActiveOutages) > 0 {
		fmt.Printf("%s\n", c.format("Active Outages:", qc.ColorRed, true))
		for _, outage := range report.ActiveOutages {
			ackStatus := ""
			if outage.Acknowledged {
				if outage.AcknowledgedBy != "" {
					ackStatus = c.format(fmt.Sprintf(" (acknowledged by %s)", outage.AcknowledgedBy), qc.ColorYellow, false)
				} else {
					ackStatus = c.format(" (acknowledged)", qc.ColorYellow, false)
				}
			}
			fmt.Printf("  • %s - down for %v%s\n",
				c.format(outage.TargetName, qc.ColorRed, false),
				outage.Duration.Round(time.Second),
				ackStatus)
		}
		fmt.Println()
	} else {
		fmt.Printf("%s\n\n", c.format("✓ No active outages", qc.ColorGreen, false))
	}

	// Resolved outages
	if len(report.ResolvedOutages) > 0 {
		fmt.Printf("%s\n", c.format("Resolved Outages:", qc.ColorGreen, true))
		for _, resolved := range report.ResolvedOutages {
			fmt.Printf("  • %s - was down for %v\n",
				c.format(resolved.TargetName, qc.ColorGreen, false),
				resolved.DownDuration.Round(time.Second))
		}
		fmt.Println()
	}

	// Metrics
	fmt.Printf("%s\n", c.format("Metrics:", qc.ColorCyan, true))
	fmt.Printf("  • Alerts sent: %s\n", c.format(fmt.Sprintf("%d", report.AlertsSent), qc.ColorWhite, true))
	fmt.Printf("  • Notifications sent: %s\n", c.format(fmt.Sprintf("%d", report.NotificationsSent), qc.ColorWhite, true))
	fmt.Println()

	return nil
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
	payload := map[string]any{
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
	payload := map[string]any{
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
func (w *WebhookAlertStrategy) sendWebhook(_ context.Context, payload map[string]any) error {
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

// SendStatusReport sends a status report via webhook
func (w *WebhookAlertStrategy) SendStatusReport(ctx context.Context, report *StatusReportData) error {
	payload := map[string]any{
		"type":               "status_report",
		"active_outages":     len(report.ActiveOutages),
		"resolved_outages":   len(report.ResolvedOutages),
		"alerts_sent":        report.AlertsSent,
		"notifications_sent": report.NotificationsSent,
		"period_start":       report.ReportPeriodStart.Format(time.RFC3339),
		"period_end":         report.ReportPeriodEnd.Format(time.RFC3339),
	}
	return w.sendWebhook(ctx, payload)
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

	payload := map[string]any{
		"text":   message,
		"mrkdwn": true,
		"attachments": []map[string]any{
			{
				"color":     "danger",
				"mrkdwn_in": []string{"fields"},
				"fields": []map[string]any{
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

	payload := map[string]any{
		"text":   message,
		"mrkdwn": true,
		"attachments": []map[string]any{
			{
				"color":     "good",
				"mrkdwn_in": []string{"fields"},
				"fields": []map[string]any{
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
func (s *SlackAlertStrategy) sendSlackWebhook(ctx context.Context, payload map[string]any) error {
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

	payload := map[string]any{
		"text":   message,
		"mrkdwn": true,
		"attachments": []map[string]any{
			{
				"color":     "good",
				"mrkdwn_in": []string{"fields"},
				"fields": []map[string]any{
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

// SendAlertWithAck sends an alert to Slack with acknowledgement button
func (s *SlackAlertStrategy) SendAlertWithAck(ctx context.Context, target *Target, result *CheckResult, ackURL string) error {
	title := fmt.Sprintf("🚨 *%s* is DOWN", target.Name)
	if result.AlertCount > 1 {
		title = fmt.Sprintf("🚨 *%s* is DOWN [Alert #%d]", target.Name, result.AlertCount)
	}
	message := fmt.Sprintf("%s\n• URL: %s\n• Status: %d\n• Time: %v\n• Error: %s",
		title, target.URL, result.StatusCode, result.ResponseTime, result.Error)

	payload := map[string]any{
		"text":   message,
		"mrkdwn": true,
		"attachments": []map[string]any{
			{
				"color":     "danger",
				"mrkdwn_in": []string{"fields"},
				"fields": []map[string]any{
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
						"title": "Alert Count",
						"value": fmt.Sprintf("`%d`", result.AlertCount),
						"short": true,
					},
					{
						"title": "Timestamp",
						"value": fmt.Sprintf("<!date^%d^{date} {time}|%s>",
							result.Timestamp.Unix(),
							result.Timestamp.Format("2006-01-02 15:04:05")),
						"short": true,
					},
					{
						"title": "Acknowledge",
						"value": fmt.Sprintf("<%s|Click here to acknowledge this alert>", ackURL),
						"short": false,
					},
				},
			},
		},
	}

	return s.sendSlackWebhook(ctx, payload)
}

// SendAcknowledgement sends acknowledgement notification to Slack
func (s *SlackAlertStrategy) SendAcknowledgement(ctx context.Context, target *Target, acknowledgedBy, note, contact string) error {
	message := fmt.Sprintf("✅ Alert acknowledged for *%s*\n• By: %s", target.Name, acknowledgedBy)
	if contact != "" {
		message += fmt.Sprintf("\n• Contact: %s", contact)
	}
	if note != "" {
		message += fmt.Sprintf("\n• Note: %s", note)
	}

	payload := map[string]any{
		"text":   message,
		"mrkdwn": true,
		"attachments": []map[string]any{
			{
				"color":     "good",
				"mrkdwn_in": []string{"fields"},
				"fields": []map[string]any{
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
						"title": "Acknowledged By",
						"value": acknowledgedBy,
						"short": true,
					},
					{
						"title": "Time",
						"value": fmt.Sprintf("<!date^%d^{date} {time}|%s>",
							time.Now().Unix(),
							time.Now().Format("2006-01-02 15:04:05 MST")),
						"short": true,
					},
				},
			},
		},
	}

	// Add contact info if provided
	if contact != "" {
		attachment := payload["attachments"].([]map[string]any)[0]
		fields := attachment["fields"].([]map[string]any)
		fields = append(fields, map[string]any{
			"title": "Contact",
			"value": contact,
			"short": false,
		})
		attachment["fields"] = fields
	}

	// Add note if provided
	if note != "" {
		attachment := payload["attachments"].([]map[string]any)[0]
		fields := attachment["fields"].([]map[string]any)
		fields = append(fields, map[string]any{
			"title": "Note",
			"value": note,
			"short": false,
		})
		attachment["fields"] = fields
	}

	return s.sendSlackWebhook(ctx, payload)
}

// Name returns the strategy name
func (s *SlackAlertStrategy) Name() string {
	return "slack"
}

// SendStatusReport sends a status report to Slack
func (s *SlackAlertStrategy) SendStatusReport(ctx context.Context, report *StatusReportData) error {
	periodDuration := report.ReportPeriodEnd.Sub(report.ReportPeriodStart)

	// Build message
	var message strings.Builder
	message.WriteString(fmt.Sprintf("📊 *Status Report* (last %v)\n\n", periodDuration.Round(time.Minute)))

	// Active outages
	if len(report.ActiveOutages) > 0 {
		message.WriteString(fmt.Sprintf("🔴 *Active Outages (%d):*\n", len(report.ActiveOutages)))
		for _, outage := range report.ActiveOutages {
			ackInfo := ""
			if outage.Acknowledged {
				if outage.AcknowledgedBy != "" {
					ackInfo = fmt.Sprintf(" _(acknowledged by %s)_", outage.AcknowledgedBy)
				} else {
					ackInfo = " _(acknowledged)_"
				}
			}
			message.WriteString(fmt.Sprintf("• %s - down for %v%s\n",
				outage.TargetName, outage.Duration.Round(time.Second), ackInfo))
		}
		message.WriteString("\n")
	} else {
		message.WriteString("✅ *No active outages*\n\n")
	}

	// Resolved outages
	if len(report.ResolvedOutages) > 0 {
		message.WriteString(fmt.Sprintf("✅ *Resolved Outages (%d):*\n", len(report.ResolvedOutages)))
		for _, resolved := range report.ResolvedOutages {
			message.WriteString(fmt.Sprintf("• %s - was down for %v\n",
				resolved.TargetName, resolved.DownDuration.Round(time.Second)))
		}
		message.WriteString("\n")
	}

	// Metrics
	message.WriteString("📈 *Metrics:*\n")
	message.WriteString(fmt.Sprintf("• Alerts sent: %d\n", report.AlertsSent))
	message.WriteString(fmt.Sprintf("• Notifications sent: %d\n", report.NotificationsSent))

	payload := map[string]any{
		"text":   message.String(),
		"mrkdwn": true,
	}

	return s.sendSlackWebhook(ctx, payload)
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

// HandleNotificationWithAck handles incoming notifications with acknowledgement URL by printing to console
func (c *ConsoleNotificationStrategy) HandleNotificationWithAck(ctx context.Context, notification *WebhookNotification, ackURL string) error {
	fmt.Printf("📨 NOTIFICATION: %s\n", notification.Type)
	fmt.Printf("   Target: %s\n", notification.Target)
	fmt.Printf("   Message: %s\n", notification.Message)
	fmt.Printf("   Timestamp: %s\n", notification.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Printf("   Acknowledge: %s\n", ackURL)
	fmt.Println()
	return nil
}

// SendNotificationAcknowledgement sends acknowledgement notification to the console
func (c *ConsoleNotificationStrategy) SendNotificationAcknowledgement(ctx context.Context, hookName, acknowledgedBy, note, contact string) error {
	fmt.Printf("✅ ACKNOWLEDGED: Notification for hook '%s' has been acknowledged\n", hookName)
	fmt.Printf("   Hook: %s\n", hookName)
	fmt.Printf("   Acknowledged By: %s\n", acknowledgedBy)
	fmt.Printf("   Time: %s\n", time.Now().Format("2006-01-02 15:04:05 MST"))
	if contact != "" {
		fmt.Printf("   Contact: %s\n", contact)
	}
	if note != "" {
		fmt.Printf("   Note: %s\n", note)
	}
	fmt.Println()
	return nil
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

// HandleNotificationWithAck posts a notification to Slack with an acknowledgement URL
func (s *SlackNotificationStrategy) HandleNotificationWithAck(ctx context.Context, notification *WebhookNotification, ackURL string) error {
	title := "Notification"
	if notification.Type != "" {
		title = notification.Type
	}
	message := fmt.Sprintf("%s: %s", title, notification.Message)
	if notification.Target != "" {
		message = fmt.Sprintf("%s — %s", notification.Target, message)
	}
	message += "\n\n🔗 <" + ackURL + "|Acknowledge this notification>"

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
	fmt.Printf("📡 SLACK: Sent notification with acknowledgement to %s\n", sanitizeSlackWebhookURL(s.webhookURL))
	return nil
}

// SendNotificationAcknowledgement sends an acknowledgement notification to Slack
func (s *SlackNotificationStrategy) SendNotificationAcknowledgement(ctx context.Context, hookName, acknowledgedBy, note, contact string) error {
	message := fmt.Sprintf("✅ *Notification Acknowledged*\nHook: %s\nAcknowledged by: %s", hookName, acknowledgedBy)
	if contact != "" {
		message += fmt.Sprintf("\nContact: %s", contact)
	}
	if note != "" {
		message += fmt.Sprintf("\nNote: %s", note)
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
	fmt.Printf("📡 SLACK: Sent acknowledgement notification to %s\n", sanitizeSlackWebhookURL(s.webhookURL))
	return nil
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

// HandleNotificationWithAck sends an email notification with an acknowledgement link
func (e *EmailNotificationStrategy) HandleNotificationWithAck(ctx context.Context, notification *WebhookNotification, ackURL string) error {
	subject := fmt.Sprintf("%s — %s", safeNonEmpty(notification.Type, "Notification"), notification.Target)
	body := fmt.Sprintf(
		`<html><body>
<h3>%s</h3>
<p><strong>Target:</strong> %s</p>
<p><strong>Message:</strong> %s</p>
<p><strong>Timestamp:</strong> %s</p>
<hr>
<p><a href="%s" style="background-color: #4CAF50; color: white; padding: 10px 20px; text-decoration: none; border-radius: 4px; display: inline-block;">Acknowledge Notification</a></p>
<p style="font-size: 12px; color: #666;">Or copy this link: <a href="%s">%s</a></p>
</body></html>`,
		safeNonEmpty(notification.Type, "Notification"),
		notification.Target,
		notification.Message,
		notification.Timestamp.Format("2006-01-02 15:04:05"),
		ackURL,
		ackURL,
		ackURL,
	)
	return sendSMTPHTML(e.smtpHost, e.smtpPort, e.username, e.password, e.username, e.to, subject, body, false)
}

// SendNotificationAcknowledgement sends an acknowledgement email
func (e *EmailNotificationStrategy) SendNotificationAcknowledgement(ctx context.Context, hookName, acknowledgedBy, note, contact string) error {
	subject := fmt.Sprintf("✅ Notification Acknowledged — %s", hookName)
	contactSection := ""
	if contact != "" {
		contactSection = fmt.Sprintf("<p><strong>Contact:</strong> %s</p>", contact)
	}
	noteSection := ""
	if note != "" {
		noteSection = fmt.Sprintf("<p><strong>Note:</strong> %s</p>", note)
	}
	body := fmt.Sprintf(
		`<html><body>
<h3>✅ Notification Acknowledged</h3>
<p><strong>Hook:</strong> %s</p>
<p><strong>Acknowledged by:</strong> %s</p>
%s
%s
<p><strong>Timestamp:</strong> %s</p>
</body></html>`,
		hookName,
		acknowledgedBy,
		contactSection,
		noteSection,
		time.Now().Format("2006-01-02 15:04:05 MST"),
	)
	return sendSMTPHTML(e.smtpHost, e.smtpPort, e.username, e.password, e.username, e.to, subject, body, false)
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

// SendAlertWithAck sends a DOWN alert via email with acknowledgement link
func (e *EmailAlertStrategy) SendAlertWithAck(ctx context.Context, target *Target, result *CheckResult, ackURL string) error {
	subject := fmt.Sprintf("🚨 %s is DOWN", target.Name)
	if result.AlertCount > 1 {
		subject = fmt.Sprintf("🚨 %s is DOWN [Alert #%d]", target.Name, result.AlertCount)
	}
	body := fmt.Sprintf(
		"<html><body>"+
			"<h2 style=\"color:#c62828\">%s is DOWN</h2>"+
			"<ul>"+
			"<li><strong>URL:</strong> %s</li>"+
			"<li><strong>Status:</strong> %d</li>"+
			"<li><strong>Response Time:</strong> %s</li>"+
			"<li><strong>Alert Count:</strong> %d</li>"+
			"<li><strong>Error:</strong> %s</li>"+
			"<li><strong>Timestamp:</strong> %s</li>"+
			"</ul>"+
			"<p><a href=\"%s\" style=\"display:inline-block;padding:10px 20px;background-color:#4CAF50;color:white;text-decoration:none;border-radius:5px;\">Acknowledge Alert</a></p>"+
			"<p><small>Click the button above to acknowledge that you are investigating this alert.</small></p>"+
			"</body></html>",
		target.Name,
		target.URL,
		result.StatusCode,
		result.ResponseTime.String(),
		result.AlertCount,
		result.Error,
		result.Timestamp.Format("2006-01-02 15:04:05"),
		ackURL,
	)
	return sendSMTPHTML(e.smtpHost, e.smtpPort, e.username, e.password, e.username, e.to, subject, body, e.debug)
}

// SendAcknowledgement sends acknowledgement notification via email
func (e *EmailAlertStrategy) SendAcknowledgement(ctx context.Context, target *Target, acknowledgedBy, note, contact string) error {
	subject := fmt.Sprintf("✅ Alert Acknowledged: %s", target.Name)

	contactSection := ""
	if contact != "" {
		contactSection = fmt.Sprintf("<li><strong>Contact:</strong> %s</li>", contact)
	}
	noteSection := ""
	if note != "" {
		noteSection = fmt.Sprintf("<li><strong>Note:</strong> %s</li>", note)
	}

	body := fmt.Sprintf(
		"<html><body>"+
			"<h2 style=\"color:#2e7d32\">Alert Acknowledged</h2>"+
			"<ul>"+
			"<li><strong>Target:</strong> %s</li>"+
			"<li><strong>URL:</strong> %s</li>"+
			"<li><strong>Acknowledged By:</strong> %s</li>"+
			"<li><strong>Time:</strong> %s</li>"+
			"%s"+
			"%s"+
			"</ul>"+
			"<p>This alert has been acknowledged and is being investigated.</p>"+
			"</body></html>",
		target.Name,
		target.URL,
		acknowledgedBy,
		time.Now().Format("2006-01-02 15:04:05 MST"),
		contactSection,
		noteSection,
	)
	err := sendSMTPHTML(e.smtpHost, e.smtpPort, e.username, e.password, e.username, e.to, subject, body, e.debug)
	if err != nil {
		return err
	}
	fmt.Printf("📧 EMAIL: Acknowledgement notification sent to %s\n", e.to)
	return nil
}

// Name returns the strategy name
func (e *EmailAlertStrategy) Name() string {
	return "email"
}

// SendStatusReport sends a status report via email
func (e *EmailAlertStrategy) SendStatusReport(ctx context.Context, report *StatusReportData) error {
	periodDuration := report.ReportPeriodEnd.Sub(report.ReportPeriodStart)
	subject := fmt.Sprintf("📊 Status Report - %v period", periodDuration.Round(time.Minute))

	var body strings.Builder
	body.WriteString("<html><body>")
	body.WriteString("<h2 style=\"color:#1976d2\">📊 Status Report</h2>")
	body.WriteString(fmt.Sprintf("<p><strong>Period:</strong> %s to %s (%v)</p>",
		report.ReportPeriodStart.Format("15:04:05"),
		report.ReportPeriodEnd.Format("15:04:05"),
		periodDuration.Round(time.Minute)))

	// Active outages
	if len(report.ActiveOutages) > 0 {
		body.WriteString(fmt.Sprintf("<h3 style=\"color:#c62828\">🔴 Active Outages (%d)</h3><ul>", len(report.ActiveOutages)))
		for _, outage := range report.ActiveOutages {
			ackInfo := ""
			if outage.Acknowledged {
				if outage.AcknowledgedBy != "" {
					ackInfo = fmt.Sprintf(" <em>(acknowledged by %s)</em>", outage.AcknowledgedBy)
				} else {
					ackInfo = " <em>(acknowledged)</em>"
				}
			}
			body.WriteString(fmt.Sprintf("<li>%s - down for %v%s</li>",
				outage.TargetName, outage.Duration.Round(time.Second), ackInfo))
		}
		body.WriteString("</ul>")
	} else {
		body.WriteString("<p style=\"color:#2e7d32\">✅ <strong>No active outages</strong></p>")
	}

	// Resolved outages
	if len(report.ResolvedOutages) > 0 {
		body.WriteString(fmt.Sprintf("<h3 style=\"color:#2e7d32\">✅ Resolved Outages (%d)</h3><ul>", len(report.ResolvedOutages)))
		for _, resolved := range report.ResolvedOutages {
			body.WriteString(fmt.Sprintf("<li>%s - was down for %v</li>",
				resolved.TargetName, resolved.DownDuration.Round(time.Second)))
		}
		body.WriteString("</ul>")
	}

	// Metrics
	body.WriteString("<h3>📈 Metrics</h3><ul>")
	body.WriteString(fmt.Sprintf("<li>Alerts sent: %d</li>", report.AlertsSent))
	body.WriteString(fmt.Sprintf("<li>Notifications sent: %d</li>", report.NotificationsSent))
	body.WriteString("</ul>")
	body.WriteString("</body></html>")

	return sendSMTPHTML(e.smtpHost, e.smtpPort, e.username, e.password, e.username, e.to, subject, body.String(), e.debug)
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

// FileAlertStrategy implements file-based alerting with OTEL-like JSON logs
type FileAlertStrategy struct {
	filePath              string
	debug                 bool
	maxSizeBeforeCompress int64 // in bytes (converted from MB in config)
	lastRotationCheck     time.Time
	rotationMutex         sync.Mutex
}

// NewFileAlertStrategy creates a new file alert strategy
func NewFileAlertStrategy(filePath string) *FileAlertStrategy {
	return &FileAlertStrategy{
		filePath:              filePath,
		debug:                 false,
		maxSizeBeforeCompress: 0, // disabled by default
		lastRotationCheck:     time.Now(),
	}
}

// NewFileAlertStrategyWithDebug creates a new file alert strategy with debug option
func NewFileAlertStrategyWithDebug(filePath string, debug bool) *FileAlertStrategy {
	return &FileAlertStrategy{
		filePath:              filePath,
		debug:                 debug,
		maxSizeBeforeCompress: 0, // disabled by default
		lastRotationCheck:     time.Now(),
	}
}

// NewFileAlertStrategyWithRotation creates a new file alert strategy with rotation
func NewFileAlertStrategyWithRotation(filePath string, debug bool, maxSizeMB int64) *FileAlertStrategy {
	return &FileAlertStrategy{
		filePath:              filePath,
		debug:                 debug,
		maxSizeBeforeCompress: maxSizeMB * 1024 * 1024, // convert MB to bytes
		lastRotationCheck:     time.Now(),
	}
}

// SendAlert sends a DOWN alert to the log file in OTEL-like JSON format
func (f *FileAlertStrategy) SendAlert(ctx context.Context, target *Target, result *CheckResult) error {
	logEntry := map[string]any{
		"timestamp":             result.Timestamp.Format(time.RFC3339Nano),
		"level":                 "error",
		"service.name":          "quick_watch",
		"alert.type":            "down",
		"target.name":           target.Name,
		"target.url":            target.URL,
		"http.status_code":      result.StatusCode,
		"http.response_time_ms": result.ResponseTime.Milliseconds(),
		"error.message":         result.Error,
		"attributes": map[string]any{
			"check_strategy": target.CheckStrategy,
			"threshold":      target.Threshold,
		},
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Writing DOWN alert to %s\n", f.filePath)
	}

	return f.appendLogEntry(logEntry)
}

// SendAllClear sends an UP notification to the log file in OTEL-like JSON format
func (f *FileAlertStrategy) SendAllClear(ctx context.Context, target *Target, result *CheckResult) error {
	logEntry := map[string]any{
		"timestamp":             result.Timestamp.Format(time.RFC3339Nano),
		"level":                 "info",
		"service.name":          "quick_watch",
		"alert.type":            "all_clear",
		"target.name":           target.Name,
		"target.url":            target.URL,
		"http.status_code":      result.StatusCode,
		"http.response_time_ms": result.ResponseTime.Milliseconds(),
		"attributes": map[string]any{
			"check_strategy": target.CheckStrategy,
			"threshold":      target.Threshold,
		},
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Writing ALL_CLEAR to %s\n", f.filePath)
	}

	return f.appendLogEntry(logEntry)
}

// SendStartupMessage sends a startup notification to the log file
func (f *FileAlertStrategy) SendStartupMessage(ctx context.Context, version string, targetCount int) error {
	logEntry := map[string]any{
		"timestamp":       time.Now().Format(time.RFC3339Nano),
		"level":           "info",
		"service.name":    "quick_watch",
		"event.name":      "startup",
		"service.version": version,
		"attributes": map[string]any{
			"target_count": targetCount,
		},
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Writing STARTUP to %s\n", f.filePath)
	}

	if err := f.appendLogEntry(logEntry); err != nil {
		return err
	}

	fmt.Printf("📄 FILE: Startup notification written to %s\n", f.filePath)
	return nil
}

// appendLogEntry appends a JSON log entry to the file
func (f *FileAlertStrategy) appendLogEntry(entry map[string]any) error {
	// Check if rotation is needed (once per hour)
	if err := f.checkAndRotate(); err != nil {
		// Log error but don't fail the write
		fmt.Printf("⚠️  FILE: Rotation check failed: %v\n", err)
	}

	jsonData, err := json.Marshal(entry)
	if err != nil {
		if f.debug {
			fmt.Printf("🐛 FILE DEBUG: Failed to marshal JSON: %v\n", err)
		}
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: JSON: %s\n", string(jsonData))
	}

	// Ensure parent directory exists
	dir := filepath.Dir(f.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		if f.debug {
			fmt.Printf("🐛 FILE DEBUG: Failed to create directory: %v\n", err)
		}
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Open file in append mode, create if doesn't exist
	file, err := os.OpenFile(f.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		if f.debug {
			fmt.Printf("🐛 FILE DEBUG: Failed to open file: %v\n", err)
		}
		return fmt.Errorf("failed to open log file %s: %w", f.filePath, err)
	}
	defer file.Close()

	// Write JSON line
	if _, err := file.Write(append(jsonData, '\n')); err != nil {
		if f.debug {
			fmt.Printf("🐛 FILE DEBUG: Failed to write: %v\n", err)
		}
		return fmt.Errorf("failed to write to log file: %w", err)
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Successfully wrote to %s\n", f.filePath)
	}

	fmt.Printf("📄 FILE: Alert logged to %s\n", f.filePath)
	return nil
}

// checkAndRotate checks if rotation is needed and performs it
func (f *FileAlertStrategy) checkAndRotate() error {
	// Skip if rotation is disabled
	if f.maxSizeBeforeCompress <= 0 {
		return nil
	}

	f.rotationMutex.Lock()
	defer f.rotationMutex.Unlock()

	// Check if an hour has passed since last check
	if time.Since(f.lastRotationCheck) < time.Hour {
		return nil
	}

	f.lastRotationCheck = time.Now()

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Checking file size for rotation\n")
	}

	// Check file size
	fileInfo, err := os.Stat(f.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet, nothing to rotate
			return nil
		}
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if fileInfo.Size() < f.maxSizeBeforeCompress {
		if f.debug {
			fmt.Printf("🐛 FILE DEBUG: File size %d bytes is below threshold %d bytes\n", fileInfo.Size(), f.maxSizeBeforeCompress)
		}
		return nil
	}

	// Rotate and compress
	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: File size %d bytes exceeds threshold %d bytes, rotating\n", fileInfo.Size(), f.maxSizeBeforeCompress)
	}

	return f.rotateAndCompress()
}

// rotateAndCompress compresses the current log file and starts fresh
func (f *FileAlertStrategy) rotateAndCompress() error {
	timestamp := time.Now().Format("20060102-150405")
	archiveName := fmt.Sprintf("%s.%s.tar.gz", f.filePath, timestamp)

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Creating archive %s\n", archiveName)
	}

	// Create tar.gz archive
	archiveFile, err := os.Create(archiveName)
	if err != nil {
		return fmt.Errorf("failed to create archive file: %w", err)
	}
	defer archiveFile.Close()

	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Read the original file
	sourceFile, err := os.Open(f.filePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	// Get file info for tar header
	fileInfo, err := sourceFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Create tar header
	header := &tar.Header{
		Name:    filepath.Base(f.filePath),
		Size:    fileInfo.Size(),
		Mode:    int64(fileInfo.Mode()),
		ModTime: fileInfo.ModTime(),
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	// Copy file contents to tar
	if _, err := io.Copy(tarWriter, sourceFile); err != nil {
		return fmt.Errorf("failed to write file to tar: %w", err)
	}

	// Close all writers to flush
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("failed to close tar writer: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}
	if err := archiveFile.Close(); err != nil {
		return fmt.Errorf("failed to close archive file: %w", err)
	}

	sourceFile.Close()

	// Remove the original file
	if err := os.Remove(f.filePath); err != nil {
		return fmt.Errorf("failed to remove original file: %w", err)
	}

	fmt.Printf("📦 FILE: Rotated and compressed log to %s\n", archiveName)

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Rotation complete, fresh file will be created on next write\n")
	}

	return nil
}

// SendAlertWithAck sends a DOWN alert to the log file with acknowledgement URL
func (f *FileAlertStrategy) SendAlertWithAck(ctx context.Context, target *Target, result *CheckResult, ackURL string) error {
	logEntry := map[string]any{
		"timestamp":             result.Timestamp.Format(time.RFC3339Nano),
		"level":                 "error",
		"service.name":          "quick_watch",
		"alert.type":            "down",
		"alert.count":           result.AlertCount,
		"target.name":           target.Name,
		"target.url":            target.URL,
		"http.status_code":      result.StatusCode,
		"http.response_time_ms": result.ResponseTime.Milliseconds(),
		"error.message":         result.Error,
		"acknowledgement_url":   ackURL,
		"attributes": map[string]any{
			"check_strategy": target.CheckStrategy,
			"threshold":      target.Threshold,
		},
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Writing DOWN alert with ack URL to %s\n", f.filePath)
	}

	return f.appendLogEntry(logEntry)
}

// SendAcknowledgement sends acknowledgement notification to the log file
func (f *FileAlertStrategy) SendAcknowledgement(ctx context.Context, target *Target, acknowledgedBy, note, contact string) error {
	attributes := map[string]any{}
	if note != "" {
		attributes["note"] = note
	}
	if contact != "" {
		attributes["contact"] = contact
	}

	logEntry := map[string]any{
		"timestamp":       time.Now().Format(time.RFC3339Nano),
		"level":           "info",
		"service.name":    "quick_watch",
		"event.name":      "alert_acknowledged",
		"target.name":     target.Name,
		"target.url":      target.URL,
		"acknowledged_by": acknowledgedBy,
		"attributes":      attributes,
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Writing ACKNOWLEDGEMENT to %s\n", f.filePath)
	}

	if err := f.appendLogEntry(logEntry); err != nil {
		return err
	}

	fmt.Printf("📄 FILE: Acknowledgement logged to %s\n", f.filePath)
	return nil
}

// Name returns the strategy name
func (f *FileAlertStrategy) Name() string {
	return "file"
}

// SendStatusReport logs a status report to file
func (f *FileAlertStrategy) SendStatusReport(ctx context.Context, report *StatusReportData) error {
	periodDuration := report.ReportPeriodEnd.Sub(report.ReportPeriodStart)

	logEntry := map[string]any{
		"timestamp":    time.Now().Format(time.RFC3339Nano),
		"level":        "info",
		"service.name": "quick_watch",
		"event.type":   "status_report",
		"report": map[string]any{
			"period_start":       report.ReportPeriodStart.Format(time.RFC3339),
			"period_end":         report.ReportPeriodEnd.Format(time.RFC3339),
			"period_duration":    periodDuration.String(),
			"active_outages":     len(report.ActiveOutages),
			"resolved_outages":   len(report.ResolvedOutages),
			"alerts_sent":        report.AlertsSent,
			"notifications_sent": report.NotificationsSent,
		},
		"active_outages":   report.ActiveOutages,
		"resolved_outages": report.ResolvedOutages,
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Writing status report to %s\n", f.filePath)
	}

	if err := f.appendLogEntry(logEntry); err != nil {
		return err
	}

	fmt.Printf("📄 FILE: Status report logged to %s\n", f.filePath)
	return nil
}

// HandleNotification handles incoming notifications by logging to file
func (f *FileAlertStrategy) HandleNotification(ctx context.Context, notification *WebhookNotification) error {
	logEntry := map[string]any{
		"timestamp":    notification.Timestamp.Format(time.RFC3339Nano),
		"level":        "info",
		"service.name": "quick_watch",
		"event.type":   "hook_notification",
		"hook.name":    notification.Target,
		"hook.type":    notification.Type,
		"message":      notification.Message,
	}

	if len(notification.Data) > 0 {
		logEntry["hook.data"] = notification.Data
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Writing hook notification to %s\n", f.filePath)
	}

	return f.appendLogEntry(logEntry)
}

// HandleNotificationWithAck handles incoming notifications with acknowledgement link
func (f *FileAlertStrategy) HandleNotificationWithAck(ctx context.Context, notification *WebhookNotification, ackURL string) error {
	logEntry := map[string]any{
		"timestamp":           notification.Timestamp.Format(time.RFC3339Nano),
		"level":               "info",
		"service.name":        "quick_watch",
		"event.type":          "hook_notification",
		"hook.name":           notification.Target,
		"hook.type":           notification.Type,
		"message":             notification.Message,
		"acknowledgement_url": ackURL,
	}

	if len(notification.Data) > 0 {
		logEntry["hook.data"] = notification.Data
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Writing hook notification with ack to %s\n", f.filePath)
	}

	return f.appendLogEntry(logEntry)
}

// SendNotificationAcknowledgement logs hook acknowledgement to file
func (f *FileAlertStrategy) SendNotificationAcknowledgement(ctx context.Context, hookName, acknowledgedBy, note, contact string) error {
	logEntry := map[string]any{
		"timestamp":       time.Now().Format(time.RFC3339Nano),
		"level":           "info",
		"service.name":    "quick_watch",
		"event.type":      "hook_acknowledged",
		"hook.name":       hookName,
		"acknowledged_by": acknowledgedBy,
	}

	if contact != "" {
		logEntry["acknowledgement_contact"] = contact
	}
	if note != "" {
		logEntry["acknowledgement_note"] = note
	}

	if f.debug {
		fmt.Printf("🐛 FILE DEBUG: Writing hook acknowledgement to %s\n", f.filePath)
	}

	return f.appendLogEntry(logEntry)
}
