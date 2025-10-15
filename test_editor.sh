#!/bin/bash
# Mock editor for testing quick_watch edit functionality

echo "Mock editor opened file: $1"
echo "Making changes to the file..."

# Read the file, make some changes, and write back
cat > "$1" << 'EOF'
# Quick Watch Configuration
# Edit this file to modify your monitors
#
# To add a new monitor, add a new entry under 'monitors:'
# To remove a monitor, delete its entry
# To modify a monitor, edit its properties
#
# Example monitor entry:
#   my-api:
#     name: "My API"
#     url: "https://api.example.com/health"
#     method: "GET"
#     headers:
#       Authorization: "Bearer token"
#     threshold: 30
#     check_strategy: "http"
#     alert_strategy: "console"
#

version: "1.0"
monitors:
  https://httpbin.org/status/200:
    name: "API Health Check"
    url: "https://httpbin.org/status/200"
    method: "GET"
    headers: {}
    threshold: 30
    check_strategy: "http"
    alert_strategy: "console"
  https://httpbin.org/delay/1:
    name: "Slow Service"
    url: "https://httpbin.org/delay/1"
    method: "GET"
    headers: {}
    threshold: 45
    check_strategy: "http"
    alert_strategy: "console"
  https://httpbin.org/status/404:
    name: "Error Service"
    url: "https://httpbin.org/status/404"
    method: "GET"
    headers: {}
    threshold: 20
    check_strategy: "http"
    alert_strategy: "console"
settings:
  webhook_port: 8080
  webhook_path: "/webhook"
  check_interval: 5
  default_threshold: 30
strategies:
  check:
    http:
      timeout: 10
      follow_redirects: true
  alert:
    console:
      format: "detailed"
EOF

echo "Mock editor saved changes and exiting..."
