Status: ready-for-agent
Blocked by:

# Add paginated search and local-day reporting

Extend the storage and application filters with bounded limit/offset pagination while preserving the Discord default limit. Correct daily statistics grouping so stored UTC timestamps are grouped by `Asia/Ho_Chi_Minh` calendar day.

## Acceptance Criteria

- Search accepts validated limit and offset values; zero limit keeps the existing 20-row behavior.
- Web callers can request 25 rows per page and receive total/truncated metadata.
- Daily report groups timestamps by the configured +07:00 local day, including values around UTC midnight.
- Repository tests cover pagination and local-day boundaries.

## Comments

Implemented with bounded limit/offset search, preserved 20-row Discord default, and fixed +07:00 daily grouping. Covered by storage tests.
