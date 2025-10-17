# Quick Watch

A simple Go CLI tool for targeting URLs and services with configurable alerts and webhook notifications. This tool provides the simplest possible targeting with threshold-based alerting and external webhook support.

Part of the `Quick Tools` family of tools from [Bevel Work](https://bevel.work/quick-tools).

## ✨ Features

- **Simple URL Monitoring**: Check URLs every 5 seconds for response time, status codes, and response size
- **Threshold-Based Alerting**: Configure how long a service must be continuously down before firing alerts (prevents transient failures from triggering alerts)
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
  
  database-ports:
    name: "Database Ports"
    url: "db.example.com"
    check_strategy: "tcp"
    ports:
      - 5432  # PostgreSQL
      - 6379  # Redis
    threshold: 30
    alerts: "console"

settings:
  webhook_port: 8080
  webhook_path: "/webhook"
  server_address: "https://monitor.example.com:8080"  # Optional: Public-facing server address for alert URLs (defaults to http://localhost:PORT)
  check_interval: 5
  default_threshold: 30  # seconds (30s)
```

## Advanced Features

### TCP Port Checking

Monitor TCP ports on servers to ensure services are accessible. Perfect for databases, SSH, custom services, and any TCP-based application.

**Configuration:**
```yaml
targets:
  - name: "Database Server Ports"
    url: "db.example.com"  # Hostname or IP address
    check_strategy: "tcp"
    ports:
      - 5432  # PostgreSQL
      - 6379  # Redis
    threshold: 30
    alerts: "console slack-alerts"

  - name: "SSH Access"
    url: "server.example.com"
    check_strategy: "tcp"
    ports:
      - 22
    threshold: 15
    alerts: "console"
```

**Features:**
- ✅ Checks multiple ports simultaneously
- ✅ Reports which ports are open/closed
- ✅ 10-second timeout per port check
- ✅ Works with hostnames or IP addresses
- ✅ Response body shows port status for debugging
- ✅ All standard alerting features (threshold, backoff, acknowledgements)

**Example Alerts:**
```
🚨 ALERT: Database Server Ports is DOWN
   Failed ports: [6379]
   Successful: [5432]
```

**When to Use:**
- Database connectivity monitoring (PostgreSQL, MySQL, MongoDB, Redis)
- SSH access verification
- Custom TCP services
- Port availability checks
- Firewall rule validation

**Note**: For HTTP/HTTPS health checks, use `check_strategy: "http"` instead.

### Threshold-Based Alerting

Quick Watch uses a **threshold** to prevent transient failures or single slow responses from triggering alerts. The threshold specifies how long a service must be **continuously down** before the first alert is sent.

**Configuration:**
```yaml
targets:
  - name: "API Service"
    url: "https://api.example.com/health"
    threshold: 30  # seconds (default: 30)
```

**How it works:**
1. **Check fails** at T=0s: Quick Watch marks the target as "starting to fail" but **does NOT send an alert yet**
2. **Check at T=5s**: Still failing, but only 5 seconds elapsed → no alert
3. **Check at T=10s**: Still failing, but only 10 seconds elapsed → no alert
4. **Check at T=30s**: Still failing, **threshold exceeded** → **FIRST ALERT SENT**

This prevents:
- ❌ Single slow requests (e.g., 10-second timeout) from alerting immediately
- ❌ Network blips or transient errors from causing alert noise
- ❌ Load balancer health check delays from triggering false alerts
- ✅ Only **sustained outages** trigger alerts

### Exponential Backoff Alerts

After the threshold is exceeded and the first alert is sent, Quick Watch automatically implements exponential backoff to prevent alert fatigue:

| Alert # | Time Since Last Alert | Cumulative Time Down |
|---------|----------------------|-----------------------|
| 1       | After threshold      | 30 seconds (threshold)|
| 2       | 5 seconds            | 35 seconds            |
| 3       | 10 seconds           | 45 seconds            |
| 4       | 20 seconds           | 65 seconds            |
| 5       | 40 seconds           | 105 seconds (~2min)   |
| 6       | 80 seconds           | 185 seconds (~3min)   |
| 7       | 160 seconds          | 345 seconds (~6min)   |
| 8       | 320 seconds          | 665 seconds (~11min)  |

**Backoff Formula**: `5 × 2^(alert_count-1)` seconds

**Important**: 
- The **threshold** must be exceeded before the first alert
- Once the first alert is sent, **exponential backoff** controls subsequent alerts
- Once an alert is **acknowledged**, all subsequent alerts stop until the service recovers
- When a service recovers, all counters reset and the next incident starts fresh
- No additional configuration needed - works automatically for all targets

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
- **Unhealthy targets automatically sorted to the top**
- Real-time status indicators (✅ Healthy, ❌ Down, 🔔 Acknowledged)
- **Live filter** to search targets by name or URL (no page refresh needed)
- **Clear filter button** to reset search
- Filter count shows "X of Y targets" when filtering
- Quick navigation to individual target details
- Auto-refreshes every 5 seconds (pauses when actively filtering)

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
- Green line for successful response times (in seconds, up to 4 significant digits)
- Failed checks drop to 0s and turn the line red for that segment
- Red crosses mark failed check points at y=0
- Time-based X-axis with automatic scaling (HH:MM:SS format)
- Hover for detailed timestamps and precise values
- **Live streaming updates** every 5 seconds via AJAX (no page reloads!)
- No animations on refresh for smooth, non-jarring updates

**📋 GitHub Actions-Style Terminal Log:**
- Streaming log viewer showing all check history
- **Most recent entries at the top** for easy access to current status
- **Click any entry to expand** and see full details (just like GitHub Actions job logs!)
- Color-coded entries:
  - ✅ Green: Successful checks
  - ❌ Red: Failed checks
  - 🔄 Blue: Recovery events
  - 🔔 Yellow: Acknowledged failures
- Each entry shows:
  - ▶ Expand indicator (rotates when expanded)
  - Timestamp
  - Status icon
  - Response time or error message
  - HTTP status code
  - Alert count (if alerts were sent)
  - Acknowledgement status
- **Expanded view shows:**
  - Full timestamp with timezone
  - Detailed response time and size
  - Content-Type header
  - Complete error messages
  - Alert and acknowledgement details
  - **Full response body** (for JSON health checks and other responses)
- **Expanded entries stay open** during live updates (no page reload needed!)
- New entries appear at the top automatically
- Stores up to 1000 check entries per target

**🎯 Target Information:**
- Current status badge (Healthy/Down/Acknowledged)
- Target URL
- Back button to navigate to target list

**📊 Performance Statistics:**
- **Average Page Size**: Mean response size across all successful checks
  - Displays in appropriate units (bytes, KB, MB)
  - Only counts successful responses
- **P95 Response Time**: 95th percentile response time
  - Shows worst-case performance for 95% of requests
  - Displayed in seconds with up to 3 significant digits
  - Excludes failed checks
- **Total Checks**: Complete history count
  - Tracks all checks performed (up to 1000 entries)

**💡 Response Body Capture & Live Updates:**
- Automatically captures response bodies for all checks (up to 10KB per response)
- Especially useful for JSON health endpoints (e.g., `/health`, `/healthcheck`)
- View full response by clicking on any log entry to expand it
- Response bodies are stored with each check in the history
- **All data streams live** - graph, stats, logs, and status update every 5 seconds
- **No page reloads** - expanded entries stay open while data refreshes

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
      "WasRecovered": false,
      "ContentType": "application/json",
      "ResponseBody": "{\"status\":\"healthy\",\"uptime\":123456}"
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
    name: "Database Ports"
    url: "db.example.com"
    check_strategy: "tcp"
    ports:
      - 5432  # PostgreSQL
      - 6379  # Redis
    threshold: 60  # seconds (60s)
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
