# Server Address Configuration

## Overview

The `server_address` setting allows you to configure the public-facing URL used in alert acknowledgement links and webhook URLs. This prevents sending localhost or private IP addresses in alerts sent to remote team members.

## Problem Solved

**Before this feature:**
- Acknowledgement URLs defaulted to `http://localhost:8080/api/acknowledge/...`
- Team members receiving alerts via Slack, email, etc. couldn't click acknowledgement links
- Links would only work on the machine running Quick Watch

**After this feature:**
- Acknowledgement URLs use your public server address
- Team members can acknowledge alerts from anywhere
- Works with reverse proxies, load balancers, and cloud deployments

## Configuration

Add the `server_address` field to your settings in `watch-state.yml`:

```yaml
settings:
  webhook_port: 8080
  server_address: "https://monitor.example.com:8080"  # Your public URL
  acknowledgements_enabled: true
```

## Examples

### Behind a Reverse Proxy

If you're using nginx/Apache to proxy requests:

```yaml
settings:
  webhook_port: 8080
  server_address: "https://monitoring.company.com"  # No port needed
  acknowledgements_enabled: true
```

Your reverse proxy might listen on 443 and forward to localhost:8080.

### Cloud Deployment

For a cloud VM or container with a public IP/domain:

```yaml
settings:
  webhook_port: 8080
  server_address: "https://monitor.example.com:8080"
  acknowledgements_enabled: true
```

### Docker with Port Mapping

If you're running in Docker with port mapping (e.g., `-p 9000:8080`):

```yaml
settings:
  webhook_port: 8080
  server_address: "http://your-server-ip:9000"
  acknowledgements_enabled: true
```

### Local Development

For local testing, omit the `server_address` field:

```yaml
settings:
  webhook_port: 8080
  acknowledgements_enabled: true
  # server_address not set - uses http://localhost:8080
```

## How It Works

1. **Without `server_address` configured:**
   - Quick Watch detects the `webhook_port` setting
   - Generates URLs like: `http://localhost:8080/api/acknowledge/abc123`
   - Suitable only for local testing

2. **With `server_address` configured:**
   - Quick Watch uses the configured address
   - Generates URLs like: `https://monitor.example.com:8080/api/acknowledge/abc123`
   - Team members can access these URLs from anywhere

## Testing

The implementation includes a comprehensive test (`TestServerAddressConfiguration`) that verifies:
- Custom server addresses are properly used in acknowledgement URLs
- Empty server addresses fall back to relative paths
- Localhost defaults work correctly

## Related Features

This setting affects:
- **Acknowledgement URLs** in alerts (console, Slack, email, file logs)
- **Webhook notification acknowledgements**
- **All alert strategies** that support acknowledgements

## Security Considerations

- Use HTTPS in production: `server_address: "https://..."`
- Ensure your server is accessible to your team but protected from unauthorized access
- Consider using authentication at the reverse proxy level
- The acknowledgement tokens are randomly generated and single-use per alert

## Migration

Existing configurations will continue to work:
- If `server_address` is not set, Quick Watch uses the previous default behavior
- No breaking changes - this is a purely additive feature
- Update your configuration when you're ready to deploy acknowledgements to production

