# Quick Watch Documentation

Quick Watch is a powerful, flexible monitoring tool that checks the health of your services and alerts you when something goes wrong. Built with Go, it offers multiple check strategies, alert methods, and a beautiful web interface for monitoring your infrastructure.

## Quick Start

### Installation

**Using Homebrew (Recommended):**

```bash
# Add the Bevel tap
brew tap bevelwork/tap

# Install Quick Watch
brew install quick-watch

# Verify installation
quick-watch --version
```

**Using Go:**

```bash
go install github.com/bevelwork/quick_watch@latest
```

**Build from Source:**

```bash
git clone https://github.com/bevelwork/quick_watch.git
cd quick_watch
go build -o quick-watch .
./quick-watch --version
```

### First Steps

1. **Start the server** (creates default configuration):
   ```bash
   quick-watch server
   ```

2. **Open your browser** to view the dashboard:
   ```
   http://localhost:8090/
   ```

3. **Add your first target**:
   ```bash
   quick-watch targets
   ```
   
   This opens your editor with an example configuration. Add your first service:
   ```yaml
   api-health:
     name: "API Health Check"
     url: "https://api.example.com/health"
     method: "GET"
     threshold: 30
     check_strategy: "http"
     alerts: ["console"]
   ```

4. **Configure alerts**:
   ```bash
   quick-watch alerts
   ```

That's it! Quick Watch is now monitoring your service and will alert you if it goes down.

## Core Concepts

### Targets
Services or endpoints you want to monitor. Each target has a check strategy (HTTP, TCP, webhook) and gets checked every 5 seconds.

### Alerts
How you get notified when a target fails. Supports console output, Slack, email, and file logging.

### Hooks
Webhook endpoints that can receive external notifications and trigger alerts based on incoming data.

### Settings
Global configuration including server ports, check intervals, thresholds, and status report schedules.

## Documentation Structure

- **[Targets Guide](./targets.md)** - Everything about configuring and monitoring targets
- **[Alerts Guide](./alerts.md)** - Setting up notifications via Slack, email, console, and files
- **[Settings Guide](./settings.md)** - Global configuration and server settings
- **[Hooks Guide](./hooks.md)** - Webhook integration and external notifications

## Key Features

### 🎯 Multiple Check Strategies
- **HTTP/HTTPS**: Monitor web services and APIs
- **TCP**: Check if ports are open and responding
- **Webhook**: Receive notifications from external systems

### 🔔 Flexible Alerting
- **Exponential Backoff**: Alerts increase in interval (5s, 10s, 20s, 40s...) to prevent alert fatigue
- **Threshold-Based**: Wait for sustained failures before alerting
- **Acknowledgements**: Interactive acknowledgement system with contact info sharing
- **Multiple Channels**: Console, Slack, Email, File logging

### 📊 Web Dashboard
- **Real-time Monitoring**: Auto-refreshing dashboard with live status updates
- **Target Details**: Individual pages with response time graphs and check history
- **Search & Filter**: Quick filtering by name or URL
- **GitHub Actions-Style Logs**: Expandable check history with full details

### 📈 Performance Tracking
- Response time graphs with 100 check history
- P95 response time calculation
- Average page size tracking
- Full response body capture (useful for JSON health endpoints)

## Configuration File

Quick Watch uses a YAML configuration file (`watch-state.yml`) that stores all targets, alerts, hooks, and settings:

```yaml
version: "1.0"

targets:
  api-health:
    name: "API Health Check"
    url: "https://api.example.com/health"
    method: "GET"
    threshold: 30
    check_strategy: "http"
    alerts: ["console", "slack-alerts"]

alerts:
  console:
    type: "console"
    enabled: true
  
  slack-alerts:
    type: "slack"
    enabled: true
    settings:
      webhook_url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"

settings:
  webhook_port: 8090
  check_interval: 5
  default_threshold: 30
  server_address: "https://monitor.example.com:8090"
  status_report:
    enabled: true
    interval: 3600
    alerts: ["console", "slack-alerts"]
```

## Command Reference

### Server Mode
```bash
# Start the monitoring server
quick-watch server

# Use a custom state file
quick-watch server --state custom-state.yml
```

### Configuration Management
```bash
# Edit targets in your $EDITOR
quick-watch targets

# Edit alerts configuration
quick-watch alerts

# Edit global settings
quick-watch settings

# Validate configuration
quick-watch validate
```

## API Endpoints

When running in server mode, Quick Watch provides a REST API:

- **GET /** - Main dashboard (web UI)
- **GET /targets/{name}** - Individual target detail page
- **GET /api/targets** - List all targets (JSON)
- **GET /api/history/{name}** - Get target check history (JSON)
- **GET /api/status** - Overall system status
- **GET /health** - Health check endpoint
- **POST /api/acknowledge/{token}** - Acknowledge an alert

## Examples

### Monitor Multiple Services

```yaml
targets:
  frontend:
    name: "Frontend Application"
    url: "https://example.com"
    threshold: 30
    alerts: ["slack-alerts"]
  
  api:
    name: "Backend API"
    url: "https://api.example.com/health"
    method: "GET"
    threshold: 60
    alerts: ["slack-alerts", "email"]
  
  database:
    name: "Database Ports"
    url: "db.example.com"
    check_strategy: "tcp"
    ports: [5432, 6379]
    threshold: 30
    alerts: ["console", "slack-alerts"]
```

### Slack Notifications

```yaml
alerts:
  slack-alerts:
    type: "slack"
    enabled: true
    settings:
      webhook_url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
```

### Email Alerts

```yaml
alerts:
  email:
    type: "email"
    enabled: true
    settings:
      smtp_host: "smtp.gmail.com"
      smtp_port: 587
      from: "alerts@example.com"
      to: "admin@example.com"
      password_env: "SMTP_PASSWORD"
```

## Getting Help

- **Documentation**: [GitHub Docs](https://github.com/bevelwork/quick_watch/tree/main/docs)
- **Issues**: [GitHub Issues](https://github.com/bevelwork/quick_watch/issues)
- **More Tools**: [Bevel Quick-Tools](https://bevel.work/quick-tools)

## Next Steps

1. Read the [Targets Guide](./targets.md) to learn about different check strategies
2. Set up [Alerts](./alerts.md) for your preferred notification channels
3. Configure [Global Settings](./settings.md) for your environment
4. Explore [Hooks](./hooks.md) for webhook-based monitoring

