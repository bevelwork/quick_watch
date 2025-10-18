# Hooks Guide

Hooks are webhook endpoints that receive notifications from external systems. Unlike targets that actively check services, hooks passively wait for incoming notifications and trigger alerts based on that data.

## Table of Contents

- [What Are Hooks?](#what-are-hooks)
- [Managing Hooks](#managing-hooks)
- [Hook Configuration](#hook-configuration)
- [Triggering Hooks](#triggering-hooks)
- [Use Cases](#use-cases)
- [Examples](#examples)

## What Are Hooks?

Hooks enable **event-driven monitoring** where external systems notify Quick Watch about problems, rather than Quick Watch polling for them.

### When to Use Hooks

**Use Hooks For:**
- Deployment notifications from CI/CD pipelines
- Build failures from Jenkins, GitHub Actions, etc.
- Manual incident reports
- Third-party service webhooks
- Application-level errors
- Scheduled job failures
- Custom alerting from your applications

**Use Targets For:**
- Health check endpoints
- Service availability monitoring
- Response time tracking
- Port connectivity checks

### How Hooks Work

1. **Configure Hook**: Define hook name and alert channels
2. **Get Webhook URL**: Quick Watch generates unique URL
3. **Trigger Hook**: External system POSTs to webhook URL
4. **Alert Sent**: Quick Watch sends alert via configured channels
5. **Auto-Recovery**: Hook automatically recovers after specified duration

## Managing Hooks

### Interactive Editor

Edit hooks through the interactive editor:

```bash
quick-watch settings
```

Scroll to the `hooks` section in the configuration file.

### Configuration File

Hooks are stored in `watch-state.yml`:

```yaml
hooks:
  deployment-failed:
    name: "Deployment Failures"
    alerts: ["console", "slack-alerts"]
    duration: 600
    threshold: 60
```

## Hook Configuration

### Basic Hook

```yaml
hooks:
  my-hook:
    name: "My Custom Hook"
    alerts: ["console"]
    duration: 300
    threshold: 60
```

### Configuration Fields

| Field | Required | Type | Default | Description |
|-------|----------|------|---------|-------------|
| `name` | Yes | string | - | Human-readable hook name |
| `alerts` | Yes | array | - | Alert strategies to use |
| `duration` | No | integer | - | Auto-recovery time (seconds) |
| `threshold` | No | integer | 30 | Delay before first alert |

### name

The display name for the hook in alerts and dashboards.

```yaml
deployment-hook:
  name: "Production Deployment Notifications"
```

### alerts

List of alert strategies that receive notifications when the hook is triggered.

```yaml
deployment-hook:
  name: "Deployment Hook"
  alerts: ["console", "slack-alerts", "email"]
```

### duration

How long (in seconds) until the hook automatically recovers. After this time, the alert state clears even if not manually acknowledged.

```yaml
deployment-hook:
  name: "Deployment Hook"
  duration: 600  # Auto-recover after 10 minutes
  alerts: ["slack-alerts"]
```

**When to Use:**
- Short duration (60-300s): Deployment notifications, build failures
- Medium duration (300-900s): Job failures, batch process errors
- Long duration (900-3600s): Manual incident reports
- No duration: Manual acknowledgement required

### threshold

Seconds to wait before sending the first alert. Useful for preventing alerts from transient trigger attempts.

```yaml
deployment-hook:
  name: "Deployment Hook"
  threshold: 60  # Wait 1 minute before alerting
  alerts: ["slack-alerts"]
```

**Note:** Most hooks use `threshold: 0` since the trigger itself is the significant event.

## Triggering Hooks

### Webhook URL Format

```
http://{server}:{port}/hooks/{hook-name}
```

Example:
```
http://monitor.example.com:8090/hooks/deployment-failed
```

### HTTP Methods

**POST (Recommended):**
```bash
curl -X POST http://localhost:8090/hooks/deployment-failed \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Production deployment failed - rollback initiated"
  }'
```

**GET (Simple):**
```bash
curl "http://localhost:8090/hooks/deployment-failed?message=Deployment+failed"
```

### Request Parameters

| Parameter | Required | Method | Description |
|-----------|----------|--------|-------------|
| `message` | No | POST/GET | Custom alert message |
| `duration` | No | POST/GET | Override configured duration |

### Request Body (POST)

```json
{
  "message": "Custom alert message",
  "duration": 300
}
```

### Query Parameters (GET)

```
/hooks/deployment-failed?message=Build%20failed&duration=300
```

### Response

**Success (200 OK):**
```json
{
  "status": "triggered",
  "hook": "Deployment Failures",
  "message": "Production deployment failed",
  "recovery_time": "2025-10-17T15:30:00Z",
  "duration_seconds": 600,
  "acknowledgement_url": "http://monitor.example.com/api/acknowledge/abc123"
}
```

**Error (400 Bad Request):**
```json
{
  "error": "Hook not found: invalid-hook-name"
}
```

## Use Cases

### CI/CD Integration

**GitHub Actions:**

```yaml
# .github/workflows/deploy.yml
name: Deploy to Production

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Deploy
        run: ./deploy.sh
      
      - name: Notify on Failure
        if: failure()
        run: |
          curl -X POST ${{ secrets.QUICKWATCH_URL }}/hooks/github-deploy \
            -H "Content-Type: application/json" \
            -d "{
              \"message\": \"GitHub deployment failed for ${{ github.ref }}\",
              \"duration\": 600
            }"
```

**GitLab CI:**

```yaml
# .gitlab-ci.yml
deploy:
  stage: deploy
  script:
    - ./deploy.sh
  after_script:
    - |
      if [ $CI_JOB_STATUS == 'failed' ]; then
        curl -X POST https://monitor.example.com/hooks/gitlab-deploy \
          -H "Content-Type: application/json" \
          -d "{
            \"message\": \"GitLab deployment failed in $CI_PROJECT_NAME\",
            \"duration\": 600
          }"
      fi
```

**Jenkins:**

```groovy
// Jenkinsfile
pipeline {
    agent any
    stages {
        stage('Deploy') {
            steps {
                sh './deploy.sh'
            }
        }
    }
    post {
        failure {
            sh '''
                curl -X POST https://monitor.example.com/hooks/jenkins-deploy \
                  -H "Content-Type: application/json" \
                  -d '{"message":"Jenkins deployment failed","duration":600}'
            '''
        }
    }
}
```

### Build System Integration

**npm/Node.js:**

```json
{
  "scripts": {
    "build": "webpack --mode production || curl -X POST http://localhost:8090/hooks/build-failed -d '{\"message\":\"Frontend build failed\"}'",
    "test": "jest || curl -X POST http://localhost:8090/hooks/test-failed -d '{\"message\":\"Test suite failed\"}'"
  }
}
```

**Make:**

```makefile
.PHONY: deploy
deploy:
	./deploy.sh || \
	curl -X POST http://monitor.example.com/hooks/deploy-failed \
	  -H "Content-Type: application/json" \
	  -d '{"message":"Makefile deployment failed","duration":600}'
```

### Scheduled Jobs / Cron

**Cron with Error Checking:**

```bash
#!/bin/bash
# /etc/cron.daily/backup.sh

if ! /usr/local/bin/backup.sh; then
    curl -X POST http://monitor.example.com/hooks/backup-failed \
      -H "Content-Type: application/json" \
      -d "{
        \"message\": \"Daily backup failed on $(hostname)\",
        \"duration\": 3600
      }"
fi
```

**systemd Timer:**

```ini
# /etc/systemd/system/backup.service
[Unit]
Description=Daily Backup

[Service]
Type=oneshot
ExecStart=/usr/local/bin/backup.sh
ExecStopPost=/bin/bash -c 'if [ $EXIT_STATUS != 0 ]; then curl -X POST http://monitor.example.com/hooks/backup-failed -d "{\"message\":\"Backup failed\"}"; fi'
```

### Application Integration

**Python:**

```python
import requests

def notify_quickwatch(hook_name, message, duration=300):
    try:
        response = requests.post(
            f"http://monitor.example.com/hooks/{hook_name}",
            json={"message": message, "duration": duration}
        )
        response.raise_for_status()
    except Exception as e:
        print(f"Failed to notify QuickWatch: {e}")

# Usage
try:
    process_data()
except Exception as e:
    notify_quickwatch("data-processing-error", f"Data processing failed: {str(e)}")
```

**Node.js:**

```javascript
const axios = require('axios');

async function notifyQuickWatch(hookName, message, duration = 300) {
  try {
    await axios.post(
      `http://monitor.example.com/hooks/${hookName}`,
      { message, duration }
    );
  } catch (error) {
    console.error('Failed to notify QuickWatch:', error);
  }
}

// Usage
process.on('unhandledRejection', (error) => {
  notifyQuickWatch('node-unhandled-error', `Unhandled rejection: ${error.message}`);
});
```

**Go:**

```go
package main

import (
    "bytes"
    "encoding/json"
    "net/http"
)

func notifyQuickWatch(hookName, message string, duration int) error {
    payload := map[string]interface{}{
        "message":  message,
        "duration": duration,
    }
    
    jsonData, _ := json.Marshal(payload)
    
    _, err := http.Post(
        "http://monitor.example.com/hooks/"+hookName,
        "application/json",
        bytes.NewBuffer(jsonData),
    )
    
    return err
}

// Usage
func main() {
    if err := doWork(); err != nil {
        notifyQuickWatch("go-app-error", err.Error(), 600)
    }
}
```

### Manual Incident Reporting

**Bash Script:**

```bash
#!/bin/bash
# quick-incident.sh

HOOK_URL="http://monitor.example.com/hooks/manual-incident"

read -p "Incident description: " MESSAGE

curl -X POST "$HOOK_URL" \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"$MESSAGE\",\"duration\":3600}"

echo "Incident reported to QuickWatch"
```

**Slack Slash Command:**

Create a slash command that triggers your hook:

```bash
# Slack webhook handler (e.g., AWS Lambda, Cloud Functions)
# /incident Production database is slow

MESSAGE="$1"

curl -X POST "http://monitor.example.com/hooks/manual-incident" \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"Slack incident: $MESSAGE\",\"duration\":3600}"
```

### Third-Party Service Integration

**Sentry Error Tracking:**

Configure Sentry webhook to point to your hook:

```
Webhook URL: http://monitor.example.com/hooks/sentry-errors
```

**DataDog Alerts:**

Configure DataDog webhook integration:

```
http://monitor.example.com/hooks/datadog-alert?message={{alert_title}}
```

**PagerDuty:**

Use PagerDuty webhooks for escalation:

```
http://monitor.example.com/hooks/pagerduty-escalation
```

## Examples

### Complete Hook Configuration

```yaml
hooks:
  # Deployment notifications
  deployment-failed:
    name: "Production Deployment Failures"
    alerts: ["console", "slack-alerts", "email"]
    duration: 600
    threshold: 0
  
  # Build failures
  build-failed:
    name: "Build Failures"
    alerts: ["console", "slack-dev"]
    duration: 300
    threshold: 0
  
  # Test failures
  test-failed:
    name: "Test Suite Failures"
    alerts: ["console", "slack-dev"]
    duration: 300
    threshold: 0
  
  # Backup failures
  backup-failed:
    name: "Backup Job Failures"
    alerts: ["console", "slack-ops", "email"]
    duration: 3600
    threshold: 0
  
  # Manual incidents
  manual-incident:
    name: "Manual Incident Reports"
    alerts: ["console", "slack-oncall", "email"]
    duration: 3600
    threshold: 0
  
  # Application errors
  app-critical-error:
    name: "Application Critical Errors"
    alerts: ["console", "slack-oncall", "email"]
    duration: 1800
    threshold: 0
```

### CI/CD Complete Example

```yaml
hooks:
  github-deploy:
    name: "GitHub Deployment"
    alerts: ["slack-deployments"]
    duration: 600
  
  gitlab-deploy:
    name: "GitLab Deployment"
    alerts: ["slack-deployments"]
    duration: 600
  
  jenkins-build:
    name: "Jenkins Build"
    alerts: ["slack-ci"]
    duration: 300
```

**GitHub Actions Integration:**

```yaml
# .github/workflows/ci.yml
jobs:
  notify-failure:
    if: failure()
    runs-on: ubuntu-latest
    steps:
      - name: Notify QuickWatch
        run: |
          curl -X POST ${{ secrets.QUICKWATCH_URL }}/hooks/github-deploy \
            -H "Content-Type: application/json" \
            -d "{
              \"message\": \"${{ github.workflow }} failed on ${{ github.ref }}\",
              \"duration\": 600
            }"
```

### Multi-Environment Setup

```yaml
hooks:
  # Development
  dev-deploy-failed:
    name: "Dev Deployment Failures"
    alerts: ["console"]
    duration: 300
  
  # Staging
  staging-deploy-failed:
    name: "Staging Deployment Failures"
    alerts: ["slack-dev"]
    duration: 600
  
  # Production
  prod-deploy-failed:
    name: "Production Deployment Failures"
    alerts: ["slack-oncall", "email"]
    duration: 900
```

### Scheduled Jobs Monitoring

```yaml
hooks:
  daily-backup:
    name: "Daily Backup Job"
    alerts: ["console", "slack-ops"]
    duration: 3600  # 1 hour
  
  hourly-sync:
    name: "Hourly Data Sync"
    alerts: ["console"]
    duration: 1800  # 30 minutes
  
  monthly-report:
    name: "Monthly Report Generation"
    alerts: ["console", "email"]
    duration: 7200  # 2 hours
```

**Cron Integration:**

```bash
# /etc/cron.d/quickwatch-jobs

# Daily backup - 2 AM
0 2 * * * root /usr/local/bin/backup.sh || curl -X POST http://localhost:8090/hooks/daily-backup -d '{"message":"Backup failed"}'

# Hourly sync
0 * * * * app /usr/local/bin/sync.sh || curl -X POST http://localhost:8090/hooks/hourly-sync -d '{"message":"Sync failed"}'
```

## Best Practices

### Hook Naming

```yaml
# ✅ Good - descriptive names
github-deploy-prod
jenkins-build-frontend
backup-daily-db
sentry-critical-errors

# ❌ Bad - vague names
hook1
test
errors
thing
```

### Duration Selection

```yaml
# Quick recovery (1-5 minutes) - transient issues
build-failed:
  duration: 300

# Medium recovery (10-30 minutes) - deployment issues
deployment-failed:
  duration: 900

# Long recovery (1-2 hours) - scheduled jobs
backup-failed:
  duration: 3600

# Manual acknowledgement - critical incidents
manual-incident:
  duration: 0  # or omit duration
```

### Alert Strategy Selection

```yaml
# Development hooks - minimal alerts
dev-build:
  alerts: ["console"]

# Staging hooks - team notifications
staging-deploy:
  alerts: ["slack-dev"]

# Production hooks - multiple channels
prod-deploy:
  alerts: ["slack-oncall", "email"]

# Critical hooks - all channels
critical-error:
  alerts: ["console", "slack-oncall", "email", "file-log"]
```

### Security Considerations

1. **Authentication**: Quick Watch doesn't require authentication by default
   - Use reverse proxy for auth (Nginx, Traefik)
   - Use VPN or private networks
   - Use firewall rules to restrict access
   
2. **HTTPS**: Always use HTTPS in production
   ```yaml
   settings:
     server_address: "https://monitor.example.com"
   ```

3. **Rate Limiting**: Implement at reverse proxy level

4. **Input Validation**: Quick Watch sanitizes input, but be cautious with message content

### Error Handling

Always handle webhook failures gracefully:

```python
# ✅ Good - non-blocking notification
try:
    notify_quickwatch("error", str(e))
except:
    pass  # Don't let notification failure break your app

# ❌ Bad - blocking on notification
notify_quickwatch("error", str(e))  # Might hang/fail your app
```

### Testing Hooks

```bash
# Test hook with simple message
curl -X POST http://localhost:8090/hooks/test-hook \
  -d '{"message":"Test notification"}'

# Test with duration
curl -X POST http://localhost:8090/hooks/test-hook \
  -d '{"message":"Test notification","duration":60}'

# Test with GET
curl "http://localhost:8090/hooks/test-hook?message=Test"

# Verify response
curl -v -X POST http://localhost:8090/hooks/test-hook \
  -d '{"message":"Test"}' | jq
```

## Troubleshooting

### Hook Not Found

```json
{"error": "Hook not found: invalid-name"}
```

**Solutions:**
1. Check hook name matches configuration exactly
2. Verify hook is defined in `watch-state.yml`
3. Restart Quick Watch after configuration changes
4. Check for typos in URL

### Hook Not Triggering Alerts

1. **Verify alerts configured**:
   ```yaml
   my-hook:
     alerts: ["console", "slack-alerts"]
   ```

2. **Check alert strategies enabled**:
   ```yaml
   alerts:
     slack-alerts:
       enabled: true
   ```

3. **Test with console first**:
   ```yaml
   my-hook:
     alerts: ["console"]
   ```

4. **Check server logs** for errors

### Auto-Recovery Not Working

1. **Verify duration set**:
   ```yaml
   my-hook:
     duration: 600  # Must be present
   ```

2. **Wait for duration**: Auto-recovery happens after specified time

3. **Check acknowledgement**: Manual ack stops auto-recovery

### Webhook URL Not Working Externally

1. **Check server_address**:
   ```yaml
   settings:
     server_address: "https://monitor.example.com"
   ```

2. **Verify DNS resolution**
3. **Check firewall rules**
4. **Test from external network**
5. **Verify reverse proxy configuration**

## Webhook URL Discovery

Find webhook URLs for your hooks:

```bash
# Start server and check logs
quick-watch server

# Look for lines like:
# Hook route registered: /hooks/deployment-failed -> alerts=[slack-alerts]
# Hook route registered: /hooks/build-failed -> alerts=[console]
```

Or visit the API:

```bash
curl http://localhost:8090/api/status | jq
```

## Next Steps

- Review [Targets Guide](./targets.md) for active monitoring
- Configure [Alerts](./alerts.md) for hook notifications
- Set up [Settings](./settings.md) for webhook server configuration
- Integrate hooks with your CI/CD pipeline

