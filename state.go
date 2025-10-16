package main

import (
	"fmt"
	"os"
	"maps"
	"path/filepath"
	"strings"
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
	Version  string                    `yaml:"version"`
	Created  time.Time                 `yaml:"created"`
	Updated  time.Time                 `yaml:"updated"`
	Targets  map[string]Target         `yaml:"targets"`
	Settings ServerSettings            `yaml:"settings"`
	Alerts   map[string]NotifierConfig `yaml:"alerts"`
	Hooks    map[string]Hook           `yaml:"hooks"`
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
	Enabled         bool     `yaml:"enabled"`           // enable startup messages
	Alerts          []string `yaml:"alerts"`            // list of alert strategies to use
	CheckAllTargets bool     `yaml:"check_all_targets"` // check all targets on startup
}

// NewStateManager creates a new state manager
func NewStateManager(filePath string) *StateManager {
	return &StateManager{
		filePath: filePath,
		state: &WatchState{
			Version: "1.0",
			Created: time.Now(),
			Updated: time.Now(),
			Targets: make(map[string]Target),
			Settings: ServerSettings{
				WebhookPort:      8080,
				WebhookPath:      "/webhook",
				CheckInterval:    5,
				DefaultThreshold: 30,
				Startup: StartupConfig{
					Enabled:         true,
					Alerts:          []string{"console"},
					CheckAllTargets: false,
				},
			},
			Alerts: make(map[string]NotifierConfig),
			Hooks:  make(map[string]Hook),
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

	// Backward compatibility: if targets/alerts absent, read legacy keys
	if len(sm.state.Targets) == 0 || len(sm.state.Alerts) == 0 || len(sm.state.Settings.Startup.Alerts) == 0 {
		var legacy struct {
			Version  string                    `yaml:"version"`
			Created  time.Time                 `yaml:"created"`
			Updated  time.Time                 `yaml:"updated"`
			Targets  map[string]Target         `yaml:"targets"`
			Settings ServerSettings            `yaml:"settings"`
			Alerts   map[string]NotifierConfig `yaml:"notifiers"`
		}
		if err := yaml.Unmarshal(data, &legacy); err == nil {
			if len(legacy.Targets) > 0 && len(sm.state.Targets) == 0 {
				sm.state.Targets = legacy.Targets
			}
			if len(legacy.Alerts) > 0 && len(sm.state.Alerts) == 0 {
				sm.state.Alerts = legacy.Alerts
			}
			// Startup legacy keys migration
			if len(sm.state.Settings.Startup.Alerts) == 0 {
				// try legacy settings.startup.notifiers
				if len(legacy.Settings.Startup.Alerts) > 0 {
					sm.state.Settings.Startup.Alerts = legacy.Settings.Startup.Alerts
				}
			}
			if sm.state.Version == "" {
				sm.state.Version = legacy.Version
			}
			if sm.state.Created.IsZero() {
				sm.state.Created = legacy.Created
			}
			if sm.state.Updated.IsZero() {
				sm.state.Updated = legacy.Updated
			}
		}
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

// AddTarget adds a new target to the state
func (sm *StateManager) AddTarget(target Target) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	// Use URL as key for uniqueness
	key := target.URL
	if target.Name == "" {
		target.Name = fmt.Sprintf("Target-%s", key)
	}

	// Set defaults if not provided
	if target.Method == "" {
		target.Method = "GET"
	}
	if target.Threshold == 0 {
		target.Threshold = sm.state.Settings.DefaultThreshold
	}
	if target.CheckStrategy == "" {
		target.CheckStrategy = "http"
	}
	if target.Headers == nil {
		target.Headers = make(map[string]string)
	}

	sm.state.Targets[key] = target
	return sm.saveUnlocked()
}

// RemoveTarget removes a target by URL
func (sm *StateManager) RemoveTarget(url string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if _, exists := sm.state.Targets[url]; !exists {
		return fmt.Errorf("target with URL %s not found", url)
	}

	delete(sm.state.Targets, url)
	return sm.saveUnlocked()
}

// GetTarget retrieves a target by URL
func (sm *StateManager) GetTarget(url string) (Target, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	target, exists := sm.state.Targets[url]
	return target, exists
}

// ListTargets returns all targets
func (sm *StateManager) ListTargets() map[string]Target {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]Target)
	maps.Copy(result, sm.state.Targets)
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

// GetTargetConfig converts the state to TargetConfig for the engine
func (sm *StateManager) GetTargetConfig() *TargetConfig {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	targets := make([]Target, 0, len(sm.state.Targets))
	for _, target := range sm.state.Targets {
		targets = append(targets, target)
	}

	return &TargetConfig{
		Targets: targets,
		Webhook: WebhookConfig{
			Port: sm.state.Settings.WebhookPort,
			Path: sm.state.Settings.WebhookPath,
		},
	}
}

// GetStateInfo returns basic state information
func (sm *StateManager) GetStateInfo() map[string]any {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	return map[string]any{
		"version":  sm.state.Version,
		"created":  sm.state.Created,
		"updated":  sm.state.Updated,
		"targets":  len(sm.state.Targets),
		"settings": sm.state.Settings,
	}
}

// GetAlerts returns all notifiers
func (sm *StateManager) GetAlerts() map[string]NotifierConfig {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.state.Alerts
}

// UpdateAlerts updates the notifiers configuration
func (sm *StateManager) UpdateAlerts(notifiers map[string]NotifierConfig) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.state.Alerts = notifiers
	sm.state.Updated = time.Now()

	return sm.saveUnlocked()
}

// GetNotifier returns a specific notifier by name
func (sm *StateManager) GetNotifier(name string) (NotifierConfig, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	notifier, exists := sm.state.Alerts[name]
	return notifier, exists
}

// ListHooks returns all configured hooks
func (sm *StateManager) ListHooks() map[string]Hook {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	result := make(map[string]Hook)
	maps.Copy(result, sm.state.Hooks)
	return result
}

// GetHook returns a hook by name
func (sm *StateManager) GetHook(name string) (Hook, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	hook, ok := sm.state.Hooks[name]
	return hook, ok
}

// UpsertHook adds or updates a hook
func (sm *StateManager) UpsertHook(name string, hook Hook) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	if hook.Name == "" {
		hook.Name = name
	}
	// Path stores the hook name (route is always /hooks/<name>)
	if hook.Path == "" {
		hook.Path = name
	}
	// Normalize any legacy full paths to just the name
	hook.Path = strings.TrimPrefix(hook.Path, "/hooks/")
	hook.Path = strings.TrimPrefix(hook.Path, "/")
	if len(hook.Methods) == 0 {
		hook.Methods = []string{"POST"}
	}
	if len(hook.Alerts) == 0 {
		hook.Alerts = []string{"console"}
	}
	if hook.Metadata == nil {
		hook.Metadata = make(map[string]string)
	}
	sm.state.Hooks[name] = hook
	return sm.saveUnlocked()
}

// RemoveHook deletes a hook by name
func (sm *StateManager) RemoveHook(name string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	if _, ok := sm.state.Hooks[name]; !ok {
		return fmt.Errorf("hook %s not found", name)
	}
	delete(sm.state.Hooks, name)
	return sm.saveUnlocked()
}
