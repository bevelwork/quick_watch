# Alerts Guide

Alerts are how Quick Watch notifies you when targets fail health checks. Quick Watch supports multiple alert strategies including console output, Slack, email, and file logging.

## Table of Contents

- [Managing Alerts](#managing-alerts)
- [Alert Strategies](#alert-strategies)
- [Alert Configuration](#alert-configuration)
- [Acknowledgements](#acknowledgements)
- [Status Reports](#status-reports)
- [Examples](#examples)

## Managing Alerts

### Interactive Editor

Edit alerts through the interactive editor:

```bash
quick-watch alerts
```

This opens your `$EDITOR` with the current alert configuration and helpful examples.

### Configuration File

Alerts are stored in `watch-state.yml`:

```yaml
alerts:
  console:
    type: "console"
    enabled: true
    description: "Print alerts to console"
  
  slack-alerts:
    type: "slack"
    enabled: true
    description: "Team notifications"
    settings:
      webhook_url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
```

## Alert Strategies

### Console Alerts

Print alerts directly to the console/terminal where Quick Watch is running.

**Configuration:**

```yaml
console:
  type: "console"
  enabled: true
  description: "Console output"
  settings:
    style: "stylized"  # or "plain"
```

**Style Options:**
- `stylized` (default): Colored output with emoji icons
- `plain`: Simple text output without colors

**Use Cases:**
- Development and testing
- Docker logs
- SystemD journal logs
- Simple deployments

**Example Output:**

```
🚨 ALERT: Production API is DOWN
Target: https://api.example.com/health
Status: Connection timeout
Down Since: 2025-10-17 14:30:00 EST
Alert #3
Acknowledge: http://monitor.example.com/api/acknowledge/abc123

✅ RECOVERED: Production API is back UP
Target: https://api.example.com/health
Downtime: 5m 30s
```

### Slack Alerts

Send notifications to Slack channels via incoming webhooks.

**Setup:**

1. Create a Slack webhook:
   - Go to https://api.slack.com/messaging/webhooks
   - Create a new webhook for your workspace
   - Copy the webhook URL

2. Configure in Quick Watch:

```yaml
slack-alerts:
  type: "slack"
  enabled: true
  description: "Team notifications in #alerts"
  settings:
    webhook_url: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXX"
```

**Features:**
- Rich formatted messages with emoji
- Acknowledgement buttons
- Downtime information
- Alert count tracking
- Contact information sharing

**Message Format:**

```
🚨 ALERT #3: Production API is DOWN

📍 Target: https://api.example.com/health
⏱️ Down Since: 2 minutes ago
❌ Status: HTTP 503 - Service Unavailable

👉 Acknowledge: http://monitor.example.com/api/acknowledge/abc123
```

**Best Practices:**
- Use dedicated `#alerts` channel
- Configure channel notifications
- Set up channel description with on-call info
- Consider separate channels for critical vs. non-critical

### Email Alerts

Send email notifications via SMTP.

**Configuration:**

```yaml
email:
  type: "email"
  enabled: true
  description: "Email alerts to ops team"
  settings:
    smtp_host: "smtp.gmail.com"
    smtp_port: 587
    from: "alerts@example.com"
    to: "ops@example.com"
    password_env: "SMTP_PASSWORD"
```

**Settings:**

| Field | Required | Description |
|-------|----------|-------------|
| `smtp_host` | Yes | SMTP server hostname |
| `smtp_port` | Yes | SMTP server port (usually 587 for TLS) |
| `from` | Yes | From email address |
| `to` | Yes | Recipient email address |
| `password_env` | Yes | Environment variable containing SMTP password |

**Security:**
- Passwords are read from environment variables only
- Never store passwords in configuration files
- Use app-specific passwords for Gmail
- Enable 2FA and use OAuth tokens when possible

**Example with Gmail:**

1. Create app-specific password:
   - Go to Google Account settings
   - Security → 2-Step Verification → App passwords
   - Generate password for "Mail"

2. Set environment variable:
   ```bash
   export SMTP_PASSWORD="your-app-specific-password"
   ```

3. Configure Quick Watch:
   ```yaml
   email:
     type: "email"
     enabled: true
     settings:
       smtp_host: "smtp.gmail.com"
       smtp_port: 587
       from: "alerts@yourdomain.com"
       to: "oncall@yourdomain.com"
       password_env: "SMTP_PASSWORD"
   ```

**Email Content:**

- **Subject**: `[ALERT #3] Production API is DOWN`
- **Body**: Formatted text with all alert details
- **Includes**: Target URL, error details, downtime, acknowledgement link

### File Alerts

Write alerts to a log file for archival and processing.

**Configuration:**

```yaml
file-log:
  type: "file"
  enabled: true
  description: "Alert log file"
  settings:
    file_path: "/var/log/quickwatch/alerts.log"
```

**Settings:**

| Field | Required | Description |
|-------|----------|-------------|
| `file_path` | Yes | Full path to log file |

**Log Format:**

```
[2025-10-17 14:30:00 EST] ALERT #3: Production API is DOWN
Target: https://api.example.com/health
Status: Connection timeout
Down Since: 2025-10-17 14:25:00 EST
---
[2025-10-17 14:35:30 EST] RECOVERED: Production API is back UP
Target: https://api.example.com/health
Downtime: 5m 30s
---
```

**Use Cases:**
- Long-term alert history
- Compliance and auditing
- Log aggregation (ELK, Splunk)
- Backup notification channel
- Integration with other tools

**Best Practices:**
- Use logrotate to manage file size
- Set appropriate file permissions
- Consider separate files per environment
- Include in backup strategy

## Alert Configuration

### Assigning Alerts to Targets

Targets can use one or more alert strategies:

**Single Alert:**
```yaml
api-health:
  name: "API Health"
  url: "https://api.example.com/health"
  alerts: ["console"]
```

**Multiple Alerts:**
```yaml
production-api:
  name: "Production API"
  url: "https://api.example.com/health"
  alerts: ["console", "slack-alerts", "email"]
```

**No Alerts (Monitoring Only):**
```yaml
test-endpoint:
  name: "Test Endpoint"
  url: "https://test.example.com"
  alerts: []
```

### Alert Priority

For critical services, use multiple alert channels:

```yaml
# Critical - All channels
critical-payment-api:
  name: "Payment API"
  url: "https://payments.example.com/health"
  threshold: 10
  alerts: ["console", "slack-alerts", "email", "file-log"]

# Important - Slack + Email
important-user-api:
  name: "User API"
  url: "https://users.example.com/health"
  threshold: 30
  alerts: ["slack-alerts", "email"]

# Standard - Slack only
standard-service:
  name: "Analytics Service"
  url: "https://analytics.example.com/health"
  threshold: 60
  alerts: ["slack-alerts"]

# Low Priority - Console + File
background-job:
  name: "Background Job"
  url: "https://jobs.example.com/health"
  threshold: 120
  alerts: ["console", "file-log"]
```

### Environment Variables

For sensitive data (API keys, passwords), use environment variables:

```yaml
slack-production:
  type: "slack"
  enabled: true
  settings:
    webhook_url: "${SLACK_WEBHOOK_URL}"

email-production:
  type: "email"
  enabled: true
  settings:
    smtp_host: "smtp.gmail.com"
    smtp_port: 587
    from: "alerts@example.com"
    to: "oncall@example.com"
    password_env: "SMTP_PASSWORD"
```

Set variables before starting:

```bash
export SLACK_WEBHOOK_URL="https://hooks.slack.com/services/..."
export SMTP_PASSWORD="your-app-password"
quick-watch server
```

## Acknowledgements

Quick Watch includes an interactive acknowledgement system to help teams coordinate during incidents.

### How It Works

1. **Alert Sent**: Includes acknowledgement link
2. **Click Link**: Opens interactive web form
3. **Immediate Ack**: Alert is acknowledged instantly
4. **Provide Details**: Fill in name, contact info, notes
5. **Submit**: Information distributed to all alert channels
6. **No More Alerts**: Until service recovers

### Acknowledgement URL

Every alert includes a unique acknowledgement URL:

```
http://monitor.example.com/api/acknowledge/abc123def456
```

### Acknowledgement Form

When you visit the URL, you'll see a form requesting:

- **Your Name**: Who's handling the incident
- **Contact Me Here**: Slack, Zoom, phone, email
- **Notes**: What you're investigating, ETA, etc.

### Acknowledgement Notifications

After submitting, all configured alert strategies receive:

```
🔔 ACKNOWLEDGED: Production API

👤 Acknowledged By: Jane Smith
📞 Contact: Slack @jane, Zoom: https://zoom.us/j/123456
📝 Notes: Investigating database connection pool. ETA 10 minutes.
⏰ Acknowledged At: 2025-10-17 14:32:00 EST
```

### Benefits

- **Team Coordination**: Everyone knows who's on it
- **Contact Info**: Easy to reach responder
- **Context**: Notes provide status updates
- **Alert Fatigue**: Stops repeated alerts
- **Timezone Info**: Timestamps include timezone

### Best Practices

- Click acknowledgement link immediately when taking ownership
- Provide meaningful contact information
- Update notes if investigation continues
- Use company communication tools (Slack, Teams)
- Include ETA if known

## Status Reports

Quick Watch can send periodic status reports summarizing system health.

### Configuration

```yaml
settings:
  status_report:
    enabled: true
    interval: 3600  # seconds (1 hour)
    alerts: ["console", "slack-alerts"]
```

### Report Content

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

Trigger a report manually:

```bash
# Via API
curl http://localhost:8090/trigger/status_report

# Via browser (shows HTML report)
open http://localhost:8090/trigger/status_report
```

### Use Cases

- **Daily Summaries**: Set interval to 86400 (24 hours)
- **Hourly Updates**: Set interval to 3600 (1 hour)
- **Shift Handoffs**: Trigger manually during handoff
- **Stakeholder Updates**: Regular status emails

## Examples

### Development Setup

```yaml
alerts:
  console:
    type: "console"
    enabled: true
    settings:
      style: "stylized"

targets:
  local-api:
    name: "Local API"
    url: "http://localhost:3000/health"
    alerts: ["console"]
```

### Production Setup

```yaml
alerts:
  console:
    type: "console"
    enabled: true
    settings:
      style: "stylized"
  
  slack-alerts:
    type: "slack"
    enabled: true
    description: "Team notifications in #alerts"
    settings:
      webhook_url: "${SLACK_WEBHOOK_URL}"
  
  email:
    type: "email"
    enabled: true
    description: "On-call email alerts"
    settings:
      smtp_host: "smtp.gmail.com"
      smtp_port: 587
      from: "alerts@company.com"
      to: "oncall@company.com"
      password_env: "SMTP_PASSWORD"
  
  file-log:
    type: "file"
    enabled: true
    description: "Alert archive"
    settings:
      file_path: "/var/log/quickwatch/alerts.log"

targets:
  production-api:
    name: "Production API"
    url: "https://api.example.com/health"
    threshold: 30
    alerts: ["console", "slack-alerts", "email", "file-log"]
```

### Multi-Environment Setup

```yaml
alerts:
  slack-dev:
    type: "slack"
    enabled: true
    description: "#dev-alerts channel"
    settings:
      webhook_url: "${SLACK_DEV_WEBHOOK}"
  
  slack-prod:
    type: "slack"
    enabled: true
    description: "#production-alerts channel"
    settings:
      webhook_url: "${SLACK_PROD_WEBHOOK}"
  
  email-oncall:
    type: "email"
    enabled: true
    description: "On-call rotation"
    settings:
      smtp_host: "smtp.gmail.com"
      smtp_port: 587
      from: "alerts@example.com"
      to: "oncall@example.com"
      password_env: "SMTP_PASSWORD"

targets:
  dev-api:
    name: "Dev API"
    url: "https://api.dev.example.com/health"
    alerts: ["slack-dev"]
  
  staging-api:
    name: "Staging API"
    url: "https://api.staging.example.com/health"
    alerts: ["slack-dev"]
  
  production-api:
    name: "Production API"
    url: "https://api.production.example.com/health"
    threshold: 30
    alerts: ["slack-prod", "email-oncall"]
```

### Tiered Alert Strategy

```yaml
alerts:
  console:
    type: "console"
    enabled: true
  
  slack-team:
    type: "slack"
    enabled: true
    description: "Team channel"
    settings:
      webhook_url: "${SLACK_TEAM_WEBHOOK}"
  
  slack-oncall:
    type: "slack"
    enabled: true
    description: "On-call channel (critical only)"
    settings:
      webhook_url: "${SLACK_ONCALL_WEBHOOK}"
  
  email-oncall:
    type: "email"
    enabled: true
    settings:
      smtp_host: "smtp.gmail.com"
      smtp_port: 587
      from: "critical@example.com"
      to: "oncall@example.com"
      password_env: "SMTP_PASSWORD"

targets:
  # Tier 1: Critical - All channels
  payment-api:
    name: "Payment API"
    url: "https://payments.example.com/health"
    threshold: 10
    alerts: ["console", "slack-team", "slack-oncall", "email-oncall"]
  
  # Tier 2: Important - Team + On-call Slack
  user-api:
    name: "User API"
    url: "https://users.example.com/health"
    threshold: 30
    alerts: ["console", "slack-team", "slack-oncall"]
  
  # Tier 3: Standard - Team Slack only
  analytics:
    name: "Analytics API"
    url: "https://analytics.example.com/health"
    threshold: 60
    alerts: ["console", "slack-team"]
  
  # Tier 4: Background - Console only
  background-job:
    name: "Background Job"
    url: "https://jobs.example.com/health"
    threshold: 120
    alerts: ["console"]
```

## Best Practices

### Alert Configuration

1. **Start Simple**: Begin with console alerts, add channels as needed
2. **Environment Segregation**: Different channels for dev/staging/prod
3. **Avoid Alert Fatigue**: Use appropriate thresholds and backoff
4. **Test Regularly**: Trigger test alerts to verify configuration
5. **Document Procedures**: Include runbooks in channel descriptions

### Slack Setup

1. **Dedicated Channels**: Use separate channels like `#alerts`, `#critical-alerts`
2. **Channel Notifications**: Configure @channel or @here for critical alerts
3. **Pin Information**: Pin runbooks and escalation procedures
4. **Integrate Tools**: Link to dashboards, logging, docs
5. **Mute During Maintenance**: Temporarily disable alerts during planned work

### Email Setup

1. **Distribution Lists**: Use groups/aliases, not individual emails
2. **App Passwords**: Never use your actual password
3. **Test Deliverability**: Verify emails aren't marked as spam
4. **Consider Alternatives**: Email can be delayed; Slack is usually faster
5. **Archive Rules**: Set up filters for alert emails

### File Logging

1. **Log Rotation**: Use logrotate or equivalent
2. **Permissions**: Restrict access to alert logs
3. **Backup**: Include in backup strategy
4. **Monitoring**: Monitor log file size
5. **Parsing**: Design format for easy parsing

### Multi-Channel Strategy

```yaml
# Critical: All channels
critical-alerts: ["console", "slack-oncall", "email", "file-log"]

# Important: Slack + File
important-alerts: ["console", "slack-team", "file-log"]

# Standard: Slack only
standard-alerts: ["console", "slack-team"]

# Development: Console only
dev-alerts: ["console"]
```

## Troubleshooting

### Slack Alerts Not Sending

1. **Verify webhook URL**: Test in browser or with curl
2. **Check network**: Ensure outbound HTTPS is allowed
3. **Verify enabled**: Check `enabled: true` in configuration
4. **Test webhook**:
   ```bash
   curl -X POST YOUR_WEBHOOK_URL \
     -H 'Content-Type: application/json' \
     -d '{"text":"Test message"}'
   ```

### Email Alerts Not Sending

1. **Check password**: Verify environment variable is set
2. **Try telnet**: `telnet smtp.gmail.com 587`
3. **Check credentials**: Verify from/to addresses
4. **Enable less secure apps**: Or use app-specific password
5. **Check spam folder**: Emails might be filtered

### Alerts Too Frequent

1. **Increase threshold**: Wait longer before first alert
2. **Check service**: Ensure service is actually healthy
3. **Network issues**: Investigate connectivity
4. **Use acknowledgements**: Stop alerts once acknowledged

### Missing Acknowledgement Notifications

1. **Verify acknowledgement strategies**: Check alert implements `AcknowledgementAwareAlert`
2. **Check all configured**: Console, Slack, Email, File all support acknowledgements
3. **Test directly**: Visit acknowledgement URL manually

## Next Steps

- Configure [Global Settings](./settings.md)
- Set up [Hooks](./hooks.md) for webhook-based monitoring
- Review [Targets](./targets.md) for check configuration

