Status: ready-for-agent
Blocked by: 02

# Compose web server with the bot process

Add validated web bind configuration, start the HTTP server from the existing binary, and shut it down with the Discord session and reminder scheduler.

## Acceptance Criteria

- `WEB_ADDR` defaults to `127.0.0.1:8080` and rejects invalid addresses.
- Listener failures terminate startup/run with a useful error.
- Signal shutdown drains HTTP requests with a bounded timeout.
- `.env.example`, container metadata and compose expose only the intended host-local endpoint.

## Comments

Implemented `WEB_ADDR`, same-process HTTP lifecycle, bounded graceful shutdown, host-local compose publishing, and CGO-free embedded assets.
