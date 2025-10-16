package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"

	qc "github.com/bevelwork/quick_color"
	"gopkg.in/yaml.v3"
)

// handleEditMonitors opens the state file in the user's editor for modification
func handleEditMonitors(stateFile string) {
	stateManager := NewStateManager(stateFile)

	// Load existing state
	if err := stateManager.Load(); err != nil {
		log.Printf("Warning: Could not load existing state: %v", err)
	}

	// Get the editor from environment
	editor := os.Getenv("EDITOR")
	if editor == "" {
		// Try common editors
		editors := []string{"vim", "nano", "emacs", "code", "subl"}
		for _, e := range editors {
			if _, err := exec.LookPath(e); err == nil {
				editor = e
				break
			}
		}
		if editor == "" {
			fmt.Printf("%s No editor found. Please set $EDITOR environment variable.\n",
				qc.Colorize("Error:", qc.ColorRed))
			return
		}
	}

	fmt.Printf("%s Opening editor: %s\n", qc.Colorize("✏️ Info:", qc.ColorCyan), editor)
	fmt.Printf("State file: %s\n", stateFile)
	fmt.Println()

	// Create a temporary file with current state
	tempFile, err := createTempStateFile(stateManager)
	if err != nil {
		fmt.Printf("%s Failed to create temporary file: %v\n",
			qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}
	defer os.Remove(tempFile)

	// Open editor
	cmd := exec.Command(editor, tempFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("%s Editor exited with error: %v\n",
			qc.Colorize("⚠️ Warning:", qc.ColorYellow), err)
	}

	// Read the modified file
	modifiedData, err := ioutil.ReadFile(tempFile)
	if err != nil {
		fmt.Printf("%s Failed to read modified file: %v\n",
			qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Validate the modified YAML
	if err := validateYAML(modifiedData); err != nil {
		fmt.Printf("%s Invalid YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Println("Please fix the errors and try again.")
		return
	}

	// Parse the modified configuration (simplified structure)
	var monitorsData map[string]interface{}
	if err := yaml.Unmarshal(modifiedData, &monitorsData); err != nil {
		fmt.Printf("%s Failed to parse YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Extract monitors from the simplified structure
	monitorsMap := make(map[string]Monitor)
	monitorFieldsMap := make(map[string]*MonitorFields)
	if monitorsInterface, exists := monitorsData["monitors"]; exists {
		if monitorsMapInterface, ok := monitorsInterface.(map[string]interface{}); ok {
			for url, monitorInterface := range monitorsMapInterface {
				if monitorMap, ok := monitorInterface.(map[string]interface{}); ok {
					monitor := Monitor{}
					fields := &MonitorFields{}

					if name, ok := monitorMap["name"].(string); ok {
						monitor.Name = name
					}
					if urlVal, ok := monitorMap["url"].(string); ok {
						monitor.URL = urlVal
					}
					if method, ok := monitorMap["method"].(string); ok {
						monitor.Method = method
						fields.Method = true
					}
					if threshold, ok := monitorMap["threshold"].(int); ok {
						monitor.Threshold = threshold
						fields.Threshold = true
					}
					if statusCodes, ok := monitorMap["status_codes"].([]interface{}); ok {
						fields.StatusCodes = true
						for _, code := range statusCodes {
							if codeStr, ok := code.(string); ok {
								monitor.StatusCodes = append(monitor.StatusCodes, codeStr)
							}
						}
					}
					if sizeAlerts, ok := monitorMap["size_alerts"].(map[string]interface{}); ok {
						fields.SizeAlerts = true
						if enabled, ok := sizeAlerts["enabled"].(bool); ok {
							monitor.SizeAlerts.Enabled = enabled
						}
						if historySize, ok := sizeAlerts["history_size"].(int); ok {
							monitor.SizeAlerts.HistorySize = historySize
						}
						if threshold, ok := sizeAlerts["threshold"].(float64); ok {
							monitor.SizeAlerts.Threshold = threshold
						}
					}
					if checkStrategy, ok := monitorMap["check_strategy"].(string); ok {
						monitor.CheckStrategy = checkStrategy
						fields.CheckStrategy = true
					}
					if alertStrategy, ok := monitorMap["alert_strategy"].(string); ok {
						monitor.AlertStrategy = alertStrategy
						fields.AlertStrategy = true
					}

					// Check if headers was explicitly set (even if empty)
					if _, headersExists := monitorMap["headers"]; headersExists {
						fields.Headers = true
						if headers, ok := monitorMap["headers"].(map[string]interface{}); ok {
							monitor.Headers = make(map[string]string)
							for k, v := range headers {
								if vStr, ok := v.(string); ok {
									monitor.Headers[k] = vStr
								}
							}
						}
					}

					monitorsMap[url] = monitor
					monitorFieldsMap[url] = fields
				}
			}
		}
	}

	// Validate monitors (strict validation, no defaults applied)
	if err := validateMonitors(monitorsMap, stateManager); err != nil {
		fmt.Printf("%s Invalid monitors: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Printf("%s Please fix the validation errors and try again.\n", qc.Colorize("💡 Tip:", qc.ColorYellow))
		return
	}

	// Apply defaults for missing values
	applyDefaults(monitorsMap)

	// Clean defaults for explicitly set fields
	for url, monitor := range monitorsMap {
		if fields, exists := monitorFieldsMap[url]; exists {
			cleanDefaults(&monitor, fields)
			monitorsMap[url] = monitor
		}
	}

	// Save the changes by updating the state manager
	for url, monitor := range monitorsMap {
		if err := stateManager.AddMonitor(monitor); err != nil {
			fmt.Printf("%s Failed to save monitor %s: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), url, err)
			return
		}
	}

	// Show summary
	fmt.Printf("\n%s Edit Summary\n", qc.Colorize("📝 =", qc.ColorBlue))
	fmt.Printf("Monitors configured: %d\n", len(monitorsMap))
	fmt.Println()

	if len(monitorsMap) == 0 {
		fmt.Printf("%s No monitors configured\n", qc.Colorize("ℹ️ Info:", qc.ColorYellow))
		return
	}

	fmt.Printf("%s Configured Monitors:\n", qc.Colorize("📋 Info:", qc.ColorBlue))
	fmt.Println()

	i := 0
	for _, monitor := range monitorsMap {
		// Alternate row colors for better readability
		rowColor := qc.AlternatingColor(i, qc.ColorWhite, qc.ColorCyan)

		entry := fmt.Sprintf(
			"  %s %-30s %s",
			qc.Colorize(fmt.Sprintf("%d.", i+1), qc.ColorYellow),
			monitor.Name,
			monitor.URL,
		)
		fmt.Println(qc.Colorize(entry, rowColor))
		fmt.Printf("     Method: %s, Threshold: %ds, Check: %s, Alert: %s\n",
			monitor.Method, monitor.Threshold, monitor.CheckStrategy, monitor.AlertStrategy)
		i++
	}

	fmt.Printf("\n%s Configuration saved successfully!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
}

// createTempStateFile creates a temporary file with the current state for editing
func createTempStateFile(stateManager *StateManager) (string, error) {
	// Create temporary file
	tempFile, err := ioutil.TempFile("", "quick_watch_edit_*.yml")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	// Get current monitors
	monitors := stateManager.ListMonitors()

	// Create simplified YAML structure with only monitors
	monitorsOnly := map[string]interface{}{
		"monitors": make(map[string]Monitor),
	}

	// Copy monitors and clean defaults
	for url, monitor := range monitors {
		// Clean defaults from the monitor before adding to YAML
		cleanAllDefaults(&monitor)
		monitorsOnly["monitors"].(map[string]Monitor)[url] = monitor
	}

	// Marshal to YAML
	data, err := yaml.Marshal(monitorsOnly)
	if err != nil {
		return "", err
	}

	// Add helpful comments
	commentedData := addEditComments(data)

	// Write to temp file
	if _, err := tempFile.Write(commentedData); err != nil {
		return "", err
	}

	return tempFile.Name(), nil
}

// addEditComments adds helpful comments to the YAML for editing
func addEditComments(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	commentedLines := []string{
		"# To remove a monitor: delete its entry",
		"# To modify a monitor: edit its properties",
		"# To add a monitor: add a object with the list.",
		"# Required fields: name, url",
		"# Alert strategies: console, slack",
		"#",
		"# Full example with all options:",
		"#   comprehensive-example:",
		"#     name: \"Comprehensive Monitor\"",
		"#     url: \"https://api.example.com/health\"",
		"#     method: \"GET\"                    # HTTP method (default: GET)",
		"#     headers:                          # Custom headers (default: {})",
		"#       Authorization: \"Bearer token\"",
		"#       User-Agent: \"QuickWatch/1.0\"",
		"#     threshold: 60                     # Down threshold in seconds (default: 30)",
		"#     status_codes: [\"200\", \"201\"]    # Acceptable status codes (default: [\"*\"])",
		"#     size_alerts:                      # Page size change detection (default: disabled)",
		"#       enabled: true",
		"#       history_size: 100",
		"#       threshold: 0.5",
		"#     check_strategy: \"http\"           # Check strategy (default: http)",
		"#     alert_strategy: \"console\"        # Alert strategy (default: console)",
		"",
	}

	commentedLines = append(commentedLines, lines...)
	return []byte(strings.Join(commentedLines, "\n"))
}

// validateYAML validates that the YAML is well-formed
func validateYAML(data []byte) error {
	var temp interface{}
	return yaml.Unmarshal(data, &temp)
}

// validateMonitors validates monitor configurations without applying defaults
func validateMonitors(monitors map[string]Monitor, stateManager *StateManager) error {
	validHTTPMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
		"HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
	}

	validCheckStrategies := map[string]bool{
		"http": true,
	}

	// Get valid alert strategies from notifiers
	validAlertStrategies := make(map[string]bool)
	// Add default console strategy
	validAlertStrategies["console"] = true

	// Add notifier-based strategies
	if stateManager != nil {
		notifiers := stateManager.GetNotifiers()
		for name, notifier := range notifiers {
			if notifier.Enabled {
				validAlertStrategies[name] = true
			}
		}
	}

	for url, monitor := range monitors {
		// Required fields validation
		if monitor.URL == "" {
			return fmt.Errorf("monitor %s: url is REQUIRED and cannot be empty", url)
		}
		if monitor.Name == "" {
			return fmt.Errorf("monitor %s: name is REQUIRED and cannot be empty", url)
		}

		// Validate URL format (basic check)
		if !strings.HasPrefix(monitor.URL, "http://") && !strings.HasPrefix(monitor.URL, "https://") {
			return fmt.Errorf("monitor %s: url must start with http:// or https://", url)
		}

		// Validate method if provided (don't apply default, just validate)
		if monitor.Method != "" && !validHTTPMethods[strings.ToUpper(monitor.Method)] {
			return fmt.Errorf("monitor %s: invalid method '%s', must be one of: GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS, TRACE, CONNECT", url, monitor.Method)
		}

		// Validate threshold if provided (don't apply default, just validate)
		if monitor.Threshold < 0 {
			return fmt.Errorf("monitor %s: threshold must be a positive integer, got %d", url, monitor.Threshold)
		}

		// Validate check strategy if provided (don't apply default, just validate)
		if monitor.CheckStrategy != "" && !validCheckStrategies[monitor.CheckStrategy] {
			return fmt.Errorf("monitor %s: invalid check_strategy '%s', must be one of: http", url, monitor.CheckStrategy)
		}

		// Validate alert strategy if provided (don't apply default, just validate)
		if monitor.AlertStrategy != "" && !validAlertStrategies[monitor.AlertStrategy] {
			return fmt.Errorf("monitor %s: invalid alert_strategy '%s', must be one of: console", url, monitor.AlertStrategy)
		}
	}
	return nil
}

// applyDefaults applies default values to monitors where properties are missing
func applyDefaults(monitors map[string]Monitor) {
	for url, monitor := range monitors {
		// Clean defaults and show INFO messages
		cleanAllDefaults(&monitor)

		// Apply defaults only for missing values
		if monitor.Method == "" {
			monitor.Method = "GET"
		}
		if monitor.Threshold == 0 {
			monitor.Threshold = 30
		}
		if monitor.CheckStrategy == "" {
			monitor.CheckStrategy = "http"
		}
		if monitor.AlertStrategy == "" {
			monitor.AlertStrategy = "console"
		}
		if monitor.Headers == nil {
			monitor.Headers = make(map[string]string)
		}
		if len(monitor.StatusCodes) == 0 {
			monitor.StatusCodes = []string{"*"}
		}
		if monitor.SizeAlerts.HistorySize == 0 {
			monitor.SizeAlerts = SizeAlertConfig{
				Enabled:     true,
				HistorySize: 100,
				Threshold:   0.5, // 50% change threshold
			}
		}

		// Update the map with the modified monitor
		monitors[url] = monitor
	}
}

// editSettings allows editing global settings using $EDITOR
func editSettings(stateManager *StateManager) {
	fmt.Printf("%s Info: Opening editor: %s\n", qc.Colorize("✏️ Info:", qc.ColorCyan), os.Getenv("EDITOR"))
	fmt.Printf("State file: %s\n\n", stateManager.filePath)

	// Create temporary file with current settings
	tempFile, err := createTempSettingsFile(stateManager)
	if err != nil {
		fmt.Printf("%s Failed to create temporary file: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}
	defer os.Remove(tempFile)

	// Open editor
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim" // Default editor
	}

	cmd := exec.Command(editor, tempFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("%s Editor exited with error: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Read the modified settings
	modifiedData, err := ioutil.ReadFile(tempFile)
	if err != nil {
		fmt.Printf("%s Failed to read modified file: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Validate the modified YAML
	if err := validateSettingsYAML(modifiedData); err != nil {
		fmt.Printf("%s Invalid YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Println("Please fix the errors and try again.")
		return
	}

	// Parse the modified settings
	var settingsData map[string]interface{}
	if err := yaml.Unmarshal(modifiedData, &settingsData); err != nil {
		fmt.Printf("%s Failed to parse YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Extract settings
	settings := ServerSettings{
		WebhookPort:      8080,
		WebhookPath:      "/webhook",
		CheckInterval:    5,
		DefaultThreshold: 30,
		Startup: StartupConfig{
			Enabled:   true,
			Notifiers: []string{"console"},
		},
	}

	if webhookPort, ok := settingsData["webhook_port"].(int); ok {
		settings.WebhookPort = webhookPort
	}
	if webhookPath, ok := settingsData["webhook_path"].(string); ok {
		settings.WebhookPath = webhookPath
	}
	if checkInterval, ok := settingsData["check_interval"].(int); ok {
		settings.CheckInterval = checkInterval
	}
	if defaultThreshold, ok := settingsData["default_threshold"].(int); ok {
		settings.DefaultThreshold = defaultThreshold
	}

	// Parse startup configuration
	if startupData, ok := settingsData["startup"].(map[string]interface{}); ok {
		if enabled, ok := startupData["enabled"].(bool); ok {
			settings.Startup.Enabled = enabled
		}
		if notifiers, ok := startupData["notifiers"].([]interface{}); ok {
			settings.Startup.Notifiers = make([]string, len(notifiers))
			for i, notifier := range notifiers {
				if notifierStr, ok := notifier.(string); ok {
					settings.Startup.Notifiers[i] = notifierStr
				}
			}
		}
		if checkAllMonitors, ok := startupData["check_all_monitors"].(bool); ok {
			settings.Startup.CheckAllMonitors = checkAllMonitors
		}
	}

	// Handle legacy startup_message setting for backward compatibility
	if startupMessage, ok := settingsData["startup_message"].(bool); ok {
		settings.Startup.Enabled = startupMessage
		if startupMessage && len(settings.Startup.Notifiers) == 0 {
			settings.Startup.Notifiers = []string{"console"}
		}
	}

	// Validate settings
	if err := validateSettings(settings); err != nil {
		fmt.Printf("%s Invalid settings: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Printf("%s Please fix the validation errors and try again.\n", qc.Colorize("💡 Tip:", qc.ColorYellow))
		return
	}

	// Update settings in state manager
	if err := stateManager.UpdateSettings(settings); err != nil {
		fmt.Printf("%s Failed to update settings: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	fmt.Printf("%s Settings updated successfully!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
}

// createTempSettingsFile creates a temporary file with the current settings for editing
func createTempSettingsFile(stateManager *StateManager) (string, error) {
	// Create temporary file
	tempFile, err := ioutil.TempFile("", "quick_watch_settings_*.yml")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	// Get current settings
	settings := stateManager.GetSettings()

	// Create settings YAML structure
	settingsOnly := map[string]interface{}{
		"webhook_port":      settings.WebhookPort,
		"webhook_path":      settings.WebhookPath,
		"check_interval":    settings.CheckInterval,
		"default_threshold": settings.DefaultThreshold,
		"startup": map[string]interface{}{
			"enabled":            settings.Startup.Enabled,
			"notifiers":          settings.Startup.Notifiers,
			"check_all_monitors": settings.Startup.CheckAllMonitors,
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(settingsOnly)
	if err != nil {
		return "", err
	}

	// Add helpful comments
	commentedData := addSettingsComments(data)

	// Write to temp file
	if _, err := tempFile.Write(commentedData); err != nil {
		return "", err
	}

	return tempFile.Name(), nil
}

// addSettingsComments adds helpful comments to the settings YAML for editing
func addSettingsComments(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	commentedLines := []string{
		"# Quick Watch Global Settings",
		"# Edit the settings below",
		"#",
		"# webhook_port: Port for webhook server (default: 8080)",
		"# webhook_path: Path for webhook endpoint (default: /webhook)",
		"# check_interval: How often to check monitors in seconds (default: 5)",
		"# default_threshold: Default down threshold in seconds (default: 30)",
		"# startup:",
		"#   enabled: true/false (default: true)",
		"#   notifiers: [\"console\", \"slack-alerts\"] (default: [\"console\"])",
		"#   check_all_monitors: true/false (default: false)",
		"#",
		"",
	}

	commentedLines = append(commentedLines, lines...)
	return []byte(strings.Join(commentedLines, "\n"))
}

// validateSettingsYAML validates that the settings YAML is well-formed
func validateSettingsYAML(data []byte) error {
	var temp interface{}
	return yaml.Unmarshal(data, &temp)
}

// validateSettings validates settings configuration
func validateSettings(settings ServerSettings) error {
	if settings.WebhookPort < 1 || settings.WebhookPort > 65535 {
		return fmt.Errorf("webhook_port must be between 1 and 65535, got %d", settings.WebhookPort)
	}
	if settings.WebhookPath == "" {
		return fmt.Errorf("webhook_path cannot be empty")
	}
	if settings.CheckInterval < 1 {
		return fmt.Errorf("check_interval must be at least 1 second, got %d", settings.CheckInterval)
	}
	if settings.DefaultThreshold < 1 {
		return fmt.Errorf("default_threshold must be at least 1 second, got %d", settings.DefaultThreshold)
	}

	// Validate startup configuration
	if settings.Startup.Enabled && len(settings.Startup.Notifiers) == 0 {
		return fmt.Errorf("startup is enabled but no notifiers specified")
	}

	return nil
}

// editNotifiers allows editing notification configurations using $EDITOR
func editNotifiers(stateManager *StateManager) {
	fmt.Printf("%s Info: Opening editor: %s\n", qc.Colorize("✏️ Info:", qc.ColorCyan), os.Getenv("EDITOR"))
	fmt.Printf("State file: %s\n\n", stateManager.filePath)

	// Create temporary file with current notifiers
	tempFile, err := createTempNotifiersFile(stateManager)
	if err != nil {
		fmt.Printf("%s Failed to create temporary file: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}
	defer os.Remove(tempFile)

	// Open editor
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim" // Default editor
	}

	cmd := exec.Command(editor, tempFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("%s Editor exited with error: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Read the modified notifiers
	modifiedData, err := ioutil.ReadFile(tempFile)
	if err != nil {
		fmt.Printf("%s Failed to read modified file: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Validate the modified YAML
	if err := validateNotifiersYAML(modifiedData); err != nil {
		fmt.Printf("%s Invalid YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Println("Please fix the errors and try again.")
		return
	}

	// Parse the modified notifiers
	var notifiersData map[string]interface{}
	if err := yaml.Unmarshal(modifiedData, &notifiersData); err != nil {
		fmt.Printf("%s Failed to parse YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Extract notifiers
	notifiers := make(map[string]NotifierConfig)
	for name, notifierInterface := range notifiersData {
		if notifierMap, ok := notifierInterface.(map[string]interface{}); ok {
			notifier := NotifierConfig{
				Name:     name,
				Enabled:  true,
				Settings: make(map[string]interface{}),
			}

			if notifierType, ok := notifierMap["type"].(string); ok {
				notifier.Type = notifierType
			}
			if enabled, ok := notifierMap["enabled"].(bool); ok {
				notifier.Enabled = enabled
			}
			if description, ok := notifierMap["description"].(string); ok {
				notifier.Description = description
			}
			if settings, ok := notifierMap["settings"].(map[string]interface{}); ok {
				notifier.Settings = settings
			}

			notifiers[name] = notifier
		}
	}

	// Validate notifiers
	if err := validateNotifiers(notifiers); err != nil {
		fmt.Printf("%s Invalid notifiers: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Printf("%s Please fix the validation errors and try again.\n", qc.Colorize("💡 Tip:", qc.ColorYellow))
		return
	}

	// Update notifiers in state manager
	if err := stateManager.UpdateNotifiers(notifiers); err != nil {
		fmt.Printf("%s Failed to update notifiers: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	fmt.Printf("%s Notifiers updated successfully!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
}

// createTempNotifiersFile creates a temporary file with the current notifiers for editing
func createTempNotifiersFile(stateManager *StateManager) (string, error) {
	// Create temporary file
	tempFile, err := ioutil.TempFile("", "quick_watch_notifiers_*.yml")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	// Get current notifiers
	notifiers := stateManager.GetNotifiers()

	// If no notifiers exist, create default ones
	if len(notifiers) == 0 {
		notifiers = map[string]NotifierConfig{
			"console": {
				Name:        "console",
				Type:        "console",
				Enabled:     true,
				Description: "Default console output",
				Settings: map[string]interface{}{
					"style": "stylized",
					"color": true,
				},
			},
		}
	}

	// Marshal to YAML
	data, err := yaml.Marshal(notifiers)
	if err != nil {
		return "", err
	}

	// Add helpful comments
	commentedData := addNotifiersComments(data)

	// Write to temp file
	if _, err := tempFile.Write(commentedData); err != nil {
		return "", err
	}

	return tempFile.Name(), nil
}

// addNotifiersComments adds helpful comments to the notifiers YAML for editing
func addNotifiersComments(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	commentedLines := []string{
		"# Quick Watch Notification Configurations",
		"# Edit the notifiers below",
		"#",
		"# Notifier Types:",
		"#   console: Output to console (supports 'plain' or 'stylized' style)",
		"#   slack: Send to Slack webhook",
		"#",
		"# Console Settings:",
		"#   style: 'plain' or 'stylized' (default: stylized)",
		"#   color: true/false (default: true)",
		"#",
		"# Slack Settings:",
		"#   webhook_url: Slack webhook URL (required)",
		"#   channel: Slack channel (optional)",
		"#   username: Bot username (optional)",
		"#   icon_emoji: Bot icon emoji (optional)",
		"#",
		"# Example notifiers:",
		"#   console:",
		"#     type: console",
		"#     enabled: true",
		"#     description: \"Console output\"",
		"#     settings:",
		"#       style: stylized",
		"#       color: true",
		"#",
		"#   slack-alerts:",
		"#     type: slack",
		"#     enabled: true",
		"#     description: \"Slack alerts channel\"",
		"#     settings:",
		"#       webhook_url: \"https://hooks.slack.com/services/...\"",
		"#       channel: \"#alerts\"",
		"#       username: \"QuickWatch\"",
		"#       icon_emoji: \":robot_face:\"",
		"#",
		"",
	}

	commentedLines = append(commentedLines, lines...)
	return []byte(strings.Join(commentedLines, "\n"))
}

// validateNotifiersYAML validates that the notifiers YAML is well-formed
func validateNotifiersYAML(data []byte) error {
	var temp interface{}
	return yaml.Unmarshal(data, &temp)
}

// validateNotifiers validates notifier configurations
func validateNotifiers(notifiers map[string]NotifierConfig) error {
	for name, notifier := range notifiers {
		if notifier.Name == "" {
			return fmt.Errorf("notifier %s: name cannot be empty", name)
		}
		if notifier.Type == "" {
			return fmt.Errorf("notifier %s: type is required", name)
		}

		switch notifier.Type {
		case "console":
			// Validate console settings
			if style, ok := notifier.Settings["style"].(string); ok {
				if style != "plain" && style != "stylized" {
					return fmt.Errorf("notifier %s: console style must be 'plain' or 'stylized', got '%s'", name, style)
				}
			}
		case "slack":
			// Validate Slack settings
			webhookURL, ok := notifier.Settings["webhook_url"].(string)
			if !ok || webhookURL == "" {
				return fmt.Errorf("notifier %s: slack webhook_url is required", name)
			}
			if !strings.HasPrefix(webhookURL, "https://hooks.slack.com/") {
				return fmt.Errorf("notifier %s: slack webhook_url must be a valid Slack webhook URL", name)
			}
		default:
			return fmt.Errorf("notifier %s: unknown type '%s', must be 'console' or 'slack'", name, notifier.Type)
		}
	}
	return nil
}

// validateStateFile validates a state file
func validateStateFile(stateFile string, verbose bool) {
	if verbose {
		fmt.Printf("%s Validating state file: %s\n", qc.Colorize("🔍 Info:", qc.ColorCyan), stateFile)
	}

	// Create state manager
	stateManager := NewStateManager(stateFile)
	if err := stateManager.Load(); err != nil {
		fmt.Printf("%s Failed to load state file: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		os.Exit(1)
	}

	// Get monitors and notifiers
	monitorConfig := stateManager.GetMonitorConfig()
	monitors := monitorConfig.Monitors
	notifiers := stateManager.GetNotifiers()

	// Validate monitors
	validAlertStrategies := getValidAlertStrategies(notifiers)

	errors := []string{}
	warnings := []string{}

	// Check each monitor
	for url, monitor := range monitors {
		// Check required fields
		if monitor.Name == "" {
			errors = append(errors, fmt.Sprintf("Monitor %s: name is required", url))
		}
		if monitor.URL == "" {
			errors = append(errors, fmt.Sprintf("Monitor %s: url is required", url))
		}

		// Check alert strategy
		if monitor.AlertStrategy != "" {
			if !validAlertStrategies[monitor.AlertStrategy] {
				errors = append(errors, fmt.Sprintf("Monitor %s: invalid alert_strategy '%s', must be one of: %s",
					url, monitor.AlertStrategy, getValidStrategiesList(validAlertStrategies)))
			} else if verbose {
				fmt.Printf("  ✓ Monitor %s: alert_strategy '%s' is valid\n", url, monitor.AlertStrategy)
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("Monitor %s: no alert_strategy specified, will use default 'console'", url))
		}

		// Check URL format
		if monitor.URL != "" && !strings.HasPrefix(monitor.URL, "http://") && !strings.HasPrefix(monitor.URL, "https://") {
			errors = append(errors, fmt.Sprintf("Monitor %s: url must start with http:// or https://", url))
		}

		// Check HTTP method
		validMethods := map[string]bool{
			"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
			"HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
		}
		if monitor.Method != "" && !validMethods[monitor.Method] {
			errors = append(errors, fmt.Sprintf("Monitor %s: invalid method '%s'", url, monitor.Method))
		}
	}

	// Check notifiers
	for name, notifier := range notifiers {
		if notifier.Enabled {
			if notifier.Type == "slack" {
				if webhookURL, ok := notifier.Settings["webhook_url"].(string); !ok || webhookURL == "" {
					errors = append(errors, fmt.Sprintf("Notifier %s: slack webhook_url is required", name))
				} else if !strings.HasPrefix(webhookURL, "https://hooks.slack.com/") {
					errors = append(errors, fmt.Sprintf("Notifier %s: slack webhook_url must be a valid Slack webhook URL", name))
				}
			}
		}
	}

	// Print results
	if len(errors) == 0 && len(warnings) == 0 {
		fmt.Printf("%s Configuration is valid!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
		if verbose {
			fmt.Printf("  • %d monitors configured\n", len(monitors))
			fmt.Printf("  • %d notifiers configured\n", len(notifiers))
		}
		os.Exit(0)
	}

	// Print warnings
	if len(warnings) > 0 {
		for _, warning := range warnings {
			fmt.Printf("%s %s\n", qc.Colorize("⚠️  Warning:", qc.ColorYellow), warning)
		}
	}

	// Print errors
	if len(errors) > 0 {
		for _, error := range errors {
			fmt.Printf("%s %s\n", qc.Colorize("❌ Error:", qc.ColorRed), error)
		}
		os.Exit(1)
	}
}

// validateConfigFile validates a configuration file
func validateConfigFile(configFile string, verbose bool) {
	if verbose {
		fmt.Printf("%s Validating config file: %s\n", qc.Colorize("🔍 Info:", qc.ColorCyan), configFile)
	}

	// Read and parse the full config file
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		fmt.Printf("%s Failed to read config file: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		os.Exit(1)
	}

	// Parse the full YAML structure
	var configData map[string]interface{}
	if err := yaml.Unmarshal(data, &configData); err != nil {
		fmt.Printf("%s Failed to parse YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		os.Exit(1)
	}

	// Extract monitors and notifiers
	monitors := make(map[string]Monitor)
	notifiers := make(map[string]NotifierConfig)

	// Parse monitors
	if monitorsData, exists := configData["monitors"]; exists {
		if monitorsMap, ok := monitorsData.(map[string]interface{}); ok {
			for url, monitorInterface := range monitorsMap {
				if monitorMap, ok := monitorInterface.(map[string]interface{}); ok {
					monitor := Monitor{}
					if name, ok := monitorMap["name"].(string); ok {
						monitor.Name = name
					}
					if urlVal, ok := monitorMap["url"].(string); ok {
						monitor.URL = urlVal
					}
					if method, ok := monitorMap["method"].(string); ok {
						monitor.Method = method
					}
					if threshold, ok := monitorMap["threshold"].(int); ok {
						monitor.Threshold = threshold
					}
					if checkStrategy, ok := monitorMap["check_strategy"].(string); ok {
						monitor.CheckStrategy = checkStrategy
					}
					if alertStrategy, ok := monitorMap["alert_strategy"].(string); ok {
						monitor.AlertStrategy = alertStrategy
					}
					monitors[url] = monitor
				}
			}
		}
	}

	// Parse notifiers
	if notifiersData, exists := configData["notifiers"]; exists {
		if notifiersMap, ok := notifiersData.(map[string]interface{}); ok {
			for name, notifierInterface := range notifiersMap {
				if notifierMap, ok := notifierInterface.(map[string]interface{}); ok {
					notifier := NotifierConfig{
						Name:     name,
						Enabled:  true,
						Settings: make(map[string]interface{}),
					}
					if notifierType, ok := notifierMap["type"].(string); ok {
						notifier.Type = notifierType
					}
					if enabled, ok := notifierMap["enabled"].(bool); ok {
						notifier.Enabled = enabled
					}
					if description, ok := notifierMap["description"].(string); ok {
						notifier.Description = description
					}
					if settings, ok := notifierMap["settings"].(map[string]interface{}); ok {
						notifier.Settings = settings
					}
					notifiers[name] = notifier
				}
			}
		}
	}

	// Validate monitors
	validAlertStrategies := getValidAlertStrategies(notifiers)

	errors := []string{}
	warnings := []string{}

	// Check each monitor
	for url, monitor := range monitors {
		// Check required fields
		if monitor.Name == "" {
			errors = append(errors, fmt.Sprintf("Monitor %s: name is required", url))
		}
		if monitor.URL == "" {
			errors = append(errors, fmt.Sprintf("Monitor %s: url is required", url))
		}

		// Check alert strategy
		if monitor.AlertStrategy != "" {
			if !validAlertStrategies[monitor.AlertStrategy] {
				errors = append(errors, fmt.Sprintf("Monitor %s: invalid alert_strategy '%s', must be one of: %s",
					url, monitor.AlertStrategy, getValidStrategiesList(validAlertStrategies)))
			} else if verbose {
				fmt.Printf("  ✓ Monitor %s: alert_strategy '%s' is valid\n", url, monitor.AlertStrategy)
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("Monitor %s: no alert_strategy specified, will use default 'console'", url))
		}

		// Check URL format
		if monitor.URL != "" && !strings.HasPrefix(monitor.URL, "http://") && !strings.HasPrefix(monitor.URL, "https://") {
			errors = append(errors, fmt.Sprintf("Monitor %s: url must start with http:// or https://", url))
		}

		// Check HTTP method
		validMethods := map[string]bool{
			"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
			"HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
		}
		if monitor.Method != "" && !validMethods[monitor.Method] {
			errors = append(errors, fmt.Sprintf("Monitor %s: invalid method '%s'", url, monitor.Method))
		}
	}

	// Check notifiers
	for name, notifier := range notifiers {
		if notifier.Enabled {
			if notifier.Type == "slack" {
				if webhookURL, ok := notifier.Settings["webhook_url"].(string); !ok || webhookURL == "" {
					errors = append(errors, fmt.Sprintf("Notifier %s: slack webhook_url is required", name))
				} else if !strings.HasPrefix(webhookURL, "https://hooks.slack.com/") {
					errors = append(errors, fmt.Sprintf("Notifier %s: slack webhook_url must be a valid Slack webhook URL", name))
				}
			}
		}
	}

	// Print results
	if len(errors) == 0 && len(warnings) == 0 {
		fmt.Printf("%s Configuration is valid!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
		if verbose {
			fmt.Printf("  • %d monitors configured\n", len(monitors))
			fmt.Printf("  • %d notifiers configured\n", len(notifiers))
		}
		os.Exit(0)
	}

	// Print warnings
	if len(warnings) > 0 {
		for _, warning := range warnings {
			fmt.Printf("%s %s\n", qc.Colorize("⚠️  Warning:", qc.ColorYellow), warning)
		}
	}

	// Print errors
	if len(errors) > 0 {
		for _, error := range errors {
			fmt.Printf("%s %s\n", qc.Colorize("❌ Error:", qc.ColorRed), error)
		}
		os.Exit(1)
	}
}

// getValidAlertStrategies returns a map of valid alert strategies from notifiers
func getValidAlertStrategies(notifiers map[string]NotifierConfig) map[string]bool {
	validStrategies := make(map[string]bool)
	validStrategies["console"] = true // Default console strategy

	for name, notifier := range notifiers {
		if notifier.Enabled {
			validStrategies[name] = true
		}
	}

	return validStrategies
}

// getValidStrategiesList returns a comma-separated list of valid strategies
func getValidStrategiesList(validStrategies map[string]bool) string {
	strategies := []string{}
	for strategy := range validStrategies {
		strategies = append(strategies, strategy)
	}
	return strings.Join(strategies, ", ")
}

// isValidAlertStrategy checks if an alert strategy is valid for the engine
func isValidAlertStrategy(strategy string, engine *MonitoringEngine) bool {
	_, exists := engine.alertStrategies[strategy]
	return exists
}
