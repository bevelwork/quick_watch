package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// StateManager manages the YAML-backed state for quick_watch
type StateManager struct {
	filePath string
	state    *WatchState
	mutex    sync.RWMutex
}

// WatchState represents the complete state of the watch system
type WatchState struct {
	Version   string                    `yaml:"version"`
	Created   time.Time                 `yaml:"created"`
	Updated   time.Time                 `yaml:"updated"`
	Monitors  map[string]Monitor        `yaml:"monitors"`
	Settings  ServerSettings            `yaml:"settings"`
	Notifiers map[string]NotifierConfig `yaml:"notifiers"`
}

// ServerSettings represents server configuration
type ServerSettings struct {
	WebhookPort      int           `yaml:"webhook_port"`
	WebhookPath      string        `yaml:"webhook_path"`
	CheckInterval    int           `yaml:"check_interval"`    // seconds (default: 5s)
	DefaultThreshold int           `yaml:"default_threshold"` // seconds (default: 30s)
	Startup          StartupConfig `yaml:"startup"`           // startup message configuration
}

// StartupConfig represents startup message configuration
type StartupConfig struct {
	Enabled          bool     `yaml:"enabled"`            // enable startup messages
	Notifiers        []string `yaml:"notifiers"`          // list of notifiers to use
	CheckAllMonitors bool     `yaml:"check_all_monitors"` // check all monitors on startup
}

// NewStateManager creates a new state manager
func NewStateManager(filePath string) *StateManager {
	return &StateManager{
		filePath: filePath,
		state: &WatchState{
			Version:  "1.0",
			Created:  time.Now(),
			Updated:  time.Now(),
			Monitors: make(map[string]Monitor),
			Settings: ServerSettings{
				WebhookPort:      8080,
				WebhookPath:      "/webhook",
				CheckInterval:    5,
				DefaultThreshold: 30,
				Startup: StartupConfig{
					Enabled:          true,
					Notifiers:        []string{"console"},
					CheckAllMonitors: false,
				},
			},
			Notifiers: make(map[string]NotifierConfig),
		},
	}
}

// Load loads the state from the YAML file
func (sm *StateManager) Load() error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	// Check if file exists
	if _, err := os.Stat(sm.filePath); os.IsNotExist(err) {
		// Create directory if it doesn't exist
		dir := filepath.Dir(sm.filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %v", err)
		}
		// Save initial state
		return sm.saveUnlocked()
	}

	// Read and parse YAML file
	data, err := os.ReadFile(sm.filePath)
	if err != nil {
		return fmt.Errorf("failed to read state file: %v", err)
	}

	if err := yaml.Unmarshal(data, sm.state); err != nil {
		return fmt.Errorf("failed to parse state file: %v", err)
	}

	return nil
}

// Save saves the state to the YAML file
func (sm *StateManager) Save() error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	return sm.saveUnlocked()
}

// saveUnlocked saves the state without acquiring the lock (internal use)
func (sm *StateManager) saveUnlocked() error {
	sm.state.Updated = time.Now()

	data, err := yaml.Marshal(sm.state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %v", err)
	}

	if err := os.WriteFile(sm.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %v", err)
	}

	return nil
}

// AddMonitor adds a new monitor to the state
func (sm *StateManager) AddMonitor(monitor Monitor) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	// Use URL as key for uniqueness
	key := monitor.URL
	monitor.Name = monitor.Name // Ensure name is set
	if monitor.Name == "" {
		monitor.Name = fmt.Sprintf("Monitor-%s", key)
	}

	// Set defaults if not provided
	if monitor.Method == "" {
		monitor.Method = "GET"
	}
	if monitor.Threshold == 0 {
		monitor.Threshold = sm.state.Settings.DefaultThreshold
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

	sm.state.Monitors[key] = monitor
	return sm.saveUnlocked()
}

// RemoveMonitor removes a monitor by URL
func (sm *StateManager) RemoveMonitor(url string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if _, exists := sm.state.Monitors[url]; !exists {
		return fmt.Errorf("monitor with URL %s not found", url)
	}

	delete(sm.state.Monitors, url)
	return sm.saveUnlocked()
}

// GetMonitor retrieves a monitor by URL
func (sm *StateManager) GetMonitor(url string) (Monitor, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	monitor, exists := sm.state.Monitors[url]
	return monitor, exists
}

// ListMonitors returns all monitors
func (sm *StateManager) ListMonitors() map[string]Monitor {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]Monitor)
	for k, v := range sm.state.Monitors {
		result[k] = v
	}
	return result
}

// UpdateSettings updates server settings
func (sm *StateManager) UpdateSettings(settings ServerSettings) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.Settings = settings
	return sm.saveUnlocked()
}

// GetSettings returns current server settings
func (sm *StateManager) GetSettings() ServerSettings {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return sm.state.Settings
}

// GetMonitorConfig converts the state to MonitorConfig for the engine
func (sm *StateManager) GetMonitorConfig() *MonitorConfig {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	monitors := make([]Monitor, 0, len(sm.state.Monitors))
	for _, monitor := range sm.state.Monitors {
		monitors = append(monitors, monitor)
	}

	return &MonitorConfig{
		Monitors: monitors,
		Webhook: WebhookConfig{
			Port: sm.state.Settings.WebhookPort,
			Path: sm.state.Settings.WebhookPath,
		},
	}
}

// GetStateInfo returns basic state information
func (sm *StateManager) GetStateInfo() map[string]interface{} {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return map[string]interface{}{
		"version":  sm.state.Version,
		"created":  sm.state.Created,
		"updated":  sm.state.Updated,
		"monitors": len(sm.state.Monitors),
		"settings": sm.state.Settings,
	}
}

// GetNotifiers returns all notifiers
func (sm *StateManager) GetNotifiers() map[string]NotifierConfig {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.Notifiers
}

// UpdateNotifiers updates the notifiers configuration
func (sm *StateManager) UpdateNotifiers(notifiers map[string]NotifierConfig) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.Notifiers = notifiers
	sm.state.Updated = time.Now()

	return sm.saveUnlocked()
}

// GetNotifier returns a specific notifier by name
func (sm *StateManager) GetNotifier(name string) (NotifierConfig, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	notifier, exists := sm.state.Notifiers[name]
	return notifier, exists
}
