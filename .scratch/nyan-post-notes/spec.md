Status: ready-for-agent

# Ghi chú trong Nyan Post

## Problem Statement

Nyan Post hiện chỉ gửi text hoặc file trực tiếp đến Discord qua webhook. Người dùng cần một tab ngay trên màn hình Nyan Post để lưu các đoạn ghi chú text trên server, đọc lại và quản lý chúng mà không tạo tin nhắn Discord.

Giao diện Nyan Post hiện nằm trong web service `discord-webhook`. SQLite hiện do service `nyan-go` sở hữu; web service chưa có database hoặc volume riêng.

## Solution

Thêm tab `Ghi chú` vào cùng trang Nyan Post. Tab cho phép tạo, xem, sửa và xóa ghi chú text. Dữ liệu được lưu bền vững trên server bằng một SQLite database riêng cho Nyan Post, dùng volume Docker hiện có nhưng file database tách khỏi ledger của bot.

Các endpoint ghi chú dùng cùng cơ chế mật khẩu hiện tại của Nyan Post. Ghi chú là sổ dùng chung cho mọi người có mật khẩu; bản đầu không thêm tài khoản hoặc phân quyền theo người dùng.

## User Stories

1. As a Nyan Post user, I want a `Ghi chú` tab beside the Discord posting tab, so that I can switch between publishing and private server storage.
2. As a Nyan Post user, I want the existing Discord posting tab to keep working unchanged, so that adding notes does not disrupt publishing.
3. As an authorized user, I want to open the notes tab after the existing password login, so that I do not need a second login.
4. As an unauthorized user, I want note requests rejected by the server, so that knowing the endpoint URL is insufficient to read or change notes.
5. As a user, I want to enter a text note, so that I can save a short thought or reminder.
6. As a user, I want empty or whitespace-only notes rejected, so that the list contains useful entries.
7. As a user, I want a visible text length limit, so that I know when a note is too long.
8. As a user, I want Vietnamese text, emoji and Unicode preserved, so that saved notes match what I entered.
9. As a user, I want to save a note and receive confirmation only after SQLite confirms the write, so that I can trust the result.
10. As a user, I want newly saved notes to appear without a full page reload, so that note taking remains quick.
11. As a user, I want notes listed newest first, so that recent thoughts are immediately visible.
12. As a user, I want to load notes from the server when opening the tab, so that the browser is not the source of truth.
13. As a user, I want to edit an existing note, so that I can correct or extend it.
14. As a user, I want to delete an existing note, so that obsolete text does not remain in the notebook.
15. As a user, I want delete and save failures shown clearly, so that I know whether the server changed anything.
16. As a user, I want note content displayed as plain text, so that saved text cannot inject HTML or script into the page.
17. As a user, I want the note editor to retain my text after a failed request, so that I do not lose work.
18. As a keyboard user, I want labeled controls and visible focus in the notes tab, so that I can manage notes without a mouse.
19. As a screen-reader user, I want loading, success and error states announced, so that I can understand note operations.
20. As an operator, I want note data persisted across container restarts, so that server restart does not erase the notebook.
21. As an operator, I want the notes database separate from ledger tables, so that web-note traffic cannot corrupt or couple to expense data.
22. As an operator, I want the database path configurable by environment variable, so that deployment can choose the mounted data directory.
23. As an operator, I want the web service to fail clearly when the notes database cannot be opened or migrated, so that a broken deployment is visible.
24. As an operator, I want note content excluded from logs, so that ordinary logs do not become a copy of the notebook.
25. As a maintainer, I want deterministic HTTP and SQLite tests, so that note behavior can be verified without Discord or a browser service.
26. As a maintainer, I want the schema versioned through migrations, so that future note fields can be added safely.

## Implementation Decisions

- **Boundary:** Implement the feature at the existing `discord-webhook` HTTP handler and embedded page. Keep Discord posting and note persistence as separate tabs and separate backend handlers.
- **Storage ownership:** Open a dedicated SQLite file for notes in the web service. Do not make the web service write to the bot ledger database. The two services may use the same Docker volume, but must use different database files.
- **Configuration:** Add a required notes SQLite path for the web service, with the deployment default pointing to the persistent data volume. Preserve the existing Discord password and webhook configuration. Startup fails if required configuration is missing or the database cannot be opened.
- **Schema:** Add a versioned notes schema with an integer primary key, non-empty text content, creation timestamp and update timestamp. Store timestamps in the same stable format used by the existing SQLite storage layer.
- **Repository seam:** Add one narrow notes repository behind the HTTP handlers. It owns migrations, validation at the persistence boundary, create/list/update/delete operations and SQLite error translation. The handler does not build SQL directly.
- **HTTP contract:** Add authenticated JSON endpoints for listing, creating, updating and deleting notes. Every endpoint rechecks the existing password header. Responses contain note IDs, content and timestamps; errors use the service's existing JSON error shape and never expose SQL details.
- **Request validation:** Bound JSON request size and maximum note length on the server. Reject malformed JSON, missing content and whitespace-only content. Trim only for validation; preserve the user's entered content when it is valid.
- **Ordering and pagination:** List notes newest first by creation/update ordering with a deterministic ID tie-breaker. The first version may use a bounded page size; do not load an unbounded table into memory.
- **Identity model:** Treat notes as one shared notebook protected by the existing shared password. Do not add user accounts, OAuth, JWT, per-user ownership or browser persistence in this feature.
- **Rendering safety:** Insert note content with text-safe DOM APIs. Do not interpret content as HTML or Markdown. Do not place note content in logs or error messages.
- **Concurrency:** Configure SQLite for the low-write web workload and handle transient database locking as a normal server error. Do not share the ledger connection or migration state between processes.
- **Deployment:** Mount the persistent data volume into `discord-webhook`, pass the notes database path, and preserve the existing read-only image root plus writable data volume. Update development and production Compose configurations consistently.
- **UI behavior:** Keep the existing login gate and Discord form. Add a clear tab control, loading state, note editor, list view, edit action, delete action and live status region. Use the existing Nyan Post visual language and responsive/accessibility patterns.
- **Data retention:** Delete is permanent in the notes table for the first version. No audit history, recycle bin or restore workflow is required.

## Testing Decisions

- Test external behavior through the highest useful seam: the authenticated HTTP handler backed by a temporary SQLite database. Avoid testing SQL helper internals or exact HTML/CSS structure.
- Add repository tests using temporary database files for migration creation, create, list ordering, update, delete, validation and persistence across reopening the database.
- Add HTTP tests for missing/invalid authentication, malformed JSON, empty content, length limits, successful CRUD, missing note IDs, database errors and bounded list responses.
- Verify Unicode and emoji survive a create/list/update round trip unchanged.
- Verify note content containing HTML-like text is returned as text and cannot be treated as executable markup by the page's rendering path.
- Verify Discord endpoints remain unaffected and note requests never call the Discord webhook client.
- Verify the notes database is distinct from the ledger database and that opening the web service does not mutate ledger schema.
- Verify Compose configuration supplies a persistent notes path and the service starts with the notes database available.
- Retain the existing full Go test suite as the regression check. Browser visual checks remain manual and focus on tab switching, mobile layout, keyboard operation and status announcements.

## Out of Scope

- Per-user notes, accounts, roles, OAuth, Discord identity mapping or separate passwords.
- Sharing notes into Discord, importing Discord messages or attaching files to notes.
- Markdown rendering, rich text, tags, folders, search, pinning, reminders or full-text indexing.
- Note history, audit log, soft-delete, restore or export/import.
- Multi-instance web deployment, distributed locking, PostgreSQL migration or a separate cache.
- Reusing the ledger database tables or changing expense-bot behavior.
- Public Internet exposure, TLS termination, reverse proxy setup or domain provisioning.
- Automatic background sync, offline browser storage or localStorage persistence.

## Further Notes

- Feasibility is high for a single web instance and low write volume. A dedicated SQLite file avoids cross-process contention with the existing ledger database.
- The existing shared-password model means every authenticated visitor can read, edit and delete every note. This is an intentional first-version tradeoff and should be revisited before exposing the page to multiple untrusted users.
- The current repository baseline passes `go test ./...` before this feature.
- The implementation should preserve the existing Discord webhook limits, authentication rate limiting, origin checks, security headers and no-store behavior.

## Comments

- 2026-10-24: Synthesized from the request to add a server-persisted text-notes tab to Nyan Post, with SQLite as the priority storage and a dedicated notes database as the recommended architecture.
