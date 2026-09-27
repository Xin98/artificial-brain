# ITER-0005 decisions

These choices are specific to conversation sessions, free chat, and history. The summary table lives in the [iteration design](../../superpowers/specs/2026-09-27-iter-0005-conversation-sessions-design.md) §1; broader architecture remains governed by the [MVP design](../../superpowers/specs/2026-08-13-ai-native-personal-workbench-mvp-design.md).

## D1 — Unified turn envelope (schema v1), v1 proposal nested unchanged

`ModelPort` becomes `Complete(ctx, MessageInput) (json.RawMessage, error)` returning `{"schemaVersion":"1","reply":"…","proposal":{…}|null}`. `ValidateProposal` (v1) stays untouched as the inner choke point; a new `application.ValidateModelTurn` validates the envelope (exact top-level keys, version `"1"`, reply 1..2000 runes, proposal null or v1-valid). Rejected: a proposal "v2" merging reply into the proposal — it would churn the frozen validator, both adapters' pinned outputs, and every proposal test.

## D2 — Dispatch rules

Envelope invalid OR proposal present-but-invalid → `unsupported` (fail-closed; preserves the injection-proposal test semantics — an invalid action never executes and never masquerades as chat). Proposal `null` or intent `unknown` → new `chat` kind with `reply` (replaces today's `unsupported` for non-intent input). Registered intents keep every existing gate: MissingFields → clarification; confidence <0.6 → clarification; delete → candidates/confirmation exactly as before. Clarification additionally carries the model `reply` so the UI shows a natural-language question. The `Router` and the confirmation safety flow are untouched.

## D3 — Full transcript persistence

Every turn persists a user row AND an assistant row in the same unit of work (previously only dispatched user rows). User-row `resolved_intent` ∈ {`todo.create`,`todo.list`,`todo.delete`,`chat`,`clarification`,`unsupported`}; assistant rows carry `role="assistant"`, NULL intent, and `body` = model reply (chat/clarification) or a deterministic server-built Chinese summary (dispatched kinds, e.g. `已创建待办「X」，提醒时间 …`; unsupported: `这个请求暂时不支持。`). No response-payload jsonb — history renders text; interactive widgets stay live-turn-only (avoids stale confirmation countdowns). This intentionally changes the long-standing "no audit row for clarification/unsupported" behavior.

## D4 — Storage: nullable `session_id` on `conversation.messages`

Migration 010 adds `conversation.sessions` and a nullable cascading FK column on the existing messages table instead of a new table: legacy rows stay valid with NULL, no copy, append-only migration. Rejected: a separate `session_messages` table (duplicates the audit seam and complicates `ListByUser`).

## D5 — `sessionId` optional; auto-create when absent

`POST /messages` without `sessionId` auto-creates the session inside the turn's UoW (title = first 30 runes of trimmed text, fallback `新会话`); an explicit id is ownership-checked (workspace+user) else 404 `session_not_found`. Every messages response carries `sessionId` so clients pin the session. Invariants: title 1..50 runes; sidebar ≤100 sessions `updated_at desc`; history ≤200 latest messages ascending; `updated_at` touched each turn. No pagination/archive in v1.

## D6 — History window to the model

New config `CONVERSATION_HISTORY_TURNS` (default 10, bounds 0..50, 0 disables), fail-closed validation. Bodies truncated to 500 runes each when building model input (token safety). The deterministic adapter ignores History (stays corpus-exact); the OpenAI adapter maps it to alternating user/assistant messages between system and the live user message.

## D7 — Deterministic adapter (dev/CI, never a real LLM)

Corpus lines and their proposal objects stay byte-identical (nested under `"proposal"`); each family gains a fixed reply (createExact `好的，我记下了「<title>」的提醒安排。`; createMissingDue `好的，请问「<title>」要在什么时间提醒？`; delete `好的，我先找一下与「<keyword>」相关的待办。`; list `好的，这就为你查询待办。`). Unknown/unmatched → exported constant `EchoReplyTemplate = 你说的是：「%s」。这个我暂时不能直接执行。我可以帮你创建、查询或删除待办，也可以继续聊天。` with the first 100 runes of trimmed input, keeping CI assertions deterministic and the UI demoable.

## D8 — OpenAI adapter unified prompt

New Chinese system prompt: assistant persona; MUST return exactly one JSON object envelope (no markdown/fences); reply = natural language in the user's language, non-empty ≤2000 chars; `proposal=null` unless the turn is a todo operation; the embedded v1 proposal JSON-schema text and per-intent argument rules carried over verbatim; missing info → `missingFields` plus a reply that asks; no fabrication; current time + timezone as today. Same timeout, single retry, `response_format=json_object`, config gating; unit tests only ever hit `httptest`.

## D9 — Confirmations stay sessionless in v1

`POST /api/v1/confirmations` / `confirm` are unchanged; confirmed deletes remain live-UI-only (the persisted transcript keeps the `confirmation_required` summary). Documented limitation; optional `session_id` on `confirmation_requests` is a follow-up.

## D10 — Portability export of conversation history: out of scope

`contracts/export-schemas/` untouched; conversation data does not enter export bundles in this iteration.
