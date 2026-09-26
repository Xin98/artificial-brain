# ITER-0005 implementation plan — conversation sessions, free chat, and history

Execute in the numbered tasks below; each task ends with a green tree and a ledger entry in [progress.md](../../iterations/ITER-0005/progress.md). The [approved design](../specs/2026-09-27-iter-0005-conversation-sessions-design.md) is authoritative for shapes and decisions D1–D10.

## Task 1 — Iteration ledger, policy refresh, and branch (yellow: AGENTS.md ×4)

Create `docs/iterations/ITER-0005/{brief,spec,plan,progress,decisions,test-matrix}.md`, these superpowers docs, and refresh the zone text in root/`backend`/`apps/web`/`deploy` `AGENTS.md`: yellow pointer moves to the ITER-0005 register; red zone freezes migrations **001–009**; the no-new-dependency and no-real-provider-from-CI rules carry forward. Branch `iter-0005-conversation-sessions`.

## Task 2 — Migration 010 and schema 9→10 (yellow)

`deploy/migrations/010_conversation_sessions.sql` per design §4 (sessions table, indexes, nullable FK column, down section). Bump the three pins: `backend/internal/platform/database/schema.go` (`CurrentSchemaVersion = 10`), `migrate_integration_test.go`, `tests/smoke/migration_test.sh`. Verify: `TEST_DATABASE_URL=… go test ./backend/internal/platform/database -race` (idempotent re-run), `make migration-test`.

## Task 3 — Additive backend foundation (green)

All under `backend/internal/modules/conversation/`:

- `domain/session.go` (`Session`, `NewSession`, `Renamed`, `DefaultSessionTitle`, `MaxSessionTitleRunes=50`), `domain/turn.go` (`ModelTurn{Reply, Proposal *IntentProposal}`), `domain/errors.go` += `ErrSessionNotFound`/`ErrSessionTitleInvalid`/`ErrInvalidModelTurn`; domain tests.
- `application/turnvalidation.go` (`ValidateModelTurn`, `MaxReplyRunes=2000`; exact top-level keys `schemaVersion`/`reply`/`proposal`; version `"1"`; reply 1..2000 runes; proposal null or v1-valid via the untouched `ValidateProposal`) + `turnvalidation_test.go`; `intentvalidation_test.go` stays as the v1 regression pin.
- `ports/store.go`: `RoleAssistant`; `MessageLog.SessionID *string`; `MessageLogEntry`; `MessageLogStore.ListBySession`; new `SessionStore{Create,Get,List,Rename,Delete,Touch}`. **`ModelPort` still `Propose` in this task** so the tree compiles.
- `dto/response.go`: `KindChat`, `MessageResponse.Reply/.SessionID`, `SessionView`, `SessionListView`, `MessageView`, `SessionHistoryView`.
- `adapters/outbound/postgres/sessions.go` (ambient-tx executor, scoped conditional writes with rows-affected → `ErrSessionNotFound`, list `updated_at desc, id desc limit`); `messages.go` Append writes `session_id`, new `ListBySession` (desc-limit then reverse); integration tests (truncate += `conversation.sessions`).
- Widen `application/command/fakes_test.go` for the new port methods.

Verify: `go test ./backend/internal/modules/conversation/... -race` (postgres tests with `TEST_DATABASE_URL`).

## Task 4 — Config key (yellow)

`platform/config`: `ConversationHistoryTurns int` from `CONVERSATION_HISTORY_TURNS` (default 10, fail-closed bounds 0..50) + `config_iter0005_test.go` (default/0/50/51/-1/garbage). `.env.example` += `CONVERSATION_HISTORY_TURNS=10`; `compose.yaml` api service passthrough. Verify: config tests, `docker compose config --quiet`.

## Task 5 — Atomic backend cutover (green module + yellow wiring)

The `Propose`→`Complete` rename, `Handle` signature change, wiring, and composition tests must land together.

1. `ports/model.go`: `ModelPort.Complete`; `MessageInput.History []HistoryMessage{Role,Text}` (oldest first).
2. deterministic adapter: envelope output, fixed per-family replies, exported `EchoReplyTemplate`, history ignored; update `adapter_test.go` + `corpus_eval_test.go` — every existing proposal pin unchanged (decoded from `envelope.proposal`), new reply/echo pins.
3. openai adapter: `Complete`, unified prompt (design §6), history mapping system→history→user; update `adapter_test.go` (canned envelope via httptest only; prompt contains `"reply"`/`"proposal"` + carried-over v1 fragments; retry/timeout semantics unchanged).
4. application: `ProcessMessageHandler` v2 (+`Sessions`, `NewSessionID`, `HistoryTurns`; `Handle(ctx, ws, user, sessionID, text, tz)`), `application/transcript.go` summary builder, `command/{create_session,rename_session,delete_session}.go`, `application/query/{list_sessions,get_history}.go` (limits 100/200); rewrite `command_test.go`/`fakes_test.go` per the test inventory.
5. HTTP: `sessionId` on messages + `session_not_found`→404; new `sessions.go` with the five routes; `handler_test.go` — auth table covers all 8 routes, per-route 200/201/204/404/422 cases, strict-body rejections.
6. `cmd/api/wiring.go`: build `convpostgres.NewSessionStore(pool)`, inject `Sessions`/`NewSessionID`/`HistoryTurns`, construct session commands + queries into `conversationhttp.Handler`; `selectModel` shape unchanged.
7. `composition_integration_test.go`: `今天天气怎么样` → `chat` + echo reply; audit `wantIntents` → `["todo.create","todo.list","todo.delete","todo.list","chat"]` (assistant rows NULL, skipped) + one shared non-null `session_id`; new `TestConversationSessionsEndToEnd` (auto-create, list, explicit-session post, history order/contents, two-session isolation, rename, delete→204+cascade, unknown session→404).

Verify: `go test ./backend/... -race`, `make harness-test`, `make architecture-test`.

## Task 6 — Contracts (yellow)

`contracts/openapi/conversation.yaml`: request += optional `sessionId`; kind enum += `chat`; response += `reply`/`sessionId`; five new paths + closed-object schemas `SessionView`, `SessionListResponse`, `SessionCreateRequest`, `SessionRenameRequest`, `SessionHistoryResponse`, `MessageView` (role enum `[user,assistant]`); 401/404/422 error envelopes. Update `tests/contract/conversation_contract_test.go` in the same commit. Verify: `go test ./tests/contract -race`.

## Task 7 — Web fetch layer (green)

`features/conversation/fetch-conversation.ts`: kinds += `"chat"`; types/`ALLOWED_KEYS` += `reply?`/`sessionId?`; base keys `["kind","correlationId","sessionId"]`; `chat` = base+non-empty `reply`; `clarification` += `reply`; `postConversationMessage(…, sessionId?)` includes it only when set. New `fetch-sessions.ts` (`listSessions/createSession/renameSession/deleteSession/fetchSessionMessages`) with strict validators + failure classification. Tests for both. Verify: `pnpm --filter @artificial-brain/web test`.

## Task 8 — Web UI (green)

New `session-sidebar.tsx` (presentational) and `conversation-view.tsx` (owns sessions + activeId; create/select/rename/delete with `window.confirm`); `chat-panel.tsx` refactor (turn model `{id,userText,assistant:{source:"response"|"text",…}}`, history load on `sessionId`, `case "chat"`, clarification prefers `reply`, sessionId pinned via ref + `onSessionCreated` without re-keying); `conversation/page.tsx` renders `ConversationView`; `globals.css` additive rules for every new class token. Tests: new `session-sidebar.test.tsx`, `conversation-view.test.tsx`; `chat-panel.test.tsx` — existing cases keep passing + chat bubble, history-as-text, sessionId in body, `onSessionCreated`, clarification reply. Verify: web `format:check`, `lint`, `test`, `build`.

## Task 9 — Smoke (yellow)

Extend `tests/smoke/stack_test.sh` conversation block: free-chat message → `.kind=="chat"` and `.reply` contains `你说的是`; create session → post with explicit `sessionId` → `GET sessions` shows it → `GET .../messages` contains user text + assistant body → `PATCH` rename → `DELETE` 204 → `GET` 404. Deterministic adapter only. Verify: `make smoke-test`.

## Task 10 — Unified verification, docs, handoff

Fill `progress.md`/`decisions.md`/`test-matrix.md`; run the verification sequence (toolchain-check, harness-test, backend `-race` with DB, contract, architecture, web quartet, `make verify`, `make migration-test`, `make smoke-test`); zero-diff checks (`go.mod`, `package.json`, `pnpm-lock.yaml`, `git diff --name-only deploy/migrations/` shows only 010); manual `make dev` walkthrough; independent clean-context regression → `regression-report.md`.

## Test inventory (files that change behavior assertions)

`application/command/command_test.go` (Handle +sessionId; envelope fakes; unsupported test split: unknown→chat+2 rows, invalid proposal→unsupported+2 rows; clarification tests 0→2 rows; happy paths assert SessionID + assistant row; new: chat persistence, auto-create title, unknown session→ErrSessionNotFound, history window, invalid envelope, HistoryTurns=0) · `fakes_test.go` · `domain/domain_test.go` · `turnvalidation_test.go` (new) · deterministic `adapter_test.go`/`corpus_eval_test.go` · openai `adapter_test.go` · postgres `integration_test.go` · http `handler_test.go` · `cmd/api/composition_integration_test.go` · `tests/contract/conversation_contract_test.go` · `platform/config/config_iter0005_test.go` (new) · `migrate_integration_test.go` + `migration_test.sh` (pin 10) · `stack_test.sh` · web `fetch-conversation.test.ts`, `chat-panel.test.tsx`, new `fetch-sessions.test.ts`, `session-sidebar.test.tsx`, `conversation-view.test.tsx`.
