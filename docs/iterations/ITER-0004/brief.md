# ITER-0004 brief

Purpose: close the MVP with two deployment forms and full data portability — one image stack serves cloud and private deployments (a fixed private admin provisioned from `PRIVATE_ADMIN_PHONE`, every other phone rejected with `registration_closed`), and users can take their data with them: streaming Export Bundles (versioned JSON ZIP + human-readable CSV), two-phase preview/confirm import with Source Identity idempotency where conflicts never overwrite, verifiable backup/restore commands, an offline bundle target, and a same-version upgrade drill.

Scope is migration 008 (schema 7→8), the Portability bounded context (domain, application, archive/postgres/HTTP adapters), the identity/todo/reminder import-export seams, platform configuration for deployment mode and bundle limits, the portability OpenAPI contract and export bundle schemas, the `/data` web page, `deploy/private/**` assets and runbooks, Make backup/restore/offline-bundle targets, compose env passthrough, the new smoke blocks (portability, private mode, backup/restore, upgrade drill), and iteration evidence. Out of scope: a reverse proxy/TLS terminator in the box, cross-version upgrade drills, scheduled backup containers, open-ended chat, MCP, real-provider calls from CI, and any new Go or web dependencies.

The governing [design](../../superpowers/specs/2026-08-21-iter-0004-private-deployment-and-data-portability-design.md) and [implementation plan](../../superpowers/plans/2026-08-21-iter-0004-private-deployment-and-data-portability.md) are authoritative. The design decisions P1–P10 are recorded in the iteration design §1 table; the implementation decisions D1–D10 (consumer-owned ports + cmd shims, streaming bundle with manifest last, bytea-stored validated bundle with deterministic re-decide, NULL-plan delivery history, provisioned fixed admin, application-layer login gate, dedicated export handlers, no reverse proxy in the box, CONFIRM-gated backup/restore scripts, and same-architecture upgrade drill) are recorded in [decisions.md](decisions.md).

## Acceptance criteria

1. 私域模式（`DEPLOYMENT_MODE=private`）：固定管理员可完成两步验证码登录进入工作区；其他手机号被 `registration_closed` 拒绝；管理员 provision 幂等；`PRIVATE_ADMIN_PHONE` 缺失时 `config.Load` 失败。
2. 云端模式行为与 ITER-0003 完全一致（登录、待办、对话、提醒全链路无回归）。
3. `POST /api/v1/portability/export` 返回合法 zip：manifest 版本/来源/计数/校验和齐备，四个数据文件齐全且 sha256 自洽，CSV 人工可读；不含验证码、会话、密钥、供应商凭证。
4. 上传合法 bundle 返回预览：new/skipped/conflict/invalid 分类正确；预览不落任何业务数据。
5. 确认后按 channels→todos→deliveries 顺序导入：导入的 todo 无任何提醒计划；导入的通道未验证；投递历史 origin=imported 且无投递副作用；committed 后再次 confirm 返回 409 与既有报告，不重复执行。
6. 同一 bundle 重复导入全部 skipped，无重复记录（验收场景 10）。
7. 来源标识相同但内容变化的记录判为 conflict：跳过并出现在报告，数据库内原记录不变。
8. 坏包各形态（超大、结构残缺、版本不支持、校验和失败、记录非法）均以对应稳定错误码拒绝，不产生半导入状态。
9. `make backup` 产出归档 + sha256；`make restore BACKUP=… CONFIRM=restore` 恢复后数据完好；无 CONFIRM 拒绝执行。
10. smoke 含升级演练块（写入→重建→迁移幂等→数据完好）与私域模式块；`make verify`、`make migration-test`、`make smoke-test` 干净检出全绿；go.mod 与 web 依赖零新增；迭代账本齐备，独立干净上下文回归 PASS。
