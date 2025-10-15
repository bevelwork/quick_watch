package main

import (
	"gopkg.in/yaml.v3"
)

// YAMLConfig represents the YAML configuration structure
type YAMLConfig struct {
	Version   string                    `yaml:"version,omitempty"`
	Monitors  map[string]Monitor        `yaml:"monitors"`
	Settings  ServerSettings           `yaml:"settings,omitempty"`
	Strategies map[string]interface{}   `yaml:"strategies,omitempty"`
}

// ConvertToMonitorConfig converts YAMLConfig to MonitorConfig
func (yc *YAMLConfig) ConvertToMonitorConfig() *MonitorConfig {
	monitors := make([]Monitor, 0, len(yc.Monitors))
	for _, monitor := range yc.Monitors {
		monitors = append(monitors, monitor)
	}

	config := &MonitorConfig{
		Monitors: monitors,
		Webhook: WebhookConfig{
			Port: yc.Settings.WebhookPort,
			Path: yc.Settings.WebhookPath,
		},
	}

	return config
}

// LoadYAMLConfig loads configuration from YAML data
func LoadYAMLConfig(data []byte) (*MonitorConfig, error) {
	var yamlConfig YAMLConfig
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		return nil, err
	}

	return yamlConfig.ConvertToMonitorConfig(), nil
}
