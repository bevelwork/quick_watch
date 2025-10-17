package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Server represents the quick_watch server
type Server struct {
	stateManager *StateManager
	engine       *TargetEngine
	server       *http.Server
	state        string // "stopped", "starting", "running", "stopping"
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

	// Get settings
	settings := s.stateManager.GetSettings()

	// Configure acknowledgements
	port := settings.WebhookPort
	if port == 0 {
		port = 8080
	}
	// Use configured server address or default to localhost
	serverAddress := settings.ServerAddress
	if serverAddress == "" {
		serverAddress = fmt.Sprintf("http://localhost:%d", port)
	}
	s.engine.SetAcknowledgementConfig(serverAddress, settings.AcknowledgementsEnabled)

	// Start targeting
	if err := s.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start targeting engine: %v", err)
	}

	// Send startup message if enabled and configured
	if settings.Startup.Enabled {
		s.sendStartupMessage(ctx)
	}

	// Start status report ticker if enabled
	if settings.StatusReport.Enabled {
		s.startStatusReportTicker(ctx, settings.StatusReport)
	}

	// Set up unified HTTP server with all routes
	mux := http.NewServeMux()

	// Webhook endpoints (from legacy WebhookServer)
	webhookPath := settings.WebhookPath
	if webhookPath == "" {
		webhookPath = "/webhook"
	}
	mux.HandleFunc(webhookPath, s.handleWebhook)

	// Register dynamic hook routes
	s.registerHookRoutes(mux)

	// API endpoints
	mux.HandleFunc("/api/targets", s.handleTargets)
	mux.HandleFunc("/api/targets/", s.handleTargetByURL)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/acknowledge/", s.handleAcknowledge)
	mux.HandleFunc("/api/trigger/", s.handleTrigger)

	// Trigger endpoints
	mux.HandleFunc("/trigger/status_report", s.handleTriggerStatusReport)

	// Target detail pages
	mux.HandleFunc("/targets/", s.handleTargetDetail)
	mux.HandleFunc("/targets", s.handleTargetList)
	mux.HandleFunc("/api/history/", s.handleTargetHistoryAPI)

	// Health and info endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/info", s.handleInfo)
	mux.HandleFunc("/status", s.handleWebhookStatus)

	// Root endpoint
	mux.HandleFunc("/", s.handleRoot)

	// Server is configured with port from settings (already set above)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	s.state = "running"

	// Log unified server startup
	log.Printf("Starting Quick Watch unified server on port %d", port)

	// Use configured server address or localhost
	displayAddr := serverAddress
	if displayAddr == "" {
		displayAddr = fmt.Sprintf("http://localhost:%d", port)
		log.Printf("⚠️  Server address not configured - using localhost")
	}

	log.Printf("Webhook endpoint: %s%s", displayAddr, webhookPath)
	log.Printf("API endpoints: %s/api/*", displayAddr)
	log.Printf("Target pages: %s/targets", displayAddr)
	log.Printf("Health check: %s/health", displayAddr)
	log.Printf("Status: %s/status", displayAddr)

	// Start server in goroutine
	go func() {
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

	s.state = "stopped"
	return nil
}

// registerHookRoutes registers named hook routes from state manager
func (s *Server) registerHookRoutes(mux *http.ServeMux) {
	if s.stateManager == nil {
		return
	}
	hooks := s.stateManager.ListHooks()
	for name, hook := range hooks {
		// Always mount under /hooks/<name>
		routePath := "/hooks/" + name
		// Capture variables for handler closure
		h := hook
		mux.HandleFunc(routePath, func(wr http.ResponseWriter, r *http.Request) {
			// Method check
			if len(h.Methods) > 0 {
				allowed := false
				for _, m := range h.Methods {
					if r.Method == m {
						allowed = true
						break
					}
				}
				if !allowed {
					http.Error(wr, "Method not allowed", http.StatusMethodNotAllowed)
					return
				}
			}

			// Auth check
			if h.Auth.BearerToken != "" {
				auth := r.Header.Get("Authorization")
				expected := "Bearer " + h.Auth.BearerToken
				if auth != expected {
					http.Error(wr, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
			if h.Auth.Username != "" || h.Auth.Password != "" {
				u, p, ok := r.BasicAuth()
				if !ok || u != h.Auth.Username || p != h.Auth.Password {
					wr.Header().Set("WWW-Authenticate", "Basic realm=restricted")
					http.Error(wr, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}

			// Build notification from request
			body := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)

			// Resolve message precedence: URL param 'msg' > body.msg > hook default
			msg := h.Message
			if q := r.URL.Query().Get("msg"); strings.TrimSpace(q) != "" {
				msg = q
				body["msg"] = q
			} else if v, ok := body["msg"].(string); ok && strings.TrimSpace(v) != "" {
				msg = v
			}
			if msg == "" {
				msg = "hook triggered"
			}
			notification := &WebhookNotification{
				Type:      "hook",
				Target:    h.Name,
				Message:   msg,
				Timestamp: time.Now(),
				Data:      body,
			}

			// Generate acknowledgement token if enabled
			var ackURL string
			if s.stateManager != nil && s.engine != nil {
				settings := s.stateManager.GetSettings()
				if settings.AcknowledgementsEnabled {
					// Generate token (same format as target ack tokens)
					token := fmt.Sprintf("%x", time.Now().UnixNano())
					hookState := &HookState{
						HookName:    h.Name,
						Message:     msg,
						TriggeredAt: time.Now(),
						AckToken:    token,
					}

					s.engine.ackMutex.Lock()
					s.engine.hookAckTokenMap[token] = hookState
					s.engine.ackMutex.Unlock()

					ackURL = s.engine.GetAcknowledgementURL(token)
				}
			}

			// Dispatch to selected notification strategies
			if len(h.Alerts) == 0 {
				h.Alerts = []string{"console"}
			}
			for _, alertName := range h.Alerts {
				if strat, exists := s.engine.notificationStrategies[alertName]; exists {
					// Use acknowledgement-aware method if available
					if ackSender, ok := strat.(AcknowledgementAwareNotification); ok && ackURL != "" {
						if err := ackSender.HandleNotificationWithAck(r.Context(), notification, ackURL); err != nil {
							log.Printf("Hook %s notify via %s failed: %v", h.Name, alertName, err)
						} else {
							// Track metric: notification sent
							s.engine.metrics.mutex.Lock()
							s.engine.metrics.NotificationsSent++
							s.engine.metrics.mutex.Unlock()
						}
					} else {
						if err := strat.HandleNotification(r.Context(), notification); err != nil {
							log.Printf("Hook %s notify via %s failed: %v", h.Name, alertName, err)
						} else {
							// Track metric: notification sent
							s.engine.metrics.mutex.Lock()
							s.engine.metrics.NotificationsSent++
							s.engine.metrics.mutex.Unlock()
						}
					}
				}
			}

			wr.WriteHeader(http.StatusOK)
			wr.Write([]byte("OK"))
		})
		log.Printf("Hook route registered: %s -> alerts=%v", routePath, hook.Alerts)
	}
}

// handleWebhook handles incoming webhook notifications
func (s *Server) handleWebhook(wr http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(wr, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var notification WebhookNotification
	if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
		http.Error(wr, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Set timestamp if not provided
	if notification.Timestamp.IsZero() {
		notification.Timestamp = time.Now()
	}

	// Handle the notification
	if err := s.engine.HandleWebhookNotification(r.Context(), &notification); err != nil {
		log.Printf("Error handling webhook notification: %v", err)
		http.Error(wr, "Internal server error", http.StatusInternalServerError)
		return
	}

	wr.WriteHeader(http.StatusOK)
	wr.Write([]byte("OK"))
}

// handleWebhookStatus handles status requests (webhook-style endpoint)
func (s *Server) handleWebhookStatus(wr http.ResponseWriter, r *http.Request) {
	wr.Header().Set("Content-Type", "application/json")
	wr.WriteHeader(http.StatusOK)

	targets := s.engine.GetTargetStatus()
	status := map[string]any{
		"timestamp": time.Now(),
		"service":   "quick_watch",
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

	json.NewEncoder(wr).Encode(status)
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

// handleTrigger handles webhook target trigger requests
func (s *Server) handleTrigger(w http.ResponseWriter, r *http.Request) {
	// Extract target name from path
	path := strings.TrimPrefix(r.URL.Path, "/api/trigger/")
	if path == "" {
		http.Error(w, "Target name required", http.StatusBadRequest)
		return
	}

	targetName := path

	// Get message and duration from request
	var message string
	var duration int

	if r.Method == "POST" {
		// Parse JSON body
		var requestData struct {
			Message  string `json:"message"`
			Duration int    `json:"duration"`
		}
		if err := json.NewDecoder(r.Body).Decode(&requestData); err == nil {
			message = requestData.Message
			duration = requestData.Duration
		}
	}

	// Also check query params (for GET requests or as fallback)
	if message == "" {
		message = r.URL.Query().Get("message")
	}
	if message == "" {
		message = r.FormValue("message")
	}
	if message == "" {
		message = "Webhook triggered"
	}

	if duration == 0 {
		if d := r.URL.Query().Get("duration"); d != "" {
			if parsed, err := strconv.Atoi(d); err == nil {
				duration = parsed
			}
		}
	}
	if duration == 0 {
		if d := r.FormValue("duration"); d != "" {
			if parsed, err := strconv.Atoi(d); err == nil {
				duration = parsed
			}
		}
	}

	// Trigger the webhook target
	state, err := s.engine.TriggerWebhookTarget(targetName, message, duration)
	if err != nil {
		log.Printf("Error triggering webhook target %s: %v", targetName, err)
		http.Error(w, fmt.Sprintf("Failed to trigger target: %v", err), http.StatusBadRequest)
		return
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]any{
		"status":  "triggered",
		"target":  state.Target.Name,
		"message": message,
	}

	if state.RecoveryTime != nil {
		response["recovery_time"] = state.RecoveryTime.Format(time.RFC3339)
		response["duration_seconds"] = duration
	}

	if state.CurrentAckToken != "" && s.engine.acksEnabled {
		response["acknowledgement_url"] = s.engine.GetAcknowledgementURL(state.CurrentAckToken)
	}

	json.NewEncoder(w).Encode(response)

	log.Printf("✅ Webhook target '%s' triggered: %s", targetName, message)
}

// handleAcknowledge handles alert acknowledgement requests
// GET: Immediately acknowledges and shows contact form
// POST: Updates acknowledgement info and sends notifications
func (s *Server) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	// Extract token from path
	path := strings.TrimPrefix(r.URL.Path, "/api/acknowledge/")
	if path == "" {
		http.Error(w, "Token required", http.StatusBadRequest)
		return
	}

	token := path

	// Check if this is a target alert or hook by looking up the token
	s.engine.ackMutex.RLock()
	state, isTargetToken := s.engine.ackTokenMap[token]
	var hookState *HookState
	var isHook bool
	if !isTargetToken {
		hookState, isHook = s.engine.hookAckTokenMap[token]
	}
	s.engine.ackMutex.RUnlock()

	if !isTargetToken && !isHook {
		log.Printf("Error: Token not found: %s", token)
		http.Error(w, "Invalid or expired acknowledgement token", http.StatusBadRequest)
		return
	}

	// Handle POST request (form submission)
	if r.Method == "POST" {
		// Parse form data
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		acknowledgedBy := r.FormValue("name")
		if acknowledgedBy == "" {
			acknowledgedBy = "Anonymous"
		}
		note := r.FormValue("notes")
		contact := r.FormValue("contact")

		if isTargetToken {
			// Update target acknowledgement
			_, err := s.engine.AcknowledgeAlert(token, acknowledgedBy, note, contact)
			if err != nil {
				log.Printf("Error updating target acknowledgement: %v", err)
				http.Error(w, "Failed to update acknowledgement", http.StatusInternalServerError)
				return
			}

			// Send updated notifications to all strategies
			for _, strat := range state.AlertStrategies {
				if ackStrat, ok := strat.(AcknowledgementAwareAlert); ok {
					if err := ackStrat.SendAcknowledgement(r.Context(), state.Target, acknowledgedBy, note, contact); err != nil {
						log.Printf("Failed to send acknowledgement notification via %s: %v", strat.Name(), err)
					}
				}
			}

			// Show success message
			s.showAcknowledgementSuccess(w, state.Target.Name, state.Target.URL, acknowledgedBy, note, contact, false)
		} else {
			// Update hook acknowledgement
			s.engine.ackMutex.Lock()
			hookState.AcknowledgedBy = acknowledgedBy
			hookState.AcknowledgementNote = note
			hookState.AcknowledgementContact = contact
			s.engine.ackMutex.Unlock()

			// Send acknowledgement notification to all notification strategies
			hooks := s.stateManager.ListHooks()
			if hook, exists := hooks[hookState.HookName]; exists {
				for _, alertName := range hook.Alerts {
					if strat, exists := s.engine.notificationStrategies[alertName]; exists {
						if ackStrat, ok := strat.(AcknowledgementAwareNotification); ok {
							if err := ackStrat.SendNotificationAcknowledgement(r.Context(), hookState.HookName, acknowledgedBy, note, contact); err != nil {
								log.Printf("Failed to send hook acknowledgement notification via %s: %v", alertName, err)
							}
						}
					}
				}
			}

			// Show success message
			s.showAcknowledgementSuccess(w, hookState.HookName, hookState.Message, acknowledgedBy, note, contact, true)
		}
		return
	}

	// Handle GET request - immediately acknowledge and show form
	if isTargetToken {
		// Acknowledge target alert if not already acknowledged
		if state.AcknowledgedAt == nil {
			_, err := s.engine.AcknowledgeAlert(token, "Pending", "", "")
			if err != nil {
				log.Printf("Error acknowledging target alert: %v", err)
				http.Error(w, "Failed to acknowledge alert", http.StatusInternalServerError)
				return
			}
		}

		// Show contact form
		s.showAcknowledgementForm(w, token, state.Target.Name, state.Target.URL, false, state.AcknowledgedBy, state.AcknowledgementNote, state.AcknowledgementContact)
	} else {
		// Acknowledge hook if not already acknowledged
		if hookState.AcknowledgedAt == nil {
			s.engine.ackMutex.Lock()
			now := time.Now()
			hookState.AcknowledgedAt = &now
			hookState.AcknowledgedBy = "Pending"
			s.engine.ackMutex.Unlock()
		}

		// Show contact form
		s.showAcknowledgementForm(w, token, hookState.HookName, hookState.Message, true, hookState.AcknowledgedBy, hookState.AcknowledgementNote, hookState.AcknowledgementContact)
	}

}

// showAcknowledgementForm displays the interactive acknowledgement form
func (s *Server) showAcknowledgementForm(w http.ResponseWriter, token, name, urlOrMessage string, isHook bool, existingName, existingNote, existingContact string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// Pre-fill form if user already submitted once
	if existingName == "Pending" {
		existingName = ""
	}

	title := "Alert Acknowledged"
	itemLabel := "Target"
	if isHook {
		title = "Notification Acknowledged"
		itemLabel = "Hook"
	}

	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>%s</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            max-width: 700px;
            margin: 50px auto;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            background: white;
            border-radius: 12px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(135deg, #4CAF50 0%%, #45a049 100%%);
            color: white;
            padding: 30px;
            text-align: center;
        }
        .header .icon {
            font-size: 64px;
            margin-bottom: 10px;
        }
        .header h1 {
            margin: 0;
            font-size: 28px;
            font-weight: 600;
        }
        .content {
            padding: 30px;
        }
        .alert-info {
            background-color: #e3f2fd;
            border-left: 4px solid #2196F3;
            padding: 15px;
            margin-bottom: 25px;
            border-radius: 4px;
        }
        .alert-info p {
            margin: 5px 0;
            color: #1565c0;
        }
        .alert-info strong {
            color: #0d47a1;
        }
        .form-section {
            margin-bottom: 30px;
        }
        .form-section h2 {
            color: #333;
            font-size: 20px;
            margin-bottom: 15px;
            border-bottom: 2px solid #4CAF50;
            padding-bottom: 10px;
        }
        .form-group {
            margin-bottom: 20px;
        }
        label {
            display: block;
            margin-bottom: 8px;
            color: #555;
            font-weight: 500;
            font-size: 14px;
        }
        input[type="text"],
        textarea {
            width: 100%%;
            padding: 12px;
            border: 2px solid #ddd;
            border-radius: 6px;
            font-size: 14px;
            font-family: inherit;
            box-sizing: border-box;
            transition: border-color 0.3s;
        }
        input[type="text"]:focus,
        textarea:focus {
            outline: none;
            border-color: #4CAF50;
        }
        textarea {
            resize: vertical;
            min-height: 100px;
        }
        .helper-text {
            font-size: 12px;
            color: #777;
            margin-top: 5px;
        }
        .submit-btn {
            background: linear-gradient(135deg, #4CAF50 0%%, #45a049 100%%);
            color: white;
            border: none;
            padding: 14px 32px;
            font-size: 16px;
            font-weight: 600;
            border-radius: 6px;
            cursor: pointer;
            width: 100%%;
            transition: transform 0.2s, box-shadow 0.2s;
        }
        .submit-btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 4px 12px rgba(76, 175, 80, 0.3);
        }
        .submit-btn:active {
            transform: translateY(0);
        }
        .success-message {
            background-color: #f1f8e9;
            border: 2px solid #8bc34a;
            color: #33691e;
            padding: 15px;
            border-radius: 6px;
            margin-bottom: 20px;
            text-align: center;
            font-weight: 500;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div class="icon">✅</div>
            <h1>%s</h1>
            <p style="margin: 10px 0 0 0; opacity: 0.9;">Alert has been acknowledged. Please provide your contact information.</p>
        </div>
        <div class="content">
            <div class="alert-info">
                <p><strong>%s:</strong> %s</p>
                <p><strong>%s:</strong> %s</p>
            </div>
            
            <div class="form-section">
                <h2>👤 Contact Information</h2>
                <p style="color: #666; margin-bottom: 20px;">Help your team reach you if they need assistance with this issue.</p>
                
                <form method="POST" action="/api/acknowledge/%s">
                    <div class="form-group">
                        <label for="name">Your Name *</label>
                        <input type="text" id="name" name="name" required 
                               placeholder="e.g., John Doe" value="%s">
                        <div class="helper-text">Who's handling this issue?</div>
                    </div>
                    
                    <div class="form-group">
                        <label for="contact">Contact Me Here *</label>
                        <input type="text" id="contact" name="contact" required 
                               placeholder="e.g., Slack: @john, Zoom: https://zoom.us/j/123, Phone: +1-555-0100" value="%s">
                        <div class="helper-text">How can others reach you? (Slack channel, Zoom link, phone number, email, etc.)</div>
                    </div>
                    
                    <div class="form-group">
                        <label for="notes">Notes</label>
                        <textarea id="notes" name="notes" 
                                  placeholder="e.g., Investigating database connection issues. Will update in #incidents channel.">%s</textarea>
                        <div class="helper-text">Optional: Add any relevant notes about your investigation</div>
                    </div>
                    
                    <button type="submit" class="submit-btn">
                        📤 Share Contact Info &amp; Update Team
                    </button>
                </form>
            </div>
        </div>
    </div>
</body>
</html>`, title, title, itemLabel, name,
		func() string {
			if isHook {
				return "Message"
			} else {
				return "URL"
			}
		}(),
		urlOrMessage, token, existingName, existingContact, existingNote)

	w.Write([]byte(html))
}

// showAcknowledgementSuccess displays the success message after form submission
func (s *Server) showAcknowledgementSuccess(w http.ResponseWriter, name, urlOrMessage, acknowledgedBy, note, contact string, isHook bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	title := "Alert Acknowledged"
	itemLabel := "Target"
	if isHook {
		title = "Notification Acknowledged"
		itemLabel = "Hook"
	}

	contactSection := ""
	if contact != "" {
		contactSection = fmt.Sprintf("<p><strong>Contact:</strong> %s</p>", contact)
	}
	noteSection := ""
	if note != "" {
		noteSection = fmt.Sprintf("<p><strong>Notes:</strong> %s</p>", note)
	}

	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>%s</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            max-width: 600px;
            margin: 50px auto;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            background: white;
            border-radius: 12px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(135deg, #4CAF50 0%%, #45a049 100%%);
            color: white;
            padding: 40px;
            text-align: center;
        }
        .header .icon {
            font-size: 72px;
            margin-bottom: 15px;
            animation: checkmark 0.5s ease-in-out;
        }
        @keyframes checkmark {
            0%% { transform: scale(0); }
            50%% { transform: scale(1.2); }
            100%% { transform: scale(1); }
        }
        .header h1 {
            margin: 0;
            font-size: 32px;
            font-weight: 600;
        }
        .content {
            padding: 30px;
        }
        .details {
            background-color: #f5f5f5;
            padding: 20px;
            border-radius: 8px;
            margin-top: 20px;
        }
        .details p {
            margin: 10px 0;
            color: #333;
            line-height: 1.6;
        }
        .details strong {
            color: #1976d2;
            display: inline-block;
            min-width: 140px;
        }
        .success-badge {
            background-color: #e8f5e9;
            color: #2e7d32;
            padding: 12px 20px;
            border-radius: 6px;
            text-align: center;
            font-weight: 600;
            margin: 20px 0;
            border-left: 4px solid #4caf50;
        }
        .footer {
            text-align: center;
            margin-top: 30px;
            padding-top: 20px;
            border-top: 1px solid #e0e0e0;
            color: #777;
            font-size: 14px;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div class="icon">✅</div>
            <h1>Information Shared!</h1>
            <p style="margin: 15px 0 0 0; opacity: 0.9; font-size: 16px;">Your team has been notified with your contact information.</p>
        </div>
        <div class="content">
            <div class="success-badge">
                📢 Team members have been notified via all configured alert channels
            </div>
            
            <div class="details">
                <h3 style="margin-top: 0; color: #333;">Details:</h3>
                <p><strong>%s:</strong> %s</p>
                <p><strong>%s:</strong> %s</p>
                <p><strong>Acknowledged By:</strong> %s</p>
                <p><strong>Time:</strong> %s</p>
                %s
                %s
            </div>
            
            <div class="footer">
                <p>✓ All configured alert strategies have been updated</p>
                <p><small>You can close this window now</small></p>
            </div>
        </div>
    </div>
</body>
</html>`, title, itemLabel, name,
		func() string {
			if isHook {
				return "Message"
			} else {
				return "URL"
			}
		}(),
		urlOrMessage, acknowledgedBy, time.Now().Format("2006-01-02 15:04:05 MST"),
		contactSection, noteSection)

	w.Write([]byte(html))
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
			} else if console, ok := alertStrategy.(*ConsoleAlertStrategy); ok {
				// For console alerts, print a stylized startup line
				console.SendStartupMessage(version, targetCount)
			} else if email, ok := alertStrategy.(*EmailAlertStrategy); ok {
				// For email alerts, send startup email
				if err := email.SendStartupMessage(ctx, version, targetCount); err != nil {
					log.Printf("Failed to send startup message to %s: %v", alertName, err)
				} else {
					log.Printf("Startup message sent to %s successfully", alertName)
				}
			} else if file, ok := alertStrategy.(*FileAlertStrategy); ok {
				// For file alerts, write startup log
				if err := file.SendStartupMessage(ctx, version, targetCount); err != nil {
					log.Printf("Failed to send startup message to %s: %v", alertName, err)
				} else {
					log.Printf("Startup message sent to %s successfully", alertName)
				}
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

// startStatusReportTicker starts a ticker to send periodic status reports
func (s *Server) startStatusReportTicker(ctx context.Context, config StatusReportConfig) {
	interval := config.Interval
	if interval <= 0 {
		interval = 60 // default to 60 minutes
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Minute)

	log.Printf("📊 Status reports enabled: sending every %d minutes to %v", interval, config.Alerts)
	log.Printf("   Manual trigger: POST %s/trigger/status_report", s.engine.serverAddress)

	go func() {
		for {
			select {
			case <-ticker.C:
				s.sendStatusReport(ctx, config.Alerts)
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
}

// sendStatusReport generates and sends a status report
func (s *Server) sendStatusReport(ctx context.Context, alertNames []string) {
	// Generate the report
	report := s.engine.GenerateStatusReport()

	log.Printf("📊 Sending status report: %d active, %d resolved, %d alerts, %d notifications",
		len(report.ActiveOutages), len(report.ResolvedOutages), report.AlertsSent, report.NotificationsSent)

	// Send to each configured alert strategy
	for _, alertName := range alertNames {
		if strategy, exists := s.engine.alertStrategies[alertName]; exists {
			if err := strategy.SendStatusReport(ctx, report); err != nil {
				log.Printf("Failed to send status report to %s: %v", alertName, err)
			}
		} else {
			log.Printf("Warning: Alert strategy '%s' not found for status report", alertName)
		}
	}
}

// handleTriggerStatusReport handles manual status report trigger requests
func (s *Server) handleTriggerStatusReport(w http.ResponseWriter, r *http.Request) {
	// Accept both GET and POST
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed. Use GET or POST to trigger a status report.", http.StatusMethodNotAllowed)
		return
	}

	settings := s.stateManager.GetSettings()

	// Check if status reports are configured
	if !settings.StatusReport.Enabled {
		if r.Method == http.MethodGet {
			s.showStatusReportError(w, "Status reports are not enabled in settings")
		} else {
			http.Error(w, "Status reports are not enabled in settings", http.StatusServiceUnavailable)
		}
		return
	}

	if len(settings.StatusReport.Alerts) == 0 {
		if r.Method == http.MethodGet {
			s.showStatusReportError(w, "No alert strategies configured for status reports")
		} else {
			http.Error(w, "No alert strategies configured for status reports", http.StatusBadRequest)
		}
		return
	}

	// Generate and send the status report
	log.Printf("📊 Manual status report triggered via %s", r.Method)
	s.sendStatusReport(r.Context(), settings.StatusReport.Alerts)

	// Get a fresh report for the response (the previous one was consumed)
	// We'll generate summary data from the current state
	activeCount := 0
	for _, state := range s.engine.targets {
		if state.IsDown {
			activeCount++
		}
	}

	// Return HTML for GET, JSON for POST
	if r.Method == http.MethodGet {
		s.showStatusReportSuccess(w, activeCount, settings.StatusReport.Alerts)
	} else {
		response := map[string]any{
			"status":  "success",
			"message": "Status report generated and sent",
			"summary": map[string]any{
				"active_outages": activeCount,
				"sent_to":        settings.StatusReport.Alerts,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}

	log.Printf("✅ Status report webhook completed successfully")
}

// showStatusReportSuccess displays HTML success page for status report trigger
func (s *Server) showStatusReportSuccess(w http.ResponseWriter, activeOutages int, sentTo []string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	alertsList := ""
	for _, alert := range sentTo {
		alertsList += fmt.Sprintf("<li>%s</li>", alert)
	}

	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Status Report Triggered</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            max-width: 600px;
            margin: 50px auto;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            background: white;
            border-radius: 12px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(135deg, #1976d2 0%%, #1565c0 100%%);
            color: white;
            padding: 40px;
            text-align: center;
        }
        .header .icon {
            font-size: 72px;
            margin-bottom: 15px;
            animation: pulse 0.5s ease-in-out;
        }
        @keyframes pulse {
            0%% { transform: scale(0.9); }
            50%% { transform: scale(1.1); }
            100%% { transform: scale(1); }
        }
        .header h1 {
            margin: 0;
            font-size: 32px;
            font-weight: 600;
        }
        .content {
            padding: 30px;
        }
        .success-badge {
            background-color: #e8f5e9;
            color: #2e7d32;
            padding: 12px 20px;
            border-radius: 6px;
            text-align: center;
            font-weight: 600;
            margin-bottom: 20px;
            border-left: 4px solid #4caf50;
        }
        .details {
            background-color: #f5f5f5;
            padding: 20px;
            border-radius: 8px;
            margin-top: 20px;
        }
        .details h3 {
            margin-top: 0;
            color: #333;
        }
        .details p {
            margin: 10px 0;
            color: #555;
        }
        .details ul {
            list-style: none;
            padding-left: 0;
        }
        .details li {
            padding: 8px;
            background: white;
            margin: 5px 0;
            border-radius: 4px;
            border-left: 3px solid #1976d2;
        }
        .footer {
            text-align: center;
            margin-top: 30px;
            padding-top: 20px;
            border-top: 1px solid #e0e0e0;
            color: #777;
            font-size: 14px;
        }
        .back-button {
            display: inline-block;
            margin-top: 20px;
            padding: 10px 20px;
            background-color: #1976d2;
            color: white;
            text-decoration: none;
            border-radius: 5px;
            transition: background-color 0.3s;
        }
        .back-button:hover {
            background-color: #1565c0;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div class="icon">📊</div>
            <h1>Status Report Triggered!</h1>
            <p style="margin: 15px 0 0 0; opacity: 0.9; font-size: 16px;">Report has been generated and distributed</p>
        </div>
        <div class="content">
            <div class="success-badge">
                ✅ Status report successfully sent to all configured alert strategies
            </div>
            
            <div class="details">
                <h3>Report Summary</h3>
                <p><strong>Active Outages:</strong> %d</p>
                <p><strong>Sent to:</strong></p>
                <ul>%s</ul>
                <p><strong>Triggered at:</strong> %s</p>
            </div>
            
            <div class="footer">
                <p>The report has been distributed to all configured alert strategies.</p>
                <p>Check your console, Slack, email, or other configured destinations for the full report.</p>
                <a href="/" class="back-button">← Back to Home</a>
            </div>
        </div>
    </div>
</body>
</html>`, activeOutages, alertsList, time.Now().Format("2006-01-02 15:04:05 MST"))

	w.Write([]byte(html))
}

// showStatusReportError displays HTML error page for status report trigger
func (s *Server) showStatusReportError(w http.ResponseWriter, errorMessage string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)

	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Status Report Error</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            max-width: 600px;
            margin: 50px auto;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            background: white;
            border-radius: 12px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(135deg, #d32f2f 0%%, #c62828 100%%);
            color: white;
            padding: 40px;
            text-align: center;
        }
        .header .icon {
            font-size: 72px;
            margin-bottom: 15px;
        }
        .header h1 {
            margin: 0;
            font-size: 32px;
            font-weight: 600;
        }
        .content {
            padding: 30px;
        }
        .error-message {
            background-color: #ffebee;
            color: #c62828;
            padding: 15px;
            border-radius: 6px;
            border-left: 4px solid #d32f2f;
            margin-bottom: 20px;
        }
        .help {
            background-color: #e3f2fd;
            padding: 15px;
            border-radius: 6px;
            border-left: 4px solid #1976d2;
        }
        .help h3 {
            margin-top: 0;
            color: #1565c0;
        }
        .back-button {
            display: inline-block;
            margin-top: 20px;
            padding: 10px 20px;
            background-color: #1976d2;
            color: white;
            text-decoration: none;
            border-radius: 5px;
            transition: background-color 0.3s;
        }
        .back-button:hover {
            background-color: #1565c0;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div class="icon">⚠️</div>
            <h1>Cannot Generate Report</h1>
        </div>
        <div class="content">
            <div class="error-message">
                <strong>Error:</strong> %s
            </div>
            
            <div class="help">
                <h3>How to fix this:</h3>
                <ol>
                    <li>Enable status reports in your configuration</li>
                    <li>Configure at least one alert strategy</li>
                    <li>Restart Quick Watch</li>
                </ol>
                <p><strong>Example configuration:</strong></p>
                <pre style="background: white; padding: 10px; border-radius: 4px; overflow-x: auto;">settings:
  status_report:
    enabled: true
    interval: 60
    alerts: ["console", "slack"]</pre>
            </div>
            
            <a href="/" class="back-button">← Back to Home</a>
        </div>
    </div>
</body>
</html>`, errorMessage)

	w.Write([]byte(html))
}

// handleTargetList handles the /targets endpoint - shows all targets
func (s *Server) handleTargetList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	targets := s.engine.GetTargetStatus()

	// Sort targets: unhealthy first, then healthy
	sortedTargets := make([]*TargetState, len(targets))
	copy(sortedTargets, targets)

	// Separate into two groups
	var unhealthy []*TargetState
	var healthy []*TargetState

	for _, state := range sortedTargets {
		if state.IsDown {
			unhealthy = append(unhealthy, state)
		} else {
			healthy = append(healthy, state)
		}
	}

	// Combine: unhealthy first
	sortedTargets = append(unhealthy, healthy...)

	// Build target cards
	targetCards := ""
	for _, state := range sortedTargets {
		urlSafeName := state.GetURLSafeName()
		statusClass := "healthy"
		statusIcon := "✅"
		statusText := "Healthy"

		if state.IsDown {
			statusClass = "down"
			statusIcon = "❌"
			statusText = "Down"
			if state.AcknowledgedAt != nil {
				statusIcon = "🔔"
				statusText = "Down (Acknowledged)"
			}
		}

		downtime := ""
		if state.DownSince != nil {
			duration := time.Since(*state.DownSince)
			downtime = fmt.Sprintf(`<div class="downtime">Down for: %s</div>`, formatDuration(duration))
		}

		lastCheck := "Never"
		responseTime := "N/A"
		if state.LastCheck != nil {
			lastCheck = state.LastCheck.Timestamp.Format("2006-01-02 15:04:05 MST")
			if state.LastCheck.ResponseTime > 0 {
				// Convert nanoseconds to seconds with 3 significant digits
				seconds := state.LastCheck.ResponseTime.Seconds()
				if seconds == 0 {
					responseTime = "0s"
				} else {
					// Use toPrecision equivalent in Go
					formatted := fmt.Sprintf("%.3g", seconds)
					responseTime = formatted + "s"
				}
			}
		}

		targetCards += fmt.Sprintf(`
			<a href="/targets/%s" class="target-card %s" data-target-name="%s" data-target-url="%s">
				<div class="target-header">
					<span class="status-icon">%s</span>
					<h3>%s</h3>
					<span class="status-badge %s">%s</span>
				</div>
				<div class="target-url">%s</div>
				%s
				<div class="target-meta">
					<div><strong>Last Check:</strong> %s</div>
					<div><strong>Response Time:</strong> %s</div>
				</div>
			</a>
		`, urlSafeName, statusClass, strings.ToLower(state.Target.Name), strings.ToLower(state.Target.URL), statusIcon, state.Target.Name, statusClass, statusText, state.Target.URL, downtime, lastCheck, responseTime)
	}

	emptyState := ""
	if len(targets) == 0 {
		emptyState = `<div class="empty-state"><h2>No targets configured</h2><p>Add targets to your configuration to start monitoring</p></div>`
	}

	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Quick Watch - Targets</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            background-color: #0d1117;
            color: #c9d1d9;
            line-height: 1.6;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            padding: 40px 20px;
        }
        header {
            margin-bottom: 30px;
        }
        h1 {
            font-size: 32px;
            color: #f0f6fc;
            margin-bottom: 10px;
        }
        .subtitle {
            color: #8b949e;
            font-size: 16px;
            margin-bottom: 20px;
        }
        .filter-container {
            margin-bottom: 20px;
            display: flex;
            gap: 10px;
            align-items: center;
        }
        .filter-input {
            flex: 1;
            max-width: 400px;
            padding: 10px 15px;
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 6px;
            color: #c9d1d9;
            font-size: 14px;
            outline: none;
        }
        .filter-input:focus {
            border-color: #58a6ff;
        }
        .clear-filter-btn {
            padding: 10px 20px;
            background: #21262d;
            border: 1px solid #30363d;
            border-radius: 6px;
            color: #c9d1d9;
            font-size: 14px;
            cursor: pointer;
            transition: all 0.2s;
        }
        .clear-filter-btn:hover {
            background: #30363d;
            border-color: #58a6ff;
        }
        .filter-count {
            color: #8b949e;
            font-size: 14px;
        }
        .target-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(350px, 1fr));
            gap: 20px;
        }
        .target-card {
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 6px;
            padding: 20px;
            text-decoration: none;
            color: inherit;
            transition: all 0.2s ease;
            display: block;
        }
        .target-card.hidden {
            display: none;
        }
        .target-card:hover {
            border-color: #58a6ff;
            transform: translateY(-2px);
            box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
        }
        .target-card.down {
            border-left: 4px solid #f85149;
        }
        .target-card.healthy {
            border-left: 4px solid #3fb950;
        }
        .target-header {
            display: flex;
            align-items: center;
            gap: 10px;
            margin-bottom: 12px;
        }
        .status-icon {
            font-size: 24px;
        }
        .target-header h3 {
            flex: 1;
            font-size: 18px;
            color: #f0f6fc;
        }
        .status-badge {
            padding: 4px 12px;
            border-radius: 12px;
            font-size: 12px;
            font-weight: 600;
        }
        .status-badge.healthy {
            background: rgba(63, 185, 80, 0.15);
            color: #3fb950;
        }
        .status-badge.down {
            background: rgba(248, 81, 73, 0.15);
            color: #f85149;
        }
        .target-url {
            color: #8b949e;
            font-size: 14px;
            margin-bottom: 12px;
            word-break: break-all;
        }
        .downtime {
            background: rgba(248, 81, 73, 0.1);
            padding: 8px 12px;
            border-radius: 4px;
            margin-bottom: 12px;
            color: #f85149;
            font-size: 14px;
        }
        .target-meta {
            display: flex;
            justify-content: space-between;
            font-size: 13px;
            color: #8b949e;
            padding-top: 12px;
            border-top: 1px solid #30363d;
        }
        .target-meta strong {
            color: #c9d1d9;
        }
        .empty-state {
            text-align: center;
            padding: 60px 20px;
            color: #8b949e;
        }
        .empty-state h2 {
            font-size: 24px;
            margin-bottom: 10px;
        }
        @media (max-width: 768px) {
            .target-grid {
                grid-template-columns: 1fr;
            }
        }
    </style>
    <script>
        // Filter functionality
        let filterTimeout;
        
        function filterTargets() {
            const filterValue = document.getElementById('filterInput').value.toLowerCase();
            const cards = document.querySelectorAll('.target-card');
            let visibleCount = 0;
            
            cards.forEach(card => {
                const name = card.getAttribute('data-target-name');
                const url = card.getAttribute('data-target-url');
                
                if (name.includes(filterValue) || url.includes(filterValue)) {
                    card.classList.remove('hidden');
                    visibleCount++;
                } else {
                    card.classList.add('hidden');
                }
            });
            
            // Update count
            const filterCount = document.getElementById('filterCount');
            if (filterValue) {
                filterCount.textContent = visibleCount + ' of ' + cards.length + ' targets';
                filterCount.style.display = 'inline';
            } else {
                filterCount.style.display = 'none';
            }
        }
        
        function clearFilter() {
            document.getElementById('filterInput').value = '';
            filterTargets();
            document.getElementById('filterInput').focus();
        }
        
        // Auto-refresh every 5 seconds (but don't reload if filtering)
        setTimeout(() => {
            const filterValue = document.getElementById('filterInput').value;
            if (!filterValue) {
                window.location.reload();
            } else {
                // If filtering, just refresh after clearing filter
                setTimeout(() => window.location.reload(), 5000);
            }
        }, 5000);
    </script>
</head>
<body>
    <div class="container">
        <header>
            <h1>🎯 Quick Watch Targets</h1>
            <p class="subtitle">Monitoring %d target(s)</p>
        </header>
        
        <div class="filter-container">
            <input 
                type="text" 
                id="filterInput" 
                class="filter-input" 
                placeholder="Filter targets by name or URL..." 
                oninput="filterTargets()"
                autocomplete="off"
            />
            <button class="clear-filter-btn" onclick="clearFilter()">Clear Filter</button>
            <span id="filterCount" class="filter-count" style="display: none;"></span>
        </div>
        
        <div class="target-grid">
            %s
        </div>
        
        %s
    </div>
</body>
</html>`, len(targets), targetCards, emptyState)

	w.Write([]byte(html))
}

// handleTargetDetail handles the /targets/{name} endpoint - shows individual target details
func (s *Server) handleTargetDetail(w http.ResponseWriter, r *http.Request) {
	// Extract target name from URL
	urlSafeName := strings.TrimPrefix(r.URL.Path, "/targets/")
	if urlSafeName == "" {
		http.Redirect(w, r, "/targets", http.StatusSeeOther)
		return
	}

	// Find target by URL-safe name
	state := s.engine.FindTargetByURLSafeName(urlSafeName)
	if state == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// Get check history
	history := state.GetCheckHistory()

	// Calculate statistics
	avgPageSize := 0.0
	p95ResponseTime := 0.0
	if len(history) > 0 {
		// Calculate average page size
		var totalSize int64
		validSizeCount := 0
		for _, entry := range history {
			if entry.Success && entry.ResponseSize > 0 {
				totalSize += entry.ResponseSize
				validSizeCount++
			}
		}
		if validSizeCount > 0 {
			avgPageSize = float64(totalSize) / float64(validSizeCount)
		}

		// Calculate p95 response time
		successfulTimes := []int64{}
		for _, entry := range history {
			if entry.Success {
				successfulTimes = append(successfulTimes, entry.ResponseTime)
			}
		}
		if len(successfulTimes) > 0 {
			// Sort times to find p95
			sortedTimes := make([]int64, len(successfulTimes))
			copy(sortedTimes, successfulTimes)
			for i := 0; i < len(sortedTimes); i++ {
				for j := i + 1; j < len(sortedTimes); j++ {
					if sortedTimes[i] > sortedTimes[j] {
						sortedTimes[i], sortedTimes[j] = sortedTimes[j], sortedTimes[i]
					}
				}
			}
			p95Index := int(float64(len(sortedTimes)) * 0.95)
			if p95Index >= len(sortedTimes) {
				p95Index = len(sortedTimes) - 1
			}
			p95ResponseTime = float64(sortedTimes[p95Index]) / 1000.0 // Convert to seconds
		}
	}

	// Format statistics for display
	statsHTML := ""
	if len(history) > 0 {
		avgSizeStr := "N/A"
		if avgPageSize > 0 {
			if avgPageSize < 1024 {
				avgSizeStr = fmt.Sprintf("%.0f bytes", avgPageSize)
			} else if avgPageSize < 1024*1024 {
				avgSizeStr = fmt.Sprintf("%.2f KB", avgPageSize/1024)
			} else {
				avgSizeStr = fmt.Sprintf("%.2f MB", avgPageSize/(1024*1024))
			}
		}

		p95Str := "N/A"
		if p95ResponseTime > 0 {
			p95Str = fmt.Sprintf("%.3g", p95ResponseTime) + "s"
		}

		statsHTML = fmt.Sprintf(`
		<div class="stats-container">
			<div class="stat-card">
				<div class="stat-label">Average Page Size</div>
				<div class="stat-value">%s</div>
			</div>
			<div class="stat-card">
				<div class="stat-label">P95 Response Time</div>
				<div class="stat-value">%s</div>
			</div>
			<div class="stat-card">
				<div class="stat-label">Total Checks</div>
				<div class="stat-value">%d</div>
			</div>
		</div>`, avgSizeStr, p95Str, len(history))
	}

	// Build chart data (last 100 entries)
	chartData := []map[string]any{}
	logEntries := ""

	historyLen := len(history)
	startIdx := 0
	if historyLen > 100 {
		startIdx = historyLen - 100
	}

	// Build chart data in chronological order (for proper graph display)
	for i := startIdx; i < historyLen; i++ {
		entry := history[i]
		chartData = append(chartData, map[string]any{
			"timestamp":    entry.Timestamp.Unix() * 1000, // milliseconds for Chart.js
			"success":      entry.Success,
			"responseTime": entry.ResponseTime,
		})
	}

	// Build log entries in reverse order (most recent at top)
	entryID := 0
	for i := historyLen - 1; i >= startIdx; i-- {
		entry := history[i]

		// Build log entry (most recent at top)
		statusIcon := "✅"
		statusClass := "success"
		if !entry.Success {
			statusIcon = "❌"
			statusClass = "error"
		}
		if entry.WasRecovered {
			statusIcon = "🔄"
			statusClass = "recovered"
		}
		if entry.WasAcked {
			statusIcon = "🔔"
		}

		statusText := ""
		if entry.Success {
			// Convert milliseconds to seconds with 3 significant digits
			seconds := float64(entry.ResponseTime) / 1000.0
			if seconds == 0 {
				statusText = "OK - 0s"
			} else {
				formatted := fmt.Sprintf("%.3g", seconds)
				statusText = fmt.Sprintf("OK - %ss", formatted)
			}
		} else {
			statusText = "FAILED"
		}

		details := ""
		if entry.StatusCode > 0 {
			details += fmt.Sprintf("HTTP %d ", entry.StatusCode)
		}
		if entry.ErrorMessage != "" {
			details += entry.ErrorMessage + " "
		}
		if entry.AlertSent {
			details += fmt.Sprintf("Alert #%d sent ", entry.AlertCount)
		}
		if entry.WasAcked {
			details += "Acknowledged "
		}
		if entry.WasRecovered {
			details += "Recovered"
		}

		// Build expanded details section (always present, hidden by default)
		entryID++
		expandedContent := ""

		// Add full details
		expandedLines := []string{}
		expandedLines = append(expandedLines, fmt.Sprintf("Timestamp: %s", entry.Timestamp.Format("2006-01-02 15:04:05 MST")))
		if entry.StatusCode > 0 {
			expandedLines = append(expandedLines, fmt.Sprintf("Status Code: %d", entry.StatusCode))
		}
		if entry.Success {
			seconds := float64(entry.ResponseTime) / 1000.0
			expandedLines = append(expandedLines, fmt.Sprintf("Response Time: %.3gs", seconds))
		}
		if entry.ResponseSize > 0 {
			sizeStr := ""
			if entry.ResponseSize < 1024 {
				sizeStr = fmt.Sprintf("%d bytes", entry.ResponseSize)
			} else if entry.ResponseSize < 1024*1024 {
				sizeStr = fmt.Sprintf("%.2f KB", float64(entry.ResponseSize)/1024)
			} else {
				sizeStr = fmt.Sprintf("%.2f MB", float64(entry.ResponseSize)/(1024*1024))
			}
			expandedLines = append(expandedLines, fmt.Sprintf("Response Size: %s", sizeStr))
		}
		if entry.ContentType != "" {
			expandedLines = append(expandedLines, fmt.Sprintf("Content-Type: %s", entry.ContentType))
		}
		if entry.ErrorMessage != "" {
			expandedLines = append(expandedLines, fmt.Sprintf("Error: %s", entry.ErrorMessage))
		}
		if entry.AlertSent {
			expandedLines = append(expandedLines, fmt.Sprintf("Alert Sent: Yes (Alert #%d)", entry.AlertCount))
		}
		if entry.WasAcked {
			expandedLines = append(expandedLines, "Acknowledged: Yes")
		}
		if entry.WasRecovered {
			expandedLines = append(expandedLines, "Status: Recovered")
		}

		// Add response body if present
		if entry.ResponseBody != "" {
			expandedLines = append(expandedLines, "")
			expandedLines = append(expandedLines, "Response Body:")
			// Escape response body for HTML display
			escapedBody := strings.ReplaceAll(entry.ResponseBody, "&", "&amp;")
			escapedBody = strings.ReplaceAll(escapedBody, "<", "&lt;")
			escapedBody = strings.ReplaceAll(escapedBody, ">", "&gt;")
			expandedContent = ""
			for _, line := range expandedLines {
				expandedContent += fmt.Sprintf("<div>%s</div>", line)
			}
			expandedContent += fmt.Sprintf("<pre>%s</pre>", escapedBody)
		} else {
			for _, line := range expandedLines {
				expandedContent += fmt.Sprintf("<div>%s</div>", line)
			}
		}

		// Create clickable log entry with expandable details
		logEntries += fmt.Sprintf(`<div class="log-entry-wrapper">
			<div class="log-entry %s" onclick="toggleEntry(%d)">
				<span class="log-expand">▶</span>
				<span class="log-timestamp">%s</span>
				<span class="log-icon">%s</span>
				<span class="log-status">%s</span>
				<span class="log-details">%s</span>
			</div>
			<div id="entry-%d" class="entry-expanded" style="display:none;">
				%s
			</div>
		</div>`, statusClass, entryID,
			entry.Timestamp.Format("15:04:05"),
			statusIcon,
			statusText,
			details,
			entryID,
			expandedContent)
	}

	// Convert chart data to JSON
	chartDataJSON, _ := json.Marshal(chartData)

	// Current status
	statusBadge := ""
	if state.IsDown {
		statusBadge = `<span class="status-badge down">❌ Down</span>`
		if state.AcknowledgedAt != nil {
			statusBadge = `<span class="status-badge acked">🔔 Acknowledged</span>`
		}
	} else {
		statusBadge = `<span class="status-badge healthy">✅ Healthy</span>`
	}

	noDataMsg := ""
	if len(logEntries) == 0 {
		noDataMsg = `<div class="no-data">No check history available yet. Checks run every 5 seconds.</div>`
	}

	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s - Quick Watch</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            background-color: #0d1117;
            color: #c9d1d9;
            line-height: 1.6;
        }
        .container {
            max-width: 1400px;
            margin: 0 auto;
            padding: 20px;
        }
        header {
            margin-bottom: 30px;
            display: flex;
            align-items: center;
            gap: 20px;
        }
        .back-button {
            color: #58a6ff;
            text-decoration: none;
            font-size: 24px;
        }
        .back-button:hover {
            color: #79c0ff;
        }
        h1 {
            font-size: 28px;
            color: #f0f6fc;
            flex: 1;
        }
        .status-badge {
            padding: 8px 16px;
            border-radius: 16px;
            font-size: 14px;
            font-weight: 600;
        }
        .status-badge.healthy {
            background: rgba(63, 185, 80, 0.15);
            color: #3fb950;
        }
        .status-badge.down {
            background: rgba(248, 81, 73, 0.15);
            color: #f85149;
        }
        .status-badge.acked {
            background: rgba(187, 128, 9, 0.15);
            color: #d29922;
        }
        .target-info {
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 6px;
            padding: 20px;
            margin-bottom: 20px;
        }
        .target-url {
            color: #8b949e;
            font-size: 14px;
            margin-bottom: 10px;
        }
        .chart-container {
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 6px;
            padding: 20px;
            margin-bottom: 20px;
            height: 400px;
        }
        .terminal-container {
            background: #0d1117;
            border: 1px solid #30363d;
            border-radius: 6px;
            overflow: hidden;
        }
        .terminal-header {
            background: #161b22;
            padding: 12px 20px;
            border-bottom: 1px solid #30363d;
            font-weight: 600;
            font-size: 14px;
        }
        .terminal-body {
            padding: 16px;
            font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', 'Consolas', monospace;
            font-size: 13px;
            max-height: 600px;
            overflow-y: auto;
            background: #0d1117;
        }
        .log-entry-wrapper {
            margin-bottom: 2px;
        }
        .log-entry {
            padding: 6px 8px;
            display: flex;
            gap: 12px;
            align-items: center;
            cursor: pointer;
            border-radius: 4px;
            transition: background-color 0.15s ease;
        }
        .log-entry:hover {
            background: #161b22;
        }
        .log-entry.success {
            color: #3fb950;
        }
        .log-entry.error {
            color: #f85149;
        }
        .log-entry.recovered {
            color: #79c0ff;
        }
        .log-expand {
            color: #8b949e;
            font-size: 10px;
            transition: transform 0.2s ease;
            width: 12px;
            display: inline-block;
        }
        .log-expand.expanded {
            transform: rotate(90deg);
        }
        .log-timestamp {
            color: #8b949e;
            font-size: 12px;
            min-width: 70px;
        }
        .log-icon {
            font-size: 16px;
        }
        .log-status {
            min-width: 100px;
            font-weight: 600;
        }
        .log-details {
            color: #8b949e;
            flex: 1;
        }
        .entry-expanded {
            background: #161b22;
            border-left: 3px solid #30363d;
            margin-left: 34px;
            padding: 12px 16px;
            font-size: 12px;
            color: #c9d1d9;
            line-height: 1.6;
            border-radius: 0 4px 4px 0;
        }
        .entry-expanded div {
            margin-bottom: 4px;
        }
        .entry-expanded pre {
            margin-top: 8px;
            background: #0d1117;
            border: 1px solid #30363d;
            border-radius: 4px;
            padding: 12px;
            overflow-x: auto;
            color: #79c0ff;
            white-space: pre-wrap;
            word-wrap: break-word;
        }
        .no-data {
            text-align: center;
            padding: 40px;
            color: #8b949e;
        }
        canvas {
            max-height: 360px;
        }
        .stats-container {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 16px;
            margin-bottom: 20px;
        }
        .stat-card {
            background: #161b22;
            border: 1px solid #30363d;
            border-radius: 6px;
            padding: 16px;
        }
        .stat-label {
            font-size: 12px;
            color: #8b949e;
            margin-bottom: 8px;
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }
        .stat-value {
            font-size: 24px;
            font-weight: 600;
            color: #f0f6fc;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <a href="/targets" class="back-button">←</a>
            <h1>%s</h1>
            %s
        </header>
        
        <div class="target-info">
            <div class="target-url">%s</div>
        </div>
        
        %s
        
        <div class="chart-container">
            <canvas id="responseChart"></canvas>
        </div>
        
        <div class="terminal-container">
            <div class="terminal-header">
                📋 Check History (showing last 100 checks)
            </div>
            <div class="terminal-body">
                %s
                %s
            </div>
        </div>
    </div>
    
    <script>
        const chartData = %s;
        
        // Format labels for display
        const labels = chartData.map(d => {
            const date = new Date(d.timestamp);
            return date.toLocaleTimeString('en-US', { 
                hour: '2-digit', 
                minute: '2-digit', 
                second: '2-digit',
                hour12: false 
            });
        });
        
        // Helper function to format seconds with up to 4 significant digits
        function formatSeconds(ms) {
            const seconds = ms / 1000;
            if (seconds === 0) return '0';
            
            // Determine precision based on magnitude
            if (seconds >= 1000) {
                return seconds.toPrecision(4);
            } else if (seconds >= 100) {
                return seconds.toPrecision(4);
            } else if (seconds >= 10) {
                return seconds.toPrecision(4);
            } else if (seconds >= 1) {
                return seconds.toPrecision(4);
            } else {
                return seconds.toPrecision(4);
            }
        }
        
        const ctx = document.getElementById('responseChart').getContext('2d');
        const chart = new Chart(ctx, {
            type: 'line',
            data: {
                labels: labels,
                datasets: [{
                    label: 'Response Time (s)',
                    data: chartData.map(d => d.success ? d.responseTime / 1000 : 0),
                    borderColor: '#3fb950',
                    backgroundColor: 'rgba(63, 185, 80, 0.1)',
                    borderWidth: 2,
                    tension: 0.4,
                    pointRadius: 2,
                    pointHoverRadius: 5,
                    spanGaps: false,
                    fill: true,
                    pointBackgroundColor: chartData.map(d => d.success ? '#3fb950' : '#f85149'),
                    pointBorderColor: chartData.map(d => d.success ? '#3fb950' : '#f85149'),
                    pointHoverBackgroundColor: chartData.map(d => d.success ? '#3fb950' : '#f85149'),
                    pointHoverBorderColor: chartData.map(d => d.success ? '#fff' : '#fff'),
                    segment: {
                        borderColor: ctx => {
                            const idx = ctx.p0DataIndex;
                            const p0Success = chartData[idx]?.success;
                            const p1Success = chartData[idx + 1]?.success;
                            // Draw red line segment if either point is a failure
                            if (!p0Success || !p1Success) {
                                return '#f85149';
                            }
                            return '#3fb950';
                        }
                    }
                }, {
                    label: 'Failed Checks',
                    data: chartData.map(d => !d.success ? 0 : null),
                    borderColor: '#f85149',
                    backgroundColor: '#f85149',
                    borderWidth: 0,
                    pointRadius: 6,
                    pointStyle: 'cross',
                    pointHoverRadius: 8,
                    showLine: false
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                animation: false,
                interaction: {
                    intersect: false,
                    mode: 'index'
                },
                plugins: {
                    legend: {
                        labels: {
                            color: '#c9d1d9',
                            font: {
                                size: 12
                            }
                        }
                    },
                    tooltip: {
                        backgroundColor: '#161b22',
                        borderColor: '#30363d',
                        borderWidth: 1,
                        titleColor: '#f0f6fc',
                        bodyColor: '#c9d1d9',
                        padding: 12,
                        displayColors: true,
                        callbacks: {
                            title: function(context) {
                                const idx = context[0].dataIndex;
                                const data = window.chartData || chartData;
                                const date = new Date(data[idx].timestamp);
                                return date.toLocaleString();
                            },
                            label: function(context) {
                                const idx = context.dataIndex;
                                const data = window.chartData || chartData;
                                const entry = data[idx];
                                let label = context.dataset.label || '';
                                if (label) {
                                    label += ': ';
                                }
                                if (entry.success) {
                                    label += formatSeconds(entry.responseTime) + 's';
                                } else {
                                    label += 'Failed (0s)';
                                }
                                return label;
                            }
                        }
                    }
                },
                scales: {
                    x: {
                        grid: {
                            color: '#30363d',
                            drawBorder: false
                        },
                        ticks: {
                            color: '#8b949e',
                            maxRotation: 45,
                            minRotation: 0,
                            maxTicksLimit: 10,
                            font: {
                                size: 11
                            }
                        }
                    },
                    y: {
                        beginAtZero: true,
                        grid: {
                            color: '#30363d',
                            drawBorder: false
                        },
                        ticks: {
                            color: '#8b949e',
                            font: {
                                size: 11
                            },
                            callback: function(value) {
                                // Format y-axis ticks with up to 4 significant digits
                                if (value === 0) return '0s';
                                const str = value.toPrecision(4);
                                // Remove trailing zeros after decimal
                                return parseFloat(str) + 's';
                            }
                        }
                    }
                }
            }
        });
        
        // Track expanded entries
        const expandedEntries = new Set();
        
        // Toggle entry expansion (like GitHub Actions)
        function toggleEntry(id) {
            const expandedDiv = document.getElementById('entry-' + id);
            const expandIcon = event.currentTarget.querySelector('.log-expand');
            
            if (expandedDiv && expandIcon) {
                if (expandedDiv.style.display === 'none') {
                    expandedDiv.style.display = 'block';
                    expandIcon.classList.add('expanded');
                    expandedEntries.add(id);
                } else {
                    expandedDiv.style.display = 'none';
                    expandIcon.classList.remove('expanded');
                    expandedEntries.delete(id);
                }
            }
        }
        
        // Auto-update data every 5 seconds without page reload
        async function updateData() {
            try {
                const response = await fetch(window.location.pathname.replace('/targets/', '/api/history/'));
                if (!response.ok) return;
                
                const data = await response.json();
                const history = data.history || [];
                
                // Update status badge
                const statusBadge = document.querySelector('.status-badge');
                if (statusBadge && data.target) {
                    if (data.target.is_down) {
                        statusBadge.className = 'status-badge down';
                        statusBadge.textContent = '❌ Down';
                    } else {
                        statusBadge.className = 'status-badge healthy';
                        statusBadge.textContent = '✅ Healthy';
                    }
                }
                
                // Calculate and update statistics
                updateStatistics(history);
                
                // Update chart
                updateChart(history);
                
                // Update log entries
                updateLogEntries(history);
                
            } catch (error) {
                console.error('Failed to update data:', error);
            }
        }
        
        function updateStatistics(history) {
            if (history.length === 0) return;
            
            // Calculate average page size
            let totalSize = 0;
            let validSizeCount = 0;
            for (const entry of history) {
                if (entry.Success && entry.ResponseSize > 0) {
                    totalSize += entry.ResponseSize;
                    validSizeCount++;
                }
            }
            const avgPageSize = validSizeCount > 0 ? totalSize / validSizeCount : 0;
            
            // Calculate p95 response time
            const successfulTimes = history.filter(e => e.Success).map(e => e.ResponseTime);
            successfulTimes.sort((a, b) => a - b);
            const p95Index = Math.floor(successfulTimes.length * 0.95);
            const p95ResponseTime = successfulTimes.length > 0 ? successfulTimes[Math.min(p95Index, successfulTimes.length - 1)] / 1000.0 : 0;
            
            // Update stat values
            const statCards = document.querySelectorAll('.stat-value');
            if (statCards.length >= 3) {
                // Average page size
                let avgSizeStr = 'N/A';
                if (avgPageSize > 0) {
                    if (avgPageSize < 1024) {
                        avgSizeStr = Math.floor(avgPageSize) + ' bytes';
                    } else if (avgPageSize < 1024 * 1024) {
                        avgSizeStr = (avgPageSize / 1024).toFixed(2) + ' KB';
                    } else {
                        avgSizeStr = (avgPageSize / (1024 * 1024)).toFixed(2) + ' MB';
                    }
                }
                statCards[0].textContent = avgSizeStr;
                
                // P95 response time
                const p95Str = p95ResponseTime > 0 ? parseFloat(p95ResponseTime.toPrecision(3)) + 's' : 'N/A';
                statCards[1].textContent = p95Str;
                
                // Total checks
                statCards[2].textContent = history.length.toString();
            }
        }
        
        function updateChart(history) {
            const last100 = history.slice(-100);
            const newData = last100.map(entry => ({
                timestamp: new Date(entry.Timestamp).getTime(),
                success: entry.Success,
                responseTime: entry.ResponseTime
            }));
            
            const newLabels = newData.map(d => {
                const date = new Date(d.timestamp);
                return date.toLocaleTimeString('en-US', { 
                    hour: '2-digit', 
                    minute: '2-digit', 
                    second: '2-digit',
                    hour12: false 
                });
            });
            
            chart.data.labels = newLabels;
            chart.data.datasets[0].data = newData.map(d => d.success ? d.responseTime / 1000 : 0);
            chart.data.datasets[0].pointBackgroundColor = newData.map(d => d.success ? '#3fb950' : '#f85149');
            chart.data.datasets[0].pointBorderColor = newData.map(d => d.success ? '#3fb950' : '#f85149');
            chart.data.datasets[0].pointHoverBackgroundColor = newData.map(d => d.success ? '#3fb950' : '#f85149');
            chart.data.datasets[0].segment = {
                borderColor: ctx => {
                    const idx = ctx.p0DataIndex;
                    const p0Success = newData[idx]?.success;
                    const p1Success = newData[idx + 1]?.success;
                    if (!p0Success || !p1Success) return '#f85149';
                    return '#3fb950';
                }
            };
            chart.data.datasets[1].data = newData.map(d => !d.success ? 0 : null);
            
            // Store for tooltip callbacks
            window.chartData = newData;
            
            chart.update();
        }
        
        function updateLogEntries(history) {
            const terminalBody = document.querySelector('.terminal-body');
            if (!terminalBody) return;
            
            const last100 = history.slice(-100);
            let newHTML = '';
            
            // Iterate in reverse order to show most recent at top
            for (let i = last100.length - 1; i >= 0; i--) {
                const entry = last100[i];
                const entryID = i + 1;
                
                // Build log entry
                let statusIcon = '✅';
                let statusClass = 'success';
                if (!entry.Success) {
                    statusIcon = '❌';
                    statusClass = 'error';
                }
                if (entry.WasRecovered) {
                    statusIcon = '🔄';
                    statusClass = 'recovered';
                }
                if (entry.WasAcked) {
                    statusIcon = '🔔';
                }
                
                let statusText = '';
                if (entry.Success) {
                    const seconds = entry.ResponseTime / 1000.0;
                    if (seconds === 0) {
                        statusText = 'OK - 0s';
                    } else {
                        statusText = 'OK - ' + parseFloat(seconds.toPrecision(3)) + 's';
                    }
                } else {
                    statusText = 'FAILED';
                }
                
                let details = '';
                if (entry.StatusCode > 0) details += 'HTTP ' + entry.StatusCode + ' ';
                if (entry.ErrorMessage) details += entry.ErrorMessage + ' ';
                if (entry.AlertSent) details += 'Alert #' + entry.AlertCount + ' sent ';
                if (entry.WasAcked) details += 'Acknowledged ';
                if (entry.WasRecovered) details += 'Recovered';
                
                // Build expanded content
                let expandedLines = [];
                const timestamp = new Date(entry.Timestamp);
                expandedLines.push('Timestamp: ' + timestamp.toLocaleString() + ' ' + timestamp.toString().match(/\(([^)]+)\)$/)?.[1] || '');
                if (entry.StatusCode > 0) expandedLines.push('Status Code: ' + entry.StatusCode);
                if (entry.Success) {
                    const seconds = entry.ResponseTime / 1000.0;
                    expandedLines.push('Response Time: ' + parseFloat(seconds.toPrecision(3)) + 's');
                }
                if (entry.ResponseSize > 0) {
                    let sizeStr = '';
                    if (entry.ResponseSize < 1024) {
                        sizeStr = entry.ResponseSize + ' bytes';
                    } else if (entry.ResponseSize < 1024 * 1024) {
                        sizeStr = (entry.ResponseSize / 1024).toFixed(2) + ' KB';
                    } else {
                        sizeStr = (entry.ResponseSize / (1024 * 1024)).toFixed(2) + ' MB';
                    }
                    expandedLines.push('Response Size: ' + sizeStr);
                }
                if (entry.ContentType) expandedLines.push('Content-Type: ' + entry.ContentType);
                if (entry.ErrorMessage) expandedLines.push('Error: ' + entry.ErrorMessage);
                if (entry.AlertSent) expandedLines.push('Alert Sent: Yes (Alert #' + entry.AlertCount + ')');
                if (entry.WasAcked) expandedLines.push('Acknowledged: Yes');
                if (entry.WasRecovered) expandedLines.push('Status: Recovered');
                
                let expandedContent = '';
                if (entry.ResponseBody) {
                    expandedLines.push('');
                    expandedLines.push('Response Body:');
                    for (const line of expandedLines) {
                        expandedContent += '<div>' + escapeHtml(line) + '</div>';
                    }
                    expandedContent += '<pre>' + escapeHtml(entry.ResponseBody) + '</pre>';
                } else {
                    for (const line of expandedLines) {
                        expandedContent += '<div>' + escapeHtml(line) + '</div>';
                    }
                }
                
                const isExpanded = expandedEntries.has(entryID);
                const expandClass = isExpanded ? ' expanded' : '';
                const displayStyle = isExpanded ? 'block' : 'none';
                
                const entryTime = timestamp.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
                
                newHTML += '<div class="log-entry-wrapper">';
                newHTML += '<div class="log-entry ' + statusClass + '" onclick="toggleEntry(' + entryID + ')">';
                newHTML += '<span class="log-expand' + expandClass + '">▶</span>';
                newHTML += '<span class="log-timestamp">' + entryTime + '</span>';
                newHTML += '<span class="log-icon">' + statusIcon + '</span>';
                newHTML += '<span class="log-status">' + statusText + '</span>';
                newHTML += '<span class="log-details">' + escapeHtml(details) + '</span>';
                newHTML += '</div>';
                newHTML += '<div id="entry-' + entryID + '" class="entry-expanded" style="display:' + displayStyle + ';">';
                newHTML += expandedContent;
                newHTML += '</div>';
                newHTML += '</div>';
            }
            
            if (newHTML) {
                terminalBody.innerHTML = newHTML;
            }
        }
        
        function escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }
        
        // Make chartData global for tooltip callbacks
        window.chartData = chartData;
        
        // Start auto-update
        setInterval(updateData, 5000);
    </script>
</body>
</html>`, state.Target.Name, state.Target.Name, statusBadge, state.Target.URL, statsHTML, logEntries, noDataMsg, string(chartDataJSON))

	w.Write([]byte(html))
}

// handleTargetHistoryAPI handles the API endpoint for fetching target history as JSON
func (s *Server) handleTargetHistoryAPI(w http.ResponseWriter, r *http.Request) {
	// Extract target name from URL (format: /api/history/{name})
	urlSafeName := strings.TrimPrefix(r.URL.Path, "/api/history/")
	if urlSafeName == "" {
		http.Error(w, "Target name required", http.StatusBadRequest)
		return
	}

	// Find target by URL-safe name
	state := s.engine.FindTargetByURLSafeName(urlSafeName)
	if state == nil {
		http.Error(w, "Target not found", http.StatusNotFound)
		return
	}

	// Get check history
	history := state.GetCheckHistory()

	// Return as JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]any{
		"target": map[string]any{
			"name":     state.Target.Name,
			"url":      state.Target.URL,
			"is_down":  state.IsDown,
			"url_safe": state.GetURLSafeName(),
		},
		"history": history,
		"count":   len(history),
	}

	json.NewEncoder(w).Encode(response)
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}
