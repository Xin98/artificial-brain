# ITER-0004 Regression Report v2 (Superseding, Clean-Context)

## Reviewer scope statement

This is an **independent, superseding clean-context regression review** of ITER-0004
(private deployment and data portability), branch
`iter-0004-private-deployment-and-data-portability`, reviewed at HEAD `7a78b9a`
(merge-base `05dd59d`). It **supersedes** `docs/iterations/ITER-0004/regression-report.md`,
which passed at `17714d9` but predates the fix commit `7a78b9a` (migration 008 partial
unique index, archive parser zip-bomb cap, todo export paging order, portability record
validators, admin phone canonicalization) and is therefore stale.

The reviewer is a fresh agent with no knowledge of the implementation conversation.
Inputs used, and only these:

- Approved design spec `docs/superpowers/specs/2026-08-21-iter-0004-private-deployment-and-data-portability-design.md` (acceptance criteria §14).
- Iteration ledger `docs/iterations/ITER-0004/{brief,spec,plan,decisions,progress,test-matrix,handoff}.md`.
- Implementation plan `docs/superpowers/plans/2026-08-21-iter-0004-private-deployment-and-data-portability.md`.
- The branch diff `05dd59d..7a78b9a` (25 commits, 132 files, +15131/−82).
- `README.md` (Prerequisites, Verification) and the `Makefile`.

Not used: any implementation chat/transcript, and no reliance on the stale
`regression-report.md` conclusions. Every gate below was re-run by this reviewer at
HEAD `7a78b9a`. The reviewer modified no source/test/config/migration files; this
report is the only write.

Environment: Go 1.26.5, Node.js 24.18.0, pnpm 11.19.0 (all pinned), Docker Compose v2,
`ab-test-postgres` running (host port 5433). `TEST_DATABASE_URL` was unset in the
reviewer shell — per README, DB-backed Go tests skip there — so real-DB proof comes
from `make migration-test`'s in-container `backend-test` suite, which wires
`TEST_DATABASE_URL` itself and runs the full adapter/composition integration suite
(no skips observed in that run).

## Gate results (re-run at HEAD `7a78b9a`, in order)

| # | Command | Exit code | Key evidence |
|---|---|---|---|
| 1 | `corepack pnpm install --frozen-lockfile` | **0** | pnpm 11.19.0, "Already up to date" (frozen lockfile honored) |
| 2 | `make verify` | **0** | harness-test, format-check, lint, architecture-test, `go test ./... -race` (all packages `ok`, incl. all portability packages, `tests/contract`, `backend/cmd/api`), web tests 18 files / 106 tests passed, production build, `scripts/check-secrets.sh` |
| 3 | `make migration-test` | **0** | Empty-schema API readiness = 503 (fail-safe holds); Worker refuses empty schema; services create no `schema_version`; migrate ran twice (idempotent); **schema_version = 8** pin passed; `runtime.worker_heartbeats` present; in-container backend-test suite green — 479 `--- PASS`, 0 SKIP, incl. `TestPortabilityRoundTripBetweenTwoWorkspaces` (new=3, then 409 `import_conflict`) and `TestPrivateModeProvisioningAndLoginGate` against the real DB |
| 4 | `make smoke-test` | **0** | Fail-fast script: cloud stack e2e (ITER-0001…0003 blocks: login, todos, conversation, reminder delivery/suppression/receipt/ops) + portability block + second compose project in private mode (`APP_ENV=development`) + backup/restore drill + same-version upgrade drill (force-recreate, migrate exit 0, schema v8, counts intact, healthy) |

## Acceptance criteria mapping (design §14, ten items)

| # | Criterion | Result | Evidence at HEAD `7a78b9a` |
|---|---|---|---|
| 1 | Private mode: admin two-step login works; other phones rejected `registration_closed`; admin provision idempotent; `config.Load` fails when `PRIVATE_ADMIN_PHONE` missing | **PASS** | Smoke private block: admin `+8613800137999` logs in via dev inbox → dashboard rendered; stranger `+8613800137998` gets 403 `registration_closed` on `auth/login/request`. Composition `TestPrivateModeProvisioningAndLoginGate` (idempotent provision, structured `private admin provisioned`). `config_iter0004_test.go`: `TestLoadPrivateModeRequiresAdminPhoneForAPIRole`, invalid-phone table (incl. fix-added `missing plus` canonicalization case), `TestLoadRejectsAdminPhoneInCloudMode`. Identity HTTP tests map `ErrRegistrationClosed` → 403 on both login routes |
| 2 | Cloud mode identical to ITER-0003 (no regression in login/todo/conversation/reminder) | **PASS** | Full `make verify` suite green (all pre-existing packages `ok`); smoke cloud e2e re-runs the ITER-0001…0003 blocks end-to-end: two-step login, todo lifecycle with confirmation-gated delete, conversation, reminder plan→delivery through fake adapters, suppression, receipt webhook, ops snapshot, worker-restart recovery |
| 3 | Export returns valid zip: manifest version/source/counts/checksums, four data files + CSV, no codes/sessions/keys/provider credentials | **PASS** | Smoke: export 200 `application/zip`; entries exactly `manifest.json, preferences.json, reminder-deliveries.json, todos.csv, todos.json`; `schemaVersion == "1"`, counts asserted; sha256 integrity proven by the re-import parse (checksummed entries) succeeding. `contracts/export-schemas/**` good/bad-sample contract tests green. Channel export returns only id/kind/address/enabled (`identity/application/query/export.go` — never codes or verification state); bundle entry set is fixed and the parser rejects unexpected entries |
| 4 | Legal bundle upload returns correct new/skipped/conflict/invalid preview; preview writes no business data | **PASS** | `TestUploadImportHappyPathDecidesPerKindAgainstExistingFingerprints` classifies all four outcomes (skipped/new/conflict/new). Preview is structurally write-free: the Upload handler is constructed only with (import store, source-record fingerprint lookup, parser) — no importer ports exist at upload time; it stores the bundle + preview as `state=pending`. Smoke: first upload preview `new == totals`, re-upload preview `skipped == totals` |
| 5 | Confirm executes channels→todos→deliveries; imported todos carry no reminder plans; imported channels unverified; deliveries origin=imported with no delivery side effects; re-confirm after committed returns 409 with existing report | **PASS** | `confirm.go` executes the three loops in fixed order, each gated on `OutcomeNew`; `TestConfirmImportHappyPathExecutesChannelsTodosDeliveriesInOrder`. Composition: workspace B has 0 reminder plans after import, imported channel `verified=false`, `origin='imported'` delivery row; second confirm 409 `import_conflict`. Smoke: plan count unchanged by import, imported rows == manifest delivery count, committed re-confirm 409 `import_conflict`; reminder history of the imported copy renders via `GET /api/v1/reminders` |
| 6 | Re-importing the same bundle is fully skipped, no duplicate records | **PASS** | Smoke: re-upload preview `skipped == total, new == 0`; re-confirm report identical; todo count unchanged across the rerun; downgraded channel registers against the existing row (no duplicate in settings listing). Migration 008 `UNIQUE(source_instance_id, source_record_id)` + postgres integration tests (`SaveImported` idempotency-key mapping) |
| 7 | Same source identity with changed content → conflict: skipped, reported, original row untouched | **PARTIAL** | Classification proven: `TestDecideChangedFingerprintIsConflict`, `TestDecideKeepsInputOrderForMixedEntries`, upload preview test asserting `Conflicts: 1` with reason "fingerprint changed since last import". No-overwrite proven: all three confirm execution loops `continue` for any record whose outcome ≠ new, and `TestConfirmImportRedecidesFromStoredBytesNotStoredPreview` asserts importers are never invoked for non-new records; smoke proves non-new records leave DB rows untouched end-to-end. **Gap:** design §12 smoke block ① specifies "改一条转 conflict" (mutate one record → conflict) and test-matrix SMK-01 claims the smoke block includes "conflict on change", but the implemented smoke block (`tests/smoke/stack_test.sh` lines ~536–790) contains no bundle-mutation step — it asserts `conflicts == 0` in all four preview/report checks. No end-to-end proof of a content-conflict record through the real HTTP+DB stack exists in any gate |
| 8 | Bad bundles (oversized, broken structure, unsupported version, checksum mismatch, invalid records) rejected with stable codes, no half-import state | **PASS** | Smoke: garbage upload → 422 `bundle_invalid`; oversized (cap lowered to the 1 MiB floor for the run) → 422 `bundle_too_large`. HTTP tests map `unsupported_schema_version`, `checksum_mismatch`, `bundle_invalid` (structure/record/manifest variants). Fix-area zip-bomb cap: `TestParseRejectsOversizedEntry` / `TestParseReadEntryAtSizeLimit` (64 MiB per-entry decompressed bound → `ErrBundleStructure`). No half-import: upload validates fully before storing; confirm runs in one transaction (`TestConfirmImportImporterFailureFailsTheTransactionAndKeepsStatePending`) |
| 9 | `make backup` produces archive + sha256; `make restore BACKUP=… CONFIRM=restore` restores intact data; refuses without CONFIRM | **PASS** | Smoke drill: `backup.sh` produced `backup-20260821T195507Z.dump` + `.sha256` sidecar; a soft-deleted todo was restored (row back, dashboard pending ≥ 1, system healthy); `CONFIRM=wrong` refused before touching the database (row count unchanged). `restore.sh` gates on `CONFIRM=restore` and verifies the sha256 sidecar when present; Makefile targets wrap both scripts |
| 10 | Smoke includes upgrade drill + private-mode block; `make verify` / `migration-test` / `smoke-test` all green from clean checkout; zero new go.mod/web dependencies; complete ledger; independent clean-context regression PASS | **PASS** | Upgrade drill: force-recreate against the live volume, one-shot migrate exit 0 (idempotent), schema v8, todo + delivery counts unchanged, healthy state. Private-mode block present and green. All four gates re-ran exit 0 at HEAD (table above). `git diff 05dd59d..HEAD -- go.mod go.sum apps/web/package.json pnpm-lock.yaml` is empty. Ledger complete (brief/spec/plan/decisions/progress/test-matrix/handoff). This report is the independent clean-context regression gate |

Count: **9 PASS, 1 PARTIAL, 0 FAIL.**

## Fix-area re-verification (commit `7a78b9a`)

The stale pre-fix regression never saw this commit; every changed area was re-verified:

1. **Migration 008 partial unique index.** `008_portability.sql` drops migration 007's global `reminder_deliveries_todo_channel_unique` and replaces it with the partial index `reminder_deliveries_todo_channel_local_unique … where origin = 'local'`, so rescheduled-todo history (all restored as version 0) cannot collide while local planning uniqueness is preserved. The down section reverses exactly (drop index, re-add constraint, then reverse order teardown). Migrations 001–007 remain byte-identical. `make migration-test` reached **schema v8** and the in-container reminder/portability integration suites (including imported-delivery `SaveImported` + `List` with nullable plan) passed against the real database.
2. **Archive parser zip-bomb cap.** `readEntry` now wraps decompression in `io.LimitReader(reader, maxEntryBytes+1)` with `maxEntryBytes = 64 << 20` and reports `ErrBundleStructure` (→ 422 `bundle_invalid`) on oversized entries. Covered by `TestParseRejectsOversizedEntry` (highly compressible oversized entry) and the boundary test `TestParseReadEntryAtSizeLimit`; both ran green inside the migration-test container suite.
3. **Todo export `ListAll` ordering.** Query now `order by created_at asc, id asc` for stable offset paging on ties; port doc updated on the `TodoStore` seam. Covered by `TestStoreListAllReturnsFullHistoryOrderedAndPaged` and fix-added `TestStoreListAllStableOrderOnCreatedAtTies`, green in the container suite against the real DB.
4. **Portability validators.** `records.go` rejects todo titles over 200 runes (mirrors the todo domain limit without importing it), suppressed deliveries lacking a reason, unknown suppression reasons, and unknown receipt states — keeping preview and confirm decidable identically. Covered by new `manifest_test.go` / records cases in the fix commit, green under `make verify` and in-container.
5. **Admin phone canonicalization.** `config.Load` now rejects a `PRIVATE_ADMIN_PHONE` without a leading `+`, so provisioning and the login gate always compare identical canonical strings (a `+`-less value would permanently lock the admin out). Fix-added `missing plus` case in `TestLoadRejectsInvalidAdminPhones`; smoke's private block uses the canonical `+8613800137999` end-to-end.

Smoke portability self-import block: **export → first confirm → re-import fully skipped → 409 on committed re-confirm** passed end-to-end inside `make smoke-test` (exit 0). Note for the record: the block's **conflict-on-change** element (task instruction 3 / design §12) is **not** present in the smoke script — see criterion 7 PARTIAL.

## Compliance findings

| Check | Result | Evidence |
|---|---|---|
| Zero new runtime dependencies | **PASS** | `git diff 05dd59d..HEAD -- go.mod go.sum apps/web/package.json pnpm-lock.yaml` → empty (diff exit 0, no output); frozen-lockfile install green |
| Migrations 001–007 untouched | **PASS** | `git diff --name-status 05dd59d..HEAD -- deploy/migrations/` → only `A deploy/migrations/008_portability.sql`; `database.CurrentSchemaVersion = 8` |
| No real-provider egress in CI/smoke | **PASS** | `.github/workflows/ci.yml` runs only the four gates (unchanged in this iteration). Compose defaults: `APP_ENV=development`, `MODEL_ADAPTER=deterministic`, `REMINDER_EMAIL_ADAPTER=fake`, `REMINDER_SMS_ADAPTER=fake` — pinned by the smoke static-config test. The private-mode smoke project explicitly boots with `APP_ENV=development` + `DEV_INBOX_ENABLED=true` (fake adapters only). `config.Load` fail-closes fake adapters and dev surfaces under `APP_ENV=production` (unit-tested) |
| No committed credentials | **PASS** | `scripts/check-secrets.sh` exit 0 (also runs as the final `make verify` step) |
| Architecture policy | **PASS** | `architecture/policy` + `architecture/tests` green, incl. the new portability cross-context fixture; portability imports only other modules' `application/{command,query,dto}` surface via consumer-owned ports + cmd shims |
| Yellow-zone register | **PASS** | All yellow items (migration 008 + schema pin, platform config, cmd wiring/provisioning, contracts, architecture fixture, Makefile backup/restore/offline-bundle, compose passthrough + `.env.example`, smoke/migration pins, `deploy/private/**`, runbooks, README, AGENTS.md zone refresh) appear in the diff and match the plan's register |

## Discrepancies noted (non-blocking, recorded for the ledger)

1. **Smoke lacks the §12 conflict-mutation drill.** Design §12 smoke block ① ("导出 → 原包重导入全 skipped → 改一条转 conflict") and test-matrix SMK-01 ("conflict on change") describe a bundle-mutation step that `tests/smoke/stack_test.sh` does not implement; the block asserts `conflicts == 0` throughout. Conflict classification, report inclusion, and the never-overwrite execution gate are proven by deterministic unit/application tests and by the shared `OutcomeNew`-only execution path that smoke does exercise for skipped records — hence criterion 7 is PARTIAL rather than FAIL. Recommendation: add a mutate-one-record → `conflicts == 1` step to the smoke portability block (or correct the test-matrix wording) in a follow-up.

## Final verdict

All four acceptance gates re-ran exit 0 at HEAD `7a78b9a`; nine criteria PASS and one
criterion is PARTIAL (conflict-on-change proven below smoke level; the smoke drill
promised by design §12 is absent). No gate failed, no criterion is unevidenced, all
fix areas are re-verified with targeted tests, and zone/compliance checks are clean.

VERDICT: PASS
