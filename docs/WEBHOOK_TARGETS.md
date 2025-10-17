# Webhook Targets

## Overview

Webhook targets allow you to manually trigger alerts via HTTP requests. Unlike regular HTTP targets that continuously monitor endpoints, webhook targets are **passive** - they wait to be triggered by external systems or manual intervention.

This is useful for:
- Manual incident reporting
- Integration with external monitoring systems
- Deployment notifications
- Custom alert triggers from scripts or CI/CD pipelines

## Key Features

- **Manual Triggering**: Trigger alerts via HTTP POST/GET requests
- **Auto-Recovery**: Optionally auto-recover after a specified duration
- **Acknowledgement Support**: Full support for alert acknowledgements
- **Custom Messages**: Include custom messages when triggering
- **Flexible Duration**: Set duration per trigger or per target configuration

## Configuration

### Basic Webhook Target

Add a webhook target to your `watch-state.yml`:

```yaml
targets:
  deployment-alert:
    name: deployment-alert
    url: deployment-alert  # Can be any identifier
    check_strategy: webhook
    duration: 300  # Auto-recover after 5 minutes (optional)
    alerts:
      - console
      - slack-alerts
      - email
```

### Configuration Fields

- **`name`** (required): Unique identifier for the target
- **`url`** (required): Identifier (not actually used for HTTP calls)
- **`check_strategy`** (required): Must be set to `"webhook"`
- **`duration`** (optional): Auto-recovery time in seconds
- **`alerts`** (optional): List of alert strategies to use (default: `["console"]`)

## Triggering Webhook Targets

### Endpoint

```
POST/GET /api/trigger/{target-name}
```

### Parameters

**Query Parameters or JSON Body:**
- `message` (string, optional): Custom message for the alert (default: "Webhook triggered")
- `duration` (int, optional): Override duration in seconds (overrides target's duration)

### Examples

#### Using cURL (GET with query params):

```bash
curl "http://localhost:8090/api/trigger/deployment-alert?message=Deployment+failed&duration=600"
```

#### Using cURL (POST with JSON):

```bash
curl -X POST http://localhost:8090/api/trigger/deployment-alert \
  -H "Content-Type: application/json" \
  -d '{"message": "Deployment failed on production", "duration": 600}'
```

#### From a Script:

```bash
#!/bin/bash
# Trigger alert if deployment fails
if ! ./deploy.sh; then
    curl -X POST http://localhost:8090/api/trigger/deployment-alert \
      -H "Content-Type: application/json" \
      -d "{\"message\": \"Deployment failed at $(date)\", \"duration\": 300}"
fi
```

#### From CI/CD (GitHub Actions example):

```yaml
- name: Trigger Alert on Failure
  if: failure()
  run: |
    curl -X POST http://monitoring.company.com:8090/api/trigger/ci-failure \
      -H "Content-Type: application/json" \
      -d '{"message": "CI pipeline failed for ${{ github.repository }}", "duration": 600}'
```

### Response

**Success (200 OK):**
```json
{
  "status": "triggered",
  "target": "deployment-alert",
  "message": "Deployment failed on production",
  "recovery_time": "2025-10-16T23:15:00Z",
  "duration_seconds": 300,
  "acknowledgement_url": "http://localhost:8090/api/acknowledge/a1b2c3d4e5f6"
}
```

**Error (400 Bad Request):**
```json
{
  "error": "target not found: deployment-alert"
}
```

## Auto-Recovery

### How It Works

When a webhook target is triggered with a `duration` set (either in the target config or in the trigger request):

1. **Alert Fires**: Target goes "down", alerts are sent with acknowledgement URLs
2. **Timer Starts**: Recovery timer begins counting down
3. **Auto-Recovery**: After the duration expires, target automatically recovers
4. **All-Clear Sent**: Recovery notifications are sent to all configured alerts

### Duration Priority

1. **Trigger duration** (from API request) - highest priority
2. **Target duration** (from configuration) - fallback
3. **No duration** - stays down until manually recovered

### Example with Auto-Recovery

```yaml
targets:
  maintenance-window:
    name: maintenance-window
    url: maintenance-window
    check_strategy: webhook
    duration: 3600  # Default: 1 hour
    alerts:
      - console
      - slack-alerts
```

Trigger with default duration (1 hour):
```bash
curl "http://localhost:8090/api/trigger/maintenance-window?message=Starting+maintenance"
```

Trigger with custom duration (30 minutes):
```bash
curl "http://localhost:8090/api/trigger/maintenance-window?message=Quick+maintenance&duration=1800"
```

## Acknowledgements

Webhook targets fully support the acknowledgement feature when `acknowledgements_enabled: true` in settings.

When a webhook target is triggered:

1. **Acknowledgement URL Generated**: Included in the trigger response and alerts
2. **Slack/Email/File Alerts**: Include clickable acknowledgement links
3. **Acknowledgement Workflow**: Same as regular targets
4. **Auto-Recovery Clears Ack**: When auto-recovery occurs, acknowledgement is cleared

### Example Workflow

```bash
# 1. Trigger webhook target
curl -X POST http://localhost:8090/api/trigger/deployment-alert \
  -H "Content-Type: application/json" \
  -d '{"message": "Deployment issue detected", "duration": 600}'

# Response includes:
# {
#   "acknowledgement_url": "http://localhost:8090/api/acknowledge/abc123def456"
# }

# 2. Team member acknowledges
curl "http://localhost:8090/api/acknowledge/abc123def456?by=Alice&note=Investigating+deployment"

# 3a. Auto-recovery after 10 minutes (duration: 600)
# OR
# 3b. Manual recovery by triggering with duration=0
```

## Use Cases

### 1. Deployment Notifications

```yaml
targets:
  deployment-production:
    name: deployment-production
    url: deployment-production
    check_strategy: webhook
    duration: 600  # 10 minutes
    alerts:
      - slack-alerts
      - email
```

```bash
# In deployment script
curl -X POST http://monitoring:8090/api/trigger/deployment-production \
  -d '{"message": "Production deployment started", "duration": 900}'
```

### 2. Manual Incident Reporting

```yaml
targets:
  manual-incident:
    name: manual-incident
    url: manual-incident
    check_strategy: webhook
    alerts:
      - console
      - slack-alerts
      - email
      - file
```

```bash
# Report incident manually
curl "http://localhost:8090/api/trigger/manual-incident?message=Database+performance+degraded"
```

### 3. External System Integration

```yaml
targets:
  external-monitor:
    name: external-monitor
    url: external-monitor
    check_strategy: webhook
    duration: 300
    alerts:
      - slack-alerts
```

```bash
# From external monitoring system
curl -X POST http://quick-watch:8090/api/trigger/external-monitor \
  -H "Content-Type: application/json" \
  -d "{\"message\": \"${ALERT_MESSAGE}\", \"duration\": ${ALERT_DURATION}}"
```

### 4. CI/CD Pipeline Alerts

```yaml
targets:
  ci-pipeline:
    name: ci-pipeline
    url: ci-pipeline
    check_strategy: webhook
    duration: 1800  # 30 minutes
    alerts:
      - slack-alerts
```

In `.github/workflows/main.yml`:
```yaml
- name: Notify on Failure
  if: failure()
  run: |
    curl -X POST ${{ secrets.QUICK_WATCH_URL }}/api/trigger/ci-pipeline \
      -H "Content-Type: application/json" \
      -d "{\"message\": \"Pipeline failed: ${{ github.workflow }}\", \"duration\": 1800}"
```

## Adding Webhook Targets

### Via Command Line

```bash
./quick_watch add "webhook-alert" \
  --check-strategy webhook \
  --threshold 0 \
  --alert-strategy slack-alerts
```

Then edit to add duration:
```bash
./quick_watch targets
# Add duration: 300 to the target
```

### Via Direct YAML Edit

Edit `watch-state.yml`:
```yaml
targets:
  my-webhook-alert:
    name: my-webhook-alert
    url: my-webhook-alert
    check_strategy: webhook
    duration: 600
    alerts:
      - console
      - slack-alerts
```

### Via Settings Editor

```bash
./quick_watch targets
```

Add:
```yaml
my-webhook-alert:
  url: my-webhook-alert
  check_strategy: webhook
  duration: 600
  alerts:
    - slack-alerts
```

## Testing

### 1. Add a test webhook target:

```bash
./quick_watch add "test-webhook" --check-strategy webhook
```

### 2. Enable acknowledgements (optional):

```bash
./quick_watch settings
# Set acknowledgements_enabled: true
```

### 3. Start the server:

```bash
./quick_watch server
```

### 4. Trigger the webhook target:

```bash
curl "http://localhost:8090/api/trigger/test-webhook?message=Test+alert&duration=30"
```

### 5. You should see:
- Alert notifications in configured channels
- Acknowledgement URL in the response (if enabled)
- Auto-recovery after 30 seconds

### 6. Test acknowledgement (if enabled):

```bash
# Copy acknowledgement URL from response
curl "http://localhost:8090/api/acknowledge/{token}?by=TestUser&note=Testing"
```

### 7. Verify:
- Acknowledgement notification sent
- Auto-recovery still happens after duration

## Differences from HTTP Targets

| Feature | HTTP Target | Webhook Target |
|---------|------------|----------------|
| Monitoring | Active (polls endpoint) | Passive (waits for trigger) |
| Check Strategy | `http` | `webhook` |
| URL Usage | Makes HTTP requests to URL | URL is just an identifier |
| Auto-Check | Every 5 seconds (configurable) | Never (manual only) |
| Triggering | Automatic (based on endpoint health) | Manual (via API call) |
| Auto-Recovery | When endpoint comes back up | Based on duration timer |
| Duration Field | Not used | Optional auto-recovery time |

## Best Practices

1. **Use Descriptive Names**: Choose clear, descriptive names for webhook targets
   ```yaml
   deployment-prod-api:  # Good
   webhook-1:            # Bad
   ```

2. **Set Reasonable Durations**: Choose durations appropriate for the incident type
   - Deployments: 5-15 minutes
   - Manual incidents: No duration (manual recovery)
   - Temporary issues: 1-5 minutes

3. **Include Meaningful Messages**: Provide context in trigger messages
   ```bash
   message="Production API deployment failed - rollback initiated"  # Good
   message="Error"                                                   # Bad
   ```

4. **Use Acknowledgements**: Enable acknowledgements for team coordination
   ```yaml
   settings:
     acknowledgements_enabled: true
   ```

5. **Configure Appropriate Alerts**: Choose alert channels based on severity
   ```yaml
   critical-incident:
     alerts: [slack-alerts, email, file]  # All channels
   
   minor-notice:
     alerts: [console]  # Just console
   ```

## Troubleshooting

**"Target not found" error:**
- Verify the target exists: `./quick_watch list`
- Check target name matches exactly (case-sensitive)

**"Target is not a webhook target" error:**
- Verify `check_strategy: webhook` in configuration
- Check with: `./quick_watch list`

**Auto-recovery not working:**
- Verify duration is set (in config or trigger request)
- Check server logs for errors
- Ensure server stays running (duration timer is in-memory)

**Acknowledgement URL not included:**
- Check `acknowledgements_enabled: true` in settings
- Verify alerts are configured on the target

**Alerts not sending:**
- Check alert configurations: `./quick_watch alerts`
- Verify alerts are enabled
- Check server logs for errors

