# ITER-0002 身份、待办与对话闭环设计

- 日期：2026-08-17
- 状态：待书面复核
- 上位设计：[AI Native 个人智能工作台：MVP 总体设计](./2026-08-13-ai-native-personal-workbench-mvp-design.md)
- 迭代目标：在 ITER-0001 纵向骨架上交付第一个业务闭环——云端手机号验证码登录、个人工作区隔离、待办全生命周期、确定性仪表盘，以及把自然语言严格转换为受控意图并路由到待办公开应用接口的对话闭环；提醒只建立调度缝，不做任何投递。

## 1. 背景与设计选择

ITER-0001 证明了 Web→API→PostgreSQL 的健康交付链路、独立迁移、架构政策与 CI，但没有任何业务行为。ITER-0002 引入 Identity、Todo、Conversation 三个限界上下文的第一个可用闭环，并用 Reminder 的最小调度缝为 ITER-0003 预热。

本迭代坚持“闭环优先、交付滞后”：登录、待办、意图、确认都真实可用，但提醒投递、真实短信/邮件供应商、River、数据可移植性都不进入本迭代。为让登录在没有真实短信的情况下可演练，采用云端模式 + 假短信收件箱；为让意图解析在无真实模型时可测试，提供确定性语料适配器，并按总体设计保留 OpenAI 兼容适配器。

## 2. 目标与非目标

### 2.1 目标

1. 用户可用手机号请求登录验证码、完成校验并获得会话 Cookie，进入唯一一个个人工作区。
2. 所有业务读写显式携带工作区与用户；集成测试证明跨工作区不可见。
3. 待办可通过表单与智能意图新增、列表、完成、编辑；删除为软删除且必须经过确认。
4. 仪表盘返回确定性的待处理、今日到期、逾期、无到期、近 7 天完成等计数，提醒重试/失败计为 0。
5. 对话文本被解析为版本化、严格校验的意图提案；仅已注册意图可触达待办公开应用接口；新增意图回显解析后的绝对时间。
6. 信息缺失、时间含糊或置信度不足时澄清，而不是猜测。
7. 删除必须候选匹配并二次确认；确认请求短时有效、单次使用、绑定用户/工作区/待办/版本；自然语言批量删除被拒绝。
8. 提示注入不能改变允许意图列表、删除确认或权限规则。
9. 带到期时间的待办通过 JobScheduler 缝创建提醒计划；完成、删除或改期使未执行计划失效；不发生任何投递。
10. 架构政策、零新增依赖、统一的 Make/CI 门禁在不破坏 ITER-0001 的前提下全部保持绿色。

### 2.2 非目标

- 不实现提醒投递、真实短信/邮件供应商、供应商回执、重试与死信（ITER-0003）。
- 不引入 River、消息总线或任何新的 Go/Web 依赖。
- 不实现数据导出/导入或私域离线包（ITER-0004）。
- 不提供开放式通用聊天；不提供完成/编辑的自然语言意图（首期仅 create/delete/list）。
- 不改变 Worker 业务行为（除模式版本常量）。

## 3. 限界上下文与模块结构

新增四个业务模块，均遵循 `domain/application/adapters` 与“入站适配器→应用→领域、应用→端口←出站适配器、cmd 装配具体适配器”的依赖方向：

```text
backend/internal/modules/
  identity/      User、Personal Workspace、Login Challenge、Session、Contact Channel
  todo/          Todo 聚合根（含 ReminderVersion、乐观并发 Version）
  reminder/      Reminder Plan、JobScheduler 端口、no-op 调度适配器（本迭代不投递）
  conversation/  Intent Proposal、Clarification、Confirmation Request、Intent Router、ModelPort
```

跨上下文只经过公开应用接口：Conversation→Todo、Todo→Reminder、Identity→其余上下文。`platform` 只新增事务能力与路由演进，不导入任何业务模块。

## 4. 关键流程

### 4.1 登录（云端模式 + 假短信收件箱）

```text
POST /api/v1/auth/login/request {phone}
  -> 速率限制（同一手机号滚动窗口内挑战数上限）
  -> 生成 6 位验证码，仅保存 SHA-256 哈希，短 TTL，写入 Login Challenge
  -> 假短信适配器把明文验证码写入 identity.message_outbox（purpose=login）
  -> 202
POST /api/v1/auth/login/verify {phone, code}
  -> 校验挑战哈希、TTL、单次使用、尝试次数上限
  -> 首次验证自动注册用户与个人工作区（同一事务）
  -> 生成 32 字节随机会话令牌；仅存 sha256(token)；Set-Cookie ab_session
       (HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=SessionTTL)
  -> 200 {userId, workspaceId, expiresAt}
GET /api/v1/dev/sms-inbox?address=  （仅 APP_ENV!=production 且 DEV_INBOX_ENABLED=true 时注册）
  -> 返回该地址最近 5 条收件箱记录（含验证码），供本地登录
```

### 4.2 待办与提醒调度缝

Todo 创建/改期在同一业务事务内完成待办写入与提醒计划：

```text
CreateTodo Command
  -> UnitOfWork.Run:
       todo store.Insert
       if dueAt != nil: ReminderPlanner.Plan(ScheduledAt=DueAt, ReminderVersion, channels)
            -> reminder store 写入 Reminder Plan（(todo_id, reminder_version) 幂等）
            -> JobScheduler.Schedule（no-op 适配器）
Complete/Delete/Update
  -> UnitOfWork.Run: 状态转换 + 版本校验 + ReminderPlanner.Revoke（使旧计划失效）
```

事务能力由 `platform/database` 提供（`Executor`/`Tx`/`ExecutorFromContext`/`NewTxRunner`）；仓储从上下文解析执行器，使 Reminder 的公开 `PlanReminder`/`RevokePlans` 加入调用方事务。ITER-0003 只需把 `JobScheduler` 换成 River `InsertTx`，应用层零改动。

### 4.3 对话意图闭环

```text
POST /api/v1/conversation/messages {text, timezone}
  -> ModelPort.Propose（确定性语料适配器，或配置门控的 OpenAI 兼容适配器）
  -> 单一运行时校验关口：schemaVersion="1"、精确键集、枚举、边界、时间可解析
  -> 缺失/含糊/低置信 -> Clarification（不猜测）
  -> Intent Router 仅分发已注册意图：
       todo.create -> CreateTodo Command（回显绝对时间）
       todo.list   -> ListTodos Query
       todo.delete -> 候选匹配 -> Confirmation Request
       其他        -> unsupported
```

### 4.4 删除确认（智能与手动共用）

```text
POST /api/v1/confirmations {intent:"todo.delete", todoId}
  -> 校验待办存在且 pending，绑定 user/workspace/todo/version，短 TTL -> confirmationId
POST /api/v1/confirmations/{id}/confirm
  -> 同一事务：条件式消费（未消费且未过期）+ 版本复核 + DeleteTodo
  -> 已消费/过期/版本不匹配 -> 稳定错误码
```

不存在原始 `DELETE /api/v1/todos/{id}`，也不存在任何批量删除路径。

## 5. 数据模型与迁移

模式版本由 1 提升到 5，新增四条只追加、不可变的 tern 迁移（`deploy/migrations/002–005`），各自创建上下文 schema（`identity`、`todo`、`reminder`、`conversation`）并带 `---- create above / drop below ----` 标记。`database.CurrentSchemaVersion=5`，API/Worker 保持等值模式门。上下文之间不建跨 schema 外键；隔离由应用强制并集成测试验证。

关键表：`identity.users/workspaces/login_challenges/sessions/contact_channels/message_outbox`、`todo.todos`、`reminder.reminder_plans`、`conversation.confirmation_requests/messages`。

## 6. API 表面与契约

API 使用 Go 1.26 `http.ServeMux` 方法+通配模式；健康三路由保持不变。业务路由覆盖认证、设置、待办、仪表盘、对话、确认与门控的开发收件箱（详见实施计划的路由表）。所有响应携带 `X-Correlation-ID`；错误使用稳定 `{code, message, correlationId}` 包络。新增四个 OpenAPI 3.1.1 契约文件与遍历测试；系统健康契约与其测试保持不变。

## 7. 配置、安全与隐私

- 配置新增 `APP_ENV`、`DEV_INBOX_ENABLED`、会话/挑战/通道码/确认 TTL、`MODEL_ADAPTER` 及 OpenAI 兼容所需项；`DEV_INBOX_ENABLED=true` 与 `APP_ENV=production` 同时出现时 `config.Load` 失败（fail-closed）。配置错误只报键名，不回显值。
- 验证码与会话令牌只存哈希；验证码单次使用、短 TTL、限制尝试次数与挑战频率；日志不记录验证码、会话令牌或消息正文。
- 会话 Cookie 具备 HttpOnly/Secure/SameSite；登录轮换令牌、登出吊销。
- 所有仓储方法显式携带 `workspace_id`/`owner_user_id`；无作用域读取不存在。
- 意图提案必须先过 Schema、权限、确认与幂等策略才能转为应用命令；模型不持有数据库/供应商/HTTP 客户端访问权。

## 8. Web

Next.js App Router：新增 `shared/server/session.ts`（读取 `ab_session`、fail-closed 会话校验）、`(workbench)` 路由组（服务端无会话即重定向 `/login`）、`/login`、`/status`（原健康页迁移）。通过 `next.config.ts` 的 `rewrites` 把 `/api/v1/:path*` 代理到 `API_INTERNAL_URL`。`src/features/{auth,dashboard,todos,settings,conversation}` 以注入式 fetcher + 手写 fail-closed 校验器实现；feature 代码不读 `process.env`、不导入 `shared/server`、不向浏览器泄漏内部地址。仍为零 UI/表单/schema 库，仅在需要交互处引入 client component。

## 9. 测试设计

- Go 领域单元测试：各聚合/值对象/不变量（含乐观并发、软删除终态、ReminderVersion 递增、确认单次使用与 TTL 边界）。
- 应用测试：命令/查询、权限、端口交互，使用内存 fake 与注入 Clock。
- PostgreSQL 集成测试：各仓储、迁移、并发与事务入队；`TEST_DATABASE_URL` 缺失时跳过；证明跨工作区不可见与 Todo+ReminderPlan 原子回滚。
- AI 契约与评测：固定中英文语料、意图、参数、澄清、注入与危险操作拒绝（确定性适配器字节级可复现）。
- 意图路由契约测试：仅注册意图到达应用接口；模型/适配器不能绕过策略。
- 前端组件测试：登录两步、列表筛选、候选选择、确认、异常状态（注入 fake fetch）。
- OpenAPI 契约测试：生成类型与兼容性（遍历式，含对变异文档的拒绝）。
- Compose 冒烟：在健康链之上新增鉴权端到端块（登录→建待办→意图建待办+提醒计划→确认删除）。

## 10. 迭代工件与修改边界

沿用 `docs/iterations/ITER-0002/{brief,spec,plan,progress,decisions,test-matrix,handoff}.md`；`regression-report.md` 仅在独立干净上下文回归后生成。黄色区（契约、platform、web shared/app、cmd、deploy 迁移、CI/根构建、AGENTS.md）在实施计划中逐项列出并刻意处理；红色区（历史迁移 001、生成文件、密钥、系统健康契约、CI 顺序）不得触碰。

## 11. 验收标准

见 [brief.md](../../iterations/ITER-0002/brief.md) 的 12 条验收条件；本设计与其一一对应。

## 12. 风险与控制

- **跨上下文事务耦合**：用消费方端口 + platform 事务能力隔离，Reminder 公开接口加入调用方事务，避免应用层触碰 pgx。
- **登录在无真实短信下不可用**：假短信收件箱 + 双重门控开发端点，生产 fail-closed。
- **意图解析依赖真实模型**：确定性语料适配器保证 CI 可复现；OpenAI 兼容适配器仅 fake HTTP 契约测试。
- **模式版本漂移**：`CurrentSchemaVersion` 与迁移/冒烟/契约的固定断言同步更新，回归审查者校验 001 与健康契约未被改动。
- **架构边界被业务代码破坏**：新模块树必须通过既有 `make architecture-test`，政策不变。
- **过度引入依赖**：零新增 Go/Web 依赖；任何新增须记录决策并复核。
