package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Server represents the quick_watch server
type Server struct {
	stateManager  *StateManager
	engine        *TargetEngine
	webhookServer *WebhookServer
	server        *http.Server
	state         string // "stopped", "starting", "running", "stopping"
}

// NewServer creates a new quick_watch server
func NewServer(stateFile string) *Server {
	stateManager := NewStateManager(stateFile)
	return &Server{
		stateManager: stateManager,
		state:        "stopped",
	}
}

// Start starts the server
func (s *Server) Start(ctx context.Context) error {
	s.state = "starting"

	// Load state
	if err := s.stateManager.Load(); err != nil {
		return fmt.Errorf("failed to load state: %v", err)
	}

	// Create targeting engine
	config := s.stateManager.GetTargetConfig()
	s.engine = NewTargetEngine(config, s.stateManager)

	// Start targeting
	if err := s.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start targeting engine: %v", err)
	}

	// Send startup message if enabled and configured
	settings := s.stateManager.GetSettings()
	if settings.Startup.Enabled {
		s.sendStartupMessage(ctx)
	}

	// Start webhook server if configured
	if settings.WebhookPort > 0 {
		s.webhookServer = NewWebhookServer(settings.WebhookPort, settings.WebhookPath, s.engine, s.stateManager)
		if err := s.webhookServer.Start(ctx); err != nil {
			return fmt.Errorf("failed to start webhook server: %v", err)
		}
	}

	// Set up HTTP server for API
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/targets", s.handleTargets)
	mux.HandleFunc("/api/targets/", s.handleTargetByURL)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/settings", s.handleSettings)

	// Health and info endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/info", s.handleInfo)

	// Root endpoint
	mux.HandleFunc("/", s.handleRoot)

	s.server = &http.Server{
		Addr:    ":8081", // Default API port (8080 is used by webhook)
		Handler: mux,
	}

	s.state = "running"

	// Start server in goroutine
	go func() {
		log.Printf("Starting quick_watch server on port 8080")
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the server
func (s *Server) Stop(ctx context.Context) error {
	s.state = "stopping"

	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			return err
		}
	}

	if s.webhookServer != nil {
		if err := s.webhookServer.Stop(ctx); err != nil {
			return err
		}
	}

	s.state = "stopped"
	return nil
}

// handleRoot handles the root endpoint
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)

	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Quick Watch Server</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        .endpoint { background: #f5f5f5; padding: 10px; margin: 10px 0; border-radius: 5px; }
        .method { font-weight: bold; color: #0066cc; }
    </style>
</head>
<body>
    <h1>Quick Watch Server</h1>
    <p>Quick Watch targeting server is running.</p>
    
    <h2>API Endpoints</h2>
    <div class="endpoint">
        <span class="method">GET</span> /api/status - Get targeting status
    </div>
    <div class="endpoint">
        <span class="method">GET</span> /api/targets - List all targets
    </div>
    <div class="endpoint">
        <span class="method">POST</span> /api/targets - Add a target
    </div>
    <div class="endpoint">
        <span class="method">DELETE</span> /api/targets/{url} - Remove a target
    </div>
    <div class="endpoint">
        <span class="method">GET</span> /api/state - Get server state
    </div>
    <div class="endpoint">
        <span class="method">GET</span> /health - Health check
    </div>
</body>
</html>`

	w.Write([]byte(html))
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]any{
		"status":    "healthy",
		"timestamp": time.Now(),
		"service":   "quick_watch",
		"version":   resolveVersion(),
		"state":     s.state,
	}

	json.NewEncoder(w).Encode(response)
}

// handleInfo handles info requests
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	info := s.stateManager.GetStateInfo()
	info["state"] = s.state
	info["timestamp"] = time.Now()

	json.NewEncoder(w).Encode(info)
}

// handleStatus handles status requests
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	targets := s.engine.GetTargetStatus()
	status := map[string]any{
		"timestamp": time.Now(),
		"service":   "quick_watch",
		"state":     s.state,
		"targets":   make([]map[string]any, len(targets)),
	}

	targetList := status["targets"].([]map[string]any)
	for i, state := range targets {
		targetList[i] = map[string]any{
			"name":       state.Target.Name,
			"url":        state.Target.URL,
			"is_down":    state.IsDown,
			"down_since": state.DownSince,
			"last_check": state.LastCheck,
		}
	}

	json.NewEncoder(w).Encode(status)
}

// handleState handles state requests
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	state := s.stateManager.GetStateInfo()
	state["server_state"] = s.state
	state["timestamp"] = time.Now()

	json.NewEncoder(w).Encode(state)
}

// handleTargets handles target management
func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		s.handleListTargets(w, r)
	case "POST":
		s.handleAddTarget(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleListTargets lists all targets
func (s *Server) handleListTargets(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	targets := s.stateManager.ListTargets()
	json.NewEncoder(w).Encode(targets)
}

// handleAddTarget adds a new target
func (s *Server) handleAddTarget(w http.ResponseWriter, r *http.Request) {
	var target Target
	if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if target.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	if err := s.stateManager.AddTarget(target); err != nil {
		http.Error(w, fmt.Sprintf("Failed to add target: %v", err), http.StatusInternalServerError)
		return
	}

	// Restart targeting engine with new configuration
	config := s.stateManager.GetTargetConfig()
	s.engine = NewTargetEngine(config, s.stateManager)
	if err := s.engine.Start(r.Context()); err != nil {
		log.Printf("Failed to restart targeting engine: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "added", "url": target.URL})
}

// handleTargetByURL handles individual target operations
func (s *Server) handleTargetByURL(w http.ResponseWriter, r *http.Request) {
	// Extract URL from path
	path := strings.TrimPrefix(r.URL.Path, "/api/targets/")
	if path == "" {
		http.Error(w, "URL parameter required", http.StatusBadRequest)
		return
	}

	// URL decode if needed
	url := path

	switch r.Method {
	case "GET":
		target, exists := s.stateManager.GetTarget(url)
		if !exists {
			http.Error(w, "Target not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(target)

	case "DELETE":
		if err := s.stateManager.RemoveTarget(url); err != nil {
			http.Error(w, fmt.Sprintf("Failed to remove target: %v", err), http.StatusInternalServerError)
			return
		}

		// Restart targeting engine with new configuration
		config := s.stateManager.GetTargetConfig()
		s.engine = NewTargetEngine(config, s.stateManager)
		if err := s.engine.Start(r.Context()); err != nil {
			log.Printf("Failed to restart targeting engine: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "removed", "url": url})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSettings handles settings management
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		settings := s.stateManager.GetSettings()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(settings)

	case "POST":
		var settings ServerSettings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if err := s.stateManager.UpdateSettings(settings); err != nil {
			http.Error(w, fmt.Sprintf("Failed to update settings: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// sendStartupMessage sends startup notifications to configured alerts
func (s *Server) sendStartupMessage(ctx context.Context) {
	settings := s.stateManager.GetSettings()

	// Check if startup messages are enabled
	if !settings.Startup.Enabled {
		return
	}

	targetCount := len(s.engine.targets)
	version := resolveVersion()

	// Send startup message to each configured alert
	for _, alertName := range settings.Startup.Alerts {
		if alertStrategy, exists := s.engine.alertStrategies[alertName]; exists {
			if slack, ok := alertStrategy.(*SlackAlertStrategy); ok {
				if err := slack.SendStartupMessage(ctx, version, targetCount); err != nil {
					log.Printf("Failed to send startup message to %s: %v", alertName, err)
				} else {
					log.Printf("Startup message sent to %s successfully", alertName)
				}
			} else if _, ok := alertStrategy.(*ConsoleAlertStrategy); ok {
				// For console alerts, we can just log the startup message
				log.Printf("🚀 Quick Watch started - Version: %s, Targets: %d", version, targetCount)
			}
		} else {
			log.Printf("Warning: Startup alert '%s' not found or not available", alertName)
		}
	}

	// Check all targets if enabled
	if settings.Startup.CheckAllTargets {
		s.checkAllTargetsOnStartup(ctx)
	}
}

// checkAllTargetsOnStartup checks all targets and reports their health status
func (s *Server) checkAllTargetsOnStartup(ctx context.Context) {
	log.Printf("🔍 Checking all targets on startup...")

	settings := s.stateManager.GetSettings()
	targetConfig := s.stateManager.GetTargetConfig()

	// Check each target
	for _, target := range targetConfig.Targets {
		// Get the check strategy for this target
		checkStrategy, exists := s.engine.checkStrategies[target.CheckStrategy]
		if !exists {
			log.Printf("Warning: Check strategy '%s' not found for target %s", target.CheckStrategy, target.Name)
			continue
		}

		// Perform the check
		result, err := checkStrategy.Check(ctx, &target)
		if err != nil {
			log.Printf("Error checking target %s: %v", target.Name, err)
			continue
		}

		// Report the result to configured alerts
		for _, alertName := range settings.Startup.Alerts {
			if alertStrategy, exists := s.engine.alertStrategies[alertName]; exists {
				if slack, ok := alertStrategy.(*SlackAlertStrategy); ok {
					// Send health status to Slack
					if err := s.sendHealthStatusToSlack(ctx, slack, &target, result); err != nil {
						log.Printf("Failed to send health status to %s for %s: %v", alertName, target.Name, err)
					}
				} else if _, ok := alertStrategy.(*ConsoleAlertStrategy); ok {
					// Log health status to console
					s.logHealthStatusToConsole(&target, result)
				}
			}
		}
	}

	log.Printf("✅ Startup health check completed")
}

// sendHealthStatusToSlack sends health status to Slack
func (s *Server) sendHealthStatusToSlack(ctx context.Context, slack *SlackAlertStrategy, target *Target, result *CheckResult) error {
	if result.Success {
		// Send all-clear message for healthy services
		return slack.SendAllClear(ctx, target, result)
	} else {
		// Send alert message for unhealthy services
		return slack.SendAlert(ctx, target, result)
	}
}

// logHealthStatusToConsole logs health status to console
func (s *Server) logHealthStatusToConsole(target *Target, result *CheckResult) {
	if result.Success {
		log.Printf("✅ %s: UP - Status: %d, Time: %v", target.Name, result.StatusCode, result.ResponseTime)
	} else {
		log.Printf("❌ %s: DOWN - Error: %s", target.Name, result.Error)
	}
}
