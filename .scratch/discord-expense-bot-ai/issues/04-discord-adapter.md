Status: ready-for-human
Blocked by: 01

# Discord boundary adapter

Implement the `discordgo` gateway/sender adapter and reject unauthorized guilds, channels, users, DMs, and bot/self messages before any provider call.

## Acceptance Criteria

- Every authorized channel message is accepted without mentions, commands, or prefixes.
- Unauthorized events are rejected before application/provider work.
- Vietnamese text, files, and button interactions can be sent only to the configured private channel.
- Handler registration and send failures are testable without a live Discord connection.
