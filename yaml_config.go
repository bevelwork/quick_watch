package main

import (
	"gopkg.in/yaml.v3"
)

// YAMLConfig represents the YAML configuration structure
type YAMLConfig struct {
	Version    string                 `yaml:"version,omitempty"`
	Targets    map[string]Target      `yaml:"targets"`
	Settings   ServerSettings         `yaml:"settings,omitempty"`
	Strategies map[string]interface{} `yaml:"strategies,omitempty"`
}

// ConvertToTargetConfig converts YAMLConfig to TargetConfig
func (yc *YAMLConfig) ConvertToTargetConfig() *TargetConfig {
	targets := make([]Target, 0, len(yc.Targets))
	for _, target := range yc.Targets {
		targets = append(targets, target)
	}

	config := &TargetConfig{
		Targets: targets,
		Webhook: WebhookConfig{
			Port: yc.Settings.WebhookPort,
			Path: yc.Settings.WebhookPath,
		},
	}

	return config
}

// LoadYAMLConfig loads configuration from YAML data
func LoadYAMLConfig(data []byte) (*TargetConfig, error) {
	// First try new schema (targets)
	var yamlConfig YAMLConfig
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		return nil, err
	}
	// Backward compatibility: if no targets, try legacy targets key
	if len(yamlConfig.Targets) == 0 {
		var legacy struct {
			Targets  map[string]Target `yaml:"targets"`
			Settings ServerSettings    `yaml:"settings,omitempty"`
		}
		if err := yaml.Unmarshal(data, &legacy); err == nil {
			if len(legacy.Targets) > 0 {
				yamlConfig.Targets = legacy.Targets
				yamlConfig.Settings = legacy.Settings
			}
		}
	}

	return yamlConfig.ConvertToTargetConfig(), nil
}
