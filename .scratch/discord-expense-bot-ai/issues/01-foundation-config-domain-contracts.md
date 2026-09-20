Status: resolved
Blocked by:

# Foundation, configuration, and domain contracts

Implement startup configuration, `.env` loading, fixed `Asia/Ho_Chi_Minh` timezone, category/type/value validation, and the narrow shared interfaces used by storage, provider, Discord, and application packages.

## Acceptance Criteria

- Required configuration fails before external connections; optional API key and reminder defaults work.
- `.env.example` contains no secrets and `.env` is ignored.
- Domain validation covers positive integer VND amounts, supported types/categories, bounded text, and valid date ranges.
- Trusted Discord identity and source fields are separate from model-controlled tool arguments.
- Tests cover configuration defaults/errors and domain validation.
