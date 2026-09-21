Status: ready-for-human
Blocked by: 04, 05

# Reminder scheduler

Implement one standard-library in-process scheduler using Vietnamese local time and persisted delivery state.

## Acceptance Criteria

- The configured local send time defaults to `20:30`.
- At most one reminder is sent when the user has no transaction that local day and cooldown permits it.
- Restart/downtime causes no catch-up delivery; the scheduler waits for the next scheduled time.
- Existing activity, persisted delivery, unavailable channel, and send failure are handled without spam.
- Fake-clock tests cover timezone boundaries, cooldown, restart, failure, and clean shutdown.
