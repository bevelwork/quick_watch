package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"

	qc "github.com/bevelwork/quick_color"
	"gopkg.in/yaml.v3"
)

// handleEditTargets opens the state file in the user's editor for modification
func handleEditTargets(stateFile string) {
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
	modifiedData, err := os.ReadFile(tempFile)
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
	var targetsData map[string]interface{}
	if err := yaml.Unmarshal(modifiedData, &targetsData); err != nil {
		fmt.Printf("%s Failed to parse YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Extract targets (new) or targets (legacy) from the simplified structure
	targetsMap := make(map[string]Target)
	targetFieldsMap := make(map[string]*TargetFields)
	key := "targets"
	targetsInterface, exists := targetsData[key]
	if !exists {
		key = "targets" // legacy fallback
		targetsInterface = targetsData[key]
	}
	if targetsInterface != nil {
		// Accept either map[string]Target-like or []Target-like entries
		if targetsMapInterface, ok := targetsInterface.(map[string]interface{}); ok {
			for url, targetInterface := range targetsMapInterface {
				if targetMap, ok := targetInterface.(map[string]interface{}); ok {
					target := Target{}
					fields := &TargetFields{}

					if name, ok := targetMap["name"].(string); ok {
						target.Name = name
					}
					if urlVal, ok := targetMap["url"].(string); ok {
						target.URL = urlVal
					}
					if method, ok := targetMap["method"].(string); ok {
						target.Method = method
						fields.Method = true
					}
					if threshold, ok := targetMap["threshold"].(int); ok {
						target.Threshold = threshold
						fields.Threshold = true
					}
					if statusCodes, ok := targetMap["status_codes"].([]interface{}); ok {
						fields.StatusCodes = true
						for _, code := range statusCodes {
							if codeStr, ok := code.(string); ok {
								target.StatusCodes = append(target.StatusCodes, codeStr)
							}
						}
					}
					if sizeAlerts, ok := targetMap["size_alerts"].(map[string]interface{}); ok {
						fields.SizeAlerts = true
						if enabled, ok := sizeAlerts["enabled"].(bool); ok {
							target.SizeAlerts.Enabled = enabled
						}
						if historySize, ok := sizeAlerts["history_size"].(int); ok {
							target.SizeAlerts.HistorySize = historySize
						}
						if threshold, ok := sizeAlerts["threshold"].(float64); ok {
							target.SizeAlerts.Threshold = threshold
						}
					}
					if checkStrategy, ok := targetMap["check_strategy"].(string); ok {
						target.CheckStrategy = checkStrategy
						fields.CheckStrategy = true
					}

					// Check if headers was explicitly set (even if empty)
					if _, headersExists := targetMap["headers"]; headersExists {
						fields.Headers = true
						if headers, ok := targetMap["headers"].(map[string]interface{}); ok {
							target.Headers = make(map[string]string)
							for k, v := range headers {
								if vStr, ok := v.(string); ok {
									target.Headers[k] = vStr
								}
							}
						}
					}

					// If URL key is empty, use target.URL as key
					keyURL := url
					if keyURL == "" {
						keyURL = target.URL
					}
					targetsMap[keyURL] = target
					targetFieldsMap[url] = fields
				}
			}
		} else if targetsSlice, ok := targetsInterface.([]interface{}); ok {
			for _, targetInterface := range targetsSlice {
				if targetMap, ok := targetInterface.(map[string]interface{}); ok {
					target := Target{}
					fields := &TargetFields{}

					if name, ok := targetMap["name"].(string); ok {
						target.Name = name
					}
					if urlVal, ok := targetMap["url"].(string); ok {
						target.URL = urlVal
					}
					if method, ok := targetMap["method"].(string); ok {
						target.Method = method
						fields.Method = true
					}
					if threshold, ok := targetMap["threshold"].(int); ok {
						target.Threshold = threshold
						fields.Threshold = true
					}
					if statusCodes, ok := targetMap["status_codes"].([]interface{}); ok {
						fields.StatusCodes = true
						for _, code := range statusCodes {
							if codeStr, ok := code.(string); ok {
								target.StatusCodes = append(target.StatusCodes, codeStr)
							}
						}
					}
					if sizeAlerts, ok := targetMap["size_alerts"].(map[string]interface{}); ok {
						fields.SizeAlerts = true
						if enabled, ok := sizeAlerts["enabled"].(bool); ok {
							target.SizeAlerts.Enabled = enabled
						}
						if historySize, ok := sizeAlerts["history_size"].(int); ok {
							target.SizeAlerts.HistorySize = historySize
						}
						if threshold, ok := sizeAlerts["threshold"].(float64); ok {
							target.SizeAlerts.Threshold = threshold
						}
					}
					if checkStrategy, ok := targetMap["check_strategy"].(string); ok {
						target.CheckStrategy = checkStrategy
						fields.CheckStrategy = true
					}

					if target.URL != "" {
						targetsMap[target.URL] = target
						targetFieldsMap[target.URL] = fields
					}
				}
			}
		}
	}

	// Fallback: if nothing parsed, try robust parser that accepts multiple shapes
	if len(targetsMap) == 0 {
		if parsed, parsedFields, err := parseTargetsFromYAML(modifiedData); err == nil {
			if len(parsed) > 0 {
				targetsMap = parsed
				targetFieldsMap = parsedFields
			}
		}
	}

	// Validate targets (strict validation, no defaults applied)
	if err := validateTargets(targetsMap, stateManager); err != nil {
		fmt.Printf("%s Invalid targets: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Printf("%s Please fix the validation errors and try again.\n", qc.Colorize("💡 Tip:", qc.ColorYellow))
		return
	}

	// Do not apply defaults here: preserve existing state for unspecified fields

	// Preserve user-provided values exactly as entered; do not strip defaults

	// Save the changes by updating the state manager
	for url, target := range targetsMap {
		// Merge with existing target to avoid overwriting unspecified fields
		if existing, ok := stateManager.GetTarget(url); ok {
			fields := targetFieldsMap[url]
			if fields == nil {
				fields = &TargetFields{}
			}
			// Preserve unspecified fields
			if !fields.Method && target.Method == "" {
				target.Method = existing.Method
			}
			if !fields.Headers && len(target.Headers) == 0 && existing.Headers != nil {
				target.Headers = existing.Headers
			}
			if !fields.Threshold && target.Threshold == 0 {
				target.Threshold = existing.Threshold
			}
			if !fields.StatusCodes && len(target.StatusCodes) == 0 && len(existing.StatusCodes) > 0 {
				target.StatusCodes = existing.StatusCodes
			}
			if !fields.SizeAlerts && (target.SizeAlerts == (SizeAlertConfig{})) {
				target.SizeAlerts = existing.SizeAlerts
			}
			if !fields.CheckStrategy && target.CheckStrategy == "" {
				target.CheckStrategy = existing.CheckStrategy
			}
			if !fields.Alerts && len(target.Alerts) == 0 {
				if len(existing.Alerts) > 0 {
					target.Alerts = existing.Alerts
				} else if existing.AlertStrategy != "" {
					// Legacy fallback
					target.Alerts = []string{existing.AlertStrategy}
				}
			}
			if target.Name == "" {
				target.Name = existing.Name
			}
		}

		if err := stateManager.AddTarget(target); err != nil {
			fmt.Printf("%s Failed to save target %s: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), url, err)
			return
		}
	}

	// Show summary
	fmt.Printf("\n%s Edit Summary\n", qc.Colorize("📝 =", qc.ColorBlue))
	fmt.Printf("Targets configured: %d\n", len(targetsMap))
	fmt.Println()

	if len(targetsMap) == 0 {
		fmt.Printf("%s No targets configured\n", qc.Colorize("ℹ️ Info:", qc.ColorYellow))
		return
	}

	fmt.Printf("%s Configured Targets:\n", qc.Colorize("📋 Info:", qc.ColorBlue))
	fmt.Println()

	i := 0
	for _, target := range targetsMap {
		// Alternate row colors for better readability
		rowColor := qc.AlternatingColor(i, qc.ColorWhite, qc.ColorCyan)

		entry := fmt.Sprintf(
			"  %s %-30s %s",
			qc.Colorize(fmt.Sprintf("%d.", i+1), qc.ColorYellow),
			target.Name,
			target.URL,
		)
		fmt.Println(qc.Colorize(entry, rowColor))
		// Render effective defaults for display (editor preserves empties for state)
		displayMethod := target.Method
		if strings.TrimSpace(displayMethod) == "" {
			displayMethod = "GET"
		}
		displayThreshold := target.Threshold
		if displayThreshold == 0 {
			displayThreshold = 30
		}
		displayCheck := target.CheckStrategy
		if strings.TrimSpace(displayCheck) == "" {
			displayCheck = "http"
		}
		displayAlerts := strings.Join(target.Alerts, ", ")
		if strings.TrimSpace(displayAlerts) == "" {
			if strings.TrimSpace(target.AlertStrategy) != "" {
				displayAlerts = target.AlertStrategy
			} else {
				displayAlerts = "console"
			}
		}
		fmt.Printf("     Method: %s, Threshold: %ds, Check: %s, Alert: %s\n",
			displayMethod, displayThreshold, displayCheck, displayAlerts)
		i++
	}

	fmt.Printf("\n%s Configuration saved successfully!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
}

// createTempStateFile creates a temporary file with the current state for editing
func createTempStateFile(stateManager *StateManager) (string, error) {
	// Create temporary file
	tempFile, err := os.CreateTemp("", "quick_watch_edit_*.yml")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	// Get current targets
	targets := stateManager.ListTargets()

	// Create simplified YAML: top-level map with target name keys and minimal fields
	simplified := make(map[string]map[string]interface{})
	for _, target := range targets {
		// Prefer using existing name, fallback to URL
		name := target.Name
		if strings.TrimSpace(name) == "" {
			name = target.URL
		}
		entry := map[string]interface{}{
			"url": target.URL,
		}
		// Include alerts field to preserve user-set alerts
		if len(target.Alerts) > 0 {
			entry["alerts"] = target.Alerts
		} else if target.AlertStrategy != "" {
			entry["alerts"] = []string{target.AlertStrategy}
		}
		simplified[name] = entry
	}

	// Marshal to YAML
	data, err := yaml.Marshal(simplified)
	if err != nil {
		return "", err
	}

	// Build list of available alert names for comments: enabled alerts + default 'console' if not already present
	availableAlerts := []string{}
	hasConsole := false
	for name, alert := range stateManager.GetAlerts() {
		if alert.Enabled {
			availableAlerts = append(availableAlerts, name)
			if name == "console" {
				hasConsole = true
			}
		}
	}
	// Add default console if not already present
	if !hasConsole {
		availableAlerts = append(availableAlerts, "console")
	}
	sort.Strings(availableAlerts)

	// Add helpful comments tailored to simplified format (including available alerts)
	commentedData := addEditCommentsForSimplified(data, availableAlerts)

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
		"# To remove a target: delete its entry",
		"# To modify a target: edit its properties",
		"# To add a target: add an object to the list.",
		"# __Required fields__: name, url",
		"# Alert alerts: [console, slack]",
		"#",
		"# Full example with all options:",
		"#   my-full-config:",
		"#     name: \"Comprehensive Target\"",
		"#     url: \"https://api.example.com/health\"",
		"#     method: \"GET\"                    # HTTP method (default: GET)",
		"#     headers:                          # Custom headers (default: {})",
		"#       Authorization: \"Bearer token\"",
		"#       User-Agent: \"QuickWatch/1.0\"",
		"#     threshold: 60                     # Down threshold in seconds (default: 30s)",
		"#     status_codes: [\"200\", \"201\"]    # Acceptable status codes (default: [\"*\"])",
		"#     size_alerts:                      # Page size change detection (default: disabled)",
		"#       enabled: true",
		"#       history_size: 100",
		"#       threshold: 0.5",
		"#     check_strategy: \"http\"    # Check strategy",
		"#     alerts: [\"console\"        # Alert strategy",
		"",
	}

	commentedLines = append(commentedLines, lines...)
	return []byte(strings.Join(commentedLines, "\n"))
}

// addEditCommentsForSimplified adds comments for the simplified targets editor
func addEditCommentsForSimplified(data []byte, availableAlerts []string) []byte {
	lines := strings.Split(string(data), "\n")
	alertsComment := "#   alerts: [console]"
	if len(availableAlerts) > 0 {
		alertsComment = "#   alerts: [console]          # [" + strings.Join(availableAlerts, "|") + "]"
	}
	commentedLines := []string{
		"# Edit targets below. Each key is the target name.",
		"# Only 'url' is required. Other fields are optional and have defaults.",
		"#",
		"# Full example (defaults shown; omit to use defaults):",
		"# my-target:",
		"#   url: https://bevel.work              # REQUIRED",
		"#   method: GET                          # default: GET",
		"#   headers:                             # default: {} (none)",
		"#     Authorization: Bearer <token>",
		"#   threshold: 30                        # seconds; default: 30",
		"#   status_codes: ['*']                 # accepts any by default",
		"#   size_alerts:                         # page size change alerts (enabled by default)",
		"#     enabled: true",
		"#     history_size: 100",
		"#     threshold: 0.5                     # 50% change",
		"#   check_strategy: http                 # default: http",
		alertsComment,
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

// validateTargets validates target configurations without applying defaults
func validateTargets(targets map[string]Target, stateManager *StateManager) error {
	validHTTPMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
		"HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
	}

	validCheckStrategies := map[string]bool{
		"http": true,
	}

	// Get valid alert alerts from alerts
	validAlerts := make(map[string]bool)
	// Add default console strategy
	validAlerts["console"] = true

	// Add alert-based alerts
	if stateManager != nil {
		alerts := stateManager.GetAlerts()
		for name, alert := range alerts {
			if alert.Enabled {
				validAlerts[name] = true
			}
		}
	}

	for url, target := range targets {
		// Required fields validation
		if target.URL == "" {
			return fmt.Errorf("target %s: url is REQUIRED and cannot be empty", url)
		}
		if target.Name == "" {
			return fmt.Errorf("target %s: name is REQUIRED and cannot be empty", url)
		}

		// Validate URL format (basic check)
		if !strings.HasPrefix(target.URL, "http://") && !strings.HasPrefix(target.URL, "https://") {
			return fmt.Errorf("target %s: url must start with http:// or https://", url)
		}

		// Validate method if provided (don't apply default, just validate)
		if target.Method != "" && !validHTTPMethods[strings.ToUpper(target.Method)] {
			return fmt.Errorf("target %s: invalid method '%s', must be one of: GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS, TRACE, CONNECT", url, target.Method)
		}

		// Validate threshold if provided (don't apply default, just validate)
		if target.Threshold < 0 {
			return fmt.Errorf("target %s: threshold must be a positive integer, got %d", url, target.Threshold)
		}

		// Validate check strategy if provided (don't apply default, just validate)
		if target.CheckStrategy != "" && !validCheckStrategies[target.CheckStrategy] {
			return fmt.Errorf("target %s: invalid check_strategy '%s', must be one of: http", url, target.CheckStrategy)
		}
	}
	return nil
}

// applyDefaults applies default values to targets where properties are missing
func applyDefaults(targets map[string]Target) {
	for url, target := range targets {
		// Clean defaults and show INFO messages
		cleanAllDefaults(&target)

		// Apply defaults only for missing values
		if target.Method == "" {
			target.Method = "GET"
		}
		if target.Threshold == 0 {
			target.Threshold = 30
		}
		if target.CheckStrategy == "" {
			target.CheckStrategy = "http"
		}
		if target.Headers == nil {
			target.Headers = make(map[string]string)
		}
		if len(target.StatusCodes) == 0 {
			target.StatusCodes = []string{"*"}
		}
		if target.SizeAlerts.HistorySize == 0 {
			target.SizeAlerts = SizeAlertConfig{
				Enabled:     true,
				HistorySize: 100,
				Threshold:   0.5, // 50% change threshold
			}
		}

		// Update the map with the modified target
		targets[url] = target
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
	modifiedData, err := os.ReadFile(tempFile)
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
			Enabled: true,
			Alerts:  []string{"console"},
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
		// New key: alerts
		if al, ok := startupData["alerts"].([]interface{}); ok {
			settings.Startup.Alerts = make([]string, 0, len(al))
			for _, a := range al {
				if s, ok := a.(string); ok {
					settings.Startup.Alerts = append(settings.Startup.Alerts, s)
				}
			}
		} else if alerts, ok := startupData["alerts"].([]interface{}); ok { // legacy
			settings.Startup.Alerts = make([]string, 0, len(alerts))
			for _, alert := range alerts {
				if alertStr, ok := alert.(string); ok {
					settings.Startup.Alerts = append(settings.Startup.Alerts, alertStr)
				}
			}
		}
		if checkAllTargets, ok := startupData["check_all_targets"].(bool); ok {
			settings.Startup.CheckAllTargets = checkAllTargets
		}
	}

	// Handle legacy startup_message setting for backward compatibility
	if startupMessage, ok := settingsData["startup_message"].(bool); ok {
		settings.Startup.Enabled = startupMessage
		if startupMessage && len(settings.Startup.Alerts) == 0 {
			settings.Startup.Alerts = []string{"console"}
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
	tempFile, err := os.CreateTemp("", "quick_watch_settings_*.yml")
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
			"enabled":           settings.Startup.Enabled,
			"alerts":            settings.Startup.Alerts,
			"check_all_targets": settings.Startup.CheckAllTargets,
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
		"# check_interval: How often to check targets in seconds (default: 5s)",
		"# default_threshold: Default down threshold in seconds (default: 30s)",
		"# startup:",
		"#   enabled: true/false (default: true)",
		"#   alerts: [\"console\", \"slack-alerts\"] (default: [\"console\"])",
		"#   check_all_targets: true/false (default: false)",
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
	if settings.Startup.Enabled && len(settings.Startup.Alerts) == 0 {
		return fmt.Errorf("startup is enabled but no alerts specified")
	}

	return nil
}

// editAlerts allows editing notification configurations using $EDITOR
func editAlerts(stateManager *StateManager) {
	fmt.Printf("%s Info: Opening editor: %s\n", qc.Colorize("✏️ Info:", qc.ColorCyan), os.Getenv("EDITOR"))
	fmt.Printf("State file: %s\n\n", stateManager.filePath)

	// Create temporary file with current alerts
	tempFile, err := createTempAlertsFile(stateManager)
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

	// Read the modified alerts
	modifiedData, err := os.ReadFile(tempFile)
	if err != nil {
		fmt.Printf("%s Failed to read modified file: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Validate the modified YAML
	if err := validateAlertsYAML(modifiedData); err != nil {
		fmt.Printf("%s Invalid YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Println("Please fix the errors and try again.")
		return
	}

	// Parse the modified alerts (new) or alerts (legacy)
	var raw map[string]interface{}
	if err := yaml.Unmarshal(modifiedData, &raw); err != nil {
		fmt.Printf("%s Failed to parse YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Extract alerts
	alerts := make(map[string]NotifierConfig)
	var iter map[string]interface{}
	if v, ok := raw["alerts"].(map[string]interface{}); ok {
		iter = v
	} else if v, ok := raw["alerts"].(map[string]interface{}); ok { // legacy fallback
		iter = v
	} else {
		// Simplified top-level map format: alertName: { type: ..., ... }
		iter = raw
	}
	for name, alertInterface := range iter {
		if alertMap, ok := alertInterface.(map[string]interface{}); ok {
			alert := NotifierConfig{
				Name:     name,
				Enabled:  true,
				Settings: make(map[string]interface{}),
			}

			if alertType, ok := alertMap["type"].(string); ok {
				alert.Type = alertType
			}
			if enabled, ok := alertMap["enabled"].(bool); ok {
				alert.Enabled = enabled
			}
			if description, ok := alertMap["description"].(string); ok {
				alert.Description = description
			}
			if settings, ok := alertMap["settings"].(map[string]interface{}); ok {
				alert.Settings = settings
			}

			alerts[name] = alert
		}
	}

	// Validate alerts
	if err := validateAlerts(alerts); err != nil {
		fmt.Printf("%s Invalid alerts: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Printf("%s Please fix the validation errors and try again.\n", qc.Colorize("💡 Tip:", qc.ColorYellow))
		return
	}

	// Update alerts in state manager
	if err := stateManager.UpdateAlerts(alerts); err != nil {
		fmt.Printf("%s Failed to update alerts: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	fmt.Printf("%s Alerts updated successfully!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
}

// createTempAlertsFile creates a temporary file with the current alerts for editing
func createTempAlertsFile(stateManager *StateManager) (string, error) {
	// Create temporary file
	tempFile, err := os.CreateTemp("", "quick_watch_alerts_*.yml")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	// Get current alerts
	alerts := stateManager.GetAlerts()

	// If no alerts exist, create default console
	if len(alerts) == 0 {
		alerts = map[string]NotifierConfig{
			"console": {
				Name:        "console",
				Type:        "console",
				Enabled:     true,
				Description: "Console output",
				Settings: map[string]interface{}{
					"style": "stylized",
					"color": true,
				},
			},
		}
	}

	// Marshal to simplified top-level map, omitting the name field per entry
	simplified := make(map[string]map[string]interface{})
	for key, n := range alerts {
		entry := map[string]interface{}{
			"type":    n.Type,
			"enabled": n.Enabled,
		}
		if n.Description != "" {
			entry["description"] = n.Description
		}
		if n.Settings != nil {
			entry["settings"] = n.Settings
		}
		simplified[key] = entry
	}
	data, err := yaml.Marshal(simplified)
	if err != nil {
		return "", err
	}

	// Add helpful comments
	commentedData := addAlertsComments(data)

	// Write to temp file
	if _, err := tempFile.Write(commentedData); err != nil {
		return "", err
	}

	return tempFile.Name(), nil
}

// addAlertsComments adds helpful comments to the alerts YAML for editing
func addAlertsComments(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	commentedLines := []string{
		"# Edit alerts below. Each key is the alert name.",
		"# For console, only 'type: console' is required.",
		"# For slack, 'type: slack' and 'settings.webhook_url' are required.",
		"#",
		"# Full examples:",
		"# my-console-alert:",
		"#   type: console",
		"#   enabled: true",
		"#   description: \"Console output\"",
		"#   settings:",
		"#     style: stylized",
		"#     color: true",
		"#",
		"# my-slack-alert:",
		"#   type: slack",
		"#   enabled: true",
		"#   description: \"Slack alerts channel\"",
		"#   settings:",
		"#     webhook_url: \"https://hooks.slack.com/services/...\"",
		"#     channel: \"#alerts\"",
		"#     username: \"QuickWatch\"",
		"#     icon_emoji: \":robot_face:\"",
		"#",
		"",
	}
	commentedLines = append(commentedLines, lines...)
	return []byte(strings.Join(commentedLines, "\n"))
}

// applyTargetsYAML ingests targets YAML content (from stdin or file) and saves changes
func applyTargetsYAML(stateManager *StateManager, modifiedData []byte) {
	// Validate the YAML
	if err := validateYAML(modifiedData); err != nil {
		fmt.Printf("%s Invalid YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Println("Please fix the errors and try again.")
		return
	}

	// Parse targets with robust parser
	targetsMap, targetFieldsMap, err := parseTargetsFromYAML(modifiedData)
	if err != nil {
		fmt.Printf("%s Failed to parse YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Validate (no defaults applied here)
	if err := validateTargets(targetsMap, stateManager); err != nil {
		fmt.Printf("%s Invalid targets: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Printf("%s Please fix the validation errors and try again.\n", qc.Colorize("💡 Tip:", qc.ColorYellow))
		return
	}

	// Merge with existing state to preserve unspecified fields
	for url, target := range targetsMap {
		if existing, ok := stateManager.GetTarget(url); ok {
			fields := targetFieldsMap[url]
			if fields == nil {
				fields = &TargetFields{}
			}
			if !fields.Method && target.Method == "" {
				target.Method = existing.Method
			}
			if !fields.Headers && len(target.Headers) == 0 && existing.Headers != nil {
				target.Headers = existing.Headers
			}
			if !fields.Threshold && target.Threshold == 0 {
				target.Threshold = existing.Threshold
			}
			if !fields.StatusCodes && len(target.StatusCodes) == 0 && len(existing.StatusCodes) > 0 {
				target.StatusCodes = existing.StatusCodes
			}
			if !fields.SizeAlerts && (target.SizeAlerts == (SizeAlertConfig{})) {
				target.SizeAlerts = existing.SizeAlerts
			}
			if !fields.CheckStrategy && target.CheckStrategy == "" {
				target.CheckStrategy = existing.CheckStrategy
			}
			if !fields.Alerts && len(target.Alerts) == 0 {
				if len(existing.Alerts) > 0 {
					target.Alerts = existing.Alerts
				} else if existing.AlertStrategy != "" {
					target.Alerts = []string{existing.AlertStrategy}
				}
			}
			if target.Name == "" {
				target.Name = existing.Name
			}
		}
		if err := stateManager.AddTarget(target); err != nil {
			fmt.Printf("%s Failed to save target %s: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), url, err)
			return
		}
	}

	// Summary output
	fmt.Printf("\n%s Edit Summary\n", qc.Colorize("📝 =", qc.ColorBlue))
	fmt.Printf("Targets configured: %d\n", len(targetsMap))
	fmt.Println()
	if len(targetsMap) == 0 {
		fmt.Printf("%s No targets configured\n", qc.Colorize("ℹ️ Info:", qc.ColorYellow))
		return
	}
	fmt.Printf("%s Configured Targets:\n", qc.Colorize("📋 Info:", qc.ColorBlue))
	fmt.Println()
	i := 0
	for _, target := range targetsMap {
		rowColor := qc.AlternatingColor(i, qc.ColorWhite, qc.ColorCyan)
		entry := fmt.Sprintf("  %s %-30s %s", qc.Colorize(fmt.Sprintf("%d.", i+1), qc.ColorYellow), target.Name, target.URL)
		fmt.Println(qc.Colorize(entry, rowColor))
		displayMethod := target.Method
		if strings.TrimSpace(displayMethod) == "" {
			displayMethod = "GET"
		}
		displayThreshold := target.Threshold
		if displayThreshold == 0 {
			displayThreshold = 30
		}
		displayCheck := target.CheckStrategy
		if strings.TrimSpace(displayCheck) == "" {
			displayCheck = "http"
		}
		displayAlerts := strings.Join(target.Alerts, ", ")
		if strings.TrimSpace(displayAlerts) == "" {
			if strings.TrimSpace(target.AlertStrategy) != "" {
				displayAlerts = target.AlertStrategy
			} else {
				displayAlerts = "console"
			}
		}
		fmt.Printf("     Method: %s, Threshold: %ds, Check: %s, Alert: %s\n", displayMethod, displayThreshold, displayCheck, displayAlerts)
		i++
	}
	fmt.Printf("\n%s Configuration saved successfully!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
}

// applySettingsYAML ingests settings YAML content (from stdin or file) and saves changes
func applySettingsYAML(stateManager *StateManager, modifiedData []byte) {
	if err := validateSettingsYAML(modifiedData); err != nil {
		fmt.Printf("%s Invalid YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Println("Please fix the errors and try again.")
		return
	}
	var settingsData map[string]interface{}
	if err := yaml.Unmarshal(modifiedData, &settingsData); err != nil {
		fmt.Printf("%s Failed to parse YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}
	settings := ServerSettings{
		WebhookPort:      8080,
		WebhookPath:      "/webhook",
		CheckInterval:    5,
		DefaultThreshold: 30,
		Startup:          StartupConfig{Enabled: true, Alerts: []string{"console"}},
	}
	if v, ok := settingsData["webhook_port"].(int); ok {
		settings.WebhookPort = v
	}
	if v, ok := settingsData["webhook_path"].(string); ok {
		settings.WebhookPath = v
	}
	if v, ok := settingsData["check_interval"].(int); ok {
		settings.CheckInterval = v
	}
	if v, ok := settingsData["default_threshold"].(int); ok {
		settings.DefaultThreshold = v
	}
	if startupData, ok := settingsData["startup"].(map[string]interface{}); ok {
		if v, ok := startupData["enabled"].(bool); ok {
			settings.Startup.Enabled = v
		}
		if al, ok := startupData["alerts"].([]interface{}); ok {
			settings.Startup.Alerts = make([]string, 0, len(al))
			for _, a := range al {
				if s, ok := a.(string); ok {
					settings.Startup.Alerts = append(settings.Startup.Alerts, s)
				}
			}
		} else if al, ok := startupData["notifiers"].([]interface{}); ok {
			settings.Startup.Alerts = make([]string, 0, len(al))
			for _, a := range al {
				if s, ok := a.(string); ok {
					settings.Startup.Alerts = append(settings.Startup.Alerts, s)
				}
			}
		} else if al, ok := startupData["alert_strategies"].([]interface{}); ok {
			settings.Startup.Alerts = make([]string, 0, len(al))
			for _, a := range al {
				if s, ok := a.(string); ok {
					settings.Startup.Alerts = append(settings.Startup.Alerts, s)
				}
			}
		}
		if v, ok := startupData["check_all_targets"].(bool); ok {
			settings.Startup.CheckAllTargets = v
		}
	}
	if v, ok := settingsData["startup_message"].(bool); ok {
		settings.Startup.Enabled = v
		if v && len(settings.Startup.Alerts) == 0 {
			settings.Startup.Alerts = []string{"console"}
		}
	}
	if err := validateSettings(settings); err != nil {
		fmt.Printf("%s Invalid settings: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Printf("%s Please fix the validation errors and try again.\n", qc.Colorize("💡 Tip:", qc.ColorYellow))
		return
	}
	if err := stateManager.UpdateSettings(settings); err != nil {
		fmt.Printf("%s Failed to update settings: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Show summary similar to targets
	fmt.Printf("\n%s Edit Summary\n", qc.Colorize("📝 =", qc.ColorBlue))
	fmt.Printf("Settings configured\n")
	fmt.Println()

	// Summarize key settings
	fmt.Printf("%s Server Settings:\n", qc.Colorize("📋 Info:", qc.ColorBlue))
	fmt.Println()
	fmt.Printf("  %s Webhook Port: %d\n", qc.Colorize("-", qc.ColorYellow), settings.WebhookPort)
	fmt.Printf("  %s Webhook Path: %s\n", qc.Colorize("-", qc.ColorYellow), settings.WebhookPath)
	fmt.Printf("  %s Check Interval: %ds\n", qc.Colorize("-", qc.ColorYellow), settings.CheckInterval)
	fmt.Printf("  %s Default Threshold: %ds\n", qc.Colorize("-", qc.ColorYellow), settings.DefaultThreshold)

	// Startup summary
	startupStatus := "disabled"
	if settings.Startup.Enabled {
		startupStatus = "enabled"
	}
	fmt.Printf("  %s Startup: %s\n", qc.Colorize("-", qc.ColorYellow), startupStatus)
	if len(settings.Startup.Alerts) > 0 {
		fmt.Printf("     Startup Alerts: %s\n", strings.Join(settings.Startup.Alerts, ", "))
	}
	if settings.Startup.CheckAllTargets {
		fmt.Printf("     Startup Check All Targets: true\n")
	}

	fmt.Printf("\n%s Settings updated successfully!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
}

// applyAlertsYAML ingests alerts YAML content (from stdin or file) and saves changes
func applyAlertsYAML(stateManager *StateManager, modifiedData []byte) {
	if err := validateAlertsYAML(modifiedData); err != nil {
		fmt.Printf("%s Invalid YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Println("Please fix the errors and try again.")
		return
	}
	var raw map[string]interface{}
	if err := yaml.Unmarshal(modifiedData, &raw); err != nil {
		fmt.Printf("%s Failed to parse YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}
	alerts := make(map[string]NotifierConfig)
	var iter map[string]interface{}
	if v, ok := raw["alerts"].(map[string]interface{}); ok {
		iter = v
	} else if v, ok := raw["alerts"].(map[string]interface{}); ok {
		iter = v
	} else {
		iter = raw
	}
	for name, alertInterface := range iter {
		if alertMap, ok := alertInterface.(map[string]interface{}); ok {
			alert := NotifierConfig{Name: name, Enabled: true, Settings: make(map[string]interface{})}
			if v, ok := alertMap["type"].(string); ok {
				alert.Type = v
			}
			if v, ok := alertMap["enabled"].(bool); ok {
				alert.Enabled = v
			}
			if v, ok := alertMap["description"].(string); ok {
				alert.Description = v
			}
			if v, ok := alertMap["settings"].(map[string]interface{}); ok {
				alert.Settings = v
			}
			alerts[name] = alert
		}
	}
	if err := validateAlerts(alerts); err != nil {
		fmt.Printf("%s Invalid alerts: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Printf("%s Please fix the validation errors and try again.\n", qc.Colorize("💡 Tip:", qc.ColorYellow))
		return
	}
	if err := stateManager.UpdateAlerts(alerts); err != nil {
		fmt.Printf("%s Failed to update alerts: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Show summary
	fmt.Printf("\n%s Edit Summary\n", qc.Colorize("📝 =", qc.ColorBlue))
	fmt.Printf("Alerts configured: %d\n", len(alerts))
	fmt.Println()

	if len(alerts) == 0 {
		fmt.Printf("%s No alerts configured\n", qc.Colorize("ℹ️ Info:", qc.ColorYellow))
		return
	}

	fmt.Printf("%s Configured Alerts:\n", qc.Colorize("📋 Info:", qc.ColorBlue))
	fmt.Println()

	i := 0
	for name, alert := range alerts {
		// Alternate row colors for better readability
		rowColor := qc.AlternatingColor(i, qc.ColorWhite, qc.ColorCyan)

		status := "enabled"
		if !alert.Enabled {
			status = "disabled"
		}

		entry := fmt.Sprintf(
			"  %s %-20s %s (%s)",
			qc.Colorize(fmt.Sprintf("%d.", i+1), qc.ColorYellow),
			name,
			alert.Type,
			status,
		)
		fmt.Println(qc.Colorize(entry, rowColor))

		if alert.Description != "" {
			fmt.Printf("     Description: %s\n", alert.Description)
		}

		if len(alert.Settings) > 0 {
			fmt.Printf("     Settings: %d configured\n", len(alert.Settings))
		}

		i++
	}

	fmt.Printf("\n%s Alerts updated successfully!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
}

// parseTargetsFromYAML parses targets from any of the supported editor formats
func parseTargetsFromYAML(data []byte) (map[string]Target, map[string]*TargetFields, error) {
	var targetsData map[string]interface{}
	if err := yaml.Unmarshal(data, &targetsData); err != nil {
		return nil, nil, err
	}

	targetsMap := make(map[string]Target)
	targetFieldsMap := make(map[string]*TargetFields)

	// Prefer wrapped under "targets"
	if targetsInterface, ok := targetsData["targets"]; ok {
		parseTargetsInterface(targetsInterface, targetsMap, targetFieldsMap)
	}
	// Legacy "targets"
	if len(targetsMap) == 0 {
		if targetsInterface, ok := targetsData["targets"]; ok {
			parseTargetsInterface(targetsInterface, targetsMap, targetFieldsMap)
		}
	}
	// Simplified top-level
	if len(targetsMap) == 0 {
		parseTargetsInterface(targetsData, targetsMap, targetFieldsMap)
	}

	return targetsMap, targetFieldsMap, nil
}

// parseTargetsInterface fills maps from either map[string]any or []any structures
func parseTargetsInterface(src interface{}, out map[string]Target, fields map[string]*TargetFields) {
	switch v := src.(type) {
	case map[string]interface{}:
		for key, targetInterface := range v {
			if targetMap, ok := targetInterface.(map[string]interface{}); ok {
				target := Target{}
				f := &TargetFields{}
				if name, ok := targetMap["name"].(string); ok && name != "" {
					target.Name = name
				} else {
					target.Name = key
				}
				if urlVal, ok := targetMap["url"].(string); ok {
					target.URL = urlVal
				}
				if method, ok := targetMap["method"].(string); ok {
					target.Method = method
					f.Method = true
				}
				if threshold, ok := targetMap["threshold"].(int); ok {
					target.Threshold = threshold
					f.Threshold = true
				}
				if statusCodes, ok := targetMap["status_codes"].([]interface{}); ok {
					f.StatusCodes = true
					for _, code := range statusCodes {
						if codeStr, ok := code.(string); ok {
							target.StatusCodes = append(target.StatusCodes, codeStr)
						}
					}
				}
				if headers, ok := targetMap["headers"].(map[string]interface{}); ok {
					f.Headers = true
					target.Headers = make(map[string]string)
					for hk, hv := range headers {
						if hvs, ok := hv.(string); ok {
							target.Headers[hk] = hvs
						}
					}
				}
				if sizeAlerts, ok := targetMap["size_alerts"].(map[string]interface{}); ok {
					f.SizeAlerts = true
					if enabled, ok := sizeAlerts["enabled"].(bool); ok {
						target.SizeAlerts.Enabled = enabled
					}
					if historySize, ok := sizeAlerts["history_size"].(int); ok {
						target.SizeAlerts.HistorySize = historySize
					}
					if th, ok := sizeAlerts["threshold"].(float64); ok {
						target.SizeAlerts.Threshold = th
					}
				}
				if checkStrategy, ok := targetMap["check_strategy"].(string); ok {
					target.CheckStrategy = checkStrategy
					f.CheckStrategy = true
				}
				// Alerts: accept string or list
				if aval, ok := targetMap["alerts"]; ok {
					switch at := aval.(type) {
					case string:
						if strings.TrimSpace(at) != "" {
							target.Alerts = []string{at}
							f.Alerts = true
						}
					case []interface{}:
						for _, a := range at {
							if s, ok := a.(string); ok && strings.TrimSpace(s) != "" {
								target.Alerts = append(target.Alerts, s)
							}
						}
						if len(target.Alerts) > 0 {
							f.Alerts = true
						}
					}
				}
				if target.URL != "" {
					out[target.URL] = target
					fields[target.URL] = f
				}
			}
		}
	case []interface{}:
		for _, targetInterface := range v {
			if targetMap, ok := targetInterface.(map[string]interface{}); ok {
				target := Target{}
				f := &TargetFields{}
				if name, ok := targetMap["name"].(string); ok {
					target.Name = name
				}
				if urlVal, ok := targetMap["url"].(string); ok {
					target.URL = urlVal
				}
				if method, ok := targetMap["method"].(string); ok {
					target.Method = method
					f.Method = true
				}
				if threshold, ok := targetMap["threshold"].(int); ok {
					target.Threshold = threshold
					f.Threshold = true
				}
				if statusCodes, ok := targetMap["status_codes"].([]interface{}); ok {
					f.StatusCodes = true
					for _, code := range statusCodes {
						if codeStr, ok := code.(string); ok {
							target.StatusCodes = append(target.StatusCodes, codeStr)
						}
					}
				}
				// Alerts: accept string or list
				if aval, ok := targetMap["alerts"]; ok {
					switch at := aval.(type) {
					case string:
						if strings.TrimSpace(at) != "" {
							target.Alerts = []string{at}
							f.Alerts = true
						}
					case []interface{}:
						for _, a := range at {
							if s, ok := a.(string); ok && strings.TrimSpace(s) != "" {
								target.Alerts = append(target.Alerts, s)
							}
						}
						if len(target.Alerts) > 0 {
							f.Alerts = true
						}
					}
				}
				if target.URL != "" {
					out[target.URL] = target
					fields[target.URL] = f
				}
			}
		}
	}
}

// validateAlertsYAML validates that the alerts YAML is well-formed
func validateAlertsYAML(data []byte) error {
	var temp interface{}
	return yaml.Unmarshal(data, &temp)
}

// validateAlerts validates alert configurations
func validateAlerts(alerts map[string]NotifierConfig) error {
	for name, alert := range alerts {
		if alert.Name == "" {
			return fmt.Errorf("alert %s: name cannot be empty", name)
		}
		if alert.Type == "" {
			return fmt.Errorf("alert %s: type is required", name)
		}

		switch alert.Type {
		case "console":
			// Validate console settings
			if style, ok := alert.Settings["style"].(string); ok {
				if style != "plain" && style != "stylized" {
					return fmt.Errorf("alert %s: console style must be 'plain' or 'stylized', got '%s'", name, style)
				}
			}
		case "slack":
			// Validate Slack settings
			webhookURL, ok := alert.Settings["webhook_url"].(string)
			if !ok || webhookURL == "" {
				return fmt.Errorf("alert %s: slack webhook_url is required", name)
			}
			if !strings.HasPrefix(webhookURL, "https://hooks.slack.com/") {
				return fmt.Errorf("alert %s: slack webhook_url must be a valid Slack webhook URL", name)
			}
		default:
			return fmt.Errorf("alert %s: unknown type '%s', must be 'console' or 'slack'", name, alert.Type)
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

	// Get targets and alerts
	targetConfig := stateManager.GetTargetConfig()
	targets := targetConfig.Targets
	alerts := stateManager.GetAlerts()

	// Validate targets
	errors := []string{}
	warnings := []string{}

	// Check each target
	for _, target := range targets {
		// Check required fields
		if target.Name == "" {
			errors = append(errors, fmt.Sprintf("Target %s: name is required", target.URL))
		}
		if target.URL == "" {
			errors = append(errors, "Target: url is required")
		}

		// Check URL format
		if target.URL != "" && !strings.HasPrefix(target.URL, "http://") && !strings.HasPrefix(target.URL, "https://") {
			errors = append(errors, fmt.Sprintf("Target %s: url must start with http:// or https://", target.URL))
		}

		// Check HTTP method
		validMethods := map[string]bool{
			"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
			"HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
		}
		if target.Method != "" && !validMethods[target.Method] {
			errors = append(errors, fmt.Sprintf("Target %s: invalid method '%s'", target.URL, target.Method))
		}
	}

	// Check alerts
	for name, alert := range alerts {
		if alert.Enabled {
			if alert.Type == "slack" {
				if webhookURL, ok := alert.Settings["webhook_url"].(string); !ok || webhookURL == "" {
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
			fmt.Printf("  • %d targets configured\n", len(targets))
			fmt.Printf("  • %d alerts configured\n", len(alerts))
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
	data, err := os.ReadFile(configFile)
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

	// Extract targets and alerts
	targets := make(map[string]Target)
	alerts := make(map[string]NotifierConfig)

	// Parse targets
	if targetsData, exists := configData["targets"]; exists {
		if targetsMap, ok := targetsData.(map[string]interface{}); ok {
			for url, targetInterface := range targetsMap {
				if targetMap, ok := targetInterface.(map[string]interface{}); ok {
					target := Target{}
					if name, ok := targetMap["name"].(string); ok {
						target.Name = name
					}
					if urlVal, ok := targetMap["url"].(string); ok {
						target.URL = urlVal
					}
					if method, ok := targetMap["method"].(string); ok {
						target.Method = method
					}
					if threshold, ok := targetMap["threshold"].(int); ok {
						target.Threshold = threshold
					}
					if checkStrategy, ok := targetMap["check_strategy"].(string); ok {
						target.CheckStrategy = checkStrategy
					}
					targets[url] = target
				}
			}
		}
	}

	// Parse alerts
	if alertsData, exists := configData["alerts"]; exists {
		if alertsMap, ok := alertsData.(map[string]interface{}); ok {
			for name, alertInterface := range alertsMap {
				if alertMap, ok := alertInterface.(map[string]interface{}); ok {
					alert := NotifierConfig{
						Name:     name,
						Enabled:  true,
						Settings: make(map[string]interface{}),
					}
					if alertType, ok := alertMap["type"].(string); ok {
						alert.Type = alertType
					}
					if enabled, ok := alertMap["enabled"].(bool); ok {
						alert.Enabled = enabled
					}
					if description, ok := alertMap["description"].(string); ok {
						alert.Description = description
					}
					if settings, ok := alertMap["settings"].(map[string]interface{}); ok {
						alert.Settings = settings
					}
					alerts[name] = alert
				}
			}
		}
	}

	// Validate targets
	errors := []string{}
	warnings := []string{}

	// Check each target
	for url, target := range targets {
		// Check required fields
		if target.Name == "" {
			errors = append(errors, fmt.Sprintf("Target %s: name is required", url))
		}
		if target.URL == "" {
			errors = append(errors, fmt.Sprintf("Target %s: url is required", url))
		}

		// Check URL format
		if target.URL != "" && !strings.HasPrefix(target.URL, "http://") && !strings.HasPrefix(target.URL, "https://") {
			errors = append(errors, fmt.Sprintf("Target %s: url must start with http:// or https://", url))
		}

		// Check HTTP method
		validMethods := map[string]bool{
			"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
			"HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
		}
		if target.Method != "" && !validMethods[target.Method] {
			errors = append(errors, fmt.Sprintf("Target %s: invalid method '%s'", url, target.Method))
		}
	}

	// Check alerts
	for name, alert := range alerts {
		if alert.Enabled {
			if alert.Type == "slack" {
				if webhookURL, ok := alert.Settings["webhook_url"].(string); !ok || webhookURL == "" {
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
			fmt.Printf("  • %d targets configured\n", len(targets))
			fmt.Printf("  • %d alerts configured\n", len(alerts))
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

// getValidAlerts returns a map of valid alert alerts from alerts
func getValidAlerts(alerts map[string]NotifierConfig) map[string]bool {
	validStrategies := make(map[string]bool)
	validStrategies["console"] = true // Default console strategy

	for name, alert := range alerts {
		if alert.Enabled {
			validStrategies[name] = true
		}
	}

	return validStrategies
}

// getValidStrategiesList returns a comma-separated list of valid alerts
func getValidStrategiesList(validStrategies map[string]bool) string {
	alerts := []string{}
	for strategy := range validStrategies {
		alerts = append(alerts, strategy)
	}
	return strings.Join(alerts, ", ")
}

// isValidAlertStrategy checks if an alert strategy is valid for the engine
func isValidAlertStrategy(strategy string, engine *TargetEngine) bool {
	_, exists := engine.alertStrategies[strategy]
	return exists
}
