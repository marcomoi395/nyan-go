Status: ready-for-agent

# Natural-language inference and AI request refinement

## Problem Statement

The bot currently treats every required model field as something the user must provide explicitly. As a result, a message such as `bun mam 69k` can trigger a request for both date and category even though the bot can safely infer an expense, the `food` category, and the current local date. This makes short natural-language capture feel like a form and causes unnecessary clarification turns.

The AI request loop also performs a continuation after mutation calls even though the backend already owns validation, persistence, and the authoritative Vietnamese confirmation. That continuation spends tokens and adds latency without improving the user-visible result.

Read-only tool results currently expose more transaction metadata than the model needs. Internal identity, audit, and lifecycle fields increase token use and disclose unrelated implementation data.

## Solution

Refine the application orchestrator and Responses provider boundary so the bot makes only defensible minimal inferences, asks targeted clarification questions only when a required fact cannot be established, and completes mutations without a second AI response.

The backend remains authoritative for dates, validation, persistence, confirmations, and atomicity. The AI remains responsible for natural-language interpretation and tool selection, but it cannot invent amounts, silently repair ambiguity, or create categories outside the domain model.

## User Stories

1. As the personal user, I want to record a clear short expense such as `bun mam 69k`, so that I do not need to state the date, type, and category separately.
2. As the personal user, I want `bun rieu 25k` to become an expense in the `food` category on the current local date, so that ordinary meals are recorded in one message.
3. As the personal user, I want the bot to ask only for the missing amount when I say `bun rieu`, so that it does not ask me for category or date that it can already infer.
4. As the personal user, I want the bot to ask for the transaction subject when I say only `25k`, so that an amount without meaning is never stored as a fabricated transaction.
5. As the personal user, I want the bot to infer `expense` from clear spending language, so that I do not need to name the transaction type.
6. As the personal user, I want the bot to infer `income` from clear receiving language such as `luong 10 trieu`, so that income capture is as short as expense capture.
7. As the personal user, I want an ambiguous transaction type to produce a focused clarification, so that the bot does not default every unclear amount to an expense.
8. As the personal user, I want common Vietnamese food words such as `pho`, `bun`, `com`, `ca phe`, and `bia` mapped to `food`, so that familiar language works without category commands.
9. As the personal user, I want common transport words such as `xang`, `taxi`, and `Grab` mapped to `transport`, so that travel expenses are categorized consistently.
10. As the personal user, I want health words such as `thuoc` and `kham benh` mapped to `health`, so that medical spending is categorized correctly.
11. As the personal user, I want utilities such as electricity, water, and internet mapped to `utilities`, so that recurring household bills are categorized consistently.
12. As the personal user, I want an explicit category to override an inferred category, so that my stated intent is always respected.
13. As the personal user, I want an ambiguous category such as `mua gi do` to produce a clarification, so that `other` does not hide an interpretation failure.
14. As the personal user, I want `other` used when I explicitly say `khac` or `khong phan loai`, so that I can intentionally opt out of a specific category.
15. As the personal user, I want an omitted date to use the message receipt time in `Asia/Ho_Chi_Minh`, so that the bot does not invent a date or hour.
16. As the personal user, I want `hom nay`, `hom qua`, and similar relative dates interpreted in the fixed Vietnamese timezone, so that records match my local calendar.
17. As the personal user, I want date-only phrases to preserve the receipt clock time, so that the bot does not silently choose noon or another invented hour.
18. As the personal user, I want `sang`, `trua`, `chieu`, and `toi` interpreted using stable documented clock values, so that time-of-day language becomes searchable.
19. As the personal user, I want a date without a year to use the current year, so that ordinary dates do not require unnecessary clarification.
20. As the personal user, I want an explicitly stated future date accepted, so that planned or delayed recording remains possible.
21. As the personal user, I want exact amount expressions such as `25k`, `1tr2`, `1.200.000d`, `1 trieu 200`, and `50 nghin` normalized to integer VND, so that I can use natural Vietnamese money notation.
22. As the personal user, I want approximate expressions such as `tam 50k`, `hon 100k`, and `duoi 50k` clarified, so that the bot never stores a guessed amount.
23. As the personal user, I want ambiguous per-item versus total expressions such as `2 coc ca phe 25k` clarified, so that the stored total is not silently wrong.
24. As the personal user, I want a note to remain optional when amount, type, and category are clear, so that `25k an uong` can still be recorded.
25. As the personal user, I want the original natural-language subject preserved as a note when useful, so that I can later recognize the transaction.
26. As the personal user, I want the bot to ask only the unresolved field, so that clarification is conversational instead of a repeated form.
27. As the personal user, I want to answer a clarification with only the missing value, such as replying `25k` after `bun rieu`, so that the pending request completes naturally.
28. As the personal user, I want a clarification to remain pending for ten minutes, so that a short delayed reply still completes the original request.
29. As the personal user, I want an expired clarification to be rejected as stale, so that a later unrelated message is not attached to an old request.
30. As the personal user, I want `huy` or `huy` with the appropriate Vietnamese spelling to clear the pending request, so that I can abandon an unfinished capture.
31. As the personal user, I want multiple clear transactions in one message recorded as one atomic batch, so that a failure never leaves a partial batch.
32. As the personal user, I want a message containing one ambiguous transaction to leave the entire batch untouched, so that the bot does not partially act on my intent.
33. As the personal user, I want explicit user values to override model inferences, so that my stated date, category, amount, or type wins over defaults.
34. As the personal user, I want invalid model arguments converted into a concise Vietnamese clarification when the missing fact is recoverable, so that technical provider output does not leak into the conversation.
35. As the personal user, I want unrecoverable provider or validation errors to leave the database unchanged, so that malformed AI output cannot corrupt the ledger.
36. As the personal user, I want a successful mutation to receive a concise backend confirmation, so that the bot does not spend another AI request rewriting a response it already knows authoritatively.
37. As the personal user, I want confirmations to use stable Vietnamese templates, so that messages remain clear, testable, and consistent.
38. As the personal user, I want confirmation amounts and categories displayed in user-facing Vietnamese formatting, so that internal IDs such as `food` are not exposed unnecessarily.
39. As the personal user, I want read-only analysis to continue through AI after backend data retrieval, so that statistics can still receive natural-language observations.
40. As the personal user, I want read-only tool results to contain only fields needed for the active answer, so that internal identity and audit metadata are not disclosed to the model.
41. As the personal user, I want the bot to preserve the current no-history behavior, so that unrelated prior messages and notes do not increase token use or influence interpretation.
42. As the operator, I want mutation requests to use one fewer provider round, so that common writes consume fewer tokens and complete faster.
43. As the operator, I want the provider loop bounded at four rounds, so that an abnormal model loop cannot run indefinitely.
44. As the operator, I want numeric provider usage recorded when available, so that token and latency improvements can be measured without logging sensitive content.
45. As the operator, I want usage logs to exclude prompts, API keys, and full transaction notes, so that observability does not become financial-data leakage.

## Implementation Decisions

- **Primary application seam:** Exercise the application orchestrator through its existing provider, clock, ledger, and request-context seams. Use a fake provider and temporary SQLite storage to verify externally visible behavior from natural-language input through user-facing response and database state.
- **Provider continuation behavior:** The Responses function-call dispatcher returns immediately after a round containing one or more mutation tools. The application orchestrator then validates and applies the complete mutation batch and returns the backend confirmation. No continuation AI request is made after `create_transaction`, `update_transaction`, `delete_transaction`, or `restore_transaction`.
- **Read-only continuation behavior:** Search and statistics calls retain the continuation needed to produce a final answer or constrained analysis. The fixed tool set and current round limit remain unchanged for this feature.
- **Round limit:** Keep the maximum provider function-call rounds at four. This is a safety bound, not a normal-path request target.
- **Mutation confirmations:** Use deterministic Vietnamese backend templates, not model-generated final prose and not random rotation. A single mutation, multiple mutation batch, restore, and deletion confirmation retain operation-appropriate wording. Deletion continues to use the short-lived confirmation interaction.
- **Confirmation localization:** Store stable category IDs such as `food`; render user-facing labels such as `an uong` in confirmations. Format VND amounts consistently.
- **Minimal inference rule:** Infer only facts supported by the user message, the fixed timezone, and the documented defaults. Never invent an amount, silently resolve a materially ambiguous amount, or create an unsupported category.
- **Required facts for a write:** Amount and transaction type must be known or defensibly inferred. Category must be known or defensibly inferred. Note is optional. Date and time default to the authorized Discord event's receipt time when omitted.
- **Explicit-value precedence:** A date, time, category, type, amount, or other explicit user value overrides an inferred or default value.
- **Date normalization:** Use `Asia/Ho_Chi_Minh`. Relative dates use the local calendar. A date without a year uses the current year. Date-only phrases preserve the receipt clock time. Time-of-day phrases use stable documented values: morning `08:00`, noon `12:00`, afternoon `15:00`, and evening `20:00`. Explicit future dates are valid.
- **Amount normalization:** Accept exact Vietnamese money forms including `k`, `tr`, `trieu`, `nghin`, decimal separators, and suffixes such as `d`/`đ`. Approximate, bounded, or per-item ambiguity triggers clarification rather than rounding or guessing.
- **Category mapping:** Maintain a fixed domain category vocabulary and a compact mapping for common Vietnamese and foreign-language expressions. The AI may map variants to existing IDs, but backend validation remains authoritative. Use `other` only when the user explicitly requests an unclassified category.
- **Clarification behavior:** Return a targeted Vietnamese question for only the unresolved fact. Do not request already inferred date, category, or type. Preserve one pending request per user for ten minutes; a reply within that period is combined with the pending request, while an expired request is rejected as stale. Cancellation clears the pending request.
- **Atomicity:** A message with multiple mutations is applied as one validated batch. If any transaction is ambiguous, invalid, or unresolved, none of the batch is written.
- **Invalid arguments:** Convert recoverable missing or malformed facts into concise Vietnamese clarification. Do not silently repair financially meaningful values. Unrecoverable provider failures remain safe errors and leave the database unchanged.
- **Tool-result DTO:** Send search results using a compact model-facing representation containing only transaction ID, type, amount, category, note, occurred-at value, total count, and truncation state. Exclude Discord identities, source-message IDs, audit timestamps, deletion metadata, and unrelated internal fields. Statistics retain only normalized facts needed for the active analysis.
- **No new token cap:** Do not add `max_output_tokens` or a new output-budget configuration in this feature.
- **No dynamic tool routing:** Continue sending the existing fixed tool set. Do not add a classifier, intent router, or provider-specific tool selection optimization.
- **Usage observability:** If the provider response includes numeric usage, record input tokens, output tokens, total tokens, round count, action, status, and latency. Do not record prompts, credentials, or full notes.
- **Transport security:** HTTPS migration is explicitly out of scope for this feature. The existing HTTP provider configuration remains a known deployment risk and requires a separate security change.

## Testing Decisions

- Tests verify external behavior at the application orchestrator seam: response text, clarification state, provider-call count, and SQLite state. They do not assert private helper names, prompt wording, or model internal reasoning.
- Add provider transport coverage proving a mutation round returns without a continuation request, while read-only calls still continue to a final response.
- Add application tests for `bun rieu` asking only for amount and `bun rieu 25k` creating one expense with `food`, current local receipt time, and the natural-language note.
- Add application tests for explicit date override, relative dates, missing year, future dates, and time-of-day normalization around local midnight.
- Add amount tests for `25k`, `1tr2`, `1.200.000d`, `1 trieu 200`, and `50 nghin`, plus clarification for approximate, bounded, and per-item expressions.
- Add type-inference tests for clear expense, clear income, ambiguous money-only messages, and contradictory or unclear wording.
- Add category tests for common food, transport, health, and utility mappings; explicit category override; explicit `other`; and unresolved category clarification.
- Add clarification-state tests for targeted questions, ten-minute completion, expired pending requests, cancellation, and a reply containing only the missing amount.
- Add atomicity tests for multiple clear transactions and for a mixed batch where one transaction is ambiguous or invalid.
- Add confirmation tests for stable templates, multiple transactions, localized category labels, VND formatting, and no second AI response after mutation.
- Add DTO disclosure tests proving model-facing search output excludes user, guild, channel, source-message, audit, and deletion metadata.
- Add usage-observability tests proving numeric usage is recorded when present and sensitive content is excluded from logs.
- Use existing standard-library tests, in-memory HTTP transport, fake clocks, injected providers, and temporary SQLite databases as prior art. Keep wall-clock and network behavior deterministic.
- The accepted end-to-end examples are: `bun rieu`, `bun rieu 25k`, `bun rieu 25k hom qua`, `25k`, `mua gi do 100k`, `luong 10 trieu`, `mua 2 ly ca phe 25k`, one ambiguous item in a multi-item message, and a clarification reply of `25k`.

## Out of Scope

- HTTPS or provider transport security migration.
- Adding `max_output_tokens`, reasoning-budget controls, or dynamic tool routing.
- Adding a second classifier model or a local intent router.
- Conversation-history support across independent Discord messages.
- New transaction categories, wallets, accounts, transfers, currencies, or financial advice.
- Automatic correction of ambiguous amounts or categories.
- Randomized confirmation wording.
- Changing the existing four-round safety limit.
- Replacing backend validation with AI validation.
- Changing deletion confirmation semantics or bypassing the destructive-action button.
- Redesigning search, statistics, export, reminder, authorization, or SQLite schema behavior beyond the model-facing DTO and inference defaults required here.

## Further Notes

- The repository has no `CONTEXT.md` or ADR; this spec continues the established vocabulary of user, transaction, category, tool, statistics result, analysis, clarification, and request context.
- The current product spec says unknown categories may fall back to `other`; this refinement supersedes that behavior by requiring clarification unless the user explicitly requests `other`.
- The backend should own the receipt-time default because the AI request currently receives only user text and cannot be trusted as the source of system time.
- The existing HTTP provider configuration remains intentionally unchanged. It should be tracked separately because the API key and financial content are exposed to the provider without transport encryption.
- This refinement should be implemented after the current ledger, provider, Discord, and orchestrator seams remain green. The intended implementation order is provider early-return behavior, model-facing DTOs, inference prompt and date defaults, clarification behavior, confirmations, then observability and acceptance tests.
