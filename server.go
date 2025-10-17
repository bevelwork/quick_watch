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
	serverAddress := fmt.Sprintf("http://localhost:%d", port)
	s.engine.SetAcknowledgementConfig(serverAddress, settings.AcknowledgementsEnabled)

	// Start targeting
	if err := s.engine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start targeting engine: %v", err)
	}

	// Send startup message if enabled and configured
	if settings.Startup.Enabled {
		s.sendStartupMessage(ctx)
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
	log.Printf("Webhook endpoint: http://0.0.0.0:%d%s", port, webhookPath)
	log.Printf("API endpoints: http://0.0.0.0:%d/api/*", port)
	log.Printf("Health check: http://0.0.0.0:%d/health", port)
	log.Printf("Status: http://0.0.0.0:%d/status", port)

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
			body := map[string]interface{}{}
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
						}
					} else {
						if err := strat.HandleNotification(r.Context(), notification); err != nil {
							log.Printf("Hook %s notify via %s failed: %v", h.Name, alertName, err)
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
	status := map[string]interface{}{
		"timestamp": time.Now(),
		"service":   "quick_watch",
		"targets":   make([]map[string]interface{}, len(targets)),
	}

	targetList := status["targets"].([]map[string]interface{})
	for i, state := range targets {
		targetList[i] = map[string]interface{}{
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

	targets := s.engine.GetTargetStatus()
	status := map[string]interface{}{
		"timestamp": time.Now(),
		"service":   "quick_watch",
		"state":     s.state,
		"targets":   make([]map[string]interface{}, len(targets)),
	}

	targetList := status["targets"].([]map[string]interface{})
	for i, state := range targets {
		targetList[i] = map[string]interface{}{
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
func (s *Server) handleListTargets(w http.ResponseWriter, r *http.Request) {
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

	response := map[string]interface{}{
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
func (s *Server) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	// Extract token from path
	path := strings.TrimPrefix(r.URL.Path, "/api/acknowledge/")
	if path == "" {
		http.Error(w, "Token required", http.StatusBadRequest)
		return
	}

	token := path

	// Get acknowledger info from query params or form
	acknowledgedBy := r.URL.Query().Get("by")
	if acknowledgedBy == "" {
		acknowledgedBy = r.FormValue("by")
	}
	if acknowledgedBy == "" {
		acknowledgedBy = "Anonymous"
	}

	note := r.URL.Query().Get("note")
	if note == "" {
		note = r.FormValue("note")
	}

	// Check if this is a target alert or hook by looking up the token
	var isHook bool
	var alreadyAcknowledged bool
	var targetName, targetURL, hookMessage string
	var previouslyAcknowledgedBy string
	var previouslyAcknowledgedAt time.Time
	
	// First check if it's a target alert token
	s.engine.ackMutex.RLock()
	state, isTargetToken := s.engine.ackTokenMap[token]
	s.engine.ackMutex.RUnlock()
	
	if isTargetToken {
		// It's a target alert
		isHook = false
		targetName = state.Target.Name
		targetURL = state.Target.URL
		
		// Check if already acknowledged
		if state.AcknowledgedAt != nil {
			alreadyAcknowledged = true
			previouslyAcknowledgedBy = state.AcknowledgedBy
			previouslyAcknowledgedAt = *state.AcknowledgedAt
		} else {
			// First acknowledgement - acknowledge it
			_, err := s.engine.AcknowledgeAlert(token, acknowledgedBy, note)
			if err != nil {
				log.Printf("Error acknowledging target alert: %v", err)
				http.Error(w, "Failed to acknowledge alert", http.StatusInternalServerError)
				return
			}
			
			// Send notifications
			for _, strat := range state.AlertStrategies {
				if ackStrat, ok := strat.(AcknowledgementAwareAlert); ok {
					if err := ackStrat.SendAcknowledgement(r.Context(), state.Target, acknowledgedBy, note); err != nil {
						log.Printf("Failed to send acknowledgement notification via %s: %v", strat.Name(), err)
					}
				}
			}
		}
	} else {
		// Try as a hook token
		s.engine.ackMutex.Lock()
		hookState, exists := s.engine.hookAckTokenMap[token]
		if !exists {
			s.engine.ackMutex.Unlock()
			log.Printf("Error: Token not found: %s", token)
			http.Error(w, "Invalid or expired acknowledgement token", http.StatusBadRequest)
			return
		}
		
		isHook = true
		targetName = hookState.HookName
		hookMessage = hookState.Message
		
		// Check if already acknowledged
		if hookState.AcknowledgedAt != nil {
			alreadyAcknowledged = true
			previouslyAcknowledgedBy = hookState.AcknowledgedBy
			previouslyAcknowledgedAt = *hookState.AcknowledgedAt
			s.engine.ackMutex.Unlock()
		} else {
			// First acknowledgement - mark as acknowledged
			now := time.Now()
			hookState.AcknowledgedAt = &now
			hookState.AcknowledgedBy = acknowledgedBy
			hookState.AcknowledgementNote = note
			s.engine.ackMutex.Unlock()
			
			// Send acknowledgement notification to all notification strategies
			hooks := s.stateManager.ListHooks()
			if hook, exists := hooks[hookState.HookName]; exists {
				for _, alertName := range hook.Alerts {
					if strat, exists := s.engine.notificationStrategies[alertName]; exists {
						if ackStrat, ok := strat.(AcknowledgementAwareNotification); ok {
							if err := ackStrat.SendNotificationAcknowledgement(r.Context(), hookState.HookName, acknowledgedBy, note); err != nil {
								log.Printf("Failed to send hook acknowledgement notification via %s: %v", alertName, err)
							}
						}
					}
				}
			}
		}
	}

	// Return a nice HTML response for browser users
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	var html string
	
	if alreadyAcknowledged {
		// Already acknowledged - show info page
		if isHook {
			html = fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Already Acknowledged</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 600px;
            margin: 50px auto;
            padding: 20px;
            text-align: center;
        }
        .info {
            background-color: #2196F3;
            color: white;
            padding: 20px;
            border-radius: 10px;
            margin-bottom: 20px;
        }
        .details {
            background-color: #f5f5f5;
            padding: 20px;
            border-radius: 5px;
            text-align: left;
        }
        .icon {
            font-size: 48px;
            margin-bottom: 10px;
        }
    </style>
</head>
<body>
    <div class="info">
        <div class="icon">ℹ️</div>
        <h1>Already Acknowledged</h1>
        <p>This notification has already been acknowledged.</p>
    </div>
    <div class="details">
        <h3>Details:</h3>
        <p><strong>Hook:</strong> %s</p>
        <p><strong>Message:</strong> %s</p>
        <p><strong>Previously acknowledged by:</strong> %s</p>
        <p><strong>Acknowledged at:</strong> %s</p>
    </div>
    <p><small>You can close this window now.</small></p>
</body>
</html>`, targetName, hookMessage, previouslyAcknowledgedBy, previouslyAcknowledgedAt.Format("2006-01-02 15:04:05"))
		} else {
			html = fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Already Acknowledged</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 600px;
            margin: 50px auto;
            padding: 20px;
            text-align: center;
        }
        .info {
            background-color: #2196F3;
            color: white;
            padding: 20px;
            border-radius: 10px;
            margin-bottom: 20px;
        }
        .details {
            background-color: #f5f5f5;
            padding: 20px;
            border-radius: 5px;
            text-align: left;
        }
        .icon {
            font-size: 48px;
            margin-bottom: 10px;
        }
    </style>
</head>
<body>
    <div class="info">
        <div class="icon">ℹ️</div>
        <h1>Already Acknowledged</h1>
        <p>This alert has already been acknowledged.</p>
    </div>
    <div class="details">
        <h3>Details:</h3>
        <p><strong>Target:</strong> %s</p>
        <p><strong>URL:</strong> %s</p>
        <p><strong>Previously acknowledged by:</strong> %s</p>
        <p><strong>Acknowledged at:</strong> %s</p>
    </div>
    <p><small>You can close this window now.</small></p>
</body>
</html>`, targetName, targetURL, previouslyAcknowledgedBy, previouslyAcknowledgedAt.Format("2006-01-02 15:04:05"))
		}
	} else if isHook {
		// Hook acknowledgement response
		html = fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Notification Acknowledged</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 600px;
            margin: 50px auto;
            padding: 20px;
            text-align: center;
        }
        .success {
            background-color: #4CAF50;
            color: white;
            padding: 20px;
            border-radius: 10px;
            margin-bottom: 20px;
        }
        .details {
            background-color: #f5f5f5;
            padding: 20px;
            border-radius: 5px;
            text-align: left;
        }
        .icon {
            font-size: 48px;
            margin-bottom: 10px;
        }
    </style>
</head>
<body>
    <div class="success">
        <div class="icon">✅</div>
        <h1>Notification Acknowledged</h1>
        <p>Thank you for acknowledging this notification!</p>
    </div>
    <div class="details">
        <h3>Details:</h3>
        <p><strong>Hook:</strong> %s</p>
        <p><strong>Message:</strong> %s</p>
        <p><strong>Acknowledged by:</strong> %s</p>
        <p><strong>Time:</strong> %s</p>
        %s
    </div>
    <p><small>You can close this window now.</small></p>
</body>
</html>`, targetName, hookMessage, acknowledgedBy, time.Now().Format("2006-01-02 15:04:05"),
			func() string {
				if note != "" {
					return fmt.Sprintf("<p><strong>Note:</strong> %s</p>", note)
				}
				return ""
			}())
	} else {
		// Target alert acknowledgement response
		html = fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Alert Acknowledged</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            max-width: 600px;
            margin: 50px auto;
            padding: 20px;
            text-align: center;
        }
        .success {
            background-color: #4CAF50;
            color: white;
            padding: 20px;
            border-radius: 10px;
            margin-bottom: 20px;
        }
        .details {
            background-color: #f5f5f5;
            padding: 20px;
            border-radius: 5px;
            text-align: left;
        }
        .icon {
            font-size: 48px;
            margin-bottom: 10px;
        }
    </style>
</head>
<body>
    <div class="success">
        <div class="icon">✅</div>
        <h1>Alert Acknowledged</h1>
        <p>Thank you for acknowledging this alert!</p>
    </div>
    <div class="details">
        <h3>Details:</h3>
        <p><strong>Target:</strong> %s</p>
        <p><strong>URL:</strong> %s</p>
        <p><strong>Acknowledged by:</strong> %s</p>
        <p><strong>Time:</strong> %s</p>
        %s
    </div>
    <p><small>You can close this window now.</small></p>
</body>
</html>`, targetName, targetURL, acknowledgedBy, time.Now().Format("2006-01-02 15:04:05"),
			func() string {
				if note != "" {
					return fmt.Sprintf("<p><strong>Note:</strong> %s</p>", note)
				}
				return ""
			}())
	}

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
