# Quick Watch

A simple Go CLI tool for monitoring URLs and services with configurable alerts and webhook notifications. This tool provides the simplest possible monitoring with threshold-based alerting and external webhook support.

Part of the `Quick Tools` family of tools from [Bevel Work](https://bevel.work/quick-tools).

## ✨ Features

- **Simple URL Monitoring**: Check URLs every 5 seconds for response time, status codes, and response size
- **Threshold-Based Alerting**: Configure how long a service can be down before firing alerts
- **All-Clear Notifications**: Automatically notify when services recover
- **Webhook Support**: Receive external notifications and handle them with configurable strategies
- **Strategy Pattern**: Pluggable strategies for checks, alerts, and notifications
- **HTTP Server**: Built-in webhook endpoint for external integrations
- **Color-Coded Output**: Visual feedback with status indicators
- **Configurable**: JSON-based configuration for monitors and strategies

## Quick Start

```bash
# Add a monitor
quick_watch add https://api.example.com/health --threshold 30

# List all monitors
quick_watch list

# Edit monitors using your preferred editor
quick_watch edit

# Remove a monitor
quick_watch rm https://api.example.com/health

# Start server mode
quick_watch server

# Use YAML configuration file
quick_watch config monitors.yml
```

## Configuration

Create a `monitors.yml` file to define multiple monitors:

```yaml
version: "1.0"
monitors:
  api-health:
    name: "API Health"
    url: "https://api.example.com/health"
    method: "GET"
    headers:
      Authorization: "Bearer token"
    threshold: 30
    check_strategy: "http"
    alert_strategy: "console"

settings:
  webhook_port: 8080
  webhook_path: "/webhook"
  check_interval: 5
  default_threshold: 30
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
  -d '{"type": "alert", "message": "Service down", "monitor": "API Health"}'
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

### Basic Monitoring
```bash
# Add a monitor
quick_watch add https://api.example.com/health --threshold 60

# List all monitors
quick_watch list
```

### Monitor Management
```bash
# Add a monitor with custom settings
quick_watch add https://api.example.com/health --threshold 30 --method POST

# Remove a monitor
quick_watch rm https://api.example.com/health

# Edit all monitors using your preferred editor
quick_watch edit
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
quick_watch config monitors.yml

# Use config with webhook server
quick_watch config monitors.yml --webhook-port 8080
```

## Command Line Syntax

```bash
quick_watch <action> [options]

Actions:
  edit                    Edit monitors using $EDITOR
  add <url>               Add a monitor
  rm <url>                Remove a monitor
  list                    List all monitors
  config <file>           Use YAML configuration file
  server                  Start in server mode

Options:
  --state <file>          State file path (default: watch-state.yml)
  --method <method>       HTTP method (default: GET)
  --header <key:value>    HTTP headers (can be used multiple times)
  --threshold <seconds>   Down threshold in seconds (default: 30)
  --webhook-port <port>   Webhook server port
  --webhook-path <path>   Webhook endpoint path (default: /webhook)
  --check-strategy <str>  Check strategy (default: http)
  --alert-strategy <str>  Alert strategy (default: console)

Examples:
  quick_watch edit
  quick_watch add https://api.example.com/health --threshold 30
  quick_watch rm https://api.example.com/health
  quick_watch list
  quick_watch config monitors.yml
  quick_watch server --webhook-port 8080
```

## Configuration File Format

```yaml
version: "1.0"
monitors:
  api-health:
    name: "API Health"
    url: "https://api.example.com/health"
    method: "GET"
    headers:
      Authorization: "Bearer token"
      User-Agent: "QuickWatch/1.0"
    threshold: 30
    check_strategy: "http"
    alert_strategy: "console"
    
  database:
    name: "Database"
    url: "tcp://db.example.com:5432"
    method: "TCP"
    threshold: 60
    check_strategy: "tcp"
    alert_strategy: "slack"

settings:
  webhook_port: 8080
  webhook_path: "/webhook"
  check_interval: 5
  default_threshold: 30

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
   - Ensure the JSON is valid

## License

Apache 2.0
