# ITER-0004 私域部署与数据可移植性设计

- 日期：2026-08-21
- 状态：已复核（brainstorming 逐节确认）
- 上游约束：[MVP 总体设计](2026-08-13-ai-native-personal-workbench-mvp-design.md) §2.1（可移植性）、§4.5、§8、§9.2、§15 场景 10–11；本迭代是主设计 §14 的第四个交付切片，完成后 MVP 范围闭环。

## 1. 背景与设计选择

ITER-0001~0003 交付了 harness/骨架、身份-待办-对话闭环、可靠提醒投递。主设计遗留两块：**私域部署形态**（同一套云端业务代码在私域以单用户形态运行，替换身份/模型/消息/密钥适配器）与**数据可移植性**（Export Bundle、Import Preview、幂等导入、冲突不静默覆盖）。

总体结构采用**方案 A：Portability 作为独立限界上下文**，只经 Todo / Identity / Reminder 三个模块的公开应用接口读写，不触碰其他上下文的 domain、adapters 或数据库实现（主设计 §4.5 的既定约束）。备选方案 B（分散各模块 + cmd 聚合：跨模块编排泄漏进组合根，决策引擎无家）与方案 C（内部 HTTP / 领域事件聚合：违反 MVP §6.1 禁止自回调，事件总线超出 MVP）均被否决。

Brainstorming 阶段逐节确认的决策（偏离主设计字面处均已标注）：

| # | 决策 | 备注 |
|---|---|---|
| P1 | 私域登录 = 固定管理员 + 复用现有两步验证码流 | 登录 UI、会话、确认链路零改动 |
| P2 | `PRIVATE_ADMIN_PHONE` 私域必填（E.164），不再"首次部署生成" | 偏离 §2.1 字面：管理员手机号是唯一凭证入口，刻意配置避免"丢日志即丢管理员"；同样满足"不开放注册" |
| P3 | 私域栈**不内置** reverse-proxy，手册化企业网关接入与 LAN 风险 | 偏离 §9.2 字面；web 已代理 /api，浏览器仅见单一源；TLS/LAN 认证交给企业侧入口 |
| P4 | preferences.json = 提醒通道配置（类型/地址/启用状态），不含验证码与验证状态 | 导入后通道一律未验证 |
| P5 | reminder-deliveries.json 随包导入为**只读历史**（origin 标记），绝不补发/重投 | 对应主设计 §8"不得补发已到期提醒" |
| P6 | Web 新建 `/data` 数据页承载导出/导入多步流程 | 设置页放入口链接 |
| P7 | 备份/恢复 = `deploy/private` 脚本 + `make backup/restore`（CONFIRM 门禁）+ runbook | 对应 §9.2"可验证的备份与恢复命令" |
| P8 | 升级 = runbook + smoke 同版本升级演练块 | 无已发布版本，不做跨版本镜像演练 |
| P9 | Bundle 同步流式生成；预览纯计算、确认时从同一 bundle 确定性重算；上传 bundle 存 DB（bytea，多副本共享） | 单用户数据量小，不做异步 job |
| P10 | 冲突（同来源标识、内容不同）= 跳过 + 报告，永不覆盖 | 主设计 §2.1"冲突不静默覆盖" |

## 2. 目标与非目标

### 2.1 目标

1. 同一镜像栈支持 `cloud` / `private` 两种部署形态；私域模式固定单管理员、不开放其他登录。
2. 私域 fail-closed：`APP_ENV=production` 下 fake 适配器与 dev inbox/outbox 沿用禁用；私域正式部署用企业配置的模型/SMTP/短信适配器（验收场景 11）。
3. 一键导出 Export Bundle（版本化 JSON ZIP + 人工可读 CSV），不含验证码、会话、密钥、供应商凭证。
4. 导入两阶段：上传校验 → Import Preview → 用户确认 → 批量经公开应用接口导入 → 结果报告（new/skipped/conflicts/invalid）。
5. 重复导入幂等（Source Identity 去重）；冲突跳过并报告；导入不补发任何提醒。
6. 私域备份/恢复命令可验证；升级 runbook + 同版本升级演练进 smoke。
7. 离线部署支持：镜像打包目标 + 配置模板 + 部署手册（不内置反向代理）。
8. 迭代门禁：`make verify` / `migration-test` / `smoke-test` 全绿；独立干净上下文回归 PASS。

### 2.2 非目标

- 不内置反向代理/TLS 终端（P3，手册化）。
- 不做跨版本真实升级演练（无已发布版本，P8）。
- 不做定时备份容器、不做备份上云。
- 不新增任何 Go 三方依赖与 Web 依赖（zip/hash/csv/multipart 全标准库）。
- 不改登录 UI（私域复用两步验证码流，P1）。
- 不做导入后提醒重排自动化——未来到期提醒只有用户事后改期/编辑才重新安排（主设计 §8）。
- 不开放多用户/注册（私域固定管理员；云端行为不变）。

## 3. 私域部署形态

### 3.1 模式开关与身份门禁

- 新增 `DEPLOYMENT_MODE=cloud|private`（默认 `cloud`）。不新增 compose 文件；现有 compose.yaml 与 `deploy/private/` 配置模板覆盖两种形态。
- 私域模式（`DEPLOYMENT_MODE=private`）：
  - `PRIVATE_ADMIN_PHONE` 必填（E.164），`config.Load` 缺失即失败（P2）。
  - API 启动时幂等 provision 固定管理员用户 + 个人工作区；结构化日志事件 `private admin provisioned`（首次）/已存在则静默通过。
  - 登录门禁：`auth/login/request` 与 `verify` 仅接受管理员手机号；其他号码返回稳定错误码 `registration_closed`。
  - 验证码经已配置的 SMS 适配器送达（fake 收件箱或企业短信），登录链路零改动（P1）。
- 云端模式行为完全不变。
- Fail-closed 门禁不变：`APP_ENV=production` 禁止 fake 提醒适配器与 dev inbox/outbox。私域正式部署 `APP_ENV=production` + 真实适配器；CI smoke 以 `APP_ENV=development + DEPLOYMENT_MODE=private` 演练私域身份行为（CI 不连真实供应商的红线不变）。

### 3.2 交付物

- `deploy/private/`：部署手册（默认绑定本机；LAN 暴露风险告知 + 企业反向代理接入指引，P3）、env 配置模板、`backup.sh`、`restore.sh`。
- `make backup`：容器内 `pg_dump -Fc` → 带时间戳归档 + sha256 清单。
- `make restore BACKUP=<file> CONFIRM=restore`：校验归档存在与可读 → CONFIRM 门禁 → `pg_restore`；无 CONFIRM 拒绝执行。
- `make offline-bundle`：`docker save` 全栈镜像为离线 tar + load 步骤说明；产物不进仓库。
- `docs/runbooks/backup-restore.md`、`docs/runbooks/upgrade.md`（升级前校验版本 → 备份 → 换镜像 → 迁移 → 验证清单；历史迁移只读、只追加）。

### 3.3 升级演练

smoke 新增同版本升级演练块：写入数据 → 停旧服务 → 重建/重启服务（模拟换镜像）→ migrate 幂等 → 数据完好、健康端点绿。跨真实版本演练待首个发布版本后另立工作。

## 4. Portability 模块结构与依赖方向

```text
backend/internal/modules/portability/
  domain/            # manifest 校验、决策引擎(new/skipped/conflict/invalid)、
                     # 内容指纹、Source Identity、bundle 值对象
  application/
    command/         # ExportBundle、UploadImport(预览)、ConfirmImport
    query/           # GetImport(预览/状态/报告)
    ports/           # BundleArchive、ImportStore、InstanceIdentity、
                     # TodoImporter、ChannelImporter、DeliveryImporter、
                     # TodoExporter、ChannelExporter、DeliveryExporter
    dto/             # PreviewReport、ImportReport、RecordDecision、Manifest…
  adapters/
    inbound/http/    # export / imports 路由
    outbound/archive/  # zip 流式组装与解析、CSV、sha256
    outbound/postgres/ # ImportStore、instance_meta、source records
```

- 依赖方向沿用 `inbound adapter -> application -> domain`；ports 由 application 定义，adapters 实现。
- **跨模块仅经公开应用接口**：Todo/Identity/Reminder 各自新增公开导入命令与导出查询（见 §7.3），Portability 只导入其 `application/{command,query,dto}`，禁止 import 其 domain/adapters/postgres——架构测试固化该政策。
- 事务归属不变：写操作的事务、校验、审计仍在目标模块内；Portability 应用层负责 bundle 编排、决策与 source record 登记。

## 5. Bundle 格式与实例身份

### 5.1 Export Bundle（`export.zip`）

```text
export.zip
  manifest.json              # schemaVersion:"1"、sourceInstanceId、exportedAt、
                             # counts{todos,deliveries,channels}、files{<name>:sha256}
  todos.json                 # 全状态待办（pending/completed/deleted 全量，保留时间戳与版本）
  reminder-deliveries.json   # 投递审计历史（状态/时间戳/错误码；不含消息正文外的密钥类字段）
                             # 每条携带 sourceTodoRecordId，指向所属 todo 的来源记录 ID
  preferences.json           # 提醒通道配置：{kind,address,enabled}，不含验证码与验证状态
  todos.csv                  # 人工可读副本：标题、到期时间(UTC+输入时区)、状态、创建时间
```

- 导出**不设**大小上限；导入上限由 `PORTABILITY_MAX_BUNDLE_BYTES` 控制。
- `manifest.files` 的 sha256 在流式写每个文件时增量计算，manifest 最后生成。
- `contracts/export-schemas/` 持有四类文件的 JSON Schema（`schemaVersion: "1"`），导入校验与契约测试共用同一来源。

### 5.2 实例身份

- 迁移 008 新建 `instance_meta(key text pk, value text, created_at timestamptz)`。
- API 首启若 `instance_id` 缺失则生成 UUID 写入（幂等）；导出的 `sourceInstanceId` 取此值。
- 导入去重键 = `(sourceInstanceId, sourceRecordId)`，与目标实例自身 instance_id 无关（允许 A→B、B→A 互导）。

## 6. 导出流程

```text
POST /api/v1/portability/export（session）
-> 组装 manifest 前置数据：instance_id、counts
-> 流式写 zip：todos.json（Todo 公开导出查询，分页拉全状态）
            -> reminder-deliveries.json（Reminder 公开导出查询）
            -> preferences.json（Identity 公开通道查询，天然不含验证码）
            -> todos.csv
-> 逐文件 sha256 -> manifest.json 收尾
-> Content-Type: application/zip
   Content-Disposition: attachment; filename="artificial-brain-export-<YYYYMMDD>.zip"
```

同步流式、不落盘、无临时文件、无异步 job（P9）。

## 7. 导入流程

### 7.1 路由

| # | Method + Path | Auth | 行为 | 错误码 |
|---|---|---|---|---|
| 1 | `POST /api/v1/portability/imports` | session | multipart 上传 bundle → 校验 → 存库 → 返回 importId + 预览 | 401; 422 `bundle_too_large`/`bundle_invalid`/`unsupported_schema_version`/`checksum_mismatch` |
| 2 | `GET /api/v1/portability/imports/{importId}` | session | 预览/状态/报告查询 | 401; 404 |
| 3 | `POST /api/v1/portability/imports/{importId}/confirm` | session | 确定性重算并执行 → 最终报告 | 401; 404; 409（已确认/已过期） |

沿用 `{code, message, correlationId}` 信封与 `X-Correlation-ID`；新增稳定错误码：`registration_closed`、`unsupported_schema_version`、`checksum_mismatch`、`bundle_invalid`、`bundle_too_large`。

### 7.2 校验与两阶段语义

上传即 fail-fast 校验：大小上限 → zip 结构完整 → manifest `schemaVersion` 支持 → 逐文件 sha256 与 manifest 一致 → 记录级 schema 校验。通过后将 bundle 原样存入 `portability_imports.bundle`（bytea），`state=pending`，importId TTL 24h（过期不可 confirm，`state=expired` 由查询时惰性判定）。

- **Preview**：解析 bundle，对每条记录跑决策引擎，产出 `{new, skipped, conflicts, invalid}` 计数 + 分类明细（每类明细上限 100 条，超出以 `truncated: true` 标注）。纯计算、零写入。
- **Confirm**：从同一 bundle **重新解析、重新决策**再执行（P9）——预览态不会陈旧；执行只发生一次，`committed` 后再次 confirm 返回 409 并附既有报告（不重复执行）。

### 7.3 决策引擎与执行

每条记录四态之一：

- **new**：`(sourceInstanceId, sourceRecordId)` 在 `portability_source_records` 不存在 → 调目标模块公开命令写入，并登记 source record（`content_fingerprint` 存内容指纹）；
- **skipped**：来源标识已存在且内容指纹一致 → 幂等跳过；
- **conflict**：来源标识已存在但指纹不同 → 跳过 + 报告，**永不覆盖**（P10）；
- **invalid**：记录级校验失败（缺字段、非法时间等）→ 跳过 + 报告。

执行顺序固定：**channels（Identity.ImportChannel，一律未验证）→ todos（Todo.ImportTodo）→ delivery 历史（Reminder.ImportDeliveries，origin=imported，无 River 副作用）**。delivery 记录经 `sourceTodoRecordId` 在本次导入建立的 source record 映射中解析为实际 todoId；映射缺失的 delivery 判为 invalid。todo 导入保留原始状态（pending/completed/deleted）与时间戳。

Source Identity 去重键**不含 workspace**（主设计 §8 原文）：`UNIQUE(source_instance_id, source_record_id)` 全实例唯一。后果（接受）：同一 bundle 被第二个工作区导入时，记录按指纹一致判 skipped / 不一致判 conflict，不会跨工作区复制数据。

各模块公开接缝（绿区改动，接口签名在 Plan 冻结）：

- `Todo.ImportTodo`：复用创建不变量；**不产生任何 Reminder Plan / River Job**（过期不补发、未来不自动安排，主设计 §8）；记录 source identity。
- `Identity.ImportChannel`：创建未验证通道（enabled 按 preferences）；不发验证码。
- `Reminder.ImportDeliveries`：只读历史插入，`origin='imported'`；不触发调度与供应商。
- 三个模块另提供公开导出查询（分页、工作区域内）。

## 8. 数据 schema（迁移 008，schema 7 → 8）

```sql
-- 008_portability.sql（append-only；001–007 红区不动）
CREATE TABLE instance_meta (
  key text PRIMARY KEY, value text NOT NULL, created_at timestamptz NOT NULL
);
CREATE TABLE portability_imports (
  id uuid PRIMARY KEY, workspace_id bigint NOT NULL,
  state text NOT NULL,                    -- pending|committed|expired
  source_instance_id text NOT NULL,
  bundle bytea NOT NULL,
  report jsonb,
  created_at timestamptz NOT NULL, committed_at timestamptz
);
CREATE TABLE portability_source_records (
  workspace_id bigint NOT NULL,
  source_instance_id text NOT NULL,
  source_record_id text NOT NULL,
  target_kind text NOT NULL,              -- todo|channel|delivery
  target_id bigint NOT NULL,
  content_fingerprint text NOT NULL,
  created_at timestamptz NOT NULL,
  UNIQUE (source_instance_id, source_record_id)
);
ALTER TABLE reminder_deliveries ADD COLUMN origin text NOT NULL DEFAULT 'local';
```

`database.CurrentSchemaVersion = 8`；`tests/smoke/migration_test.sh` 版本钉 7→8；API/Worker 保持等值 schema 门禁。

## 9. 配置（`config.Config` 新增字段）

| 字段 | Env | 默认 | 校验 |
|---|---|---|---|
| `DeploymentMode` | `DEPLOYMENT_MODE` | `cloud` | `cloud`\|`private` |
| `PrivateAdminPhone` | `PRIVATE_ADMIN_PHONE` | — | private 必填，E.164；cloud 下若设置则报错（防误配） |
| `PortabilityMaxBundleBytes` | `PORTABILITY_MAX_BUNDLE_BYTES` | `33554432`（32MB） | int ≥ 1MB |

## 10. 契约

- `contracts/openapi/portability.yaml`：§7.1 三路由 + 视图模型；契约遍历测试沿用现有模式；`dashboard.yaml` 等既有契约不动。
- `contracts/export-schemas/`：manifest / todos / reminder-deliveries / preferences 的 JSON Schema；Go 契约测试以固定好/坏样例 bundle 双向验证。
- 健康路由与 system-health 契约不变。

## 11. Web `/data` 页（绿区，零新依赖）

- 新路由 `/(workbench)/data` + `apps/web/src/features/data`；设置页加入口链接。
- 导出：按钮 → fetch zip 流 → 浏览器下载；生成中/完成/失败三态。
- 导入：三步状态机——上传 → 预览表（四态计数 + 明细，截断提示）→ 确认 → 报告；错误码映射可行动文案（如 `checksum_mismatch` → "导出包已损坏，请重新导出"）。
- 组件测试覆盖状态机、错误文案与截断提示。

## 12. 测试策略

- **领域单测**：manifest 校验；决策引擎四态（同包重导入全 skipped、内容变化转 conflict、指纹稳定性）；Clock 注入，无真实等待。
- **应用测试**：preview 零写入；confirm 幂等与 409；importId 过期；执行顺序 channels→todos→deliveries。
- **集成测试**（`TEST_DATABASE_URL`）：source records UNIQUE 去重；bundle bytea 存取；`instance_meta` 首启生成且幂等；origin 标记。
- **模块接缝测试**：`ImportTodo` 不产生 Plan/Job；私域门禁（管理员可登录、他号 `registration_closed`）；固定管理员 provision 幂等；`ImportDeliveries` 无调度副作用。
- **契约测试**：OpenAPI 遍历 + export-schemas 好/坏样例。
- **Web 组件测试**：/data 三步状态机与错误文案。
- **架构测试**：Portability 仅依赖其他模块 `application/{command,query,dto}`；domain 零外部依赖。
- **smoke 新块**：① 导出 → 原包重导入全 skipped → 改一条转 conflict；② 私域模式（development + private）：管理员登录成功、他号被拒；③ backup → restore → 数据完好；④ 升级演练：写入数据 → 重建服务 → 迁移幂等 → 数据完好。

## 13. 依赖、分区与风险

- **依赖**：零新 Go 三方依赖（archive/zip、crypto/sha256、encoding/csv、mime/multipart 均标准库）；零新 Web 依赖；不新增镜像（不内置网关）。
- **黄区登记**（Plan 展开）：迁移 008 与 schema 版本、platform config、cmd 接线、contracts（openapi + export-schemas）、架构政策、compose/smoke、Makefile、`deploy/private/**`、runbooks、README、各级 AGENTS.md。
- **红区**：历史迁移 001–007 不改；CI 不连真实供应商/模型；不提交凭证；不降 CI 门禁；已完成迭代证据只可追加勘误。
- **风险**：bytea 存 bundle 受 32MB 上限与单用户规模约束，可接受；私域真实生产形态（production + 真实适配器）无 CI 覆盖，由 runbook 清单与本地演练兜底（与 ITER-0003 真实适配器同策略）。

## 14. 验收标准

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
