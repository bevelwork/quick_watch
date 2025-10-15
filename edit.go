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

	// Parse the modified configuration
	var yamlConfig YAMLConfig
	if err := yaml.Unmarshal(modifiedData, &yamlConfig); err != nil {
		fmt.Printf("%s Failed to parse YAML: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Validate monitors (strict validation, no defaults applied)
	if err := validateMonitors(yamlConfig.Monitors); err != nil {
		fmt.Printf("%s Invalid monitors: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		fmt.Printf("%s Please fix the validation errors and try again.\n", qc.Colorize("💡 Tip:", qc.ColorYellow))
		return
	}

	// Apply defaults for missing values
	applyDefaults(yamlConfig.Monitors)

	// Save the changes
	if err := saveModifiedState(stateManager, &yamlConfig); err != nil {
		fmt.Printf("%s Failed to save changes: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		return
	}

	// Show summary
	showEditSummary(yamlConfig)
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

	// Create YAML structure for editing
	yamlConfig := YAMLConfig{
		Version:  "1.0",
		Monitors: make(map[string]Monitor),
		Settings: stateManager.GetSettings(),
		Strategies: map[string]interface{}{
			"check": map[string]interface{}{
				"http": map[string]interface{}{
					"timeout":          10,
					"follow_redirects": true,
				},
			},
			"alert": map[string]interface{}{
				"console": map[string]interface{}{
					"format": "detailed",
				},
			},
		},
	}

	// Copy monitors
	for url, monitor := range monitors {
		yamlConfig.Monitors[url] = monitor
	}

	// Marshal to YAML
	data, err := yaml.Marshal(yamlConfig)
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
		"# Quick Watch Configuration",
		"# Edit this file to modify your monitors",
		"#",
		"# IMPORTANT: The 'url' property is REQUIRED for each monitor",
		"# If you provide an invalid value, validation will fail even if defaults exist",
		"#",
		"# To add a new monitor, add a new entry under 'monitors:'",
		"# To remove a monitor, delete its entry",
		"# To modify a monitor, edit its properties",
		"#",
		"# Example monitor entry with all properties:",
		"#   my-api:",
		"#     name: \"My API\"                    # REQUIRED: Display name for the monitor",
		"#     url: \"https://api.example.com/health\"  # REQUIRED: URL to monitor",
		"#     method: \"GET\"                     # DEFAULT: GET (options: GET, POST, PUT, DELETE, etc.)",
		"#     headers:                            # DEFAULT: {} (empty object)",
		"#       Authorization: \"Bearer token\"",
		"#       User-Agent: \"QuickWatch/1.0\"",
		"#     threshold: 30                       # DEFAULT: 30 (seconds before alert)",
		"#     status_codes: [\"2**\", \"302\"]     # DEFAULT: [\"*\"] (list of acceptable status codes)",
		"#     check_strategy: \"http\"            # DEFAULT: http (options: http)",
		"#     alert_strategy: \"console\"         # DEFAULT: console (options: console)",
		"#",
		"# Default values (used when properties are omitted):",
		"#   method: \"GET\"",
		"#   headers: {}",
		"#   threshold: 30",
		"#   status_codes: [\"*\"]",
		"#   check_strategy: \"http\"",
		"#   alert_strategy: \"console\"",
		"#",
		"# Validation rules:",
		"#   - url: REQUIRED, must be a valid URL",
		"#   - name: REQUIRED, must not be empty",
		"#   - method: Must be valid HTTP method if provided",
		"#   - threshold: Must be positive integer if provided",
		"#   - status_codes: Must be list of valid patterns if provided",
		"#   - check_strategy: Must be 'http' if provided",
		"#   - alert_strategy: Must be 'console' if provided",
		"#",
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

		// Update the map with the modified monitor
		monitors[url] = monitor
	}
}

// saveModifiedState saves the modified configuration to the state file
func saveModifiedState(stateManager *StateManager, yamlConfig *YAMLConfig) error {
	// Clear existing monitors
	existingMonitors := stateManager.ListMonitors()
	for url := range existingMonitors {
		if err := stateManager.RemoveMonitor(url); err != nil {
			// Ignore errors for non-existent monitors
		}
	}

	// Add new monitors
	for _, monitor := range yamlConfig.Monitors {
		if err := stateManager.AddMonitor(monitor); err != nil {
			return fmt.Errorf("failed to add monitor %s: %v", monitor.URL, err)
		}
	}

	// Update settings if provided
	if yamlConfig.Settings.WebhookPort > 0 || yamlConfig.Settings.WebhookPath != "" {
		if err := stateManager.UpdateSettings(yamlConfig.Settings); err != nil {
			return fmt.Errorf("failed to update settings: %v", err)
		}
	}

	return nil
}

// showEditSummary shows a summary of the changes made
func showEditSummary(yamlConfig YAMLConfig) {
	fmt.Printf("\n%s Edit Summary\n", qc.Colorize("📝 =", qc.ColorBlue))
	fmt.Printf("Monitors configured: %d\n", len(yamlConfig.Monitors))
	fmt.Println()

	if len(yamlConfig.Monitors) == 0 {
		fmt.Printf("%s No monitors configured\n", qc.Colorize("ℹ️ Info:", qc.ColorYellow))
		return
	}

	fmt.Printf("%s Configured Monitors:\n", qc.Colorize("📋 Info:", qc.ColorBlue))
	fmt.Println()

	i := 0
	for _, monitor := range yamlConfig.Monitors {
		// Alternate row colors for better readability
		rowColor := qc.AlternatingColor(i, qc.ColorWhite, qc.ColorCyan)

		entry := fmt.Sprintf(
			"%3d. %-30s %s",
			i+1, monitor.Name, monitor.URL,
		)
		fmt.Println(qc.Colorize(entry, rowColor))
		fmt.Printf("     Method: %s, Threshold: %ds, Check: %s, Alert: %s\n",
			monitor.Method, monitor.Threshold, monitor.CheckStrategy, monitor.AlertStrategy)
		i++
	}

	fmt.Printf("\n%s Configuration saved successfully!\n", qc.Colorize("✅ Success:", qc.ColorGreen))
}
