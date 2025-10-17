# Interactive Acknowledgement with Contact Forms

## Overview

The acknowledgement system has been enhanced with an interactive contact form that allows responders to immediately acknowledge alerts while providing their contact information to help coordinate the incident response.

## How It Works

### Two-Step Process

1. **Immediate Acknowledgement (GET)**
   - When someone clicks an acknowledgement URL, the alert is **immediately acknowledged**
   - This stops further alert notifications right away
   - A contact form is displayed to collect responder information

2. **Contact Information Submission (POST)**
   - Responder fills out the form with their details
   - Form submission distributes the information to all alert strategies
   - Team members are notified through all configured channels (Slack, email, console, etc.)

### Form Fields

1. **Your Name** (required)
   - Who's handling this issue

2. **Contact Me Here** (required)
   - Flexible contact field supporting:
     - Slack channels or DMs: `#incidents`, `@john`
     - Zoom links: `https://zoom.us/j/123456789`
     - Phone numbers: `+1-555-0100`
     - Email: `john@company.com`
     - Any other contact method

3. **Notes** (optional)
   - Investigation status
   - Action plan
   - Updates for the team

## User Experience

### Step 1: Click Acknowledgement Link

When an alert is sent, it includes an acknowledgement URL:

```
🚨 ALERT: haiven-healthcheck is DOWN
   Target: haiven-healthcheck
   URL: http://haiven-sbx-backend...
   Acknowledge: http://localhost:8080/api/acknowledge/abc123
```

### Step 2: Alert Immediately Acknowledged

Clicking the link immediately:
- ✅ Acknowledges the alert (stops further notifications)
- 🖥️ Shows a beautiful contact form

### Step 3: Fill Out Contact Form

The form collects:
- **Name**: John Doe
- **Contact**: Slack: @john, Zoom: https://zoom.us/j/123
- **Notes**: Investigating database connection. Will update in #incidents.

### Step 4: Team Notified

After submitting, all team members receive notifications with contact info:

**Console:**
```
✅ ACKNOWLEDGED: Alert for haiven-healthcheck has been acknowledged
   Target: haiven-healthcheck
   URL: http://haiven-sbx-backend...
   Acknowledged By: John Doe
   Time: 2025-10-17 08:53:35
   Contact: Slack: @john, Zoom: https://zoom.us/j/123
   Note: Investigating database connection. Will update in #incidents.
```

**Slack:**
```
✅ Alert acknowledged for haiven-healthcheck
• By: John Doe
• Contact: Slack: @john, Zoom: https://zoom.us/j/123
• Note: Investigating database connection. Will update in #incidents.

Target: haiven-healthcheck
URL: http://haiven-sbx-backend...
Time: October 17, 2025 8:53 AM
```

**Email:**
```
Subject: ✅ Alert Acknowledged: haiven-healthcheck

Alert Acknowledged
==================
Target: haiven-healthcheck
URL: http://haiven-sbx-backend...
Acknowledged By: John Doe
Time: 2025-10-17 08:53:35
Contact: Slack: @john, Zoom: https://zoom.us/j/123
Note: Investigating database connection. Will update in #incidents.
```

## Benefits

1. **Immediate Response**: Alert acknowledged instantly (stops spam)
2. **Team Coordination**: Everyone knows who's handling it and how to reach them
3. **Flexible Contact Info**: Support for Slack, Zoom, phone, email, or any contact method
4. **Universal Distribution**: Contact info sent to all configured alert channels
5. **Investigation Notes**: Share status updates with the team
6. **Beautiful UI**: Modern, responsive form design

## Technical Implementation

### Data Model

Added to `TargetState` and `HookState`:
- `AcknowledgementContact string` - Contact information

### API Endpoints

**GET /api/acknowledge/{token}**
- Immediately acknowledges alert
- Shows interactive contact form

**POST /api/acknowledge/{token}**
- Accepts form data: `name`, `contact`, `notes`
- Updates acknowledgement information
- Sends notifications to all alert strategies

### Alert Strategy Updates

All alert strategies now support the contact field:
- `ConsoleAlertStrategy.SendAcknowledgement(ctx, target, name, note, contact)`
- `SlackAlertStrategy.SendAcknowledgement(ctx, target, name, note, contact)`
- `EmailAlertStrategy.SendAcknowledgement(ctx, target, name, note, contact)`
- `FileAlertStrategy.SendAcknowledgement(ctx, target, name, note, contact)`

### Form Updates Supported

The form can be submitted multiple times to update information:
- First submission: Provides initial contact info
- Subsequent submissions: Updates contact info and notes
- Each submission triggers new notifications with updated information

## Example Scenarios

### Scenario 1: Quick Acknowledgement

1. Alert fires for `database-down`
2. Alice clicks acknowledgement link
3. Alert stops immediately
4. Alice fills out: Name=Alice, Contact=Slack: @alice
5. Team sees: "Alice is handling it, contact her on Slack @alice"

### Scenario 2: Coordination with Zoom

1. Alert fires for `api-timeout`
2. Bob acknowledges
3. Bob starts a Zoom meeting for incident response
4. Bob submits: Contact=Zoom: https://zoom.us/j/987654321
5. Team joins the Zoom call to help

### Scenario 3: Handoff

1. Alert fires, Charlie acknowledges
2. Charlie submits: Name=Charlie, Contact=Slack: @charlie
3. Charlie investigates, then hands off to Dana
4. Dana visits same URL (still valid)
5. Dana updates: Name=Dana, Contact=Slack: @dana, Notes=Charlie identified root cause, deploying fix
6. Team gets update with new contact info

## Configuration

No configuration needed! The feature works automatically when acknowledgements are enabled:

```yaml
settings:
  acknowledgements_enabled: true
```

## Files Modified

1. **types.go**
   - Added `AcknowledgementContact` field to `TargetState` and `HookState`
   - Updated `AcknowledgeAlert()` signature to accept contact parameter
   - Updated `ClearAcknowledgement()` to clear contact field

2. **strategies.go**
   - Updated all `SendAcknowledgement()` methods to accept contact parameter
   - Updated all notification strategies to display contact information

3. **server.go**
   - Rewrote `handleAcknowledge()` to support GET (form) and POST (submission)
   - Added `showAcknowledgementForm()` - displays interactive form
   - Added `showAcknowledgementSuccess()` - displays success message

4. **types_test.go**
   - Updated tests to include contact parameter

## UI Preview

The acknowledgement form features:
- ✅ Clean, modern design
- 📱 Responsive layout
- 🎨 Green gradient header
- 📝 Clear field labels with helper text
- ⚡ Real-time validation
- 🎬 Smooth animations
- ✨ Professional styling

## Testing

All tests pass including the new acknowledgement test with contact information:

```bash
$ go test -v -run TestConsoleAlertStrategy_Acknowledgement
=== RUN   TestConsoleAlertStrategy_Acknowledgement
✅ ACKNOWLEDGED: Alert for test-target has been acknowledged
   Contact: Slack: @testuser
   Note: Investigating the issue
--- PASS: TestConsoleAlertStrategy_Acknowledgement (0.00s)
PASS
```

## Summary

The interactive acknowledgement form transforms incident response by:
1. **Stopping alert spam immediately** when someone responds
2. **Enabling team coordination** with contact information
3. **Supporting any contact method** (Slack, Zoom, phone, email, etc.)
4. **Distributing updates universally** across all alert channels
5. **Providing a beautiful UX** with modern web forms

This helps teams respond faster and coordinate more effectively during incidents!

