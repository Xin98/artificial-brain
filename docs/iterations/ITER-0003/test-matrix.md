# ITER-0003 test matrix

Rows are recorded as each task reaches green. IDs group by subsystem: `RMD` reminder domain, `DLV` delivery application, `RIV` River adapters, `PRV` provider adapters, `HTP` reminder HTTP, `TDO` todo seam, `CNT` contracts, `WEB` web, `MIG` migrations/schema, `OPS` ops endpoint.

| Requirement | Command and evidence | Evidence commit | Status |
| --- | --- | --- | --- |
| MIG-01 — schema advances 5→7 via append-only migrations (River v1 inlined + delivery tables) | `make migration-test`; empty-schema gate, idempotent re-run, `schema_version=7` assertion, `river_job` in `public` plus `reminder.reminder_deliveries` and `reminder.fake_outbox` exist | | Pending |
| RMD-01 — delivery aggregate: state machine, idempotency key, terminal immutability, receipt-once | `go test ./backend/internal/modules/reminder/domain -race` | | Pending |
| DLV-01 — delivery application: plan/revoke fan-out with job-ID writeback, SendReminder suppression + idempotency, RecordReceipt dedup, stats and list queries | `go test ./backend/internal/modules/reminder/application/... -race` plus `TEST_DATABASE_URL=… go test ./backend/internal/modules/reminder/adapters/outbound/postgres -race` for the delivery store and fake-outbox adapters | | Pending |
| RIV-01 — River scheduler and worker adapters: atomic InsertTx, cancel, retry, duplicate execution, crash recovery | `TEST_DATABASE_URL=… go test ./backend/internal/modules/reminder/adapters/outbound/river ./backend/internal/modules/reminder/adapters/inbound/worker -race -p=1` | | Pending |
| PRV-01 — fake, SMTP, and Aliyun provider adapters plus SMS receipt report parser | `go test ./backend/internal/modules/reminder/adapters/outbound/fake ./backend/internal/modules/reminder/adapters/outbound/smtp ./backend/internal/modules/reminder/adapters/outbound/aliyun -race` | | Pending |
| HTP-01 — reminder HTTP: delivery list, ops route, receipt webhook signature verification, gated dev outbox | `go test ./backend/internal/modules/reminder/adapters/inbound/http -race` | | Pending |
| TDO-01 — todo title/owner seam and real dashboard reminder counters | `go test ./backend/internal/modules/todo/... -race` | | Pending |
| CNT-01 — reminder contract and extended dashboard contract | `go test ./tests/contract -race` | | Pending |
| WEB-01 — dashboard: four real reminder tiles and reminder records list | `corepack pnpm --filter @artificial-brain/web test` | | Pending |
| OPS-01 — reminder ops endpoint: queue depth, oldest wait, delivery counts, retry rate, dead letters, latency P95 from deterministic SQL | ops store queries under `TEST_DATABASE_URL=…` plus the HTTP ops route test; `make smoke-test` asserts `GET /api/v1/ops/reminder` | | Pending |
| Clean-context regression | `make verify && make migration-test && make smoke-test`; immutable reviewer report is `regression-report.md` | | Pending |
