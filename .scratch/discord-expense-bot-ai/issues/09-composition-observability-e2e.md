Status: resolved
Blocked by: 07, 08

# Composition, observability, and end-to-end validation

Wire the executable, validate configuration before connecting Discord, add graceful shutdown and redacted operational logging, and prove the complete offline flow.

## Acceptance Criteria

- Startup composes SQLite, provider, orchestrator, Discord, and reminder components only after configuration validation.
- Logs include action, success/failure, latency, provider status, and stable source/request IDs without prompts, secrets, or full notes.
- One deterministic end-to-end test proves message to multiple tools to atomic SQLite batch to Vietnamese confirmation.
- Provider failure leaves the DB unchanged; duplicate Discord delivery replays the existing result.
- `go test ./...` and `go vet ./...` pass.
