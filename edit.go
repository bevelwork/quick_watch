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
	if err := validateMonitors(monitorsMap); err != nil {
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
func validateMonitors(monitors map[string]Monitor) error {
	validHTTPMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
		"HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
	}

	validCheckStrategies := map[string]bool{
		"http": true,
	}

	validAlertStrategies := map[string]bool{
		"console": true,
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
