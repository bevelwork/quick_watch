// Package main provides a command-line tool for targeting URLs and services
// with configurable alerts and webhook notifications. This tool provides the
// simplest possible targeting with threshold-based alerting and external
// webhook support.

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"

	qc "github.com/bevelwork/quick_color"
	versionpkg "github.com/bevelwork/quick_watch/version"
)

var version = ""

func main() {
	// Print header
	printHeader()

	// Check for version flag first
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println(resolveVersion())
		return
	}

	// Check for help
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help") {
		showHelp()
		return
	}

	// Parse command-based arguments
	if len(os.Args) < 2 {
		showHelp()
		return
	}

	action := os.Args[1]
	args := os.Args[2:]

	switch action {
	case "targets", "edit":
		handleEditCommand(args)
	case "settings":
		handleSettingsCommand(args)
	case "alerts", "notifiers":
		handleNotifiersCommand(args)
	case "validate":
		handleValidateCommand(args)
	case "add":
		handleAddCommand(args)
	case "rm":
		handleRemoveCommand(args)
	case "list":
		handleListCommand(args)
	case "config":
		handleConfigCommand(args)
	case "server":
		handleServerCommand(args)
	default:
		fmt.Printf("%s Unknown action: %s\n", qc.Colorize("❌ Error:", qc.ColorRed), action)
		showHelp()
		os.Exit(1)
	}

}

// showHelp displays the help information
func showHelp() {
	fmt.Printf("Usage: %s <action> [options]\n\n", os.Args[0])
	fmt.Println("Simple Actions:")
	fmt.Println("  add <url>     Add a target with default settings")
	fmt.Println("  rm <url>      Remove a target")
	fmt.Println("  list          List all targets")
	fmt.Println("  server        Start the server")
	fmt.Println("")
	fmt.Println("Advanced Actions:")
	fmt.Println("  targets       Edit targets using $EDITOR")
	fmt.Println("  settings      Edit global settings using $EDITOR")
	fmt.Println("  alerts        Edit alert configs using $EDITOR")
	fmt.Println("")
	fmt.Println("Administrative Actions:")
	fmt.Println("  validate      Validate configuration syntax and alert strategies")
	fmt.Println("  config <file> Use YAML configuration file")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Printf("  %s targets\n", os.Args[0])
	fmt.Printf("  %s add https://api.example.com/health --threshold 30s\n", os.Args[0])
	fmt.Printf("  %s rm https://api.example.com/health\n", os.Args[0])
	fmt.Printf("  %s list\n", os.Args[0])
	fmt.Printf("  %s config\n", os.Args[0])
	fmt.Printf("  %s server --webhook-port 8080\n", os.Args[0])
}

// handleEditCommand handles the edit action
func handleEditCommand(args []string) {
	stateFile := getStateFile(args)
	// Support reading from stdin
	if slices.Contains(args, "--stdin") {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Printf("%s Failed to read stdin: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
			os.Exit(1)
		}
		sm := NewStateManager(stateFile)
		if err := sm.Load(); err != nil {
			log.Printf("Warning: Could not load existing state: %v", err)
		}
		applyTargetsYAML(sm, data)
		return
	}
	handleEditTargets(stateFile)
}

// handleAddCommand handles the add action
func handleAddCommand(args []string) {
	if len(args) == 0 {
		fmt.Printf("%s URL is required for add action\n", qc.Colorize("❌ Error:", qc.ColorRed))
		os.Exit(1)
	}

	url := args[0]
	stateFile := getStateFile(args[1:])
	method := getStringFlag(args[1:], "--method", "GET")
	headers := getStringSliceFlag(args[1:], "--header")
	threshold := getIntFlag(args[1:], "--threshold", 30)
	checkStrategy := getStringFlag(args[1:], "--check-strategy", "http")
	alertStrategy := getStringFlag(args[1:], "--alert-strategy", "console")

	handleAddTarget(stateFile, url, method, headers, threshold, checkStrategy, alertStrategy)
}

// handleRemoveCommand handles the rm action
func handleRemoveCommand(args []string) {
	if len(args) == 0 {
		fmt.Printf("%s URL is required for rm action\n", qc.Colorize("❌ Error:", qc.ColorRed))
		os.Exit(1)
	}

	url := args[0]
	stateFile := getStateFile(args[1:])
	handleRemoveTarget(stateFile, url)
}

// handleListCommand handles the list action
func handleListCommand(args []string) {
	stateFile := getStateFile(args)
	handleListTargets(stateFile)
}

// handleConfigCommand handles the config action
func handleConfigCommand(args []string) {
	if len(args) == 0 {
		fmt.Printf("%s Configuration file is required for config action\n", qc.Colorize("❌ Error:", qc.ColorRed))
		os.Exit(1)
	}

	configFile := args[0]
	webhookPort := getIntFlag(args[1:], "--webhook-port", 0)
	webhookPath := getStringFlag(args[1:], "--webhook-path", "/webhook")

	handleConfigMode(configFile, webhookPort, webhookPath)
}

// handleServerCommand handles the server action
func handleServerCommand(args []string) {
	stateFile := getStateFile(args)
	handleServerMode(stateFile)
}

// getStateFile extracts the state file from arguments
func getStateFile(args []string) string {
	return getStringFlag(args, "--state", "watch-state.yml")
}

// getStringFlag extracts a string flag from arguments
func getStringFlag(args []string, flag, defaultValue string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return defaultValue
}

// getIntFlag extracts an int flag from arguments
func getIntFlag(args []string, flag string, defaultValue int) int {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			if val, err := strconv.Atoi(args[i+1]); err == nil {
				return val
			}
		}
	}
	return defaultValue
}

// getStringSliceFlag extracts a string slice flag from arguments
func getStringSliceFlag(args []string, flag string) []string {
	var result []string
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			result = append(result, args[i+1])
		}
	}
	return result
}

// handleConfigMode handles configuration file mode
func handleConfigMode(configFile string, webhookPort int, webhookPath string) {
	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, shutting down...")
		cancel()
	}()

	// Load configuration
	config, err := loadConfiguration(configFile, "", "", []string{}, 0, "", "")
	if err != nil {
		log.Fatal(err)
	}

	// Create targeting engine
	engine := NewTargetEngine(config, nil)

	// Start webhook server if requested
	var webhookServer *WebhookServer
	if webhookPort > 0 {
		webhookServer = NewWebhookServer(webhookPort, webhookPath, engine)
		if err := webhookServer.Start(ctx); err != nil {
			log.Fatal(err)
		}
	}

	// Start targeting
	if err := engine.Start(ctx); err != nil {
		log.Fatal(err)
	}

	// Print targeting status
	printTargetStatus(engine)

	// Wait for context cancellation
	<-ctx.Done()

	// Stop webhook server if running
	if webhookServer != nil {
		webhookServer.Stop(context.Background())
	}

	fmt.Println("Target stopped.")
}

// loadConfiguration loads configuration from YAML file or command line
func loadConfiguration(configFile, url, method string, headers []string, threshold int, checkStrategy, alertStrategy string) (*TargetConfig, error) {
	var config *TargetConfig

	// If config file is provided, load from YAML file
	if configFile != "" {
		data, err := os.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %v", err)
		}

		config, err = LoadYAMLConfig(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse YAML config file: %v", err)
		}
	} else if url != "" {
		// Create single target from command line
		target := Target{
			Name:          "CLI Target",
			URL:           url,
			Method:        method,
			Headers:       parseHeaders(headers),
			Threshold:     threshold,
			CheckStrategy: checkStrategy,
			Alerts:        []string{alertStrategy},
		}
		config = &TargetConfig{
			Targets: []Target{target},
		}
	} else {
		return nil, fmt.Errorf("either --config or --url must be specified")
	}

	return config, nil
}

// StringSliceFlag implements flag.Value for string slices
type StringSliceFlag []string

func (s *StringSliceFlag) String() string {
	return fmt.Sprintf("%v", *s)
}

func (s *StringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// parseHeaders parses header strings into a map
func parseHeaders(headers []string) map[string]string {
	result := make(map[string]string)
	for _, header := range headers {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			result[key] = value
		}
	}
	return result
}

// TargetFields tracks which fields were explicitly set in the YAML
type TargetFields struct {
	Method        bool
	Headers       bool
	Threshold     bool
	StatusCodes   bool
	SizeAlerts    bool
	CheckStrategy bool
	Alerts        bool
}

// applyDefaultsAfterClean applies default values after cleaning
func applyDefaultsAfterClean(target *Target) {
	if target.Method == "" {
		target.Method = "GET"
	}
	if target.Headers == nil {
		target.Headers = make(map[string]string)
	}
	if target.Threshold == 0 {
		target.Threshold = 30
	}
	if len(target.StatusCodes) == 0 {
		target.StatusCodes = []string{"*"}
	}
	if target.SizeAlerts.HistorySize == 0 {
		target.SizeAlerts = SizeAlertConfig{
			Enabled:     true,
			HistorySize: 100,
			Threshold:   0.5,
		}
	}
	if target.CheckStrategy == "" {
		target.CheckStrategy = "http"
	}
	if target.AlertStrategy == "" {
		target.AlertStrategy = "console"
	}
}

// printHeader prints the application header
func printHeader() {
	fmt.Printf("%s %s\n", qc.Colorize("🚀 Quick Watch", qc.ColorCyan), qc.Colorize(resolveVersion(), qc.ColorWhite))
}

// printTargetStatus prints the current targeting status
func printTargetStatus(engine *TargetEngine) {
	targets := engine.GetTargetStatus()

	fmt.Printf("\n%s\n", qc.Colorize("📊 Target Status", qc.ColorBlue))
	fmt.Printf("%s %s\n", qc.Colorize("Active targets:", qc.ColorCyan), qc.Colorize(fmt.Sprintf("%d", len(targets)), qc.ColorWhite))

	for i, state := range targets {
		// Alternate row colors for better readability
		rowColor := qc.AlternatingColor(i, qc.ColorWhite, qc.ColorCyan)

		// Color code the status with icons
		statusColor := qc.ColorGreen
		statusIcon := "✅"
		statusText := "UP"
		if state.IsDown {
			statusColor = qc.ColorRed
			statusIcon = "❌"
			statusText = "DOWN"
		}

		entry := fmt.Sprintf(
			"  %s %-30s %s [%s %s]",
			qc.Colorize(fmt.Sprintf("%d.", i+1), qc.ColorYellow),
			state.Target.Name,
			state.Target.URL,
			statusIcon,
			qc.Colorize(statusText, statusColor),
		)
		fmt.Println(qc.Colorize(entry, rowColor))

		// Show additional details if available
		if state.LastCheck != nil {
			fmt.Printf("     Last check: %s (Status: %d, Time: %v)\n",
				state.LastCheck.Timestamp.Format("15:04:05"),
				state.LastCheck.StatusCode,
				state.LastCheck.ResponseTime,
			)
		}
	}

	fmt.Printf("\n%s\n", qc.Colorize("🚀 Target started. Press Ctrl+C to stop.", qc.ColorYellow))
}

// resolveVersion returns the version string. If ldflags-injected version is empty,
// it attempts to derive a dev version from version/version.go, but will not be able
// to display the compile date.
func resolveVersion() string {
	if strings.TrimSpace(version) != "" {
		return version
	}
	if strings.TrimSpace(versionpkg.Full) != "" {
		return versionpkg.Full
	}
	// LOG Warning
	log.Println(
		"[WARNING]: This version was not compiled with a version tag.",
		"Usually this means that the binary was built locally.",
	)
	return fmt.Sprintf("v%d.%d.%s", versionpkg.Major, versionpkg.Minor, "unknown")
}

// handleServerMode starts the server mode
func handleServerMode(stateFile string) {
	fmt.Printf("%s Starting Quick Watch Server\n", qc.Colorize("🚀 Info:", qc.ColorCyan))
	fmt.Printf("State file: %s\n", stateFile)
	fmt.Println()

	// Create server
	server := NewServer(stateFile)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, shutting down server...")
		cancel()
	}()

	// Start server
	if err := server.Start(ctx); err != nil {
		log.Fatal(err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Stop server
	if err := server.Stop(context.Background()); err != nil {
		log.Printf("Error stopping server: %v", err)
	}

	fmt.Println("Server stopped.")
}

// handleAddTarget adds a target to the state file
func handleAddTarget(stateFile, url, method string, headers []string, threshold int, checkStrategy, alertStrategy string) {
	stateManager := NewStateManager(stateFile)

	// Load existing state
	if err := stateManager.Load(); err != nil {
		log.Printf("Warning: Could not load existing state: %v", err)
	}

	// Create target
	target := Target{
		Name:        fmt.Sprintf("Target-%s", url),
		URL:         url,
		Method:      method,
		Headers:     parseHeaders(headers),
		Threshold:   threshold,
		StatusCodes: []string{"*"}, // Default to accept all status codes
		SizeAlerts: SizeAlertConfig{
			Enabled:     true,
			HistorySize: 100,
			Threshold:   0.5, // 50% change threshold
		},
		CheckStrategy: checkStrategy,
		// Prefer new multi-alerts field, but preserve legacy single field via applyDefaultsAfterClean
		Alerts: []string{alertStrategy},
	}

	// Preserve user-entered values as-is; apply runtime defaults only when missing
	applyDefaultsAfterClean(&target)

	// Add target
	if err := stateManager.AddTarget(target); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s Added target: %s\n", qc.Colorize("✅ Success:", qc.ColorGreen), url)
	fmt.Printf("  URL: %s\n", target.URL)
	fmt.Printf("  Method: %s\n", target.Method)
	fmt.Printf("  Threshold: %d seconds\n", target.Threshold)
	fmt.Printf("  Check Strategy: %s\n", target.CheckStrategy)
	fmt.Printf("  Alert Strategy: %s\n", target.AlertStrategy)
}

// handleRemoveTarget removes a target from the state file
func handleRemoveTarget(stateFile, url string) {
	stateManager := NewStateManager(stateFile)

	// Load existing state
	if err := stateManager.Load(); err != nil {
		log.Fatal(err)
	}

	// Remove target
	if err := stateManager.RemoveTarget(url); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s Removed target: %s\n", qc.Colorize("🗑️ Success:", qc.ColorGreen), url)
}

// handleListTargets lists all targets in the state file
func handleListTargets(stateFile string) {
	stateManager := NewStateManager(stateFile)

	// Load existing state
	if err := stateManager.Load(); err != nil {
		log.Printf("Warning: Could not load existing state: %v", err)
	}

	targets := stateManager.ListTargets()

	if len(targets) == 0 {
		fmt.Printf("%s No targets configured\n", qc.Colorize("ℹ️ Info:", qc.ColorYellow))
		return
	}

	fmt.Printf("%s Configured Targets (%d):\n", qc.Colorize("📋 Info:", qc.ColorBlue), len(targets))
	fmt.Println()

	i := 0
	for _, target := range targets {
		// Alternate row colors for better readability
		rowColor := qc.AlternatingColor(i, qc.ColorWhite, qc.ColorCyan)

		entry := fmt.Sprintf(
			"%3d. %-30s %s",
			i+1, target.Name, target.URL,
		)
		fmt.Println(qc.Colorize(entry, rowColor))
		// Display alert strategies (multiple supported)
		alerts := strings.Join(target.Alerts, ", ")
		if alerts == "" && target.AlertStrategy != "" {
			alerts = target.AlertStrategy
		}
		fmt.Printf("     Method: %s, Threshold: %ds, Check: %s, Alert: %s\n",
			target.Method, target.Threshold, target.CheckStrategy, alerts)
		i++
	}
}

// handleSettingsCommand handles the settings command
func handleSettingsCommand(args []string) {
	// Parse command line arguments
	stateFile := "watch-state.yml"

	// Parse flags
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--state":
			if i+1 < len(args) {
				stateFile = args[i+1]
				i++ // Skip next argument
			} else {
				fmt.Printf("%s --state requires a file path\n", qc.Colorize("❌ Error:", qc.ColorRed))
				os.Exit(1)
			}
		case "--stdin":
			// Handle stdin directly for settings
			stateManager := NewStateManager(stateFile)
			if err := stateManager.Load(); err != nil {
				fmt.Printf("%s Failed to load state: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
				os.Exit(1)
			}
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Printf("%s Failed to read stdin: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
				os.Exit(1)
			}
			applySettingsYAML(stateManager, data)
			return
		default:
			fmt.Printf("%s Unknown option: %s\n", qc.Colorize("❌ Error:", qc.ColorRed), args[i])
			os.Exit(1)
		}
	}

	// Create state manager
	stateManager := NewStateManager(stateFile)
	if err := stateManager.Load(); err != nil {
		fmt.Printf("%s Failed to load state: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		os.Exit(1)
	}
	if slices.Contains(args, "--stdin") {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Printf("%s Failed to read stdin: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
			os.Exit(1)
		}
		applySettingsYAML(stateManager, data)
		return
	}
	// Edit settings
	editSettings(stateManager)
}

// handleNotifiersCommand handles the notifiers command
func handleNotifiersCommand(args []string) {
	// Parse command line arguments
	stateFile := "watch-state.yml"

	// Parse flags
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--state":
			if i+1 < len(args) {
				stateFile = args[i+1]
				i++ // Skip next argument
			} else {
				fmt.Printf("%s --state requires a file path\n", qc.Colorize("❌ Error:", qc.ColorRed))
				os.Exit(1)
			}
		case "--stdin":
			// Handle stdin mode
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Printf("%s Failed to read stdin: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
				os.Exit(1)
			}
			stateManager := NewStateManager(stateFile)
			if err := stateManager.Load(); err != nil {
				fmt.Printf("%s Failed to load state: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
				os.Exit(1)
			}
			applyAlertsYAML(stateManager, data)
			return
		default:
			fmt.Printf("%s Unknown option: %s\n", qc.Colorize("❌ Error:", qc.ColorRed), args[i])
			os.Exit(1)
		}
	}

	// Create state manager
	stateManager := NewStateManager(stateFile)
	if err := stateManager.Load(); err != nil {
		fmt.Printf("%s Failed to load state: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
		os.Exit(1)
	}
	if slices.Contains(args, "--stdin") {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Printf("%s Failed to read stdin: %v\n", qc.Colorize("❌ Error:", qc.ColorRed), err)
			os.Exit(1)
		}
		applyAlertsYAML(stateManager, data)
		return
	}
	// Edit alerts
	editAlerts(stateManager)
}

// handleValidateCommand handles the validate command
func handleValidateCommand(args []string) {
	// Parse command line arguments
	stateFile := "watch-state.yml"
	configFile := ""
	verbose := false

	// Parse flags
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--state":
			if i+1 < len(args) {
				stateFile = args[i+1]
				i++ // Skip next argument
			} else {
				fmt.Printf("%s --state requires a file path\n", qc.Colorize("❌ Error:", qc.ColorRed))
				os.Exit(1)
			}
		case "--config":
			if i+1 < len(args) {
				configFile = args[i+1]
				i++ // Skip next argument
			} else {
				fmt.Printf("%s --config requires a file path\n", qc.Colorize("❌ Error:", qc.ColorRed))
				os.Exit(1)
			}
		case "--verbose", "-v":
			verbose = true
		default:
			fmt.Printf("%s Unknown option: %s\n", qc.Colorize("❌ Error:", qc.ColorRed), args[i])
			os.Exit(1)
		}
	}

	// Validate configuration
	if configFile != "" {
		validateConfigFile(configFile, verbose)
	} else {
		validateStateFile(stateFile, verbose)
	}
}
