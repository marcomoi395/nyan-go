Status: ready-for-agent
Blocked by: 03, 04, 05

# AI orchestrator, clarification, and ordered queue

Implement the single application orchestrator that owns message ordering, the Responses function loop, read-before-write planning, one atomic mutation batch, clarification state, tool dispatch, silence for `no_action`, and constrained analysis.

## Acceptance Criteria

- Authorized messages process serially in source order.
- Read-only calls finish before at most one mutation batch; later mutation batches are rejected.
- Clarification state lasts 10 minutes, a new request replaces it, and `huy`/`hủy` clears it.
- Provider/tool failures leave the database unchanged and return concise Vietnamese errors.
- AI analysis receives only normalized backend statistics and separates facts, observations, and limitations.
- Tests cover routing, ordering, clarification, batching, failures, disclosure, and no-action silence.

