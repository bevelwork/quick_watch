// Package main provides a command-line tool for monitoring URLs and services
// with configurable alerts and webhook notifications. This tool provides the
// simplest possible monitoring with threshold-based alerting and external
// webhook support.

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	qc "github.com/bevelwork/quick_color"
	versionpkg "github.com/bevelwork/quick_watch/version"
)

// Colors are provided by quick_color

// version is set at build time via ldflags
var version = ""

// StringSliceFlag implements flag.Value for string slices
type StringSliceFlag []string

func (s *StringSliceFlag) String() string {
	return fmt.Sprintf("%v", *s)
}

func (s *StringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

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
	case "edit":
		handleEditCommand(args)
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
	fmt.Println("Actions:")
	fmt.Println("  edit                    Edit monitors using $EDITOR")
	fmt.Println("  add <url>               Add a monitor")
	fmt.Println("  rm <url>                Remove a monitor")
	fmt.Println("  list                    List all monitors")
	fmt.Println("  config <file>           Use YAML configuration file")
	fmt.Println("  server                  Start in server mode")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  --state <file>          State file path (default: watch-state.yml)")
	fmt.Println("  --method <method>       HTTP method (default: GET)")
	fmt.Println("  --header <key:value>    HTTP headers (can be used multiple times)")
	fmt.Println("  --threshold <seconds>   Down threshold in seconds (default: 30)")
	fmt.Println("  --webhook-port <port>    Webhook server port")
	fmt.Println("  --webhook-path <path>    Webhook endpoint path (default: /webhook)")
	fmt.Println("  --check-strategy <str>   Check strategy (default: http)")
	fmt.Println("  --alert-strategy <str>  Alert strategy (default: console)")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Printf("  %s edit\n", os.Args[0])
	fmt.Printf("  %s add https://api.example.com/health --threshold 30\n", os.Args[0])
	fmt.Printf("  %s rm https://api.example.com/health\n", os.Args[0])
	fmt.Printf("  %s list\n", os.Args[0])
	fmt.Printf("  %s config monitors.yml\n", os.Args[0])
	fmt.Printf("  %s server --webhook-port 8080\n", os.Args[0])
}

// handleEditCommand handles the edit action
func handleEditCommand(args []string) {
	stateFile := getStateFile(args)
	handleEditMonitors(stateFile)
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

	handleAddMonitor(stateFile, url, method, headers, threshold, checkStrategy, alertStrategy)
}

// handleRemoveCommand handles the rm action
func handleRemoveCommand(args []string) {
	if len(args) == 0 {
		fmt.Printf("%s URL is required for rm action\n", qc.Colorize("❌ Error:", qc.ColorRed))
		os.Exit(1)
	}

	url := args[0]
	stateFile := getStateFile(args[1:])
	handleRemoveMonitor(stateFile, url)
}

// handleListCommand handles the list action
func handleListCommand(args []string) {
	stateFile := getStateFile(args)
	handleListMonitors(stateFile)
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

	// Create monitoring engine
	engine := NewMonitoringEngine(config)

	// Start webhook server if requested
	var webhookServer *WebhookServer
	if webhookPort > 0 {
		webhookServer = NewWebhookServer(webhookPort, webhookPath, engine)
		if err := webhookServer.Start(ctx); err != nil {
			log.Fatal(err)
		}
	}

	// Start monitoring
	if err := engine.Start(ctx); err != nil {
		log.Fatal(err)
	}

	// Print monitoring status
	printMonitoringStatus(engine)

	// Wait for context cancellation
	<-ctx.Done()

	// Stop webhook server if running
	if webhookServer != nil {
		webhookServer.Stop(context.Background())
	}

	fmt.Println("Monitoring stopped.")
}

// loadConfiguration loads configuration from YAML file or command line
func loadConfiguration(configFile, url, method string, headers []string, threshold int, checkStrategy, alertStrategy string) (*MonitorConfig, error) {
	var config *MonitorConfig

	// If config file is provided, load from YAML file
	if configFile != "" {
		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %v", err)
		}

		config, err = LoadYAMLConfig(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse YAML config file: %v", err)
		}
	} else if url != "" {
		// Create single monitor from command line
		monitor := Monitor{
			Name:          "CLI Monitor",
			URL:           url,
			Method:        method,
			Headers:       parseHeaders(headers),
			Threshold:     threshold,
			CheckStrategy: checkStrategy,
			AlertStrategy: alertStrategy,
		}
		config = &MonitorConfig{
			Monitors: []Monitor{monitor},
		}
	} else {
		return nil, fmt.Errorf("either --config or --url must be specified")
	}

	return config, nil
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

// MonitorFields tracks which fields were explicitly set in the YAML
type MonitorFields struct {
	Method        bool
	Headers       bool
	Threshold     bool
	StatusCodes   bool
	SizeAlerts    bool
	CheckStrategy bool
	AlertStrategy bool
}

// cleanAllDefaults removes all default values from monitor configuration (for temp file creation)
func cleanAllDefaults(monitor *Monitor) {
	appliedDefaults := []string{}

	// Check and clean method
	if monitor.Method == "GET" {
		monitor.Method = ""
		appliedDefaults = append(appliedDefaults, "method: GET")
	}

	// Check and clean headers
	if len(monitor.Headers) == 0 {
		monitor.Headers = nil
		appliedDefaults = append(appliedDefaults, "headers: {}")
	}

	// Check and clean threshold
	if monitor.Threshold == 30 {
		monitor.Threshold = 0
		appliedDefaults = append(appliedDefaults, "threshold: 30s")
	}

	// Check and clean status codes
	if len(monitor.StatusCodes) == 1 && monitor.StatusCodes[0] == "*" {
		monitor.StatusCodes = nil
		appliedDefaults = append(appliedDefaults, "status_codes: [\"*\"]")
	}

	// Check and clean size alerts
	if monitor.SizeAlerts.Enabled && monitor.SizeAlerts.HistorySize == 100 && monitor.SizeAlerts.Threshold == 0.5 {
		monitor.SizeAlerts = SizeAlertConfig{}
		appliedDefaults = append(appliedDefaults, "size_alerts: {enabled: true, history_size: 100, threshold: 0.5}")
	}

	// Check and clean check strategy
	if monitor.CheckStrategy == "http" {
		monitor.CheckStrategy = ""
		appliedDefaults = append(appliedDefaults, "check_strategy: http")
	}

	// Check and clean alert strategy
	if monitor.AlertStrategy == "console" {
		monitor.AlertStrategy = ""
		appliedDefaults = append(appliedDefaults, "alert_strategy: console")
	}

	// Show INFO message if any defaults were applied
	if len(appliedDefaults) > 0 {
		fmt.Printf("%s Applied defaults for %s: %s\n",
			qc.Colorize("ℹ️ INFO:", qc.ColorCyan),
			monitor.Name,
			strings.Join(appliedDefaults, ", "))
	}
}

// cleanDefaults removes default values from monitor configuration and shows INFO messages
// It only cleans fields that were explicitly set to default values
func cleanDefaults(monitor *Monitor, fields *MonitorFields) {
	appliedDefaults := []string{}

	// Check and clean method (only if it was explicitly set)
	if fields.Method && monitor.Method == "GET" {
		monitor.Method = ""
		appliedDefaults = append(appliedDefaults, "method: GET")
	}

	// Check and clean headers (only if it was explicitly set)
	if fields.Headers && len(monitor.Headers) == 0 {
		monitor.Headers = nil
		appliedDefaults = append(appliedDefaults, "headers: {}")
	}

	// Check and clean threshold (only if it was explicitly set)
	if fields.Threshold && monitor.Threshold == 30 {
		monitor.Threshold = 0
		appliedDefaults = append(appliedDefaults, "threshold: 30s")
	}

	// Check and clean status codes (only if it was explicitly set)
	if fields.StatusCodes && len(monitor.StatusCodes) == 1 && monitor.StatusCodes[0] == "*" {
		monitor.StatusCodes = nil
		appliedDefaults = append(appliedDefaults, "status_codes: [\"*\"]")
	}

	// Check and clean size alerts (only if it was explicitly set)
	if fields.SizeAlerts && monitor.SizeAlerts.Enabled && monitor.SizeAlerts.HistorySize == 100 && monitor.SizeAlerts.Threshold == 0.5 {
		monitor.SizeAlerts = SizeAlertConfig{}
		appliedDefaults = append(appliedDefaults, "size_alerts: {enabled: true, history_size: 100, threshold: 0.5}")
	}

	// Check and clean check strategy (only if it was explicitly set)
	if fields.CheckStrategy && monitor.CheckStrategy == "http" {
		monitor.CheckStrategy = ""
		appliedDefaults = append(appliedDefaults, "check_strategy: http")
	}

	// Check and clean alert strategy (only if it was explicitly set)
	if fields.AlertStrategy && monitor.AlertStrategy == "console" {
		monitor.AlertStrategy = ""
		appliedDefaults = append(appliedDefaults, "alert_strategy: console")
	}

	// Show INFO message if any defaults were applied
	if len(appliedDefaults) > 0 {
		fmt.Printf("%s Applied defaults for %s: %s\n",
			qc.Colorize("ℹ️ INFO:", qc.ColorCyan),
			monitor.Name,
			strings.Join(appliedDefaults, ", "))
	}
}

// applyDefaultsAfterClean applies default values after cleaning
func applyDefaultsAfterClean(monitor *Monitor) {
	if monitor.Method == "" {
		monitor.Method = "GET"
	}
	if monitor.Headers == nil {
		monitor.Headers = make(map[string]string)
	}
	if monitor.Threshold == 0 {
		monitor.Threshold = 30
	}
	if len(monitor.StatusCodes) == 0 {
		monitor.StatusCodes = []string{"*"}
	}
	if monitor.SizeAlerts.HistorySize == 0 {
		monitor.SizeAlerts = SizeAlertConfig{
			Enabled:     true,
			HistorySize: 100,
			Threshold:   0.5,
		}
	}
	if monitor.CheckStrategy == "" {
		monitor.CheckStrategy = "http"
	}
	if monitor.AlertStrategy == "" {
		monitor.AlertStrategy = "console"
	}
}

// printHeader prints the application header
func printHeader() {
	fmt.Printf("%s\n", qc.Colorize("╔══════════════════════════════════════════════════════════════╗", qc.ColorBlue))
	fmt.Printf("%s %s %s\n",
		qc.Colorize("║", qc.ColorBlue),
		qc.Colorize("🚀 Quick Watch", qc.ColorCyan),
		qc.Colorize("║", qc.ColorBlue))
	fmt.Printf("%s\n", qc.Colorize("╚══════════════════════════════════════════════════════════════╝", qc.ColorBlue))
	fmt.Printf("%s %s\n", qc.Colorize("Version:", qc.ColorYellow), qc.Colorize(resolveVersion(), qc.ColorWhite))
}

// printMonitoringStatus prints the current monitoring status
func printMonitoringStatus(engine *MonitoringEngine) {
	monitors := engine.GetMonitorStatus()

	fmt.Printf("\n%s\n", qc.Colorize("📊 Monitoring Status", qc.ColorBlue))
	fmt.Printf("%s %s\n", qc.Colorize("Active monitors:", qc.ColorCyan), qc.Colorize(fmt.Sprintf("%d", len(monitors)), qc.ColorWhite))

	for i, state := range monitors {
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
			state.Monitor.Name,
			state.Monitor.URL,
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

	fmt.Printf("\n%s\n", qc.Colorize("🚀 Monitoring started. Press Ctrl+C to stop.", qc.ColorYellow))
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
	fmt.Printf("API endpoint: http://0.0.0.0:8081\n")
	fmt.Printf("Web interface: http://0.0.0.0:8081\n")
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

// handleAddMonitor adds a monitor to the state file
func handleAddMonitor(stateFile, url, method string, headers []string, threshold int, checkStrategy, alertStrategy string) {
	stateManager := NewStateManager(stateFile)

	// Load existing state
	if err := stateManager.Load(); err != nil {
		log.Printf("Warning: Could not load existing state: %v", err)
	}

	// Create monitor
	monitor := Monitor{
		Name:        fmt.Sprintf("Monitor-%s", url),
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
		AlertStrategy: alertStrategy,
	}

	// Clean defaults and show INFO messages
	cleanAllDefaults(&monitor)

	// Apply defaults for runtime
	applyDefaultsAfterClean(&monitor)

	// Add monitor
	if err := stateManager.AddMonitor(monitor); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s Added monitor: %s\n", qc.Colorize("✅ Success:", qc.ColorGreen), url)
	fmt.Printf("  URL: %s\n", monitor.URL)
	fmt.Printf("  Method: %s\n", monitor.Method)
	fmt.Printf("  Threshold: %d seconds\n", monitor.Threshold)
	fmt.Printf("  Check Strategy: %s\n", monitor.CheckStrategy)
	fmt.Printf("  Alert Strategy: %s\n", monitor.AlertStrategy)
}

// handleRemoveMonitor removes a monitor from the state file
func handleRemoveMonitor(stateFile, url string) {
	stateManager := NewStateManager(stateFile)

	// Load existing state
	if err := stateManager.Load(); err != nil {
		log.Fatal(err)
	}

	// Remove monitor
	if err := stateManager.RemoveMonitor(url); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s Removed monitor: %s\n", qc.Colorize("🗑️ Success:", qc.ColorGreen), url)
}

// handleListMonitors lists all monitors in the state file
func handleListMonitors(stateFile string) {
	stateManager := NewStateManager(stateFile)

	// Load existing state
	if err := stateManager.Load(); err != nil {
		log.Printf("Warning: Could not load existing state: %v", err)
	}

	monitors := stateManager.ListMonitors()

	if len(monitors) == 0 {
		fmt.Printf("%s No monitors configured\n", qc.Colorize("ℹ️ Info:", qc.ColorYellow))
		return
	}

	fmt.Printf("%s Configured Monitors (%d):\n", qc.Colorize("📋 Info:", qc.ColorBlue), len(monitors))
	fmt.Println()

	i := 0
	for _, monitor := range monitors {
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
}
