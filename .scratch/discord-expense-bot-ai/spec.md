Status: ready-for-agent

# AI bridge cho bot ghi chép thu chi cá nhân trên Discord

## Problem Statement

Người dùng muốn ghi lại thu nhập, chi tiêu và ghi chú cá nhân trực tiếp trong một Discord server duy nhất bằng câu tự nhiên như `trưa nay ăn phở 45k`, thay vì điền biểu mẫu hoặc nhớ cú pháp cứng. Bot cần hiểu yêu cầu qua một AI provider tương thích OpenAI, nhưng dữ liệu tài chính phải được kiểm soát bởi backend và lưu trong SQLite. Các yêu cầu tìm kiếm, thống kê và phân tích phải có phạm vi thời gian, múi giờ và quy chuẩn kết quả rõ ràng; yêu cầu mơ hồ phải được hỏi lại trước khi thay đổi dữ liệu.

Repo hiện là skeleton Go, đã có `discordgo` nhưng chưa có implementation, domain glossary, hoặc ADR. Spec này chốt nền tảng tối thiểu để triển khai phiên bản cá nhân, nhẹ, dễ chạy.

## Solution

Xây dựng bot Go cho một Discord guild và private channel được cấu hình cố định. Mọi message của người dùng được cho phép trong channel đó được chuyển sang AI qua OpenAI-compatible Responses API. AI có thể chọn không làm gì hoặc lập một action plan gồm nhiều function/tool nghiệp vụ. Backend kiểm tra và thực thi tool; AI không có quyền truy cập SQL hoặc DB trực tiếp.

Bot cung cấp các thao tác tối thiểu: thêm, sửa, xoá/khôi phục, tìm kiếm, xuất CSV giao dịch; lấy thống kê theo khoảng thời gian và khoảng so sánh; phân tích thống kê theo một quy chuẩn cố định; và nhắc người dùng ghi chép bằng scheduler theo múi giờ Việt Nam với giới hạn tần suất. SQLite là nguồn dữ liệu duy nhất cho phiên bản đầu.

## User Stories

1. As the personal user, I want to add an expense with natural language, so that recording a transaction takes only one short message.
2. As the personal user, I want to add income with natural language, so that salary and other receipts use the same interaction as expenses.
3. As the personal user, I want the bot to understand Vietnamese amount formats such as `45k`, `1tr2`, and `1.200.000đ`, so that I do not need to normalize amounts manually.
4. As the personal user, I want the bot to infer a category from the message, so that common expenses do not require a separate category command.
5. As the personal user, I want to provide an explicit category, note, or date in the message, so that inferred values can be overridden.
6. As the personal user, I want the bot to use Asia/Ho_Chi_Minh for relative dates such as `hôm nay`, `hôm qua`, and `tháng này`, so that records match my local calendar.
7. As the personal user, I want a concise confirmation after a successful write, so that I can verify what was recorded without reading a long AI response.
8. As the personal user, I want an ambiguous request to produce a clarifying question, so that the bot does not guess a financially meaningful value.
9. As the personal user, I want a missing amount, transaction type, or date to be requested before saving, so that incomplete input never creates a partial transaction.
10. As the personal user, I want to edit the latest or a uniquely identified transaction using natural language, so that correcting a typo is quick.
11. As the personal user, I want every deletion to require a short-lived button confirmation, so that even a correctly parsed request cannot remove records by accident.
12. As the personal user, I want deleted transactions to be recoverable through soft deletion or an audit trail, so that an accidental deletion does not destroy financial history.
13. As the personal user, I want to search transactions by date range, type, category, amount, or note, so that I can find an earlier record without knowing its ID.
14. As the personal user, I want search results to be bounded and paginated or summarized, so that a broad query does not flood the Discord channel.
15. As the personal user, I want to request totals for a clear period such as a day, month, or custom date range, so that I can understand cash flow quickly.
16. As the personal user, I want statistics grouped by category, day, or transaction type, so that I can see where money is going.
17. As the personal user, I want statistics to state the exact period, timezone, currency, and applied filters, so that I can interpret the result correctly.
18. As the personal user, I want the bot to distinguish backend-calculated facts from AI-generated observations, so that analysis cannot silently become invented data.
19. As the personal user, I want AI analysis to use only the normalized result returned by the backend, so that the model cannot fabricate transactions or totals.
20. As the personal user, I want analysis to state when there is insufficient evidence, so that the bot does not claim a cause for a spending change without supporting notes or data.
21. As the personal user, I want the bot to reject requests from other Discord guilds, so that the personal data is served only in the configured server.
22. As the personal user, I want the bot to reject unauthorized Discord users, so that other members cannot read or modify my financial records.
23. As the personal user, I want all secrets and deployment settings to come from `.env`, so that tokens and provider configuration are not hard-coded.
24. As the personal user, I want to choose any OpenAI-compatible provider through a base URL and model name, so that the bot is not coupled to a single AI vendor.
25. As the personal user, I want a clear startup error when required configuration is missing, so that deployment failures are visible before the bot accepts messages.
26. As the personal user, I want a daily reminder when I have not recorded a transaction, so that my ledger stays complete without manual scheduling.
27. As the personal user, I want reminders to honor a local send time and cooldown, so that the bot does not spam me.
28. As the personal user, I want reminders skipped when I have already completed the configured activity for the day, so that the bot does not repeat a needless prompt.
29. As the personal user, I want the bot to survive an AI provider failure without changing the DB, so that transient model errors cannot leave inconsistent records.
30. As the personal user, I want duplicate Discord delivery retries not to create duplicate transactions, so that reconnects and retries preserve ledger correctness.
31. As the personal user, I want all write operations to be attributable to a Discord user and source message, so that the ledger has an audit trail.
32. As the personal user, I want the bot to respond in Vietnamese by default, so that confirmations, questions, errors, and analysis are easy to understand.
33. As the personal user, I want every message I send in the configured private channel to be evaluated without mentions, slash commands, or command prefixes, so that the bot feels conversational.
34. As the personal user, I want one message to create multiple transactions when it clearly names multiple amounts, so that I can record a group of purchases naturally.
35. As the personal user, I want all mutations inferred from one message to succeed or fail together, so that a partial record is never created.
36. As the personal user, I want non-financial messages to be ignored after AI evaluation, so that the bot does not interrupt ordinary conversation.
37. As the personal user, I want the bot to process my messages in order, so that references such as `khoản vừa rồi` remain correct.
38. As the personal user, I want a comparison period supported for statistics, so that I can ask whether spending changed from one period to another.
39. As the personal user, I want a statistics request without a date range to ask for one, so that the period is never silently assumed.
40. As the personal user, I want restored transactions and audit data retained indefinitely, so that I can recover or reconcile any past record.
41. As the personal user, I want to request a CSV export in natural language and receive it only in the configured private channel, so that I retain access to my data without another interface.
42. As the personal user, I want the provider request to contain only the current message and data necessary for its active tool call, so that financial data disclosure is minimized.

## Implementation Decisions

- **Runtime:** Use Go with the existing `discordgo` dependency. Avoid an HTTP framework, ORM, job framework, or additional abstraction layer unless a concrete implementation need appears.
- **Discord boundary:** Accept only events from the configured `DISCORD_GUILD_ID`, `DISCORD_CHANNEL_ID`, and `DISCORD_USER_ID`. The configured channel must be private to the user and bot. Reject DMs, other guilds, other channels, and other users without calling the provider.
- **Input routing:** Send every message from the authorized user in the configured channel to the AI. Do not require mentions, slash commands, prefixes, or fixed syntax. The AI returns `no_action` for non-financial messages; the bot then remains silent.
- **AI transport:** Use `net/http` and JSON against an OpenAI-compatible Responses API endpoint. `OPENAI_BASE_URL` is the provider API root (normally ending in `/v1`) and requests use its `/responses` resource; `OPENAI_MODEL` identifies the model. The provider must support Responses API function calls and function-call outputs. Include `OPENAI_API_KEY` when authentication is required; allow an empty key for local unauthenticated compatible endpoints. Do not implement a Chat Completions or JSON-parsing fallback.
- **AI tool boundary:** Declare a fixed set of JSON-schema tools to the model. The model may choose a tool and provide arguments, but it cannot execute SQL, select a DB connection, set authorization fields, or bypass backend validation.
- **Tool set:** Provide `create_transaction`, `update_transaction`, `delete_transaction`, `restore_transaction`, `search_transactions`, `get_statistics`, and `export_transactions`. `delete_transaction` prepares a confirmation rather than deleting immediately. Do not add a wallet, transfer, or account tool in this version.
- **Application seam:** Route Discord input through one application service/orchestrator that owns the Responses API function-call loop, clarification state, ordered message queue, authorization context, tool dispatch, and user-facing result. Keep the repository layer below it deterministic and unaware of Discord or AI.
- **Action-plan execution:** A model request may yield zero, one, or many function calls. It must complete read-only lookup calls before emitting one ordered batch of mutation calls. The backend validates the full mutation batch, then executes it in model order in one SQLite transaction; one failure rolls back the entire batch. Reject later mutation batches for that source message. This allows a message such as `mua pho 45k, ca phe 30k` to create two records atomically.
- **Clarification policy:** If required values are missing, a date period is absent for statistics, multiple records match an update, or a requested amount has no defensible interpretation, return a Vietnamese clarification question and do not mutate. Preserve one pending request per user for 10 minutes; a new message replaces it, and `hủy` clears it. AI may interpret diverse languages and monetary expressions, but must not invent meaning from irreducibly ambiguous text.
- **Mutation validation:** Backend derives `user_id`, `guild_id`, channel ID, source message ID, current local time, and authorization context from the Discord event. It does not trust those values from model arguments. Validate positive integer VND amounts, supported transaction types, bounded text, hard-coded categories, and valid date ranges before writing.
- **Categories:** Hard-code income categories: salary, bonus, freelance, business, investment_return, refund, gift, other. Hard-code expense categories: food, transport, housing, utilities, shopping, health, education, entertainment, travel, insurance, tax_fee, family, pet, work, debt_finance, other. The AI maps equivalent Vietnamese or foreign-language wording to these IDs; otherwise it uses `other`.
- **Transaction model:** Store an immutable creation identity, transaction type (`income` or `expense`), amount as an integer VND value, category, note, occurred-at timestamp, creator Discord user ID, guild ID, channel ID, source message ID, created-at timestamp, updated-at timestamp, and soft-delete state. Do not model account, wallet, transfer, or funding source. Use UTC or a documented SQLite timestamp representation internally while converting user-facing boundaries through `Asia/Ho_Chi_Minh`.
- **Idempotency:** Use the Discord source message or interaction ID as an idempotency key for its complete mutation batch. A repeated delivery returns the existing result instead of repeating writes. Two distinct messages with identical text are distinct records.
- **Deletion and restore:** Every deletion creates a short-lived Discord button confirmation containing the resolved target count and summary; the button expires after five minutes. The confirmation handler deletes atomically without another AI request. Implement deletion as soft deletion, exclude deleted records from search/statistics by default, and allow an unambiguous `restore_transaction` through natural language. Keep soft-deleted records and audit history indefinitely.
- **SQLite:** Use `database/sql` with a lightweight SQLite driver and direct SQL. Do not introduce an ORM. Enable foreign keys, use a single migration/bootstrap path, and wrap each mutation in a DB transaction. The selected driver must not require a production database service; prefer a deployment without CGO if compatible with the repository runtime.
- **Schema lifecycle:** Create the minimal tables for transactions, reminder state/delivery, and optional audit events. Keep schema versioning explicit so a fresh SQLite file and an existing file both bootstrap deterministically.
- **Statistics contract:** `get_statistics` requires an inclusive local start boundary and exclusive local end boundary, optional transaction type/category filters, a grouping (`total`, `day`, `category`, or `type`), and optional comparison boundaries. The backend calculates all totals, deltas, and percentages deterministically. Every result includes currency `VND`, `Asia/Ho_Chi_Minh`, normalized boundaries, filters, transaction count, total income, total expense, net, and comparison data when requested.
- **Analysis contract:** The backend renders write confirmations and all numeric statistics authoritatively. AI may add Vietnamese analysis to a normalized statistics result only. Analysis separates `facts`, `observations`, and `limitations`; never invent missing categories, causes, transactions, values, percentages, or comparisons; and explicitly states insufficient evidence.
- **Statistics limits:** Search returns at most 20 records plus total count/truncation state; users must narrow broader searches. The backend enforces a maximum personal-data period/result size and returns grouped statistics rather than an unbounded transaction list.
- **AI loop safety:** Bound Responses API function-call rounds, set request timeouts, reject malformed arguments, and return a safe error for unsupported response items or invalid JSON. No mutation is committed until the full batch passes backend validation. Process authorized Discord messages serially, preserving channel/user order and references such as `khoản vừa rồi`.
- **Prompt and data handling:** Use a fixed system instruction that defines the tool contract, action-plan rule, clarification policy, Vietnamese response language, timezone, currency, categories, and analysis rules. Treat user text and stored notes as data, not instructions that can change policy. Send only the current user message for parsing and the minimum tool result required for the active turn; do not send chat history or unrelated transaction notes to the provider.
- **Configuration:** Load `.env` at startup, then validate required settings before connecting Discord: `DISCORD_TOKEN`, `DISCORD_GUILD_ID`, `DISCORD_USER_ID`, `DISCORD_CHANNEL_ID`, `OPENAI_BASE_URL`, `OPENAI_MODEL`, and `SQLITE_PATH`. Support optional `OPENAI_API_KEY`, `REMINDER_ENABLED`, `REMINDER_TIME` (default `20:30`), and reminder cooldown. Provide `.env.example` without secret values; never commit `.env`.
- **Timezone:** Use `Asia/Ho_Chi_Minh` as the fixed application timezone. Do not rely on host timezone. Convert relative user dates, statistics boundaries, reminder scheduling, and display timestamps through the loaded location.
- **Reminder scheduler:** Implement one lightweight in-process scheduler using the standard library ticker/timer and the configured Vietnamese local time; avoid a cron dependency for the first version. At `REMINDER_TIME`, send at most one reminder when the user has no transaction that local day. Persist delivery state in SQLite, skip an already completed day, and do not send catch-up reminders after downtime; wait for the next schedule. Skip when the configured channel is unavailable.
- **Export:** `export_transactions` creates a CSV from authorized, non-deleted records and sends it as an attachment only to the configured private channel. Delete the local temporary export immediately after the Discord send attempt; Discord attachment retention follows Discord's normal behavior.
- **Failure behavior:** AI/network failures, Discord send failures, and DB errors return a concise user-facing error and leave mutations atomic. Do not automatically retry an AI request. Log technical details server-side without logging API keys or unnecessary financial content.
- **Observability:** Log action type, success/failure, latency, provider status, and stable request/source IDs. Do not log full prompts, API credentials, or full transaction notes by default.

## Testing Decisions

- Tests assert externally visible behavior: parsed intent outcomes, clarification behavior, authorization, ordered multi-action DB state, returned statistics, idempotency, soft deletion, export cleanup, reminder throttling, and Discord-facing responses. Avoid tests coupled to private helper names or exact SQL statements.
- The highest-value seam is the application service/orchestrator with injected Responses API transport, clock, Discord sender, serial work queue, and transaction repository. This seam proves the AI-to-tool boundary without requiring a live Discord connection or provider.
- Add repository integration tests against a temporary SQLite database for schema bootstrap, atomic multi-create/update/delete, search filters/limit, soft-delete exclusion/restore, idempotency, timezone boundaries, and deterministic statistics/comparisons.
- Add AI transport contract tests using an in-memory HTTP test server that returns Responses API `function_call` items, `function_call_output` continuations, `no_action`, clarification responses, malformed JSON, provider errors, and multiple calls in one mutation batch. Verify that only declared tools can be dispatched and that Chat Completions responses are rejected.
- Add application-service tests for all-message routing in the configured channel, no-action silence, missing fields, missing statistics period, ambiguous updates, delete button expiry, unauthorized guild/channel/user, mutation validation, serial ordering, failed AI calls, failed tool calls, and the rule that model-supplied identity fields cannot override Discord context.
- Add statistics tests using fixed `Asia/Ho_Chi_Minh` timestamps around midnight, month boundaries, and daylight-saving-independent dates. Verify inclusive-start/exclusive-end behavior and exact VND totals.
- Add analysis contract tests that pass normalized backend results to the formatter and verify facts/observations/limitations, no unsupported claims, and explicit insufficiency handling. Do not test the model's internal reasoning.
- Add reminder tests with a fake clock for `20:30` local send time, already-recorded activity, cooldown, persisted last delivery, restart behavior without catch-up, and Discord send failure. Verify no duplicate reminder within the same local day.
- Add export tests that assert CSV content, private-channel-only delivery, and local temporary-file cleanup after success and failure.
- Add a small end-to-end test through the application service with a fake Discord event and fake Responses API response, proving `message -> multiple tools -> atomic SQLite batch -> Vietnamese confirmation` without external network access.
- No prior implementation test pattern exists in the skeleton repo; use standard-library testing, `httptest`, and temporary SQLite files. Keep tests deterministic and independent of wall-clock time.

## Out of Scope

- Multi-server or SaaS operation.
- Multi-user accounts, shared household ledgers, roles, or cross-user visibility.
- Cloud database, migrations between database engines, or remote backup/synchronization.
- Bank API, payment API, OCR receipt scanning, image/audio input, or automatic transaction import.
- Currency conversion and currencies other than VND.
- Investment, debt, tax, accounting, or financial advice workflows.
- A web dashboard, mobile application, or Google Sheets export.
- Arbitrary SQL, arbitrary model-generated code, or direct AI database access.
- Processing messages from other users, channels, guilds, or DMs; ordinary social-chat responses are also out of scope.
- Advanced recurring budgets, anomaly detection, or predictive forecasting beyond deterministic statistics and constrained observations.
- Guaranteed reminder delivery when Discord is unavailable.
- A full cron daemon or distributed scheduler; the first version runs one scheduler inside the bot process.

## Further Notes

- The repository has no `CONTEXT.md` or ADR yet, so this spec establishes the initial vocabulary: user, configured guild, transaction, tool, statistics result, analysis, and reminder.
- The first implementation keeps the Responses API provider behind a small interface and all domain operations callable without AI. Tests invoke the application seam directly; the user-facing product has no slash-command or command-prefix path.
- Recommended implementation order: configuration and SQLite schema; deterministic transaction service; Discord authorization and serial queue; Responses API function transport; atomic action-batch dispatch; statistics/analysis formatting; delete confirmation, restore/export, reminder scheduler; end-to-end wiring.
- Required secrets belong only in `.env`; commit `.env.example` and add `.env` to `.gitignore` before local development.
