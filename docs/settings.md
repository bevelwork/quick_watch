# Settings Guide

Settings control the global behavior of Quick Watch, including server configuration, check intervals, default thresholds, and status reporting.

## Table of Contents

- [Managing Settings](#managing-settings)
- [Server Settings](#server-settings)
- [Check Settings](#check-settings)
- [Status Reports](#status-reports)
- [Examples](#examples)

## Managing Settings

### Interactive Editor

Edit settings through the interactive editor:

```bash
quick-watch settings
```

This opens your `$EDITOR` with the current settings and helpful comments.

### Configuration File

Settings are stored in `watch-state.yml`:

```yaml
settings:
  webhook_port: 8090
  webhook_path: "/webhook"
  check_interval: 5
  default_threshold: 30
  server_address: "https://monitor.example.com:8090"
  status_report:
    enabled: true
    interval: 3600
    alerts: ["console", "slack-alerts"]
```

## Server Settings

### webhook_port

**Type:** Integer  
**Default:** `8090`  
**Description:** Port where the web server listens

```yaml
settings:
  webhook_port: 8090
```

**Use Cases:**
- Change if port 8090 is already in use
- Use standard ports (80, 443) with proper permissions
- Match your infrastructure/firewall rules

**Examples:**
```yaml
# Development
webhook_port: 8090

# Production with reverse proxy
webhook_port: 8080

# Standard HTTP (requires root/capabilities)
webhook_port: 80
```

### webhook_path

**Type:** String  
**Default:** `"/webhook"`  
**Description:** Base path for webhook endpoints

```yaml
settings:
  webhook_path: "/webhook"
```

**Generated Paths:**
- Main webhook: `http://localhost:8090/webhook`
- Custom hooks: `http://localhost:8090/hooks/{hook-name}`

**Examples:**
```yaml
# Default
webhook_path: "/webhook"

# Custom path
webhook_path: "/api/webhooks"

# Root path
webhook_path: "/"
```

### server_address

**Type:** String  
**Default:** `"http://localhost:8090"`  
**Description:** Public-facing server address used in alert links

```yaml
settings:
  server_address: "https://monitor.example.com:8090"
```

**Why This Matters:**

Without `server_address`, alerts include links like:
```
Acknowledge: http://localhost:8090/api/acknowledge/abc123
```

These won't work when received externally. With `server_address`:
```
Acknowledge: https://monitor.example.com/api/acknowledge/abc123
```

**Examples:**
```yaml
# Production with domain
server_address: "https://monitor.example.com"

# Production with port
server_address: "https://monitor.example.com:8090"

# Behind reverse proxy
server_address: "https://monitoring.company.com"

# Public IP
server_address: "http://203.0.113.42:8090"

# Development (default)
server_address: "http://localhost:8090"
```

**Best Practices:**
- Always set in production
- Use HTTPS for security
- Include port if non-standard
- Test acknowledgement links
- Match your DNS/load balancer configuration

## Check Settings

### check_interval

**Type:** Integer (seconds)  
**Default:** `5`  
**Description:** How often to check each target

```yaml
settings:
  check_interval: 5
```

**Considerations:**

- **Lower values** (1-3s): 
  - Faster detection
  - Higher CPU/network usage
  - More API calls to monitored services
  
- **Higher values** (10-30s):
  - Less resource intensive
  - Slower failure detection
  - Better for rate-limited APIs

**Examples:**
```yaml
# High-frequency monitoring
check_interval: 1

# Default - good balance
check_interval: 5

# Low-frequency monitoring
check_interval: 30

# Minimal monitoring
check_interval: 60
```

**Formula:**
```
Max detection time = check_interval + threshold
```

For example:
- `check_interval: 5` + `threshold: 30` = 35 seconds to detect
- `check_interval: 1` + `threshold: 10` = 11 seconds to detect

### default_threshold

**Type:** Integer (seconds)  
**Default:** `30`  
**Description:** Default threshold for targets that don't specify one

```yaml
settings:
  default_threshold: 30
```

**Threshold Behavior:**
- Service must be down continuously for this duration
- Prevents alerts from transient failures
- Individual targets can override this value

**Examples:**
```yaml
# Very sensitive - alert quickly
default_threshold: 10

# Balanced - standard setting
default_threshold: 30

# Tolerant - wait longer
default_threshold: 60

# Very tolerant - for flaky services
default_threshold: 120
```

**Per-Target Override:**
```yaml
settings:
  default_threshold: 30

targets:
  critical-api:
    name: "Critical API"
    url: "https://critical.example.com"
    threshold: 10  # Lower threshold for critical service
  
  background-job:
    name: "Background Job"
    url: "https://jobs.example.com"
    threshold: 120  # Higher threshold for less critical
```

## Status Reports

Status reports provide periodic summaries of system health sent to configured alert channels.

### Configuration

```yaml
settings:
  status_report:
    enabled: true
    interval: 3600
    alerts: ["console", "slack-alerts"]
```

### enabled

**Type:** Boolean  
**Default:** `false`  
**Description:** Enable/disable periodic status reports

```yaml
status_report:
  enabled: true
```

### interval

**Type:** Integer (seconds)  
**Default:** `3600` (1 hour)  
**Description:** How often to send status reports

```yaml
status_report:
  interval: 3600  # 1 hour
```

**Common Intervals:**
```yaml
# Every 15 minutes
interval: 900

# Every 30 minutes
interval: 1800

# Every hour (default)
interval: 3600

# Every 4 hours
interval: 14400

# Every 8 hours (shift changes)
interval: 28800

# Daily
interval: 86400
```

### alerts

**Type:** Array of strings  
**Default:** `[]`  
**Description:** Which alert strategies receive status reports

```yaml
status_report:
  alerts: ["console", "slack-alerts", "email"]
```

**Best Practices:**
- Use different channels than real-time alerts
- Consider separate Slack channel for reports
- Email good for daily/shift summaries
- Console useful for server logs

### Report Content

Status reports include:

```
📊 Quick Watch Status Report
Generated: 2025-10-17 15:00:00 EST

🔴 Active Outages (2):
  • Payment API (acknowledged by Jane Smith)
  • Analytics Service

✅ Resolved Outages (1):
  • Background Job (down for 5m 30s)

📨 Alerts Sent: 15
📬 Notifications Sent: 8
```

### Manual Triggering

Trigger reports on-demand:

**Via API:**
```bash
# Trigger via POST
curl -X POST http://localhost:8090/trigger/status_report

# Trigger via GET (returns HTML)
curl http://localhost:8090/trigger/status_report
```

**Via Browser:**
```
http://localhost:8090/trigger/status_report
```

**Via Command Line:**
```bash
# Using curl with formatting
curl http://localhost:8090/trigger/status_report | less
```

## Examples

### Minimal Configuration

```yaml
settings:
  webhook_port: 8090
  check_interval: 5
  default_threshold: 30
```

### Development Configuration

```yaml
settings:
  webhook_port: 8090
  webhook_path: "/webhook"
  check_interval: 5
  default_threshold: 30
  server_address: "http://localhost:8090"
  status_report:
    enabled: false
```

### Production Configuration

```yaml
settings:
  webhook_port: 8080
  webhook_path: "/webhook"
  check_interval: 5
  default_threshold: 30
  server_address: "https://monitor.example.com"
  status_report:
    enabled: true
    interval: 3600
    alerts: ["slack-alerts", "email"]
```

### High-Frequency Monitoring

```yaml
settings:
  webhook_port: 8090
  check_interval: 1
  default_threshold: 10
  server_address: "https://monitor.example.com:8090"
  status_report:
    enabled: true
    interval: 900  # 15 minutes
    alerts: ["console", "slack-alerts"]
```

### Multi-Region Setup

```yaml
# US Region
settings:
  webhook_port: 8090
  check_interval: 5
  default_threshold: 30
  server_address: "https://monitor-us.example.com"
  status_report:
    enabled: true
    interval: 3600
    alerts: ["slack-us-ops"]

# EU Region  
settings:
  webhook_port: 8090
  check_interval: 5
  default_threshold: 30
  server_address: "https://monitor-eu.example.com"
  status_report:
    enabled: true
    interval: 3600
    alerts: ["slack-eu-ops"]
```

### Behind Reverse Proxy

```yaml
# Nginx/Traefik/etc. handles HTTPS
settings:
  webhook_port: 8080  # Internal port
  webhook_path: "/webhook"
  check_interval: 5
  default_threshold: 30
  server_address: "https://monitoring.company.com"  # Public HTTPS URL
  status_report:
    enabled: true
    interval: 14400  # 4 hours
    alerts: ["slack-ops", "email"]
```

### Shift-Based Reporting

```yaml
settings:
  webhook_port: 8090
  check_interval: 5
  default_threshold: 30
  server_address: "https://monitor.example.com"
  status_report:
    enabled: true
    interval: 28800  # 8 hours (shift changes)
    alerts: ["email", "slack-oncall"]
```

## Environment-Specific Settings

### Development

```yaml
settings:
  webhook_port: 8090
  check_interval: 10  # Less frequent for dev
  default_threshold: 60
  server_address: "http://localhost:8090"
  status_report:
    enabled: false  # Disable in dev
```

### Staging

```yaml
settings:
  webhook_port: 8090
  check_interval: 5
  default_threshold: 30
  server_address: "https://monitor-staging.example.com"
  status_report:
    enabled: true
    interval: 14400  # 4 hours
    alerts: ["slack-dev"]
```

### Production

```yaml
settings:
  webhook_port: 8080
  check_interval: 5
  default_threshold: 30
  server_address: "https://monitor.example.com"
  status_report:
    enabled: true
    interval: 3600  # 1 hour
    alerts: ["slack-ops", "email"]
```

## Best Practices

### Server Configuration

1. **Use non-privileged ports** (>1024) in development
2. **Set server_address** for external access
3. **Use HTTPS** in production
4. **Configure reverse proxy** for SSL termination
5. **Document port usage** in your infrastructure

### Check Intervals

1. **Start with 5 seconds** - good default
2. **Lower for critical services** (1-3 seconds)
3. **Higher for rate-limited APIs** (30-60 seconds)
4. **Consider cost** of frequent checks
5. **Monitor resource usage** if using very low intervals

### Thresholds

1. **30 seconds default** - prevents false positives
2. **Lower for critical services** (10-15 seconds)
3. **Higher for flaky services** (60-120 seconds)
4. **Match SLAs** - align with your service level objectives
5. **Test and adjust** based on real-world behavior

### Status Reports

1. **Enable in production** for visibility
2. **Hourly for active monitoring** (3600 seconds)
3. **Daily for summaries** (86400 seconds)
4. **Shift-based for handoffs** (28800 seconds)
5. **Separate channel** from real-time alerts

### Public Address

```yaml
# ✅ Good - HTTPS with domain
server_address: "https://monitor.example.com"

# ✅ Good - HTTPS with IP and port
server_address: "https://203.0.113.42:8090"

# ⚠️ OK for dev - HTTP localhost
server_address: "http://localhost:8090"

# ❌ Bad - Internal address (won't work externally)
server_address: "http://0.0.0.0:8090"

# ❌ Bad - Private IP (won't work externally)
server_address: "http://192.168.1.100:8090"
```

## Troubleshooting

### Port Already in Use

```bash
# Check what's using the port
lsof -i :8090

# Change port in settings
settings:
  webhook_port: 8091
```

### Acknowledgement Links Don't Work

1. **Check server_address**: Must be publicly accessible
2. **Verify DNS**: Resolve to correct IP
3. **Test from external network**: Links must work from internet
4. **Check firewall**: Port must be open
5. **Verify reverse proxy**: If using one

### Status Reports Not Sending

1. **Check enabled**: `enabled: true`
2. **Verify alerts**: Alert strategies must be configured
3. **Check interval**: Make sure enough time has passed
4. **Test manually**: Use `/trigger/status_report`
5. **Check logs**: Look for errors in server output

### Checks Too Slow/Fast

```yaml
# Too many checks overwhelming system
check_interval: 1  # Try increasing to 5

# Detection too slow
check_interval: 30  # Try decreasing to 5

# Balance with threshold
check_interval: 5
default_threshold: 30  # Total detection time: 35s
```

### Server Address Not Applied

1. **Restart server**: Changes require restart
2. **Check syntax**: Ensure valid URL format
3. **Include protocol**: http:// or https://
4. **Include port**: If non-standard (not 80/443)
5. **Test links**: Verify in actual alerts

## Performance Tuning

### Low Resource Environment

```yaml
settings:
  check_interval: 10  # Less frequent checks
  default_threshold: 60
  status_report:
    interval: 14400  # Less frequent reports
```

### High-Performance Setup

```yaml
settings:
  check_interval: 1  # Very frequent
  default_threshold: 5  # Quick detection
  status_report:
    interval: 900  # Frequent updates
```

### Large Number of Targets

```yaml
settings:
  check_interval: 10  # Spread load
  default_threshold: 30
  status_report:
    interval: 3600
```

## Configuration Validation

Validate your configuration:

```bash
# Validate entire configuration
quick-watch validate

# Check specific settings
quick-watch settings
# Make changes
# Save and close editor
# Changes are validated automatically
```

## Next Steps

- Configure [Targets](./targets.md) to monitor
- Set up [Alerts](./alerts.md) for notifications
- Explore [Hooks](./hooks.md) for webhook monitoring

