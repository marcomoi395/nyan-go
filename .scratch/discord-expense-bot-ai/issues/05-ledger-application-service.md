Status: ready-for-human
Blocked by: 02

# Deterministic ledger service

Implement backend-owned transaction validation and operations: create/update/delete/restore/search, normalized statistics/comparisons, authoritative Vietnamese confirmations, and deterministic CSV generation.

## Acceptance Criteria

- Identity fields always come from trusted Discord context, never model arguments.
- One ordered mutation batch validates completely and commits atomically.
- Missing/ambiguous targets or periods return clarification without mutation.
- Statistics include boundaries, timezone, currency, filters, count, income, expense, net, and comparison data.
- Search and CSV export include only authorized non-deleted data by default.
- Tests cover atomic multi-create, ambiguity, idempotency, timezone boundaries, totals, and CSV.
