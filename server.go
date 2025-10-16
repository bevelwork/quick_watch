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
	engine        *MonitoringEngine
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

	// Create monitoring engine
	config := s.stateManager.GetMonitorConfig()
	s.engine = NewMonitoringEngine(config, s.stateManager)

	// Start monitoring
	if err := s.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start monitoring engine: %v", err)
	}

	// Send startup message if enabled and configured
	settings := s.stateManager.GetSettings()
	if settings.Startup.Enabled {
		s.sendStartupMessage(ctx)
	}

	// Start webhook server if configured
	if settings.WebhookPort > 0 {
		s.webhookServer = NewWebhookServer(settings.WebhookPort, settings.WebhookPath, s.engine)
		if err := s.webhookServer.Start(ctx); err != nil {
			return fmt.Errorf("failed to start webhook server: %v", err)
		}
	}

	// Set up HTTP server for API
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/monitors", s.handleMonitors)
	mux.HandleFunc("/api/monitors/", s.handleMonitorByURL)
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
    <p>Quick Watch monitoring server is running.</p>
    
    <h2>API Endpoints</h2>
    <div class="endpoint">
        <span class="method">GET</span> /api/status - Get monitoring status
    </div>
    <div class="endpoint">
        <span class="method">GET</span> /api/monitors - List all monitors
    </div>
    <div class="endpoint">
        <span class="method">POST</span> /api/monitors - Add a monitor
    </div>
    <div class="endpoint">
        <span class="method">DELETE</span> /api/monitors/{url} - Remove a monitor
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

	response := map[string]interface{}{
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

	monitors := s.engine.GetMonitorStatus()
	status := map[string]interface{}{
		"timestamp": time.Now(),
		"service":   "quick_watch",
		"state":     s.state,
		"monitors":  make([]map[string]interface{}, len(monitors)),
	}

	monitorList := status["monitors"].([]map[string]interface{})
	for i, state := range monitors {
		monitorList[i] = map[string]interface{}{
			"name":       state.Monitor.Name,
			"url":        state.Monitor.URL,
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

// handleMonitors handles monitor management
func (s *Server) handleMonitors(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		s.handleListMonitors(w, r)
	case "POST":
		s.handleAddMonitor(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleListMonitors lists all monitors
func (s *Server) handleListMonitors(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	monitors := s.stateManager.ListMonitors()
	json.NewEncoder(w).Encode(monitors)
}

// handleAddMonitor adds a new monitor
func (s *Server) handleAddMonitor(w http.ResponseWriter, r *http.Request) {
	var monitor Monitor
	if err := json.NewDecoder(r.Body).Decode(&monitor); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if monitor.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	if err := s.stateManager.AddMonitor(monitor); err != nil {
		http.Error(w, fmt.Sprintf("Failed to add monitor: %v", err), http.StatusInternalServerError)
		return
	}

	// Restart monitoring engine with new configuration
	config := s.stateManager.GetMonitorConfig()
	s.engine = NewMonitoringEngine(config, s.stateManager)
	if err := s.engine.Start(r.Context()); err != nil {
		log.Printf("Failed to restart monitoring engine: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "added", "url": monitor.URL})
}

// handleMonitorByURL handles individual monitor operations
func (s *Server) handleMonitorByURL(w http.ResponseWriter, r *http.Request) {
	// Extract URL from path
	path := strings.TrimPrefix(r.URL.Path, "/api/monitors/")
	if path == "" {
		http.Error(w, "URL parameter required", http.StatusBadRequest)
		return
	}

	// URL decode if needed
	url := path

	switch r.Method {
	case "GET":
		monitor, exists := s.stateManager.GetMonitor(url)
		if !exists {
			http.Error(w, "Monitor not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(monitor)

	case "DELETE":
		if err := s.stateManager.RemoveMonitor(url); err != nil {
			http.Error(w, fmt.Sprintf("Failed to remove monitor: %v", err), http.StatusInternalServerError)
			return
		}

		// Restart monitoring engine with new configuration
		config := s.stateManager.GetMonitorConfig()
		s.engine = NewMonitoringEngine(config, s.stateManager)
		if err := s.engine.Start(r.Context()); err != nil {
			log.Printf("Failed to restart monitoring engine: %v", err)
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

// sendStartupMessage sends startup notifications to configured notifiers
func (s *Server) sendStartupMessage(ctx context.Context) {
	settings := s.stateManager.GetSettings()

	// Check if startup messages are enabled
	if !settings.Startup.Enabled {
		return
	}

	monitorCount := len(s.engine.monitors)
	version := resolveVersion()

	// Send startup message to each configured notifier
	for _, notifierName := range settings.Startup.Notifiers {
		if alertStrategy, exists := s.engine.alertStrategies[notifierName]; exists {
			if slack, ok := alertStrategy.(*SlackAlertStrategy); ok {
				if err := slack.SendStartupMessage(ctx, version, monitorCount); err != nil {
					log.Printf("Failed to send startup message to %s: %v", notifierName, err)
				} else {
					log.Printf("Startup message sent to %s successfully", notifierName)
				}
			} else if _, ok := alertStrategy.(*ConsoleAlertStrategy); ok {
				// For console notifiers, we can just log the startup message
				log.Printf("🚀 Quick Watch started - Version: %s, Monitors: %d", version, monitorCount)
			}
		} else {
			log.Printf("Warning: Startup notifier '%s' not found or not available", notifierName)
		}
	}
}
