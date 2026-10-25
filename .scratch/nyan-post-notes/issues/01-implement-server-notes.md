Status: ready-for-agent
Implementation: complete

# Implement server-persisted notes in Nyan Post

Implement the complete feature described in [the spec](../spec.md).

The work includes a dedicated SQLite notes database and versioned migration, authenticated bounded JSON CRUD endpoints, the `Ghi chú` tab with accessible plain-text editing/listing behavior, and persistent Compose configuration for production and development. Keep notes shared behind the existing Nyan Post password. Keep the notes database separate from the bot ledger database.

Test the HTTP behavior and SQLite persistence, including auth, validation, Unicode, CRUD, ordering, bounded listing, and regression behavior for Discord posting. Run the repository Go checks and validate Compose configuration where available.

## Comments

- 2026-10-24: Implemented on the Nyan Post notes branch. `make ci`, embedded JavaScript syntax check, and production/development Compose validation pass.
