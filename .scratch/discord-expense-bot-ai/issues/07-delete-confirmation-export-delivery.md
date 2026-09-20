Status: ready-for-human
Blocked by: 06

# Delete confirmation and export delivery

Implement five-minute Discord button confirmation for deletion and private-channel CSV delivery with immediate local temporary-file cleanup.

## Acceptance Criteria

- No deletion occurs before a valid button confirmation.
- Confirmation binds the resolved target set to trusted user/channel context, expires after five minutes, and cannot be replayed.
- Confirmed multi-target deletion is atomic and bypasses a second AI request.
- CSV delivery is restricted to the configured private channel.
- Temporary exports are removed after successful and failed sends.
- Tests cover expiry, tampering, replay, atomic deletion, delivery failure, content, and cleanup.
