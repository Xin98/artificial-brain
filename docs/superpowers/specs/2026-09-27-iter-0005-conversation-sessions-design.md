# ITER-0005 design — conversation sessions, free chat, and history

Status: approved 2026-09-27. Supersedes the ITER-0004 out-of-scope note on open-ended chat. The [MVP design](2026-08-13-ai-native-personal-workbench-mvp-design.md) and the [ITER-0002 design](2026-08-17-iter-0002-identity-todo-conversation-loop-design.md) remain the broader authority; this document governs the Conversation upgrade only.

## 1. Decision table

| # | Decision | Summary |
| --- | --- | --- |
| D1 | Unified turn envelope | `ModelPort.Propose` becomes `Complete`; one call returns `{"schemaVersion":"1","reply":…,"proposal":{…}|null}`. The v1 proposal schema and `ValidateProposal` stay byte-compatible as the inner choke point; a new `ValidateModelTurn` validates the envelope. |
| D2 | Dispatch rules | Invalid envelope or invalid proposal → `unsupported` (fail-closed). `proposal=null` or `unknown` intent → new `chat` kind carrying `reply`. Registered intents keep every existing gate (missingFields/low-confidence clarification, candidates, confirmation). |
| D3 | Full transcript persistence | Every turn writes a user row and an assistant row in the same unit of work. Assistant bodies are the model reply (chat/clarification) or deterministic server-built Chinese summaries (dispatched kinds). History replay is text-only; interactive widgets stay live-turn-only. |
| D4 | Storage | Migration 010 adds `conversation.sessions` and a nullable `conversation.messages.session_id` FK (cascade). Legacy NULL rows remain valid; no data backfill. |
| D5 | Session semantics | `sessionId` optional on `POST /messages`; auto-create from the first message (title = first 30 runes, fallback `新会话`). Explicit ids are ownership-checked (404 `session_not_found`). Sidebar lists ≤100 sessions `updated_at desc`; history returns ≤200 latest messages ascending. Title 1..50 runes. No pagination/archive in v1. |
| D6 | History window | `CONVERSATION_HISTORY_TURNS` (default 10, bounds 0..50, 0 disables). Bodies truncated to 500 runes when building model input. The deterministic adapter ignores history; the OpenAI adapter maps it to alternating messages between system and the live user turn. |
| D7 | Deterministic dev/CI adapter | Corpus lines and their proposal objects stay byte-identical (nested under `"proposal"`); each intent family gains a fixed reply; unmatched input returns the exported `EchoReplyTemplate` echo with the first 100 runes of the input. |
| D8 | OpenAI adapter | Unified Chinese system prompt producing exactly one JSON envelope (reply + optional proposal), v1 proposal rules carried over verbatim, same timeout/single-retry/`response_format=json_object`/config gating. Never called from CI. |
| D9 | Confirmations stay sessionless | `/api/v1/confirmations` routes and semantics unchanged in v1; documented limitation, optional `session_id` binding is a follow-up. |
| D10 | Portability export of conversation history | Out of scope; `contracts/export-schemas/` untouched. |

## 2. Architecture

Dependencies keep the hexagonal direction `inbound → application → domain`; adapters implement consumer-owned ports; `cmd/api` composes. The Conversation module gains:

- **domain**: `Session` entity (ownership, title invariants, `DefaultSessionTitle`), `ModelTurn` value (reply + optional proposal), new errors (`ErrSessionNotFound`, `ErrSessionTitleInvalid`, `ErrInvalidModelTurn`).
- **application**: `ValidateModelTurn` envelope validation beside the untouched v1 `ValidateProposal`; `ProcessMessageHandler` v2 (session resolution → history read → `Complete` → envelope validation → dispatch with transcript writes); session commands (`create/rename/delete`); `application/query` (`list_sessions`, `get_history`) mirroring the other contexts' query shape; a transcript helper building deterministic assistant summaries.
- **ports**: `ModelPort.Complete` with `MessageInput.History`; `SessionStore` (Create/Get/List/Rename/Delete/Touch); `MessageLogStore.ListBySession` + `MessageLog.SessionID` + `RoleAssistant`.
- **adapters**: deterministic and openai model adapters emit the envelope; postgres gains `sessions.go` (ambient-transaction aware, scoped conditional writes) and `ListBySession`; HTTP gains `sessions.go` (five routes) and `sessionId`/`reply` on the messages flow.

Safety invariants preserved: the delete confirmation flow (single-use, version-bound, expiry) is semantically untouched; an invalid structured action can never execute and never masquerades as chat; strict request decoding and the `{code,message,correlationId}` error envelope continue on all routes.

## 3. API surface

`POST /api/v1/conversation/messages` request gains optional `sessionId`; the response envelope gains `reply` (chat always; clarification when supplied) and `sessionId` (always), and the kind enum gains `chat`. All existing kinds keep their shapes. New routes, all behind the same session-cookie auth:

| Route | Success | Errors |
| --- | --- | --- |
| `GET /api/v1/conversation/sessions` | 200 `{"sessions":[SessionView…]}` (≤100, `updated_at desc`) | 401 |
| `POST /api/v1/conversation/sessions` `{"title"?}` | 201 SessionView | 401, 422 invalid title |
| `GET /api/v1/conversation/sessions/{sessionId}/messages` | 200 `{"sessionId","title","messages":[…]}` (≤200 latest, ascending) | 401, 404 |
| `PATCH /api/v1/conversation/sessions/{sessionId}` `{"title"}` (1..50) | 200 SessionView | 401, 404, 422 |
| `DELETE /api/v1/conversation/sessions/{sessionId}` | 204 | 401, 404 |

New stable error code: `session_not_found` (404).

## 4. Data

`deploy/migrations/010_conversation_sessions.sql` (append-only; 001–009 frozen):

- `conversation.sessions(id uuid pk, workspace_id uuid not null, user_id uuid not null, title text not null, created_at timestamptz not null, updated_at timestamptz not null)` + index `(workspace_id, user_id, updated_at desc)`.
- `alter table conversation.messages add column session_id uuid null references conversation.sessions(id) on delete cascade` + index `(session_id, id)`.
- Schema version pin 9→10 in `platform/database/schema.go`, `migrate_integration_test.go`, `tests/smoke/migration_test.sh`.

Transcript rows: user rows carry `resolved_intent ∈ {todo.create, todo.list, todo.delete, chat, clarification, unsupported}`; assistant rows carry `role='assistant'`, NULL intent. This intentionally changes the pre-ITER-0005 behavior where clarification/unsupported wrote no audit row — sessions require the full transcript.

## 5. Web

Zero new dependencies. The conversation page becomes a two-pane layout: `SessionSidebar` (new/list/switch/rename/delete, delete behind `window.confirm`) + `ChatPanel`. `ChatPanel` loads history as text-only paired turns on session open, renders the new `chat` kind, prefers `reply` for clarification, posts `sessionId` when pinned, and reports auto-created sessions upward without re-keying (live turns and confirmation widgets survive). `fetch-conversation.ts` strict validation widens with `chat`/`reply`/`sessionId`; a new `fetch-sessions.ts` follows the same failure-classification pattern. Every new class token gets a `globals.css` rule (house gate).

## 6. Model adapters

- **deterministic** (dev/CI default, never a real LLM): fixed replies per intent family (`好的，我记下了「X」的提醒安排。` etc.); unmatched → `EchoReplyTemplate = 你说的是：「%s」。这个我暂时不能直接执行。我可以帮你创建、查询或删除待办，也可以继续聊天。` with the first 100 runes. Existing corpus proposal pins stay byte-identical inside the envelope.
- **openai_compatible**: unified system prompt (assistant persona; exactly one JSON object envelope, no markdown; reply in the user's language, non-empty ≤2000 chars; `proposal=null` unless the turn is a todo operation; v1 schema text and per-intent argument rules verbatim; missing info → `missingFields` plus an asking reply; no fabrication; current time + timezone as today). History maps to alternating user/assistant messages between system and the live turn.

## 7. Constraints

Migrations 001–009 byte-untouched; no real-provider egress from CI (unit tests use `httptest`; smoke/composition run the deterministic adapter); no committed credentials; no lowered gates; zero new Go modules or web dependencies; toolchain pins unchanged (Go 1.26.5, Node.js 24.18.0, pnpm 11.19.0).
