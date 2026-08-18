# ITER-0002 decisions

These choices are specific to the Identity/Todo/Conversation closed loop. Broader architecture remains governed by the [MVP design](../../superpowers/specs/2026-08-13-ai-native-personal-workbench-mvp-design.md) and the [iteration design](../../superpowers/specs/2026-08-17-iter-0002-identity-todo-conversation-loop-design.md).

## Confirmed scope decisions

- **Local login via cloud mode + fake SMS inbox.** Compose runs in cloud mode. The fake SMS adapter writes login and channel-verification codes to the `identity.message_outbox` table; a dev-only, double-gated inbox endpoint returns the latest code for a phone number so a human can complete login locally. Tests inject deterministic codes.
- **Reminder scheduling seam.** This iteration introduces the `JobScheduler` port plus a no-op adapter; due-dated Todos create a Reminder Plan but nothing is delivered. This pre-warms ITER-0003 (River) without provider calls.
- **Deterministic + OpenAI-compatible model adapters.** Both are implemented. The deterministic corpus adapter is the default and primary testable path; the OpenAI-compatible adapter is config-gated and contract-tested with fake HTTP, never pointed at a real model in CI.

## D1 — Todo→Reminder seam via consumer-owned ports and a platform transaction capability

`todo/application/ports` defines `UnitOfWork` and `ReminderPlanner`; `platform/database` gains `Executor`/`Tx`/`ExecutorFromContext`/`NewTxRunner`. Repositories resolve their execer from context, so Reminder's public `PlanReminder`/`RevokePlans` handlers join the caller's ambient transaction without holding connections — respecting "application may not import pgx" and "cross-context only via public application interfaces". `cmd/api` wires the `TxRunner` and a cmd-local shim adapting Reminder handlers to Todo's `ReminderPlanner`. This is the seam ITER-0003 swaps to River `InsertTx` with zero application change. Rejected: non-transactional orchestration (violates MVP §6.2 atomicity), outbox/saga (overbuilding), orchestration in `cmd` (composition-only rule).

## D2 — Session middleware lives in Identity's inbound adapter

Platform may not import business modules, so session/auth middleware belongs to Identity, not `platform/server`. Identity exports `NewAuthMiddleware(authenticator)` and `cmd/api` wraps each protected route explicitly. `dto.Principal{UserID, WorkspaceID, SessionID}` plus context helpers live in `identity/application/dto`; other modules' adapters import that application package, which is policy-legal and matches the CONTEXT-MAP "Identity → all" relationship.

## D3 — Routing via stdlib Go 1.26 `http.ServeMux` patterns; zero new dependencies

`platform/server` adds `RegisterHealthRoutes` and `NewAPIRouter` (Correlation plus a `ResponseWriter` interceptor that converts ServeMux's plain-text default 404 and 405 into the stable JSON envelope, preserves the Allow header, and passes `application/json` handler output through untouched). A router dependency (chi/gorilla) fails the zero-new-deps rule and buys nothing over stdlib method+path patterns.

`NewAPIHandler` is deliberately **not** reimplemented atop the mux: the ITER-0001 flat handler returns `405 method_not_allowed` for any non-GET request regardless of path (including `POST /unknown`), whereas a mux returns `404` for an unmatched path. Keeping `NewAPIHandler` unchanged keeps those pinned ITER-0001 assertions green; `cmd/api` moves to `RegisterHealthRoutes` + `NewAPIRouter` when identity routes land. No catch-all `/` route is registered, so a wrong-method request on a registered path yields 405 and a truly unmatched path yields the JSON 404 envelope.

## D4 — Delete confirmation is Conversation-owned and serves smart and manual deletes

`POST /api/v1/confirmations` accepts a concrete `todoId` with no model involvement; confirm executes Todo's public `DeleteTodo` inside one transaction (conditional `consumed_at` update plus version re-check makes it single-use and bounded). This keeps Confirmation Request ownership in Conversation per `docs/domain/conversation/CONTEXT.md`, preserves the Conversation→Todo direction, and gives MVP §2.1's "均需确认" a single business entry point. There is deliberately no raw `DELETE /api/v1/todos/{id}`.

## D5 — Fake outbox is a database table

`identity.message_outbox` is written only by fake adapters; login and channel verification share it with distinct `purpose` values. Plaintext codes exist only in this dev-only table (an explicit exception); all other code storage is hash-only. Production swaps the adapter, never the reader.

## D6 — Conversation's ports may import `todo/application/dto`

Cross-context *application* imports are legal under the architecture policy (only another context's `domain`/`adapters` are banned), and Todo's DTOs are part of its public application surface. This avoids cmd-level mapping boilerplate for the `TodoGateway` port while Conversation never touches Todo domain or adapters. If DTO churn becomes a problem, introduce an anti-corruption layer then, not now.

## D7 — Dashboard reminder counters are deterministic zeros until ITER-0003

The dashboard returns `reminderRetrying` and `reminderFailed` as `0` because delivery does not exist yet. The fields are present so the contract and UI are stable when ITER-0003 populates them.

## D8 — No cross-schema foreign keys between context schemas

Each context owns its schema (`identity`, `todo`, `reminder`, `conversation`) without cross-schema FKs, preserving context independence and future portability. Isolation is application-enforced (`workspace_id`/`owner_user_id` on every query) and proven by integration tests.

## D9 — LF line endings enforced; POSIX gates run via WSL on this Windows host

The ITER-0001 harness self-test creates a filename containing a literal newline and real POSIX symlinks, which Windows/NTFS and Git-for-Windows cannot represent, so `make harness-test` (and therefore `make verify`) cannot pass on the native Windows shell. This is an environment limitation, not a defect in ITER-0002. Two changes make the gates runnable and portable:

1. A repo-level `.gitattributes` (`* text=auto eol=lf`) enforces LF normalization so shell scripts, Go, TypeScript, YAML, and Markdown behave identically on macOS, Linux, Windows, and WSL. Existing tracked files were re-normalized to LF (index content unchanged; only working-tree EOL). This does not weaken any gate on Unix.
2. On this Windows host, the POSIX verification gates (`make verify`, `make migration-test`, `make smoke-test`) are executed through WSL (`wsl -d Ubuntu`), which has the pinned toolchain (Go 1.26.5, Node.js 24.18.0, pnpm 11.19.0, make, ruby, jq, curl) and Docker. Targeted Go/TS checks may still run natively, but the authoritative gate sequence runs in WSL.

## D10 — Linux development host: native toolchain, dedicated test database, mirrored image builds

Development moved from the Windows+WSL host in D9 to an Apsara Linux 3 machine, so the verification environment changes accordingly:

1. The pinned toolchain runs natively: Go 1.26.5 is installed at `/usr/local/go` (host `GOPROXY` points at the Aliyun mirror) and `ruby` is installed for the bounded-execution gates. Node.js 24.18.0 / pnpm 11.19.0 are still pending and must be installed before Task 15's web gates.
2. Docker bridge networks on this host have no internet egress, but `--network host` works. A gitignored local `compose.override.yaml` sets `build.network: host` for the image builds; runtime services keep the default compose network (container-to-container traffic works), so the committed `compose.yaml` stays unchanged.
3. Google-owned hosts (`proxy.golang.org`, `sum.golang.org`) and `registry.npmjs.org` are unreachable even on the host network, and Alpine's `dl-cdn` package downloads stall; Aliyun/npmmirror mirrors are reachable. `backend/Dockerfile` therefore declares `ARG GOPROXY` (default `https://proxy.golang.org,direct`) for the build stage and `ARG APK_MIRROR` (default `dl-cdn.alpinelinux.org`) for the test stage's `apk add`, and `apps/web/Dockerfile` declares `ARG NPM_REGISTRY` (default `https://registry.npmjs.org/`) ahead of the frozen `pnpm install`; defaults keep upstream behavior everywhere else, and the local override passes the Aliyun Go module proxy, APK mirror, and npmmirror registry so image builds succeed here. This is a deliberate yellow-zone edit recorded here; CI and other hosts build with the defaults.
4. Integration tests on this host use a dedicated `postgres:18.4-alpine` container (`ab-test-postgres`, host port 5433) via `TEST_DATABASE_URL`, keeping the compose dev stack and its data untouched. Packages sharing that database run with `-p=1` because each package's setup truncates its context tables.

## D11 — Cross-context units of work compose through a cmd-local joinable runner

Conversation's `ProcessMessage`/`ConfirmAction` wrap gateway dispatch and the audit append in their own `UnitOfWork`, but Todo's public handlers already own units of work bound to the platform `TxRunner`, which rejects nesting with `ErrAlreadyInTx`. `cmd/api` therefore wires a small `joinableUoW` for both contexts: when the context already carries an ambient executor it runs the work directly; otherwise it starts a transaction via `TxRunner`. Each dispatched conversation turn composes into exactly one real transaction — todo row, reminder plan, and audit row commit or roll back together — while the platform keeps its nesting guard and no context imports the other.

## D12 — Web image bakes the internal API URL; absolute FQDN on the Linux host

ITER-0002 adds the `/api/v1` rewrite proxy, and Next.js bakes rewrite destinations into the routes manifest at build time. `apps/web/Dockerfile` therefore accepts `ARG API_INTERNAL_URL` (default `http://localhost:8080`) and `compose.yaml` passes `http://api:8080` as the build arg plus the runtime default (`${API_INTERNAL_URL:-http://api:8080}`); in ordinary environments nothing changes.

On this Linux host the Docker variant injects the host's `ndots:5` cluster search suffixes into container `resolv.conf` and ignores `dns_search`, so getaddrinfo fails on short service names (the suffixes even resolve `api` to a corporate ingress). The gitignored local override therefore uses the trailing-dot absolute form `http://api.:8080` for both the web build arg and the runtime env — the dot makes the resolver skip the search list — and sets `dns_search: []` defensively for every service.

The backend runtime image also installs `tzdata` alongside the CA bundle: timezones travel per request (A1) and both the dashboard and the conversation validation choke point load IANA zones at runtime.

## Working assumptions

Timezone travels per request (no stored profile timezone); rate limiting is phone-dimension only; session rotation means a fresh token per successful login with revocation on logout; `RequestedChannels` on a Reminder Plan is a snapshot that may be empty; `Secure` cookies are usable over `http://localhost` in development; manual delete reuses the Confirmation flow; client components are introduced only for interactivity with zero UI/form/schema libraries; the deterministic corpus is embedded in Go source; clarification is stateless single-turn; candidate search is title-substring (ILIKE) capped at 11 rows; phone validation is E.164-like and email is structural-only; the worker binary is unchanged except the schema constant.
