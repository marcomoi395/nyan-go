Status: ready-for-human
Blocked by: 01

# SQLite schema and repository

Implement the pure-Go SQLite bootstrap, explicit schema versioning, transactions, audit history, idempotent mutation batches, search/statistics queries, soft deletion/restoration, and reminder state.

## Acceptance Criteria

- Fresh and existing database files bootstrap deterministically with foreign keys enabled.
- Ordered mutation batches commit atomically; failures roll back; repeated source IDs replay without duplicate writes.
- Search is filtered, excludes deleted rows by default, returns at most 20 rows, and reports total/truncation.
- Statistics use inclusive start/exclusive end and calculate exact VND totals/comparisons.
- Soft-deleted records and audit data remain recoverable indefinitely.
- Integration tests use temporary SQLite files.
