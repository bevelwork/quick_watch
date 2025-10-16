# Quick Watch

A simple Go CLI tool for targeting URLs and services with configurable alerts and webhook notifications. This tool provides the simplest possible targeting with threshold-based alerting and external webhook support.

Part of the `Quick Tools` family of tools from [Bevel Work](https://bevel.work/quick-tools).

## ✨ Features

- **Simple URL Monitoring**: Check URLs every 5 seconds for response time, status codes, and response size
- **Threshold-Based Alerting**: Configure how long a service can be down before firing alerts
- **All-Clear Notifications**: Automatically notify when services recover
- **Webhook Support**: Receive external notifications and handle them with configurable strategies
- **Strategy Pattern**: Pluggable strategies for checks, alerts, and notifications
- **HTTP Server**: Built-in webhook endpoint for external integrations
- **Color-Coded Output**: Visual feedback with status indicators
- **Configurable**: YAML-based configuration for targets and strategies

## 🔒 Privacy and Security

Quick Watch handles sensitive configuration data including:
- Target URLs and endpoints
- Slack webhook URLs and tokens
- API keys and credentials
- Internal service configurations

**Important**: The `.gitignore` file is configured to exclude files that may contain private information:
- State files (`watch-state.yml`, `*.state.yml`)
- Configuration files (`config.yml`, `*-config.yml`)
- Test files (`test-*.yml`)
- Log files and temporary data

Use `example-config.yml` as a template for your configuration, but never commit actual configuration files with real URLs, webhooks, or credentials.

## Quick Start

```bash
# Add a target
quick_watch add https://api.example.com/health --threshold 30s

# List all targets
quick_watch list

# Edit targets using your preferred editor
quick_watch targets

# Remove a target
quick_watch rm https://api.example.com/health

# Start server mode
quick_watch server

# Use YAML configuration file
quick_watch config targets.yml
```

## Configuration

Create a `targets.yml` file to define multiple targets:

```yaml
version: "1.0"
targets:
  api-health:
    name: "API Health"
    url: "https://api.example.com/health"
    method: "GET"
    headers:
      Authorization: "Bearer token"
    threshold: 30  # seconds (30s)
    check_strategy: "http"
    alerts: "console"

settings:
  webhook_port: 8080
  webhook_path: "/webhook"
  check_interval: 5
  default_threshold: 30  # seconds (30s)
```

## Strategy Patterns

### Check Strategies
- `http` - Standard HTTP health checks
- `tcp` - TCP connection checks
- `custom` - Custom check implementations

### Alert Strategies  
- `console` - Print alerts to console
- `email` - Send email notifications
- `slack` - Send Slack messages
- `webhook` - Send webhook notifications

### Notification Strategies
- `email` - Handle email notifications
- `slack` - Handle Slack notifications
- `webhook` - Handle webhook notifications
- `console` - Print notifications to console

## Webhook Integration

The tool provides a webhook endpoint for external notifications:

```bash
# Start webhook server
quick_watch --webhook-port 8080

# Send notification to webhook
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{"type": "alert", "message": "Service down", "target": "API Health"}'
```

## Install

### Required Software
- Go 1.24.4 or later

### Install with Go
```bash
go install github.com/bevelwork/quick_watch@latest
quick_watch --version
```

### Or Build from Source
```bash
git clone https://github.com/bevelwork/quick_watch.git
cd quick_watch
go build -o quick_watch .
./quick_watch --version
```

## Usage Examples

### Basic Target
```bash
# Add a target
quick_watch add https://api.example.com/health --threshold 60

# List all targets
quick_watch list
```

### Target Management
```bash
# Add a target with custom settings
quick_watch add https://api.example.com/health --threshold 30s --method POST

# Remove a target
quick_watch rm https://api.example.com/health

# Edit all targets using your preferred editor
quick_watch targets
```

### Server Mode
```bash
# Start server mode with YAML state management
quick_watch server

# Use custom state file
quick_watch server --state custom-state.yml
```

### Configuration File
```bash
# Use YAML configuration file
quick_watch config targets.yml

# Use config with webhook server
quick_watch config targets.yml --webhook-port 8080
```

## Command Line Syntax

```bash
quick_watch <action> [options]

Actions:
  add <url>     Add a target with default settings
  targets       Edit targets using $EDITOR
  settings      Edit global settings using $EDITOR
  alerts        Edit alert configs using $EDITOR

Administrative Actions:
  validate      Validate configuration syntax and alert strategies
  config <file> Use YAML configuration file

Options:
  --state <file>          State file path (default: watch-state.yml)
  --method <method>       HTTP method (default: GET)
  --header <key:value>    HTTP headers (can be used multiple times)
  --threshold <seconds>   Down threshold in seconds (default: 30s)
  --webhook-port <port>   Webhook server port
  --webhook-path <path>   Webhook endpoint path (default: /webhook)
  --check-strategy <str>  Check strategy (default: http)
  --alert-strategy <str>  Alert strategy (default: console)

Examples:
  quick_watch targets
  quick_watch add https://api.example.com/health --threshold 30s
  quick_watch rm https://api.example.com/health
  quick_watch list
  quick_watch config targets.yml
  quick_watch server --webhook-port 8080
```

## Configuration File Format

```yaml
version: "1.0"
targets:
  api-health:
    name: "API Health"
    url: "https://api.example.com/health"
    method: "GET"
    headers:
      Authorization: "Bearer token"
      User-Agent: "QuickWatch/1.0"
    threshold: 30  # seconds (30s)
    check_strategy: "http"
    alerts: "console"
  
  database:
    name: "Database"
    url: "tcp://db.example.com:5432"
    method: "TCP"
    threshold: 60  # seconds (60s)
    check_strategy: "tcp"
    alerts: "slack"

settings:
  webhook_port: 8080
  webhook_path: "/webhook"
  check_interval: 5
  default_threshold: 30  # seconds (30s)

strategies:
  check:
    http:
      timeout: 10
      follow_redirects: true
  
  alert:
    slack:
      webhook_url: "https://hooks.slack.com/services/..."
      channel: "#alerts"
    email:
      smtp_host: "smtp.gmail.com"
      smtp_port: 587
      username: "alerts@example.com"
      password: "password"
      to: "admin@example.com"
```

## Troubleshooting

### Common Issues

1. **"Connection refused"**
   - Check if the URL is accessible
   - Verify network connectivity
   - Ensure the service is running

2. **"Timeout"**
   - Increase the timeout in configuration
   - Check network latency
   - Verify the service response time

3. **"Webhook server failed to start"**
   - Check if the port is already in use
   - Ensure you have permission to bind to the port
   - Try a different port

4. **"Configuration file not found"**
   - Verify the file path is correct
   - Check file permissions
   - Ensure the YAML is valid

## License

Apache 2.0
