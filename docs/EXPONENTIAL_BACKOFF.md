# Exponential Backoff Alert System

## Overview

Quick Watch now implements an exponential backoff mechanism for repeated alerts. This prevents alert fatigue by progressively increasing the time between notifications when a target remains down.

## How It Works

### Initial Failure
When a target first goes down:
- An alert is sent immediately
- `FailureCount` is set to 1
- `LastAlertTime` is recorded

### Subsequent Failures
For each check while the target remains down:
- `FailureCount` increments
- The backoff interval is calculated using the formula: **5 × 2^(FailureCount-1) seconds**
- An alert is only sent if enough time has passed since the last alert

### Backoff Schedule
| Failure # | Backoff Time | Cumulative Time |
|-----------|--------------|-----------------|
| 1         | 5 seconds    | 5 seconds       |
| 2         | 10 seconds   | 15 seconds      |
| 3         | 20 seconds   | 35 seconds      |
| 4         | 40 seconds   | 75 seconds      |
| 5         | 80 seconds   | ~2.5 minutes    |
| 6         | 160 seconds  | ~5 minutes      |
| 7         | 320 seconds  | ~10 minutes     |
| 8         | 640 seconds  | ~21 minutes     |

### Alert Acknowledgement
When an alert is acknowledged:
- **All subsequent alerts are stopped** for that target
- The target remains in "down" state
- Alerts will **not resume** even if the target stays down
- Alerts will only resume once the target recovers and then goes down again

### Recovery
When a target recovers (comes back up):
- `FailureCount` is reset to 0
- `LastAlertTime` is cleared
- Acknowledgement is cleared
- An "all clear" notification is sent

## Integration with Existing Features

### Acknowledgement System
The exponential backoff works seamlessly with the existing acknowledgement system:
- When an acknowledgement token is generated, it remains valid
- Once acknowledged, no more alerts are sent (regardless of backoff schedule)
- The acknowledgement URL is included in all alerts (if acknowledgements are enabled)

### Webhook Targets
Webhook targets also support exponential backoff:
- Initial trigger sends an alert immediately
- If the webhook target remains down (not auto-recovered), backoff applies
- Recovery resets all counters

## Example Scenario

```
Time    Event                           Action
-----   -----------------------------   ----------------------------------
00:00   Target goes down                Alert sent immediately (failure #1)
00:05   Still down, 5s passed          Alert sent (failure #2)
00:15   Still down, 10s passed         Alert sent (failure #3)
00:35   Still down, 20s passed         Alert sent (failure #4)
01:15   Still down, 40s passed         Alert sent (failure #5)
02:35   Still down, 80s passed         Alert sent (failure #6)
02:40   User acknowledges alert        Alerts STOP (even though still down)
05:00   Still down                     No alert (acknowledged)
10:00   Still down                     No alert (acknowledged)
11:00   Target recovers                All clear sent, counters reset
11:30   Target goes down again         Alert sent immediately (new incident)
```

## Configuration

No additional configuration is required. Exponential backoff is automatically enabled for all targets.

To enable alert acknowledgements (which stop alerts when acknowledged), set in your `watch-state.yml`:

```yaml
settings:
  acknowledgements_enabled: true
```

## Technical Details

### State Tracking
Two new fields were added to `TargetState`:
- `FailureCount int` - Tracks consecutive failures
- `LastAlertTime *time.Time` - Records when the last alert was sent

### Backoff Calculation
```go
backoffSeconds := 5 * (1 << uint(state.FailureCount-1))
backoffDuration := time.Duration(backoffSeconds) * time.Second
```

### Alert Decision Logic
Alerts are sent only when:
1. Target is down
2. Not acknowledged
3. Sufficient time has passed: `time.Since(*LastAlertTime) >= backoffDuration`

## Benefits

1. **Reduced Alert Fatigue**: Fewer repeated notifications for persistent issues
2. **Faster Initial Response**: First alert sent immediately
3. **Acknowledgement-Aware**: Respects when someone is investigating
4. **Automatic Recovery**: Resets when service recovers
5. **No Configuration Required**: Works out of the box

## Testing

The implementation includes comprehensive tests:
- `TestTargetState_ExponentialBackoff` - Verifies backoff calculation
- `TestTargetState_BackoffTiming` - Validates timing logic

Run tests with:
```bash
go test -v -run TestTargetState
```

