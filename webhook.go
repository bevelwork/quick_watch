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

// WebhookServer represents the webhook server
type WebhookServer struct {
	port   int
	path   string
	engine *TargetEngine
	server *http.Server
	mux    *http.ServeMux
	state  *StateManager
}

// NewWebhookServer creates a new webhook server
func NewWebhookServer(port int, path string, engine *TargetEngine, state *StateManager) *WebhookServer {
	return &WebhookServer{
		port:   port,
		path:   path,
		engine: engine,
		state:  state,
	}
}

// Start starts the webhook server
func (w *WebhookServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	w.mux = mux

	// Webhook endpoint
	mux.HandleFunc(w.path, w.handleWebhook)

	// Health check endpoint
	mux.HandleFunc("/health", w.handleHealth)

	// Status endpoint
	mux.HandleFunc("/status", w.handleStatus)

	// Dynamic hook routes
	w.registerHookRoutes()

	w.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", w.port),
		Handler: mux,
	}

	log.Printf("Starting webhook server on port %d", w.port)
	log.Printf("Webhook endpoint: http://0.0.0.0:%d%s", w.port, w.path)
	log.Printf("Health check: http://0.0.0.0:%d/health", w.port)
	log.Printf("Status: http://0.0.0.0:%d/status", w.port)

	// Start server in goroutine
	go func() {
		if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Webhook server error: %v", err)
		}
	}()

	return nil
}

// registerHookRoutes registers named hook routes from engine/state
func (w *WebhookServer) registerHookRoutes() {
	if w.state == nil {
		return
	}
	hooks := w.state.ListHooks()
	for name, hook := range hooks {
		// Always mount under /hooks/<name>
		routePath := "/hooks/" + name
		// Capture variables for handler closure
		h := hook
		w.mux.HandleFunc(routePath, func(wr http.ResponseWriter, r *http.Request) {
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

			// Dispatch to selected notification strategies
			if len(h.Alerts) == 0 {
				h.Alerts = []string{"console"}
			}
			for _, alertName := range h.Alerts {
				if strat, exists := w.engine.notificationStrategies[alertName]; exists {
					if err := strat.HandleNotification(r.Context(), notification); err != nil {
						log.Printf("Hook %s notify via %s failed: %v", h.Name, alertName, err)
					}
				}
			}

			wr.WriteHeader(http.StatusOK)
			wr.Write([]byte("OK"))
		})
		log.Printf("Hook route registered: %s -> alerts=%v", routePath, hook.Alerts)
	}
}

// Stop stops the webhook server
func (w *WebhookServer) Stop(ctx context.Context) error {
	if w.server != nil {
		return w.server.Shutdown(ctx)
	}
	return nil
}

// handleWebhook handles incoming webhook notifications
func (w *WebhookServer) handleWebhook(wr http.ResponseWriter, r *http.Request) {
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
	if err := w.engine.HandleWebhookNotification(r.Context(), &notification); err != nil {
		log.Printf("Error handling webhook notification: %v", err)
		http.Error(wr, "Internal server error", http.StatusInternalServerError)
		return
	}

	wr.WriteHeader(http.StatusOK)
	wr.Write([]byte("OK"))
}

// handleHealth handles health check requests
func (w *WebhookServer) handleHealth(wr http.ResponseWriter, r *http.Request) {
	wr.Header().Set("Content-Type", "application/json")
	wr.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"service":   "quick_watch",
		"version":   resolveVersion(),
	}

	json.NewEncoder(wr).Encode(response)
}

// handleStatus handles status requests
func (w *WebhookServer) handleStatus(wr http.ResponseWriter, r *http.Request) {
	wr.Header().Set("Content-Type", "application/json")
	wr.WriteHeader(http.StatusOK)

	targets := w.engine.GetTargetStatus()
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
