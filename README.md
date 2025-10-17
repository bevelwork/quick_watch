# Quick Watch

A simple Go CLI tool for targeting URLs and services with configurable alerts and webhook notifications. This tool provides the simplest possible targeting with threshold-based alerting and external webhook support.

Part of the `Quick Tools` family of tools from [Bevel Work](https://bevel.work/quick-tools).

## ✨ Features

- **Simple URL Monitoring**: Check URLs every 5 seconds for response time, status codes, and response size
- **Threshold-Based Alerting**: Configure how long a service can be down before firing alerts
- **Exponential Backoff Alerts**: Intelligent alert throttling with exponential backoff (5s, 10s, 20s, 40s, etc.) to prevent alert fatigue
- **Alert Acknowledgements**: Acknowledge alerts to stop repeated notifications while investigating issues
- **Status Reports**: Periodic summaries (hourly by default) showing active/resolved outages and metrics
- **Target Detail Pages**: Beautiful web interface with real-time graphs, check history, and GitHub Actions-style terminal logs
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
  server_address: "https://monitor.example.com:8080"  # Optional: Public-facing server address for alert URLs (defaults to http://localhost:PORT)
  check_interval: 5
  default_threshold: 30  # seconds (30s)
```

## Advanced Features

### Exponential Backoff Alerts

Quick Watch automatically implements exponential backoff to prevent alert fatigue. When a target goes down, alerts are sent with progressively increasing intervals:

| Failure # | Time Since Last Alert | Cumulative Time |
|-----------|----------------------|-----------------|
| 1         | Immediate            | 0 seconds       |
| 2         | 5 seconds            | 5 seconds       |
| 3         | 10 seconds           | 15 seconds      |
| 4         | 20 seconds           | 35 seconds      |
| 5         | 40 seconds           | 75 seconds      |
| 6         | 80 seconds           | ~2.5 minutes    |
| 7         | 160 seconds          | ~5 minutes      |
| 8         | 320 seconds          | ~10 minutes     |

**Backoff Formula**: `5 × 2^(failure_count-1)` seconds

**Important**: 
- Once an alert is **acknowledged**, all subsequent alerts stop until the service recovers
- When a service recovers, counters reset and the next incident starts fresh
- No configuration needed - works automatically for all targets

For more details, see [EXPONENTIAL_BACKOFF.md](EXPONENTIAL_BACKOFF.md).

### Alert Acknowledgements

Enable acknowledgements in your configuration to allow team members to acknowledge alerts:

```yaml
settings:
  acknowledgements_enabled: true
  server_address: "https://monitor.example.com:8080"  # Your public-facing server address
```

When enabled:
- Each alert includes an acknowledgement URL
- Clicking the URL stops further alerts for that incident
- Alerts resume only after the service recovers and goes down again
- Responders can provide their name, contact info (Slack, Zoom, phone), and notes
- Contact information is distributed to all configured alert strategies

#### Configuring the Server Address

The `server_address` setting is crucial for acknowledgement URLs to work in production:

**Without `server_address`** (default):
```
Acknowledgement URL: http://localhost:8080/api/acknowledge/abc123
```
This will only work on the local machine and won't be accessible to remote team members.

**With `server_address`** (recommended for production):
```yaml
settings:
  server_address: "https://monitor.example.com:8080"
```
```
Acknowledgement URL: https://monitor.example.com:8080/api/acknowledge/abc123
```

**Common scenarios:**
- **Behind a reverse proxy**: `server_address: "https://monitoring.company.com"`
- **Cloud deployment**: `server_address: "https://monitor.example.com:8080"`
- **Docker with port mapping**: `server_address: "http://your-server-ip:9000"`
- **Local testing**: Omit `server_address` to use `http://localhost:PORT`

### Status Reports

Get periodic summaries of your system's health and activity:

```yaml
settings:
  status_report:
    enabled: true
    interval: 60  # minutes
    alerts: ["console", "slack"]
```

Each status report includes:
- **Active outages**: Currently down targets with duration and acknowledgement status
- **Resolved outages**: Targets that recovered since the last report
- **Metrics**: Number of alerts and notifications sent during the period

**Example console output:**
```
📊 STATUS REPORT (14:00:00 to 15:00:00)

Active Outages:
  • api-service - down for 45m (acknowledged by john)
  • database - down for 2m

Resolved Outages:
  • cache-server - was down for 5m

Metrics:
  • Alerts sent: 12
  • Notifications sent: 25
```

**Configuration options:**
- `enabled`: Enable/disable status reports (default: `false`)
- `interval`: How often to send reports in minutes (default: `60`)
- `alerts`: List of alert strategies to send reports to (e.g., `["console", "slack", "email"]`)

#### Manual Status Report Trigger

You can trigger a status report on-demand via webhook or browser:

**Browser (GET):**
Simply visit the URL in your browser:
```
http://localhost:8080/trigger/status_report
```
You'll see a nice HTML page confirming the report was sent.

**API/Script (POST):**
```bash
curl -X POST http://localhost:8080/trigger/status_report
```

**API Response (JSON):**
```json
{
  "status": "success",
  "message": "Status report generated and sent",
  "summary": {
    "active_outages": 2,
    "sent_to": ["console", "slack"]
  }
}
```

**Use cases:**
- **Browser**: Quick manual trigger with visual feedback
- **Bookmarks**: Save the URL for one-click status reports
- **CI/CD pipelines**: POST to trigger reports after deployments
- **Monitoring dashboards**: Add a "Generate Report" button
- **Scripts**: Automate report generation
- **External systems**: Integrate with other monitoring tools

**Requirements:**
- Status reports must be enabled in settings (`status_report.enabled: true`)
- At least one alert strategy must be configured (`status_report.alerts`)
- Both GET and POST methods are accepted

### Target Detail Pages

Quick Watch provides a beautiful web interface for monitoring individual targets with real-time data visualization and historical logs.

#### Accessing Target Pages

When running in server mode, Quick Watch automatically creates web pages for each target:

**Target List Page:**
```
http://localhost:8080/targets
```
- Shows all configured targets at a glance
- Real-time status indicators (✅ Healthy, ❌ Down, 🔔 Acknowledged)
- Quick navigation to individual target details
- Auto-refreshes every 5 seconds

**Individual Target Page:**
```
http://localhost:8080/targets/{target-name}
```
- Replace `{target-name}` with the URL-safe version of your target name
- Example: `http://localhost:8080/targets/api-health`

#### Features

**📈 Real-Time Line Graph:**
- Large, interactive response time graph using Chart.js
- Shows the last 100 checks
- Green line for response times
- Red crosses mark failed checks
- Time-based X-axis with automatic scaling
- Hover for detailed timestamps and values
- Automatically updates with new data every 5 seconds

**📋 GitHub Actions-Style Terminal Log:**
- Streaming log viewer showing all check history
- Most recent entries at the bottom (like GitHub Actions)
- Color-coded entries:
  - ✅ Green: Successful checks
  - ❌ Red: Failed checks
  - 🔄 Blue: Recovery events
  - 🔔 Yellow: Acknowledged failures
- Each entry shows:
  - Timestamp
  - Status icon
  - Response time or error message
  - HTTP status code
  - Alert count (if alerts were sent)
  - Acknowledgement status
- Auto-scrolls to show latest entries
- Stores up to 1000 check entries per target

**🎯 Target Information:**
- Current status badge (Healthy/Down/Acknowledged)
- Target URL
- Back button to navigate to target list

#### API Access

You can also fetch target history as JSON for programmatic access:

```bash
curl http://localhost:8080/api/history/api-health
```

**Response:**
```json
{
  "target": {
    "name": "API Health",
    "url": "https://api.example.com/health",
    "is_down": false,
    "url_safe": "api-health"
  },
  "history": [
    {
      "Timestamp": "2025-10-17T10:30:00Z",
      "Success": true,
      "ResponseTime": 145,
      "ResponseSize": 2048,
      "StatusCode": 200,
      "ErrorMessage": "",
      "AlertSent": false,
      "AlertCount": 0,
      "WasAcked": false,
      "WasRecovered": false
    }
  ],
  "count": 150
}
```

#### URL-Safe Names

Target names are automatically converted to URL-safe format:
- Spaces, underscores, dots, slashes → hyphens
- Converted to lowercase
- Special characters removed
- Examples:
  - `"API Health"` → `"api-health"`
  - `"My_Service.Prod"` → `"my-service-prod"`
  - `"user/profile/api"` → `"user-profile-api"`

#### Dark Mode Interface

All target pages feature a modern, dark-themed interface inspired by GitHub's design:
- Dark background (#0d1117) for reduced eye strain
- Syntax-highlighted terminal output
- Hover effects and smooth transitions
- Responsive design for mobile and desktop
- Professional color scheme matching modern developer tools

#### Use Cases

- **Operations Dashboard**: Bookmark `/targets` for quick access to all service status
- **Incident Investigation**: View detailed check history and pinpoint when issues started
- **Performance Analysis**: Analyze response time trends over time
- **Team Collaboration**: Share target URLs with team members during incidents
- **Integration**: Use the JSON API to build custom dashboards or alerts

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
  server_address: "https://monitor.example.com:8080"  # Optional: Public-facing server address for alert URLs
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
