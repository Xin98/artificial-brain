# ITER-0003 可靠提醒投递设计

- 日期：2026-08-19
- 状态：待书面复核
- 上位设计：[AI Native 个人智能工作台：MVP 总体设计](./2026-08-13-ai-native-personal-workbench-mvp-design.md)
- 迭代目标：在 ITER-0002 的提醒调度缝上交付可靠投递闭环——River 持久化调度、邮件/短信双通道投递（真实适配器 + Fake 门禁）、业务抑制与幂等、重试与死信、供应商回执验签去重、仪表盘四态真实计数与提醒记录列表、JSON 运维端点。

## 1. 背景与设计选择

ITER-0002 交付了身份、待办与对话闭环，并按计划留下提醒缝：带到期时间的待办在同一事务内创建 Reminder Plan，`JobScheduler` 端口挂着 no-op 适配器，仪表盘 `reminderRetrying`/`reminderFailed` 是占位 0。ITER-0003 把这条缝接成真实投递链路，对应总体设计交付切片 3 与验收场景 7、8、9。

本迭代的关键设计选择（均已与需求方书面确认）：

1. **真实适配器 + Fake 门禁**：实现真实 Aliyun SMS（纯标准库 HTTP + HMAC 签名，不引供应商 SDK）与标准 SMTP（`net/smtp`）适配器，由配置切换；CI 与本地只跑 Fake 适配器，沿用 identity 假发件箱 + dev inbox 的既有模式（总体设计 §7.4）。
2. **回调解缝 + 验签去重 + 测试夹具**：交付 HTTP 回执端点（签名校验、按 ProviderMessageID 去重、更新 Delivery Receipt），回执解析做成按供应商的格式解析器，用固定报文夹具（含伪造签名）做契约测试；不依赖真实阿里云环境。
3. **JSON 运维端点 + 结构化日志**：队列深度、最老任务等待、投递按状态计数、重试率、死信数、供应商延迟 P95 全部由确定性 SQL 得出，暴露为受会话保护的 JSON 端点；关键事件（死信、SLA 超标）打结构化日志。不引入 Prometheus/OpenTelemetry 依赖。
4. **Web 范围 = 四态计数 + 提醒记录列表**：仪表盘契约扩展为成功/重试中/失败/被抑制四个真实计数，新增提醒投递列表查询并在仪表盘页展示简洁记录区块。
5. **River 迁移并入 tern 版本序列**：River 官方 SQL 迁移内嵌为仓库自有迁移文件，由现有 migrate 命令一次执行，保持单一迁移管道与"应用实例禁止自行迁移"约束。
6. **投递管道形态 A**：River Worker 仅作为入站适配器调用 Reminder 的 `SendReminder` 应用命令，全部业务规则（重读 Todo/Plan/Channel/Delivery、抑制、幂等、供应商调用）收敛在应用层；抑制以执行时重读为准，revoke 时辅以尽力而为的 `JobCancel`；死信以业务 Delivery 终态表达。

## 2. 目标与非目标

### 2.1 目标

1. 带到期时间且存在已启用已验证通道的待办，在创建/改期事务内产生 Reminder Plan、每通道一条 Reminder Delivery 与对应 River Job，三者原子提交。
2. 到期后 30 秒内发起供应商调用（正常运行时）；每个已启用已验证的邮箱、短信通道分别产生独立的投递（验收场景 7）。
3. 待办在到期前完成、删除或改期时，旧 River Job 即使被领取也不会发送过期提醒（验收场景 9）；revoke 时尽力取消 River Job 以减少无效领取。
4. 瞬态失败由 River 按退避重试；重试不重复已成功通道（业务幂等键保护）；Worker 投递中崩溃后任务可恢复且重复执行不产生重复消息（验收场景 8）。
5. 永久失败或重试耗尽使 Delivery 进入 `failed` 终态（业务死信），打结构化死信事件，并在仪表盘与运维端点可见。
6. 回执端点验签通过后按 ProviderMessageID 幂等更新 Delivery Receipt；无效签名被拒绝；重复与未知回执被安全处理。
7. 仪表盘返回真实的成功/重试中/失败/被抑制计数；提醒记录列表端点返回逐条投递状态与回执；运维端点返回队列深度、最老等待、按状态计数、重试率、死信数、延迟 P95。
8. Fake 适配器是 CI/本地的唯一投递路径；真实 SMTP/Aliyun 适配器以本地测试监听与 httptest 夹具做单元测试；生产配置禁止选择 fake。
9. `make verify`、`make migration-test`、`make smoke-test` 全绿；架构政策绿；新增 Go 依赖仅 `riverqueue/river` 与 `riverqueue/riverpgxv5`（ADR-0002 授权）。
10. 迭代账本（brief、progress、decisions、test-matrix、handoff）让新 Agent 无需阅读实施对话即可继续；独立干净上下文回归 Agent 产出通过报告。

### 2.2 非目标

- 不连接真实阿里云短信或真实 SMTP 环境做集成测试（CI 无 egress，仅夹具与本地监听）。
- 不实现邮件退信/DSN、阿里云 MNS 报告、AES 加密报告解析。
- 不引入 Prometheus/OpenTelemetry 或其他可观测性依赖；不新增消息总线、Redis、RabbitMQ。
- 不实现数据导出/导入、私域离线包（ITER-0004）。
- 不实现循环提醒、提前多次提醒、邮件/短信之外的新通道。
- 不改变登录短信链路（Identity 自有缝、自有模板与假发件箱，§7.1 分离要求天然满足）。
- 不为 Web 侧新增依赖。

## 3. 模块结构与依赖方向

Reminder 模块从"只有调度缝"长成完整六边形，遵循 `domain/application/adapters` 与"入站适配器→应用→领域、应用→端口←出站适配器、cmd 装配具体适配器"：

```text
backend/internal/modules/reminder/
  domain/                     既有 Plan；新增 Delivery 聚合与状态机
  application/
    command/                  既有 PlanReminder / RevokePlans；新增 SendReminder / RecordReceipt
    query/                    新增 DeliveryStats（四态计数、运维统计） / ListDeliveries（记录列表）
    ports/                    既有 JobScheduler（演进） / PlanStore；
                              新增 DeliveryStore / EmailNotifier / SmsNotifier / ChannelResolver / ReceiptPolicy
  adapters/
    inbound/worker/           新增 River worker 适配器（唯一投递入口）
    inbound/http/             新增 提醒记录列表、运维端点、回执 webhook、dev 发件箱读取
    outbound/river/           新增 JobScheduler 实现：InsertTx 加入调用方事务、按通道扇出、尽力取消
    outbound/postgres/        既有 plans.go；新增 deliveries.go、fake_outbox 读写、统计查询
    outbound/smtp/            新增 真实 SMTP 适配器（net/smtp + PLAIN + 超时）
    outbound/aliyun/          新增 真实短信适配器（标准库 HTTP + HMAC-SHA1 RPC 签名）与回执解析器
    outbound/fake/            新增 假邮件/假短信适配器：渲染固定模板写入 reminder.fake_outbox
```

依赖与跨上下文约束：

- River 只存在于 `reminder/adapters/outbound/river` 与 `cmd` 装配；领域与应用不感知 River（ADR-0002）。
- Todo→Reminder 仍走既有 `ReminderPlanner` 缝，应用层零改动；仅请求 DTO 追加 `Title`（用于投递记录展示快照）。
- Reminder→Identity 只读通道可用性，经 Identity 公开应用接口，通过 `ChannelResolver` 端口注入（与 Todo 的 `ChannelsProvider` 同款模式）。
- Todo 仪表盘经 cmd 注入的 `ReminderStats` 端口合成提醒计数，不导入 reminder 包内部。
- `platform` 只新增配置字段与校验，不导入任何业务模块。

## 4. 领域模型：Delivery 聚合与状态机

### 4.1 ReminderDelivery

```text
DeliveryID            uuid
WorkspaceID           uuid
TodoID                uuid
TodoReminderVersion   int
PlanID                uuid
Channel               email | sms
TodoTitleSnapshot     text（计划时刻的标题快照，仅展示用）
IdempotencyKey        workspaceId:todoId:todoReminderVersion:channel（UNIQUE）
State                 scheduled | sending | succeeded | failed | suppressed
SuppressionReason?    todo_completed | todo_deleted | version_stale | channel_unavailable | plan_revoked
AttemptCount          int（每次 worker 尝试递增）
ProviderJobID?        River job id（计划时刻写回，供尽力取消）
ProviderMessageID?    供应商受理返回的消息 ID
LastErrorCode?        最近一次失败原因码
ScheduledAt / CreatedAt / SubmittedAt? / FinalizedAt?
ReceiptState?         received_ok | received_failed（供应商回执，信息性）
ReceiptAt? / ReceiptErrorCode?
```

Delivery 在计划时刻与 Plan 同事务创建：Plan 的每个 RequestedChannel 一条，初始 `scheduled`。`RequestedChannels` 为空（无可用通道）时只有 Plan、无 Delivery，合法。

### 4.2 状态机与不变量

```text
scheduled ──worker 领取──▶ sending ──供应商受理──▶ succeeded（终态）
   │                         │
   │                         ├──永久错误 / 末次尝试瞬态错误──▶ failed（终态，业务死信）
   │                         └──瞬态错误──▶ 保持 sending，River 退避后重试
   └──执行时重读命中抑制规则──▶ suppressed（终态，带 SuppressionReason）
```

- 终态（succeeded / failed / suppressed）不可再变更状态；`AttemptCount` 只在非终态递增。
- 回执不改变投递终态：`ApplyReceipt` 仅在 `succeeded` 状态可应用，按 ProviderMessageID 幂等（重复回执不产生二次变更）。这与 §9.1 "外部短信/邮件最终送达不计入应用可用性"一致。
- "重试中"不是独立状态，而是派生视图：`State=sending ∧ AttemptCount>0`。仪表盘与运维端点的重试中计数都用这一定义。
- 唯一约束同时落在 `IdempotencyKey` 与 `(TodoID, TodoReminderVersion, Channel)` 上，二者等价，保留显式键以便审计。

## 5. River 集成

### 5.1 依赖与版本

新增 `github.com/riverqueue/river` 与 `github.com/riverqueue/riverpgxv5`，锁定单一稳定版本并记录于 go.mod 与迁移文件头注释。这是本迭代仅有的新增 Go 依赖（ADR-0002 授权）。

### 5.2 调度侧（API 进程，insert-only）

- `JobScheduler` 端口演进为：

  ```go
  type ScheduledChannel struct { Channel string; JobID string }
  type JobScheduler interface {
      Schedule(ctx context.Context, job ReminderJob) ([]ScheduledChannel, error)
      Cancel(ctx context.Context, jobID string) error
  }
  ```

  no-op 适配器同步更新（Schedule 返回空切片、Cancel 返回 nil），既有单测随之演进。
- River 实现（`outbound/river`）在 `Schedule` 内从上下文取调用方环境事务（`database.TxFromContext`），用 `InsertTx` 把 job 写进业务事务：Plan、Delivery、River Job 原子提交。无环境事务时显式报错（计划路径永远在 Todo UoW 内，不做静默降级）。
- `Schedule` 按 `ReminderJob.Channels` 扇出：每个通道一个 job，kind 固定 `reminder_send`，队列 `reminder_email` / `reminder_sms`，`ScheduledAt = ScheduledAtUTC`。job args 携带 PlanID、Channel 与冗余的 WorkspaceID/TodoID/TodoReminderVersion（供领取时校验与观测）。返回的 JobID 由 `PlanReminder` 命令写回对应 Delivery 行（同一事务）。
- `Cancel` 为尽力而为的 `JobCancelByID`，失败仅记日志；抑制的正确性不依赖取消成功（见 §6.1）。
- API 进程只持有 insert-only River Client，无 fetch 循环（§6.6：API 扩容不增加队列消费）。

### 5.3 Worker 侧

- 既有 `cmd/worker`（心跳 + 健康检查）并行启动 River worker：订阅 `reminder_email`、`reminder_sms` 两个队列，各自独立并发上限（环境变量配置）；`MaxAttempts=5`，指数退避；LISTEN/NOTIFY 快速唤醒 + 周期性 fetch 兜底；leader 选举、stuck 救援、清理等维护由 River 自带机制承担。
- worker 领取 job 后调用 `SendReminder` 应用命令；`job.Attempt == MaxAttempts` 时传入 `FinalAttempt=true`（死信判定，见 §6.2）。
- 健康检查把 River worker 运行状态纳入 ready 判定；优雅停机等待在途投递完成或超时（沿用 `SHUTDOWN_TIMEOUT`）。

### 5.4 迁移

- `006_river_v1.sql`：River 官方 SQL 迁移内嵌为仓库自有迁移，文件头标注来源 River 版本；遵循 tern 格式（含 `---- create above / drop below ----` 的 drop 段）。升级 River 大版本时追加新迁移文件，不回改历史。
- `007_create_reminder_deliveries.sql`：创建 `reminder.reminder_deliveries`（字段见 §4.1，含 UNIQUE 幂等键、`(workspace_id, state)`、`provider_message_id` 索引）与 `reminder.fake_outbox`（dev 发件箱：address、channel、todo_id、body、created_at）。
- 迁移仍由 `cmd/migrate` 一次性执行；`RequireSchema` 的版本号随之提升。

## 6. 投递流程

### 6.1 抑制以执行时重读为准

完成、删除、改期待办时，既有缝 revoke 对应 Plan 并尽力 `Cancel` River Job。但取消不是正确性边界：job 即使被领取，`SendReminder` 也会重读最新事实并抑制（验收场景 9）。

### 6.2 SendReminder 应用命令

输入：job args（PlanID、Channel）与 `FinalAttempt` 标志。步骤：

1. 读 Plan：不存在 → 丢弃（记日志，正常结束）；已 revoked → 该 Delivery 抑制（plan_revoked），正常结束。
2. 读 Todo：已删除 → 抑制（todo_deleted）；已完成 → 抑制（todo_completed）；`ReminderVersion` 与 Plan 不匹配 → 抑制（version_stale）。
3. 按幂等键读 Delivery：已是终态 → 幂等返回（重复执行保护，验收场景 8）；异常缺失 → 防御性插入 `sending` 行并记日志（正常流程中 Delivery 与 Plan 同事务创建，不应发生）；随后经 `ChannelResolver` 重读联系人通道：不存在、未验证或已禁用 → 抑制（channel_unavailable），记录原因。
4. 调用对应 `EmailNotifier` / `SmsNotifier`：
   - 成功 → `succeeded`，记录 ProviderMessageID、SubmittedAt、FinalizedAt。
   - 永久错误（地址非法、模板拒收等供应商明确拒绝）→ `failed` 终态 + LastErrorCode + 结构化死信事件。
   - 瞬态错误（超时、限流、5xx）→ 若 `FinalAttempt` → `failed` 终态 + LastErrorCode + 死信事件；否则返回错误交给 River 重试。
5. 重试语义：重试作用于整个 job；已 `succeeded` 的通道在第 3 步被幂等跳过，不会重复投递。供应商支持幂等键时透传 IdempotencyKey；不支持时由本地投递记录降低重复概率，产品不宣称绝对仅发送一次（总体设计 §6.5）。
6. 供应商已受理但 worker 在持久化前崩溃的窗口：重试会再次调用供应商，可能产生供应商侧重复消息——这是已知边界，以供应商幂等键尽力缓解。

### 6.3 RecordReceipt 应用命令

按 ProviderMessageID 查找 Delivery（唯一索引）：未找到 → 安全忽略 + 日志；找到且 `succeeded` → 幂等应用回执（仅首次生效）；找到但非 succeeded → 记日志不变更。

## 7. 供应商适配器与配置

### 7.1 Fake 适配器（CI/本地默认）

- 假邮件与假短信适配器渲染固定提醒模板（与登录短信模板分离，§7.1），写入 `reminder.fake_outbox`（Postgres，跨进程可读，与 identity 假发件箱同模式但独立表、独立端口）。
- `GET /api/v1/dev/reminder-outbox?address=`（门禁：`APP_ENV != production` 且开关启用；生产环境路由不存在），返回最近记录。
- 发送带可配置延迟与失败注入（供重试/崩溃测试使用），由环境变量或测试装配控制。

### 7.2 真实 SMTP 适配器

- 标准库 `net/smtp`，PLAIN 认证、拨号与发送超时；4xx → 瞬态，5xx → 永久。
- 配置：`REMINDER_SMTP_HOST/PORT/USERNAME/PASSWORD/FROM`（经 `platform/config` 校验）。

### 7.3 真实 Aliyun 短信适配器

- 标准库 HTTP + HMAC-SHA1 RPC 签名（不引 SDK）；`Code=OK` → BizId 作为 ProviderMessageID；`isThrottled`/超时/5xx → 瞬态；非法号码类错误码 → 永久。
- 配置：`REMINDER_ALIYUN_ENDPOINT/ACCESS_KEY_ID/ACCESS_KEY_SECRET/SIGN_NAME/TEMPLATE_CODE`。
- 回执解析器（格式解析 + 签名校验）也位于该包，供回执 webhook 复用。

### 7.4 选择与门禁

- `platform/config` 新增：`REMINDER_EMAIL_ADAPTER=fake|smtp`、`REMINDER_SMS_ADAPTER=fake|aliyun`、`REMINDER_RECEIPT_SECRET`、worker 队列并发与 `REMINDER_DEV_OUTBOX_ENABLED` 等；API 与 worker 角色共享提醒段。
- `APP_ENV=production` 时选择 fake 视为配置错误，启动失败。

## 8. 回执回调

- 端点：`POST /api/v1/webhooks/receipts/sms`（无会话；请求头 `X-Receipt-Signature` 携带对原始请求体的 HMAC-SHA256 签名，密钥为 `REMINDER_RECEIPT_SECRET`；签名缺失或无效一律拒绝）。
- 请求体经按通道的格式解析器（本迭代为通用 JSON 格式 + Aliyun 夹具）转为内部回执 DTO，交 `RecordReceipt`。
- 幂等：同一 ProviderMessageID 的重复回调返回成功但不产生二次变更。
- 邮件无回执 webhook（本迭代非目标）；SMTP 同步响应即投递结果。

## 9. 仪表盘、提醒记录与运维端点

### 9.1 仪表盘

- `contracts/openapi/dashboard.yaml` 扩展：`reminderRetrying`、`reminderFailed` 填真实值，新增 `reminderSucceeded`、`reminderSuppressed`。
- 计数来源：Reminder 公开查询 `DeliveryStats(workspaceID)`；Todo dashboard handler 经 cmd 注入的 `ReminderStats` 端口合成（与 `ChannelsProvider` 同款缝）。
- 语义：succeeded / failed / suppressed 为对应终态计数；retrying = `sending ∧ AttemptCount>0`。

### 9.2 提醒记录列表

- `GET /api/v1/reminders`（会话保护）：分页与状态过滤，返回通道、Todo 标题快照、状态、尝试次数、计划/提交/完成时间、回执状态、最近错误码。
- 新增 `contracts/openapi/reminder.yaml`（列表、运维端点、回执 webhook、dev 发件箱）。
- Web：仪表盘页新增"提醒记录"区块（沿用既有 fetch + 组件测试模式），不做独立路由页。

### 9.3 运维端点

- `GET /api/v1/ops/reminder`（会话保护），全部确定性 SQL：
  - 两个队列深度（`river_job` 中 scheduled/available 且队列名以 `reminder_` 开头）；
  - 最老任务等待时长；
  - 投递按状态计数、重试率（全部 Delivery 中 AttemptCount>0 的占比）、死信数（failed 计数）；
  - 近 24 小时成功投递的发起延迟 P95（`submitted_at - scheduled_at`，直接度量 §13 "到期后 30 秒内发起"的 SLA）。
- 死信与 SLA 超标事件同时打结构化日志（§12 告警基础）。

## 10. 测试策略

- **领域单元**：Delivery 状态转换、幂等键构造、抑制原因、回执一次性应用、终态不可变。
- **应用单元**：SendReminder 全分支（各抑制原因、幂等跳过、瞬态/永久分类、末次尝试死信）；RecordReceipt 去重；PlanReminder 按通道扇出与 JobID 写回；RevokePlans 撤销 + 尽力取消。
- **Postgres 集成**：deliveries 仓储、UNIQUE 幂等键冲突、四态统计与运维查询（沿用 `TEST_DATABASE_URL` 模式）。
- **River 适配器契约**：同事务原子插入（plan+delivery+job 一起回滚）、best-effort 取消、重试重领、崩溃恢复（job 留在 available，第二 worker 领取且不重复投递——假发件箱计数=1）、重复执行幂等。
- **HTTP 测试**：回执验签通过/拒绝/重复回调/未知 ID；提醒列表与运维端点鉴权；dev outbox 门禁（生产路由不存在）。
- **真实适配器单测**：SMTP 用本地测试监听；Aliyun 用 httptest 夹具（签名、OK、限流、非法号码分类）。
- **架构测试**：依赖方向、reminder 绿区、platform 不含业务、跨上下文仅公开接口；如现行政策未覆盖 River 包位置，则按黄区流程更新政策。
- **契约测试**：reminder.yaml 新增与 dashboard.yaml 扩展的 OpenAPI 校验。
- **冒烟（compose）**：登录 → 创建"1 秒后到期"待办 → 30 秒内假发件箱出现提醒且 delivery=succeeded；到期前完成 → 被抑制且发件箱无消息；伪造回执回调 → 回执落库；运维端点返回合理计数；重启 worker 容器 → 未投递任务恢复。
- **迁移测试**：006/007 up/down 全链路。

## 11. 依赖、分区与风险

- **新增依赖**：仅 `riverqueue/river` + `riverqueue/riverpgxv5`；Web 侧零新增；无新容器服务。
- **黄区清单**（写入 plan 登记）：contracts（reminder.yaml 新增、dashboard.yaml 扩展）、platform/config 新字段、cmd/api 与 cmd/worker 装配、迁移 006/007、compose 环境变量、根 AGENTS.md 区域描述、架构政策（如需）。CI 结构不变（沿用 `make verify` / `migration-test` / `smoke-test`）。
- **红区维持**：不改历史迁移、不提交凭证、不降低门禁。
- **风险**：
  1. River 版本与内嵌迁移 SQL 漂移 → 迁移文件头标注来源版本，升级走追加新迁移；
  2. 本机 egress 受限 → 依赖经 GOPROXY 镜像获取（与 ITER-0002 环境一致）；
  3. 双通道部分成功的重试语义 → 幂等键保证重试不重复，测试显式覆盖；
  4. 受理成功但持久化前崩溃的供应商侧重复窗口 → §6.2 第 6 点声明为已知边界。

## 12. 验收标准

1. 带到期时间且存在可用通道的待办，在创建/改期事务内原子产生 Plan、每通道一条 Delivery 与 River Job；完成、删除、改期 revoke 计划并尽力取消 job。
2. 到期后 30 秒内（冒烟环境）发起投递；每个已启用已验证的邮箱、短信通道分别产生 Delivery 且 Fake 发件箱可查到消息。
3. 到期前完成/删除/改期的待办，其旧 job 被领取后仅产生 `suppressed` Delivery，Fake 发件箱无消息。
4. 瞬态失败触发 River 重试；重试跳过已成功通道；worker 崩溃恢复后不产生重复消息。
5. 永久失败或重试耗尽产生 `failed` 终态与死信日志事件，并出现在仪表盘与运维端点。
6. 回执端点：有效签名按 ProviderMessageID 幂等落库；无效签名被拒绝；重复与未知回执安全处理。
7. 仪表盘四态计数真实；提醒记录列表端点与仪表盘区块可见逐条投递；运维端点返回队列深度、最老等待、状态计数、重试率、死信数、延迟 P95。
8. CI/本地仅经 Fake 适配器投递；生产配置选择 fake 启动失败；真实适配器单测（本地监听/夹具）通过。
9. `make verify`、`make migration-test`、`make smoke-test` 在干净检出全绿；架构政策绿；go.mod 仅新增 River 两个模块。
10. 迭代账本齐备，独立干净上下文回归 Agent 产出 PASS 报告。
