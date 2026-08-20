# ITER-0003 test matrix

Rows are recorded as each task reaches green. IDs group by subsystem: `RMD` reminder domain, `DLV` delivery application, `RIV` River adapters, `PRV` provider adapters, `HTP` reminder HTTP, `TDO` todo seam, `CNT` contracts, `WEB` web, `MIG` migrations/schema, `OPS` ops endpoint.

| Requirement | Command and evidence | Evidence commit | Status |
| --- | --- | --- | --- |
| MIG-01 — schema advances 5→7 via append-only migrations (River v1 inlined + delivery tables) | `make migration-test`; empty-schema gate, idempotent re-run, `schema_version=7` assertion, `river_job` in `public` plus `reminder.reminder_deliveries` and `reminder.fake_outbox` exist | `2e8916c`; gate pin re-verified at `2ee849e` | Complete |
| RMD-01 — delivery aggregate: state machine, idempotency key, terminal immutability, receipt-once | `go test ./backend/internal/modules/reminder/domain -race` | `1eae4a4` | Complete |
| DLV-01 — delivery application: plan/revoke fan-out with job-ID writeback, SendReminder suppression + idempotency, RecordReceipt dedup, stats and list queries | `go test ./backend/internal/modules/reminder/application/... -race` plus `TEST_DATABASE_URL=… go test ./backend/internal/modules/reminder/adapters/outbound/postgres -race` for the delivery store and fake-outbox adapters | `64e0823`, `5713b72`, `237ea58`, `736ad7d` | Complete |
| RIV-01 — River scheduler and worker adapters: atomic InsertTx, cancel, retry, duplicate execution, crash recovery | `TEST_DATABASE_URL=… go test ./backend/internal/modules/reminder/adapters/outbound/river ./backend/internal/modules/reminder/adapters/inbound/worker -race -p=1` | `1c7877c` | Complete |
| PRV-01 — fake, SMTP, and Aliyun provider adapters plus SMS receipt report parser | `go test ./backend/internal/modules/reminder/adapters/outbound/fake ./backend/internal/modules/reminder/adapters/outbound/smtp ./backend/internal/modules/reminder/adapters/outbound/aliyun -race` | `9d2823b`, `63e4863` | Complete |
| HTP-01 — reminder HTTP: delivery list, ops route, receipt webhook signature verification, gated dev outbox | `go test ./backend/internal/modules/reminder/adapters/inbound/http -race` | `6c4ada6` | Complete |
| TDO-01 — todo title/owner seam and real dashboard reminder counters | `go test ./backend/internal/modules/todo/... -race` | `db10841` | Complete |
| CNT-01 — reminder contract and extended dashboard contract | `go test ./tests/contract -race` | `bee019a` | Complete |
| WEB-01 — dashboard: four real reminder tiles and reminder records list | `corepack pnpm --filter @artificial-brain/web test` | `f5a336f` | Complete |
| OPS-01 — reminder ops endpoint: queue depth, oldest wait, delivery counts, retry rate, dead letters, latency P95 from deterministic SQL | ops store queries under `TEST_DATABASE_URL=…` plus the HTTP ops route test; `make smoke-test` asserts `GET /api/v1/ops/reminder` | `6c4ada6`; smoke assertion re-verified at `2ee849e` | Complete |
| Clean-context regression | `make verify && make migration-test && make smoke-test` re-run independently plus `git diff --check`, acceptance mapping, zone compliance, credential scan; immutable reviewer report is `regression-report.md` | `2049071` | Complete (PASS) |
