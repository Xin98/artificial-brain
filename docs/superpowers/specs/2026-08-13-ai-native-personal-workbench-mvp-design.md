# AI Native 个人智能工作台：MVP 总体设计

- 日期：2026-08-13
- 状态：待书面复核
- 产品一句话：专为个人提效的 AI Native 智能工作台。
- 目标：从空仓库构建可云端使用、可私域部署的企业级稳定 MVP。

## 1. 已确认决策

1. 云端优先交付，同时从第一天保持 Docker 私域部署兼容。
2. 云端支持多用户；每个用户拥有一个个人工作区。私域首期为单用户。
3. 前端使用 Next.js；后端使用 Go 模块化单体；提醒任务由独立 Go Worker 执行；业务与任务数据使用 PostgreSQL。
4. 后端采用业务模块优先、模块内 `domain/application/adapters` 的 DDD 与六边形结构。
5. 模型只生成结构化意图提案，不能直接访问数据库或外部通知供应商。
6. 对话首期仅支持待办新增、删除和查询，不提供开放式通用聊天。
7. 智能删除必须候选匹配并二次确认，禁止自然语言直接批量删除。
8. 待办在到期时提醒一次；首期通道为邮箱和短信。
9. 到期调度使用 River + PostgreSQL，并通过应用端口隔离 River。
10. 云端使用手机号与短信验证码登录；邮箱只作为提醒通道。
11. 私域由企业配置兼容 OpenAI 接口的模型服务，不捆绑大模型。
12. 每次迭代采用 Spec、Plan、TDD、全量回归和独立干净上下文回归 Agent 门禁。
13. Superpowers 与选定的 Matt Pocock Skills 作为项目级、锁定来源的工程能力；升级前必须审计。

## 2. 交付范围

### 2.1 MVP 范围

#### 身份与设置

- 云端手机号注册/登录，短信验证码短时有效、单次使用。
- 私域首次部署生成管理员访问凭证，不开放注册。
- 用户可配置并验证邮箱、提醒手机号及通道启用状态。
- 所有业务数据按个人工作区和用户隔离。

#### 智能对话框

- 接收文字输入。
- 识别 `todo.create`、`todo.delete`、`todo.list` 和 `unknown`。
- 缺失必填信息、时间含糊或置信度不足时发起澄清。
- 将相对时间解析为用户时区下的绝对时间并回显。
- 写操作始终调用相同的应用命令，不能绕过权限、校验、审计和事务。

#### 待办

- 智能新增与表单新增。
- 智能删除与手动删除，均需确认；删除为软删除。
- 列表查看、关键词筛选、状态筛选和到期范围筛选。
- 仪表盘汇总。
- 手动完成、编辑标题/描述/到期时间；模型首期不处理完成和编辑意图。
- 无到期时间的待办合法，但不产生提醒计划。

#### 提醒

- 为已启用且已验证的邮箱、短信通道分别创建一次到期提醒。
- 支持取消、改期、重试、死信、供应商回执和业务幂等。
- 正常运行时在到期后 30 秒内发起供应商调用，目标 P95 不超过 10 秒。
- 仪表盘展示提醒成功、重试中、失败和被抑制的结果。

#### 可移植性

- 随时导出版本化 JSON ZIP，并附带便于人工读取的 CSV。
- 私域数据导入云端前必须预检。
- 重复导入幂等；冲突不静默覆盖。
- 导入不会补发过去已经到期的提醒。

### 2.2 明确不在 MVP 范围

- 开放式通用聊天。
- 飞书、钉钉、QQ、微信等提醒通道。
- 多人共享、团队协作、角色权限体系。
- 自然语言批量删除。
- 在私域安装包内捆绑或管理本地大模型。
- 微服务、Kafka、RabbitMQ、Redis 或 Temporal。
- 循环提醒、提前多次提醒和复杂工作流。
- 移动原生客户端。

## 3. 总体架构

```text
Browser
  |
  v
Next.js Web
  |
  v
Go HTTP Delivery Adapter
  |
  v
Identity / Todo / Conversation / Reminder / Portability Modules
  |                         |
  v                         v
PostgreSQL             JobScheduler Port
                              |
                              v
                         River Adapter
                              |
                              v
                        Go Worker Cluster
                          |           |
                          v           v
                       Email        SMS
```

Next.js 只负责 Web 交互和服务端渲染，不持有核心业务规则。Go API 进程处理同步请求并以 insert-only River Client 事务入队；Go Worker 进程订阅提醒队列。API 副本扩容不会增加 fetch 循环数量。

PostgreSQL 是业务事实和 River 任务的共同持久化设施。MVP 直接在保存待办/提醒计划的同一事务中插入 River Job，不额外构建一套自定义 Outbox。未来向外部消息总线发布领域事件时再引入通用 Outbox。

## 4. 限界上下文与关系

统一语言以 [CONTEXT-MAP.md](../../../CONTEXT-MAP.md) 为准。

### 4.1 Identity

拥有 User、Personal Workspace、Contact Channel 和 Login Challenge。云端一名用户拥有一个个人工作区；私域适配器提供固定的本地用户和工作区。其他上下文只接收已认证主体，不解析 Cookie、验证码或供应商细节。

### 4.2 Todo

Todo 是聚合根，核心字段包括：

```text
TodoID
WorkspaceID
OwnerUserID
Title
Description?
DueAtUTC?
TimezoneAtInput?
Status = pending | completed | deleted
ReminderVersion
CreatedAt / UpdatedAt / CompletedAt? / DeletedAt?
Version
```

领域不变量：

- 标题不能为空且受长度限制。
- 到期时间可为空。
- 删除为终态，对普通查询不可见。
- 完成和删除会使所有未执行提醒失效。
- 修改到期时间或提醒通道会递增 `ReminderVersion`。
- `overdue` 不存入状态列；它由 `pending && dueAt < now` 派生。
- 所有状态变更使用乐观并发版本，避免覆盖并发编辑。

### 4.3 Conversation

Conversation 拥有会话记录、Intent Proposal、Clarification 和 Confirmation Request，但不拥有 Todo。模型输出使用版本化严格 Schema：

```json
{
  "schemaVersion": "1",
  "intent": "todo.create | todo.delete | todo.list | unknown",
  "arguments": {},
  "confidence": 0.96,
  "missingFields": []
}
```

模型适配器之后必须执行运行时 Schema 校验。未知字段、非法枚举、越界字符串和不合法时间均视为无效提案，不能直接降级为写操作。

### 4.4 Reminder

Reminder Plan 表示用户针对某个 Todo 提醒版本选择的通道。Reminder Delivery 是业务审计事实；River Job 只是技术调度记录，允许按保留策略清理。

```text
ReminderPlan
- TodoID
- TodoReminderVersion
- ScheduledAtUTC
- RequestedChannels

ReminderDelivery
- DeliveryID
- WorkspaceID
- TodoID
- TodoReminderVersion
- Channel
- IdempotencyKey
- State
- AttemptCount
- ProviderMessageID?
- LastErrorCode?
- ScheduledAt / SubmittedAt? / FinalizedAt?
```

### 4.5 Portability

Portability 定义 Export Bundle、Import Preview、Source Identity 和 Import Conflict。它通过各上下文的公开应用接口读取/写入，禁止直接复制内部数据库表，以便不同版本之间迁移。

## 5. 代码结构与依赖方向

```text
apps/
  web/
    src/app/                         Next.js routes and composition
    src/features/                    feature-local UI and behavior
    src/shared/                      shared design system and client plumbing

backend/
  cmd/
    api/                             HTTP process composition root
    worker/                          River worker composition root
  internal/
    modules/
      identity/
        domain/
        application/
          command/
          query/
          ports/
          dto/
        adapters/
          inbound/http/
          outbound/postgres/
          outbound/sms/
      todo/
        domain/
        application/
          command/
          query/
          ports/
          dto/
        adapters/
          inbound/http/
          outbound/postgres/
      conversation/
        domain/
        application/
          command/
          query/
          ports/
          dto/
        adapters/
          inbound/http/
          outbound/openai_compatible/
          outbound/deterministic/
      reminder/
        domain/
        application/
          command/
          jobs/
          ports/
          dto/
        adapters/
          inbound/worker/
          outbound/postgres/
          outbound/river/
          outbound/email/
          outbound/sms/
      portability/
        domain/
        application/
          command/
          query/
          ports/
          dto/
        adapters/
          inbound/http/
          outbound/archive/
          outbound/postgres/
    platform/
      config/
      database/
      observability/
      security/
      transaction/

contracts/
  openapi/
  events/
  ai-intents/
  export-schemas/

architecture/
  policies/
  tests/

deploy/
  cloud/
  private/
  migrations/

tests/
  integration/
  contract/
  regression/
  e2e/

docs/
  adr/
  domain/
  iterations/
  runbooks/
  superpowers/specs/
  superpowers/plans/
```

依赖规则：

```text
inbound adapter -> application -> domain
application -> port <- outbound adapter
cmd -> concrete adapters (composition only)
```

- `domain` 只依赖 Go 标准库和本模块领域代码。
- `application` 可以依赖本模块 `domain`，不能依赖 HTTP、PostgreSQL、River、模型或供应商 SDK。
- 接口优先由使用者在 `application/ports` 定义；具体适配器实现接口。
- 跨上下文调用只能经过公开应用接口或版本化事件，禁止导入其他上下文的 `domain`、`adapters` 或数据库实现。
- `platform` 只提供技术能力，禁止放置待办、提醒等业务规则。
- 每个模块提供小而稳定的公开接口，把事务、校验和编排隐藏在实现内；调用者和测试都跨同一 seam。

## 6. 关键业务流程

### 6.1 智能新增

```text
用户文字
-> 安全与长度预检
-> 模型生成 Intent Proposal
-> Schema / 权限 / 时间校验
-> 必要时 Clarification
-> CreateTodo Command
-> 同事务保存 Todo、Reminder Plan、Reminder Delivery 和 River Jobs
-> 返回已创建待办及解析后的绝对时间
```

标题缺失必须澄清。到期时间缺失时允许创建无期限待办。相对时间按用户时区解析；存在两个合理解释时必须澄清，不能猜测。

表单新增调用同一个 `CreateTodo` 应用命令，不维护第二套规则。

### 6.2 智能删除

```text
用户文字
-> todo.delete Intent Proposal
-> 查询当前用户可见的 Todo Candidates
-> 0 个：返回未找到
-> 1 个：创建 Confirmation Request
-> 多个：要求用户明确选择后创建 Confirmation Request
-> 用户确认
-> 校验确认请求的用户、目标、版本和有效期
-> DeleteTodo Command
-> 软删除并使旧提醒版本失效
```

确认请求必须短时有效、单次使用，并绑定 Workspace、User、Todo 和 Todo Version。任何自然语言批量删除均拒绝。

### 6.3 查询和仪表盘

模型只把自然语言转换为筛选 DTO；所有结果和统计由确定性查询生成。仪表盘至少包含：

- 待处理总数。
- 今日到期数。
- 已逾期数。
- 无到期时间数。
- 最近 7 天完成数。
- 提醒重试中或失败数。

### 6.4 到期提醒

1. 创建或改期待办时，在业务事务内插入带 `ScheduledAt=DueAt` 的 River Job。
2. River 将任务持久化为 `scheduled`；维护调度器周期性将近期到期任务转为 `available`。
3. PostgreSQL `LISTEN/NOTIFY` 快速唤醒订阅队列的 Worker；周期 fetch 只作兜底。
4. 多 Worker 由 PostgreSQL 原子领取机制分配任务。
5. Worker Adapter 调用 `SendReminder` 应用命令，而非直接调用供应商。
6. 应用命令重新读取 Todo、Reminder Plan、Contact Channel 和现有 Delivery：
   - Todo 已完成/删除：标记为 suppressed。
   - `ReminderVersion` 不匹配：标记为 suppressed。
   - 通道未验证或已禁用：标记为 suppressed 并展示原因。
   - 已存在成功投递：幂等返回。
   - 其他情况：调用对应通知端口。
7. 成功提交后记录供应商消息 ID；临时失败由 River 重试；永久失败进入最终失败状态。
8. 供应商回调经验签和去重后更新 Delivery Receipt。

每个通道的幂等键为：

```text
workspaceId:todoId:todoReminderVersion:channel
```

River 保证持久化、领取和可靠重试，但不能使外部短信/邮件副作用天然“恰好一次”。供应商支持幂等键时必须透传；不支持时由本地投递记录降低重复概率，产品不得宣称绝对仅发送一次。

### 6.5 集群运行模型

```text
API replicas
- River insert-only client
- no queue fetch loop

Worker replicas (2 initially)
- subscribe reminder_email / reminder_sms
- each worker process fetches subscribed queues
- LISTEN/NOTIFY for low latency
- configurable periodic fetch for recovery

River leader
- scheduled job promotion
- stuck job rescue
- job cleanup and other maintenance
```

邮件和短信使用独立队列与并发上限，防止单一供应商故障阻塞另一通道。Worker 数量依据队列深度、最老任务等待时长和供应商限流扩缩，不跟随 API 副本数。

## 7. 身份、安全与隐私

### 7.1 云端登录

- 登录验证码仅保存哈希，短时有效、单次消费。
- 手机号、IP、设备维度执行速率限制与异常检测。
- Session 使用 `HttpOnly`、`Secure`、合适的 `SameSite` Cookie，并支持吊销和轮换。
- 登录短信和提醒短信使用不同模板、队列/配额和应用端口，避免提醒故障影响登录。

### 7.2 数据隔离

- 所有业务写入和查询显式携带 `WorkspaceID` 与 `UserID`。
- Repository 接口不暴露无作用域的普通读取方法。
- 集成测试验证跨工作区访问始终不可见。
- PostgreSQL 访问策略作为纵深防护；应用授权仍是主控制。

### 7.3 AI 安全

- 模型只接收完成当前解析所需的最少数据。
- 模型不能获得数据库、短信、邮箱或部署凭证。
- 结构化输出必须通过本地 Schema 校验。
- 提示注入内容不能改变允许意图列表、删除确认或权限规则。
- 模型不可用时，表单和普通列表仍可用。

### 7.4 敏感信息

- 手机号、邮箱、供应商回执和对话文本按敏感数据处理。
- 日志默认脱敏，不记录验证码、Session、模型密钥和消息正文。
- 云端密钥进入 Secret Manager/KMS；私域进入 Docker Secret 或权限受限的密钥文件。
- 删除、导入、通道变更和安全相关操作写入不可静默修改的审计记录。

首个生产部署适配器为阿里云短信与标准 SMTP 邮件；供应商 SDK 仅存在于 outbound adapters。私域可替换为企业自有实现。

## 8. 数据导出与导入

导出包格式：

```text
export.zip
  manifest.json
  todos.json
  reminder-deliveries.json
  preferences.json
  todos.csv
```

`manifest.json` 包含 Schema 版本、来源实例 ID、导出时间、记录计数和文件校验和。导出不包含验证码、Session、密钥或供应商访问凭证。

导入步骤：

```text
上传 Export Bundle
-> 校验归档与 Schema 版本
-> 生成 Import Preview
-> 用户确认
-> 按批次通过应用接口导入
-> 记录 Source Identity
-> 输出新增、跳过、冲突和失败报告
```

使用 `sourceInstanceId + sourceRecordId` 去重。已过期记录可以导入，但不得自动补发提醒；未来到期提醒只有在用户确认启用后才会重新安排。

## 9. 部署设计

### 9.1 云端

```text
Reverse Proxy / TLS
  |- Next.js Web
  |- Go API x N
  `- Go Worker x 2+
          |
          v
Managed PostgreSQL
```

- 迁移由发布流程中的一次性任务执行，应用实例禁止自行并发迁移。
- 采用结构化日志、OpenTelemetry traces/metrics 和统一错误关联 ID。
- PostgreSQL 执行持续备份和时间点恢复；云端初始目标为 RPO 5 分钟、RTO 1 小时。
- 应用月度可用性目标为 99.9%，外部短信/邮件最终送达不计入应用可用性，但供应商调用失败必须可观测。

### 9.2 私域

Docker Compose 包含：

```text
reverse-proxy + web + api + worker + postgres
```

- 默认绑定本机；开放到局域网时必须启用管理员凭证或企业反向代理认证。
- 共享云端业务代码和数据库结构，只替换身份、模型、消息和密钥适配器。
- 提供离线镜像包、配置模板、备份/恢复和逐版本升级手册。
- 升级前校验版本并创建备份；历史迁移只读，只能追加新迁移。
- 私域 RPO/RTO 由部署方备份能力决定，产品提供可验证的备份与恢复命令。

## 10. Agent Harness 与目录权限

### 10.1 迭代状态机

```text
Brief
-> advanced model + Superpowers brainstorming
-> Matt Pocock domain-modeling / codebase-design
-> reviewed Spec
-> advanced model + Superpowers writing-plans / Wayfinder
-> reviewed Plan or tracer-bullet tickets
-> economical model + TDD implementation
-> implementation-context verification
-> independent clean-context regression Agent
-> release approval
-> deployment and observation
```

Skill 来源和哈希由根目录 `skills-lock.json` 固定。升级 Skill 必须查看差异、安全扫描结果并运行 Harness 回归；禁止自动跟踪最新版。Skill 提示不能覆盖用户指令、仓库安全策略或 CI 门禁。

### 10.2 修改分区

#### 绿色区：批准 Plan 后可由实施 Agent 修改

- `apps/web/src/features/**`
- `backend/internal/modules/<当前模块>/{domain,application,adapters}/**`
- 与当前变更直接对应的单元、组件和模块集成测试
- 当前 `docs/iterations/<ITER-ID>/progress.md` 与 `handoff.md`

#### 黄色区：必须由高级模型更新 Spec/ADR/迁移和回滚计划

- `contracts/**`
- `backend/internal/platform/**`
- `apps/web/src/shared/**` 与 `apps/web/src/app/**`
- `backend/cmd/**`
- `deploy/**` 与数据库新迁移
- CI、根构建配置、依赖版本和代码生成配置
- 根目录或模块级 `AGENTS.md`
- `.agents/**` 与 `skills-lock.json`

#### 红色区：Agent 不得直接修改

- 已执行或已发布的历史迁移
- 自动生成文件（只能修改来源并运行生成器）
- 密钥、生产凭证、生产数据和运行中的生产状态
- `.git/**`
- 已完成迭代的证据报告；修正只能追加勘误
- 为使 CI 通过而降低测试、Lint、覆盖率或安全阈值

根目录与模块级 `AGENTS.md` 将把职责、依赖、允许路径、必跑命令、文件规模和升级条件写成就近指令。架构测试、CODEOWNERS/审批规则和 CI 同时执行，不能只依赖提示词。

### 10.3 长程任务文件

```text
docs/iterations/ITER-0001/
  brief.md
  spec.md
  plan.md
  progress.md
  decisions.md
  test-matrix.md
  regression-report.md
  handoff.md
```

每个 Plan 步骤必须声明输入、允许修改目录、验收测试、验证命令和完成证据。聊天上下文不是事实来源；新 Agent 必须能仅通过 Spec、Plan、提交、进度和 Handoff 恢复工作。

## 11. TDD、测试与质量门禁

### 11.1 TDD 循环

每个行为采用纵向小切片：

```text
写一个失败测试
-> 运行并确认因目标行为缺失而失败
-> 写最小实现
-> 运行并通过
-> 重构
-> 运行受影响测试集
```

测试只跨模块公开接口；禁止直接测试私有实现来冻结重构空间。时间相关逻辑必须注入 Clock，禁止真实等待。

### 11.2 测试层级

- Go 领域单元测试：聚合、值对象、状态转换和不变量。
- 应用测试：命令、查询、权限、事务与端口交互。
- 架构测试：依赖方向、跨上下文导入和目录政策。
- PostgreSQL 集成测试：Repository、迁移、并发和事务入队。
- River Adapter 契约测试：定时入队、取消/失效、重试、重复执行和恢复。
- AI 契约与评测：固定中英文语料、意图、参数、澄清、注入和危险操作拒绝。
- 前端组件测试：表单、列表、候选选择、确认和异常状态。
- OpenAPI 契约测试：生成类型和兼容性。
- E2E：登录、智能/手动新增、查询、删除确认、改期、完成和提醒。
- 可移植性测试：导出、校验、预检、重复导入和版本兼容。
- 非功能测试：限流、竞态、性能、恢复、安全和依赖漏洞。

真实模型、短信和邮件只在受控沙箱测试；普通 CI 使用确定性 Fake Adapter。

### 11.3 CI 顺序

```text
format
-> lint / static analysis
-> generated-code check
-> architecture tests
-> unit tests
-> integration tests
-> contract tests
-> frontend tests
-> E2E
-> AI evals
-> migration tests
-> security scan
-> independent regression approval
```

Go 执行格式化、静态分析、竞态检测和漏洞扫描；TypeScript 执行严格类型检查、Lint 和依赖约束。核心领域和变更代码设置高覆盖门槛，但不使用单一总覆盖率掩盖薄弱行为。Flaky Test 必须记录、隔离原因并修复，不能靠重跑掩盖。

### 11.4 独立干净上下文回归

每次迭代完成后单独启动回归 Agent，只提供：

- 已批准 Spec 与验收条件。
- 本次提交差异。
- 测试运行说明和环境约束。

它不能读取实施对话，不能先修改代码；必须独立执行完整回归、检查越权修改与架构漂移，并生成 `regression-report.md`。若失败，由实施 Agent 根据证据修复，之后启动一个新的干净上下文回归 Agent，旧回归上下文不复用。

## 12. 可观测性与错误处理

- 每个 HTTP 请求、AI 解析、River Job 和供应商调用共享可关联的 Trace/Correlation ID。
- 对用户返回稳定错误码和可行动消息；内部错误细节不泄漏。
- 指标至少包括请求错误率、模型解析失败率、队列深度、最老任务等待时长、投递成功率、重试率、死信数和供应商延迟。
- 告警覆盖验证码异常、跨工作区访问拒绝异常升高、提醒 SLA 超标、死信、数据库容量和备份失败。
- Runbook 覆盖供应商故障、River 积压、数据库恢复、私域升级失败和数据导入冲突。

## 13. 非功能验收目标

- 不含模型和外部供应商调用的普通应用请求，云端正常负载下 P95 小于 300ms。
- 仪表盘查询正常负载下 P95 小于 500ms。
- 到期提醒在系统正常运行时 30 秒内发起，P95 目标小于 10 秒。
- Worker 或 API 实例重启后，已提交的待办和提醒任务不丢失。
- API 横向扩容不增加 River fetch 循环；只有 Worker 副本消费队列。
- 相同导出包重复导入不会产生重复 Todo。
- 未经确认的智能删除永不执行。
- 模型不可用时仍可登录、手动管理和查看待办。

## 14. 交付切片

整体目标按依赖顺序拆成四个迭代，每个迭代都执行完整 Spec → Plan → TDD → 独立回归门禁：

1. **ITER-0001 Harness 与可运行骨架**：仓库政策、模块骨架、架构测试、CI、开发环境、数据库迁移框架和可观测性基线。
2. **ITER-0002 身份、待办与对话闭环**：登录、设置、Todo、仪表盘、结构化意图和删除确认；通知使用 Fake Adapter。
3. **ITER-0003 可靠提醒**：River、邮件、短信、供应商回调、重试、死信和运维指标。
4. **ITER-0004 私域与数据可移植性**：Docker Compose、离线配置、导出、预检、导入、备份恢复和升级演练。

该总体设计是四个迭代的共同约束；每个迭代仍需独立的可执行 Spec 与 Plan，不允许用总体设计替代任务级验收标准。

## 15. MVP 验收场景

1. 云端新用户通过手机号验证码登录并进入自己的个人工作区。
2. 用户输入“明天下午三点提醒我提交周报”，系统解析时区、创建 Todo，并展示绝对时间。
3. 用户通过表单创建无期限 Todo，不产生 Reminder Plan。
4. 用户输入模糊时间时系统澄清，而不是猜测。
5. 用户要求删除 Todo 时先看到唯一候选或候选列表；未确认前数据不变。
6. 用户不能读取、修改或推断另一个个人工作区的数据。
7. 到期时已启用且验证的短信、邮箱通道分别产生 Reminder Delivery。
8. Worker 在投递中崩溃后任务可恢复；重复执行受业务幂等保护。
9. Todo 在到期前完成、删除或改期时，旧 River Job 即使被领取也不会发送过期提醒。
10. 用户下载 Export Bundle，在云端对私域数据执行 Import Preview；重复导入无重复记录。
11. 私域实例在无官方云端连接的情况下，使用企业配置的模型、SMTP 和短信适配器运行。
12. 每个迭代只有在独立干净上下文回归报告通过后才可发布。

## 16. 参考资料

- [Next.js Self-Hosting](https://nextjs.org/docs/app/guides/self-hosting)
- [PostgreSQL LISTEN](https://www.postgresql.org/docs/current/sql-listen.html)
- [PostgreSQL SELECT locking clauses](https://www.postgresql.org/docs/current/sql-select.html)
- [River](https://riverqueue.com/)
- [River source and configuration](https://github.com/riverqueue/river)
- [OpenAI Function Calling and Structured Outputs](https://help.openai.com/en/articles/8555517-function-calling-in-the-openai-api-Function)
