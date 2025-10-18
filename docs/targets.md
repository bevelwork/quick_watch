# Targets Guide

Targets are the services, endpoints, or servers you want to monitor. Quick Watch checks each target every 5 seconds (configurable) and alerts you when problems occur.

## Table of Contents

- [Managing Targets](#managing-targets)
- [Target Configuration](#target-configuration)
- [Check Strategies](#check-strategies)
- [Threshold-Based Alerting](#threshold-based-alerting)
- [Exponential Backoff](#exponential-backoff)
- [Target Dashboard](#target-dashboard)
- [Examples](#examples)

## Managing Targets

### Interactive Editor

The easiest way to manage targets is through the interactive editor:

```bash
quick-watch targets
```

This opens your `$EDITOR` with the current configuration and helpful comments. Changes are applied immediately when you save and close the editor.

### Configuration File

Targets are stored in `watch-state.yml`:

```yaml
targets:
  api-health:
    name: "API Health Check"
    url: "https://api.example.com/health"
    method: "GET"
    threshold: 30
    check_strategy: "http"
    alerts: ["console", "slack-alerts"]
```

## Target Configuration

### Required Fields

- **name**: Human-readable name for the target
- **url**: URL to check (for HTTP) or hostname (for TCP)

### Optional Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `method` | string | `"GET"` | HTTP method (GET, POST, PUT, etc.) |
| `threshold` | integer | `30` | Seconds of downtime before first alert |
| `check_strategy` | string | `"http"` | Check type: `http`, `tcp`, or `webhook` |
| `alerts` | array | `["console"]` | List of alert strategies to use |
| `status_codes` | array | `["2xx", "3xx"]` | Expected HTTP status codes |
| `headers` | object | `{}` | Custom HTTP headers |
| `ports` | array | `[]` | TCP ports to check (for TCP strategy) |
| `duration` | integer | - | Auto-recovery duration (for webhooks) |

### Full Example

```yaml
api-health:
  name: "Production API Health"
  url: "https://api.example.com/health"
  method: "POST"
  threshold: 60
  check_strategy: "http"
  alerts: ["console", "slack-alerts", "email"]
  status_codes: ["200", "201"]
  headers:
    Authorization: "Bearer secret-token"
    User-Agent: "QuickWatch/1.0"
```

## Check Strategies

### HTTP Check Strategy

Monitors web services and APIs via HTTP/HTTPS requests.

**Configuration:**

```yaml
web-service:
  name: "Web Service"
  url: "https://example.com/health"
  method: "GET"
  check_strategy: "http"
  threshold: 30
  status_codes: ["200", "2xx"]
  headers:
    Authorization: "Bearer token"
```

**Features:**
- Follows redirects automatically
- Captures response time, size, and body
- Supports custom headers and methods
- Validates status codes
- Records full response body (up to 10KB) for debugging

**Status Code Matching:**
- Exact codes: `"200"`, `"201"`, `"404"`
- Wildcards: `"2xx"`, `"3xx"`, `"4xx"`, `"5xx"`
- Multiple codes: `["200", "201", "204"]`

### TCP Check Strategy

Monitors server connectivity by checking if TCP ports are open.

**Configuration:**

```yaml
database-ports:
  name: "Database Ports"
  url: "db.example.com"
  check_strategy: "tcp"
  ports:
    - 5432  # PostgreSQL
    - 6379  # Redis
  threshold: 30
  alerts: ["console", "slack-alerts"]
```

**Features:**
- Checks multiple ports simultaneously
- Reports which ports are open/closed
- Measures connection time per port
- No HTTP overhead - pure TCP connection testing

**Use Cases:**
- Database connectivity (PostgreSQL, MySQL, Redis)
- Custom application ports
- Network service availability
- Load balancer health checks

### Webhook Check Strategy

Receives notifications from external systems instead of actively polling.

**Configuration:**

```yaml
deployment-hook:
  name: "Deployment Notifications"
  url: "deployment-hook"
  check_strategy: "webhook"
  duration: 300
  threshold: 60
  alerts: ["console", "slack-alerts"]
```

**Features:**
- Passive monitoring - waits for external triggers
- Auto-recovery after specified duration
- Useful for deployment notifications, CI/CD pipelines
- Supports acknowledgements

**Triggering Webhooks:**

```bash
# POST to trigger the webhook
curl -X POST http://localhost:8090/api/trigger/deployment-hook \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Deployment failed in production",
    "duration": 300
  }'
```

## Threshold-Based Alerting

Thresholds prevent alerts from triggering on transient failures. A service must be continuously down for the threshold duration before the first alert is sent.

### How It Works

1. First check fails → timer starts
2. Subsequent checks continue failing → timer continues
3. Timer reaches threshold → **First alert sent**
4. Service continues failing → Exponential backoff applies
5. Service recovers → Timer resets

### Configuration

```yaml
api-health:
  name: "API Health"
  url: "https://api.example.com/health"
  threshold: 60  # Wait 60 seconds before first alert
  alerts: ["slack-alerts"]
```

### Benefits

- **Reduces noise**: One slow response won't trigger alerts
- **Prevents false positives**: Network blips don't cause pages
- **Configurable per target**: Critical services can have lower thresholds
- **Visual feedback**: Dashboard shows "down" immediately, but alerts wait for threshold

### Default Threshold

Set a global default in settings:

```yaml
settings:
  default_threshold: 30  # 30 seconds for all targets
```

Individual targets can override this:

```yaml
critical-service:
  name: "Critical Service"
  url: "https://critical.example.com"
  threshold: 10  # Lower threshold for critical service
  alerts: ["slack-alerts", "email"]

background-job:
  name: "Background Job"
  url: "https://jobs.example.com/health"
  threshold: 120  # Higher threshold for less critical service
  alerts: ["console"]
```

## Exponential Backoff

After the first alert, Quick Watch uses exponential backoff to increase the time between subsequent alerts, preventing alert fatigue.

### Backoff Schedule

- First alert: At threshold (e.g., 30 seconds)
- Second alert: +5 seconds
- Third alert: +10 seconds  
- Fourth alert: +20 seconds
- Fifth alert: +40 seconds
- Sixth alert: +80 seconds
- And so on... (doubles each time)

### Example Timeline

For a service with 30-second threshold:

```
00:00 - Service goes down
00:30 - Alert #1 (threshold reached)
00:35 - Alert #2 (+5s backoff)
00:45 - Alert #3 (+10s backoff)
01:05 - Alert #4 (+20s backoff)
01:45 - Alert #5 (+40s backoff)
03:05 - Alert #6 (+80s backoff)
```

### Acknowledgements

When you acknowledge an alert:
- Backoff stops immediately
- No more alerts sent until service recovers
- You can provide contact information for team coordination

### Alert Count

Each alert includes the count in the message:

```
🚨 Alert #3: API Health Check is DOWN
Target: https://api.example.com/health
Error: Connection timeout
Downtime: 2m 15s
```

This helps you understand:
- How long the issue has persisted
- When to escalate (e.g., Alert #5 = serious issue)
- Alert fatigue is being managed

## Target Dashboard

### Main Dashboard (/)

Shows all targets with:
- Real-time status (✅ Healthy, ❌ Down, 🔔 Acknowledged)
- Last check timestamp
- Response time
- Check strategy badge
- Auto-refresh every 5 seconds

**Features:**
- Unhealthy targets automatically sorted to top
- Live filtering by name or URL
- Click any target to see detailed history

### Individual Target Pages (/targets/{name})

Each target has a dedicated page with:

**📈 Response Time Graph**
- Last 100 checks
- Interactive Chart.js visualization
- Green for healthy, red for failures
- Hover for detailed timestamps

**📊 Performance Statistics**
- Average page size
- P95 response time
- Total checks performed

**📋 GitHub Actions-Style Log**
- Most recent entries at top
- Click to expand for full details
- Shows response bodies (great for JSON health endpoints)
- Maintains expanded state during live updates

**🎯 Target Configuration**
- Expandable "Show Details" section
- Check strategy, method, threshold
- Custom headers, ports (for TCP)
- Alert strategies

## Examples

### Simple HTTP Health Check

```yaml
api-health:
  name: "API Health"
  url: "https://api.example.com/health"
  threshold: 30
  alerts: ["console"]
```

### Production API with Authentication

```yaml
production-api:
  name: "Production API"
  url: "https://api.production.com/v1/health"
  method: "GET"
  threshold: 60
  check_strategy: "http"
  status_codes: ["200"]
  headers:
    Authorization: "Bearer ${API_TOKEN}"
    X-Environment: "production"
  alerts: ["slack-alerts", "email"]
```

### Database Port Monitoring

```yaml
database-cluster:
  name: "Database Cluster"
  url: "db.production.internal"
  check_strategy: "tcp"
  ports:
    - 5432   # PostgreSQL primary
    - 5433   # PostgreSQL replica
    - 6379   # Redis
    - 11211  # Memcached
  threshold: 30
  alerts: ["slack-alerts", "email"]
```

### Multiple Environments

```yaml
staging-api:
  name: "Staging API"
  url: "https://api.staging.example.com/health"
  threshold: 120
  alerts: ["console"]

production-api:
  name: "Production API"
  url: "https://api.production.example.com/health"
  threshold: 30
  alerts: ["slack-alerts", "email"]
```

### External Service Dependencies

```yaml
stripe-api:
  name: "Stripe API"
  url: "https://api.stripe.com/healthcheck"
  threshold: 60
  alerts: ["console", "slack-alerts"]

sendgrid-api:
  name: "SendGrid API"
  url: "https://api.sendgrid.com/v3/health"
  threshold: 60
  headers:
    Authorization: "Bearer ${SENDGRID_API_KEY}"
  alerts: ["console", "slack-alerts"]
```

### Webhook for CI/CD

```yaml
deployment-notifications:
  name: "Deployment Notifications"
  url: "deployments"
  check_strategy: "webhook"
  duration: 600
  threshold: 60
  alerts: ["slack-alerts"]
```

Trigger from your CI/CD:

```bash
# Notify of deployment failure
curl -X POST https://monitor.example.com/api/trigger/deployments \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Production deployment failed - rollback initiated",
    "duration": 600
  }'
```

## Best Practices

### Threshold Configuration

- **Critical services**: 10-30 seconds
- **Standard services**: 30-60 seconds
- **Background jobs**: 60-120 seconds
- **External APIs**: 60-120 seconds

### Check Strategies

- Use **HTTP** for web services and APIs
- Use **TCP** for databases and custom ports
- Use **webhook** for event-driven monitoring

### Alert Configuration

- Start with `console` for testing
- Add `slack-alerts` for team notifications
- Add `email` for critical services
- Use multiple channels for redundancy

### Naming Conventions

```yaml
# Good names:
production-api-health
staging-database-ports
stripe-payment-api
frontend-website

# Avoid:
api
db
test
thing
```

### Organization

Group related targets:

```yaml
# Frontend
frontend-web:
  name: "Frontend Website"
  url: "https://example.com"

frontend-cdn:
  name: "Frontend CDN"
  url: "https://cdn.example.com"

# Backend
backend-api:
  name: "Backend API"
  url: "https://api.example.com/health"

backend-admin:
  name: "Admin API"
  url: "https://admin-api.example.com/health"

# Infrastructure
database-primary:
  name: "Database Primary"
  url: "db-primary.internal"
  check_strategy: "tcp"
  ports: [5432]

database-replica:
  name: "Database Replica"
  url: "db-replica.internal"
  check_strategy: "tcp"
  ports: [5432]
```

## Troubleshooting

### Target Not Appearing

1. Check YAML syntax: `quick-watch validate`
2. Verify target has required fields (`name`, `url`)
3. Check server logs for errors
4. Restart server if needed

### False Positives

- Increase `threshold` to wait longer before alerting
- Check `status_codes` configuration
- Verify service is actually healthy
- Check for network issues

### Alerts Not Firing

1. Verify target has `alerts` configured
2. Check alert strategies are enabled
3. Confirm threshold settings
4. Test alert configuration: `quick-watch alerts`

### Slow Response Times

- Check target's actual performance
- Verify network latency
- Consider increasing timeout (future feature)
- Check if service is under load

## Next Steps

- Configure [Alerts](./alerts.md) for your targets
- Set up [Global Settings](./settings.md)
- Explore [Hooks](./hooks.md) for webhook-based monitoring

