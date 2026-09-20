Status: resolved
Blocked by: 01

# OpenAI-compatible Responses client

Implement the `net/http` Responses API transport, fixed JSON-schema tools, optional authentication, function-call continuations, bounded rounds/timeouts, strict decoding, and safe provider errors.

## Acceptance Criteria

- Requests target `<OPENAI_BASE_URL>/responses` and use `OPENAI_MODEL`.
- Only the seven declared tools can be returned or dispatched.
- Function calls and outputs follow the Responses API contract without a Chat Completions fallback.
- Requests disclose only the current message and active tool results.
- Malformed JSON, unsupported items, timeouts, and provider failures return safe errors.
- `httptest` contract tests cover zero, one, and multiple function calls.
