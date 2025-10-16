package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// WebhookServer represents the webhook server
type WebhookServer struct {
	port   int
	path   string
	engine *MonitoringEngine
	server *http.Server
}

// NewWebhookServer creates a new webhook server
func NewWebhookServer(port int, path string, engine *MonitoringEngine) *WebhookServer {
	return &WebhookServer{
		port:   port,
		path:   path,
		engine: engine,
	}
}

// Start starts the webhook server
func (w *WebhookServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Webhook endpoint
	mux.HandleFunc(w.path, w.handleWebhook)

	// Health check endpoint
	mux.HandleFunc("/health", w.handleHealth)

	// Status endpoint
	mux.HandleFunc("/status", w.handleStatus)

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

	monitors := w.engine.GetMonitorStatus()
	status := map[string]interface{}{
		"timestamp": time.Now(),
		"service":   "quick_watch",
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

	json.NewEncoder(wr).Encode(status)
}
