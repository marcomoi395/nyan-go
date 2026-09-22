Status: ready-for-agent
Blocked by: 01

# Build the SSR web adapter

Add the standard-library HTTP handler, embedded templates and CSS for dashboard, transaction list/search, transaction creation, deletion confirmation and reports.

## Acceptance Criteria

- Routes and forms match the feature spec and require no JavaScript.
- Trusted config supplies ledger identity; each mutation gets a unique web source ID.
- POST requests enforce CSRF and reject cross-site submissions.
- Form validation preserves submitted values and gives a useful Vietnamese error.
- HTML uses semantic controls, labels, visible focus and responsive navigation.
- `httptest` covers read pages, create, delete, filters and CSRF rejection.

## Comments

Implemented as `internal/web` with embedded SSR templates/CSS, trusted ledger scope, POST/Redirect/Get, CSRF protection, responsive navigation, and HTTP tests.
