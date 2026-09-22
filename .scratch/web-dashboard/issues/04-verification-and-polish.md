Status: ready-for-agent
Blocked by: 01, 02, 03

# Verify dashboard behavior and deployment

Complete integration coverage, run formatting/static tests, and verify the production build contains templates and assets.

## Acceptance Criteria

- `gofmt`, `go test ./...` and `go vet ./...` pass.
- Production binary builds with `CGO_ENABLED=0`.
- Existing Discord behavior and its 20-row search cap remain unchanged.
- Feature spec and implementation agree; no extra dependency is added.

## Comments

Verified with desktop/mobile browser QA, `go test ./...`, `go vet ./...`, `git diff --check`, and a `CGO_ENABLED=0` production build. No dependency added.
