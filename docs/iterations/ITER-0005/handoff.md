# ITER-0005 handoff

ITER-0005 (conversation sessions: free chat, history, session persistence) is implemented on branch `iter-0005-conversation-sessions`. The branch baseline is `bbd8d48` (the merged tip of `feat/make-deploy`, which itself includes the latest master merge); the ITER-0005 commits run `2b8b4ce..5b2ce1b` (`bbd8d48..5b2ce1b`: 71 files changed, +5449/−307, before this ledger's final touch-ups). Start with [brief.md](brief.md), [progress.md](progress.md), [decisions.md](decisions.md), and the [test matrix](test-matrix.md); the governing design is `docs/superpowers/specs/2026-09-27-iter-0005-conversation-sessions-design.md`.

## Current state

All 10 tasks are implemented and verified; the independent clean-context regression returned **PASS** at `5b2ce1b` ([regression-report.md](regression-report.md): all gates exit 0, 14 PASS / 0 PARTIAL / 0 FAIL requirement mapping, compliance clean). ITER-0005 is iteration-complete and ready for merge review. The Conversation module moved from a single-turn intent parser to a session-based assistant:

- **Unified model turn (D1/D2/D8).** `ModelPort` is now `Complete(ctx, MessageInput) (json.RawMessage, error)` returning the envelope `{"schemaVersion":"1","reply":"…1..2000 runes…","proposal":{…v1…}|null}`. `application.ValidateModelTurn` validates the envelope; the v1 `ValidateProposal` is untouched as the inner choke point. Invalid envelope/proposal stays fail-closed `unsupported`; null/unknown proposal becomes the new `chat` kind carrying the model reply; every registered-intent gate (missing fields, low confidence, delete confirmation) is unchanged, and clarification now carries the optional model reply.
- **Sessions (D4/D5).** Migration 010 adds `conversation.sessions` plus a nullable cascading `messages.session_id` (schema 9→10; migrations 001–009 byte-untouched). `POST /api/v1/conversation/messages` accepts an optional `sessionId` and auto-creates a session inside the turn's unit of work (title = first 30 runes, fallback 新会话); foreign sessions answer 404 `session_not_found`. Five new routes: list/create sessions, per-session history (≤200 ascending), rename (1..50 runes), delete (204, cascade). Every messages response carries `sessionId`.
- **Full transcript (D3).** Each turn writes user + assistant rows in the same UoW; dispatched kinds get a deterministic Chinese assistant summary. History replay is text-only — interactive components (confirmation countdown, candidate buttons) render only in live turns. The confirmation flow (`/api/v1/confirmations`) is deliberately session-unbound in v1 (D9).
- **History window (D6).** `CONVERSATION_HISTORY_TURNS` (default 10, fail-closed bounds 0..50, 0 = off) bounds the replay sent to the model; each entry truncates at 500 runes.
- **Adapters (D7/D8).** The deterministic adapter keeps every corpus proposal pin byte-identical inside the envelope, adds fixed per-family replies, and answers unmatched input with the `EchoReplyTemplate` echo (first 100 runes). The OpenAI-compatible adapter maps history system→history→user against a new unified Chinese prompt; unit tests stay on `httptest` only — CI never egresses to a real provider.
- **Web.** `chat` kind + strict `reply`/`sessionId` validation; new `fetch-sessions.ts`; `conversation-view.tsx` (sidebar state, delete via `window.confirm`, auto-create refreshes the sidebar **without** re-keying the panel so live turns survive) + presentational `session-sidebar.tsx` (inline rename, `aria-current`); `chat-panel.tsx` pins `sessionId` in a ref and replays history as text-only turns. 165 vitest tests, eslint/tsc/next build green.
- **Contracts + smoke.** `conversation.yaml` grew the five session paths and six closed schemas; the contract harness gained DELETE ops, `minLength`, and the content-less-204 convention (empty schema name) with a mutation pin. The smoke gate exercises the free-chat echo and the full session lifecycle end to end, and all smoke compose invocations are now hermetic (`--env-file /dev/null`) so a developer's local root `.env` cannot flip the stack into private mode.

Useful URLs once a stack is up:

- `http://localhost:3000/conversation` — sidebar + history-aware chat (session-gated)
- `POST /api/v1/conversation/messages` — body `{text, timezone, sessionId?}`; response `{kind, correlationId, sessionId?, reply?, …}` with `kind ∈ {todo_created, todo_list, todo_deleted, clarification, confirmation_required, unsupported, chat}`
- `GET|POST /api/v1/conversation/sessions`, `GET …/sessions/{id}/messages`, `PATCH|DELETE …/sessions/{id}`
- the ITER-0004 surface (dashboard, `/data` portability, reminders, private mode) is unchanged

## How to continue

1. Run the acceptance sequence exactly as CI does: `corepack pnpm install --frozen-lockfile`, `make verify`, `make migration-test`, `make smoke-test` (the smoke script fail-fasts; it needs `curl`, `jq`, Ruby, and `unzip`).
2. The independent clean-context regression follows the house contract: a reviewer with only the approved spec, this ledger, the `bbd8d48..HEAD` diff, and the README/Makefile re-runs the gates, maps the acceptance criteria to evidence, checks zone compliance (zero new Go/web dependencies; migrations 001–009 untouched; no real-provider egress in CI; no credentials), and writes `regression-report.md`.
3. Respect the zones: business-module work is green; the yellow register in [plan.md](plan.md) (policy files, migration 010 + pins, platform config, wiring, public contract, globals.css, smoke) is deliberately handled; red zone for this iteration: no real providers from CI, migrations 001–009 frozen, no new dependencies, no gate lowering.
4. Known limitations carried forward: confirmation flow not session-bound (D9); conversation history not in the portability export (D10); no session pagination/archival (v1 caps: 100 sessions listed, 200 messages replayed).

## Environment prerequisites

Go 1.26.5 (or newer 1.26 patch), Node.js 24.18.0, pnpm 11.19.0, Docker Compose v2, `curl`, `jq`, Ruby, and `unzip` (on this host both Ruby and `unzip` had gone missing since ITER-0004 and were reinstalled via dnf during Task 10). Integration tests need `TEST_DATABASE_URL` (this host uses the dedicated test container on host port 5433; packages sharing that database run with `-p=1`).

### Verification environment on the current Linux host

- The host has 1.8 GB RAM with the maintainer's live private stack running on ports 80/8080 — **do not run `make dev`/`docker compose up` against the root project**; use an isolated `--project-name` with `API_PORT=0 WEB_PORT=0` (the smoke script's pattern) or the ephemeral projects the gates already create.
- A 3 GB swapfile (`/swapfile`) is enabled; `next build` needs it. `vm.swappiness` was found at 0 (the kernel OOM-killed large test binaries instead of swapping) and was raised to 100.
- The full `-race` backend suite must run serially: `TEST_DATABASE_URL=… go test ./backend/... -race -count=1 -p 1`. The ITER-0004 `portability/…/archive` package (zero diff on this branch) spikes ~1 GB RSS in its decompression-cap tests and gets OOM-killed when its tests run concurrently; it passes green with `GOMEMLIMIT=900MiB GOGC=50 … -parallel 1`. For the same environmental reason `make verify` was run with `GOFLAGS=-p=1 GOMEMLIMIT=900MiB GOGC=50`, and `make migration-test` (whose in-container `go test` cannot take shell env) was run through a host-local `docker` PATH shim that injects `-e GOMEMLIMIT=900MiB -e GOGC=50` into `docker compose … run` invocations only. No gate semantics change anywhere: every test still runs in full.
- The root `.env` holds the maintainer's live private-deployment config (gitignored, mode 600). It must never be committed; smoke compose invocations pass `--env-file /dev/null` so it cannot leak into gate runs.
- Container egress notes carried over from ITER-0002–0004 (gitignored `compose.override.yaml` with Aliyun/npmmirror build args; absolute `http://api.:8080` form) are unchanged.
