# Acknowledgement Console Notification Fix

## Problem

When an alert was acknowledged, the console alert strategy was not printing an acknowledgement notification, even though it was configured in the alerts list.

## Root Cause

The `ConsoleAlertStrategy` did not implement the `AcknowledgementAwareAlert` interface. Specifically, it was missing two required methods:

1. `SendAlertWithAck()` - Send alerts with acknowledgement URL
2. `SendAcknowledgement()` - Send acknowledgement confirmation

Similarly, the `ConsoleNotificationStrategy` (for hook notifications) was missing:

1. `HandleNotificationWithAck()` - Handle notifications with acknowledgement URL
2. `SendNotificationAcknowledgement()` - Send acknowledgement confirmation for hooks

## Solution

Added the missing methods to both strategies:

### ConsoleAlertStrategy (for target alerts)

```go
// SendAlertWithAck sends an alert to the console with acknowledgement URL
func (c *ConsoleAlertStrategy) SendAlertWithAck(ctx context.Context, target *Target, result *CheckResult, ackURL string) error

// SendAcknowledgement sends acknowledgement notification to the console
func (c *ConsoleAlertStrategy) SendAcknowledgement(ctx context.Context, target *Target, acknowledgedBy, note string) error
```

### ConsoleNotificationStrategy (for hook notifications)

```go
// HandleNotificationWithAck handles incoming notifications with acknowledgement URL by printing to console
func (c *ConsoleNotificationStrategy) HandleNotificationWithAck(ctx context.Context, notification *WebhookNotification, ackURL string) error

// SendNotificationAcknowledgement sends acknowledgement notification to the console
func (c *ConsoleNotificationStrategy) SendNotificationAcknowledgement(ctx context.Context, hookName, acknowledgedBy, note string) error
```

## Output Example

### Before Fix
When acknowledging an alert with console strategy configured:
- ✅ Acknowledgement was recorded
- ✅ Alerts stopped as expected
- ❌ **No console notification shown**

### After Fix
When acknowledging an alert with console strategy configured:
- ✅ Acknowledgement was recorded
- ✅ Alerts stopped as expected
- ✅ **Console notification displayed:**

```
✅ ACKNOWLEDGED: Alert for test-target has been acknowledged
   Target: test-target
   URL: https://example.com/health
   Acknowledged By: test-user
   Time: 2025-10-17 08:40:45
   Note: Investigating the issue
```

## Alert with Acknowledgement URL

Alerts now also display the acknowledgement URL when sent:

```
🚨 ALERT: test-target is DOWN - https://example.com/health (Status: 500, Time: 100ms)
   Target: test-target
   URL: https://example.com/health
   Time: 2025-10-17 08:40:37
   Response Time: 100ms
   Acknowledge: http://localhost:8080/api/acknowledge/abc123
```

## Files Modified

1. **strategies.go** - Added acknowledgement methods to:
   - `ConsoleAlertStrategy`
   - `ConsoleNotificationStrategy`

2. **types_test.go** - Added test:
   - `TestConsoleAlertStrategy_Acknowledgement`

## Testing

All tests pass, including the new acknowledgement test:

```bash
$ go test -v -run TestConsoleAlertStrategy_Acknowledgement
=== RUN   TestConsoleAlertStrategy_Acknowledgement
✅ ACKNOWLEDGED: Alert for test-target has been acknowledged
   Target: test-target
   URL: https://example.com/health
   Acknowledged By: test-user
   Time: 2025-10-17 08:40:45
   Note: Investigating the issue

--- PASS: TestConsoleAlertStrategy_Acknowledgement (0.00s)
PASS
```

## Behavior

Now when you acknowledge an alert:

1. **Web UI**: Shows HTML confirmation page
2. **Console**: Prints acknowledgement notification (if console alert is configured)
3. **Slack**: Sends acknowledgement message (if Slack alert is configured)
4. **Email**: Sends acknowledgement email (if email alert is configured)
5. **File**: Logs acknowledgement to file (if file alert is configured)

All alert strategies now consistently support acknowledgements!

