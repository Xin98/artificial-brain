# 云上部署与邮箱身份接入设计（ECS private 部署 + 双标识登录 + DashScope 接入）

日期：2026-09-01
分支：`feat/260901_cloud_deploy`
状态：已与用户逐项确认（决策记录见 §1）

## 1. 背景与决策记录

用户已购买阿里云 ECS、RDS MySQL 与大模型 token plan，要求完成云上部署与大模型能力接入。经澄清，关键决策如下：

| # | 决策点 | 结论 | 理由 |
| --- | --- | --- | --- |
| D1 | 数据库 | ECS 上 Docker 跑 PostgreSQL；已购 RDS MySQL 闲置 | 系统强依赖 PostgreSQL（River 队列、tern 迁移、pg_dump 备份），MySQL 不可用 |
| D2 | 部署形态 | `DEPLOYMENT_MODE=private` 单管理员 | 单人自用，短信/邮件成本与滥用风险可控 |
| D3 | 登录标识 | 邮箱 + 手机号双支持；邮箱本次生产可用，手机号留缝待短信服务 | 用户未开通阿里云短信服务（签名/模板审核未就绪），邮箱经个人 SMTP 立即可用 |
| D4 | 邮件通道 | 个人邮箱 SMTP（QQ/163 等，授权码认证） | 零额外成本，单人发送量远低于日限额 |
| D5 | 大模型 | 阿里云百炼 DashScope OpenAI 兼容端点 + `qwen-max` | 用户已购百炼 token plan；后端 `openai_compatible` 适配器已实现，纯配置接入 |
| D6 | 访问方式 | ECS 公网 IP 直连 + 安全组锁定来源 IP，HTTP | 大陆域名需 ICP 备案（周期数周），单人场景安全组锁源已足够 |
| D7 | 发布方式 | ECS 上 `git pull` + `docker compose build/up` | 单人单机最简单直接，不引入 ACR/CI 推镜像 |
| D8 | 部署载体 | Docker Compose（确认维持） | 仓库既有部署产物与冒烟测试均围绕 compose 栈；生产级单机部署的主流形态 |

## 2. 总体架构

沿用现有五服务 compose 栈（postgres / migrate / api / worker / web），无新增中间件：

```
浏览器 ──HTTP──▶ ECS:3000 (web, Next.js)
                   │ rewrite /api/*（compose 内网）
                   ▼
                api:8080 ──▶ DashScope 兼容端点（qwen-max，意图解析）
                   │      └▶ 个人邮箱 SMTP：登录/联系通道验证码
                   ▼
             PostgreSQL（compose 服务，Docker 卷）
                   ▲
             worker:8081 ── River 队列：提醒投递（邮件通道；短信停用）
```

- `APP_ENV=production`、`DEPLOYMENT_MODE=private`；
- 安全组只放行 22（管理）与 3000（应用），均锁定用户常用出口 IP；8080/8081/5432 永不外暴露；
- 大模型调用方向为 api → DashScope（出网），不需要入网端口。

## 3. Identity 双标识（邮箱 + 手机号）

### 3.1 迁移 009（append-only，001–008 不动）

`deploy/migrations/009_email_identity.sql`：

```sql
alter table identity.users alter column phone drop not null;
alter table identity.users add column email text;
alter table identity.users add constraint users_email_unique unique (email);
alter table identity.users add constraint users_identifier_present
  check (phone is not null or email is not null);

alter table identity.login_challenges add column email text;
alter table identity.login_challenges add constraint login_challenges_one_identifier
  check ((phone is not null) <> (email is not null));
create index login_challenges_email_created_at_idx
  on identity.login_challenges (email, created_at desc);
```

- 存量数据 phone 全部非空，新约束天然满足，平滑升级；
- Postgres 唯一索引对多个 NULL 不冲突，纯邮箱用户不会撞 `users_phone_unique`；
- schema 版本号 v8 → v9，`make migration-test` 门禁同步更新。

### 3.2 登录 API

`POST /api/v1/auth/login/request` 与 `POST /api/v1/auth/login/verify` 请求体由
`{"phone": ...}` 变为 `{"phone": ...}` **或** `{"email": ...}`，恰好一个：

| 输入 | 响应 |
| --- | --- |
| 两者都填 / 都不填 | 422 `identifier_invalid` |
| 邮箱格式非法 | 422 `invalid_email` |
| 手机号格式非法 | 422 `invalid_phone`（沿用现有语义） |
| 非管理员标识（private 模式） | `registration_closed`（沿用） |
| 生产环境手机号请求验证码 | 503 `sms_unavailable`（见 §4） |
| 验证码发送失败（SMTP 瞬时错误） | 502 `verification_send_failed` |

错误包仍为 `{code, message, correlationId}` 稳定形态。

### 3.3 私域管理员与 provisioning

- 新增配置 `PRIVATE_ADMIN_EMAIL`；private 模式要求 `PRIVATE_ADMIN_PHONE` 与
  `PRIVATE_ADMIN_EMAIL` **至少配置一个**，`config.Load` 强制；两者都配置时，
  首启 provisioning 创建**同一个管理员用户并同时挂载两个标识**（邮箱、手机均可登录）；
- cloud 模式下两者都必须未设置（与现有 `PRIVATE_ADMIN_PHONE` 规则对称）；
- 非 private（开发/云）模式首次验证登录时，按所用标识创建用户与工作区。

### 3.4 应用层与域

- `RequestLoginChallengeHandler` / `VerifyLoginChallengeHandler` 由
  `Handle(ctx, phone)` 改为接收标识（邮箱或手机号二选一）；挑战存储按
  phone/email 各自查询，用户存储新增按邮箱查找；
- 域校验复用现有 `ErrInvalidPhone` / `ErrInvalidEmail`；
- 联系通道（`/settings/contact-channels`）投递走同一验证码发送缝（§4）：
  生产下邮箱通道可正常验证，添加手机通道返回 `sms_unavailable`。

### 3.5 Web 登录页

单输入框「手机号或邮箱」，前端按是否含 `@` 自动归入请求体对应字段；错误展示
兼容 `invalid_email` / `invalid_phone` / `sms_unavailable`。其余登录 UI、会话、
路由守卫不动。

## 4. 验证码投递：Identity 新增 SMTP 出站适配器

`MessageOutbox` 端口（`Write(ctx, OutboxMessage{Address, Channel, Purpose, Code})`）
**零改动**，新增适配器并按环境装配：

- 新增 `backend/internal/modules/identity/adapters/outbound/smtpoutbox`：
  标准库 `net/smtp`，PLAIN 认证（仅当配置用户名时尝试）、可注入拨号、超时保护；
  5xx = 永久失败、4xx/拨号/超时 = 瞬时失败（与提醒模块 SMTP 适配器同语义）；
  邮件主题按 Purpose 区分（登录验证码 / 通道验证码）；只处理 `Channel=email`
  的消息，收到短信消息返回错误（装配层保证不会发生）；
- **装配规则**：
  - 非生产（`APP_ENV != production`）：现有 `fakeoutbox`，dev inbox 行为完全不变，
    本地与 CI 冒烟不受影响；
  - 生产：邮箱消息走 `smtpoutbox` 真实发送，**明文验证码不落库**；手机消息
    无真实短信通道，返回 `sms_unavailable`（fail-closed）。将来开通阿里云短信
    后，在同一缝新增短信适配器即可；
- **生产下缺失 SMTP 配置 → `config.Load` 启动失败**（fail-closed）。

### SMTP 配置变量

新增（供 Identity 使用）：

```
SMTP_HOST / SMTP_PORT / SMTP_USERNAME / SMTP_PASSWORD / SMTP_FROM / SMTP_TIMEOUT
```

`SMTP_TIMEOUT` 默认 `10s`（与 `REMINDER_SMTP_TIMEOUT` 一致）；其余无默认值，
生产环境下 `SMTP_HOST`、`SMTP_PORT`、`SMTP_FROM` 缺失即 `config.Load` 失败；
`SMTP_USERNAME`/`SMTP_PASSWORD` 成对出现（授权码认证）。

提醒模块保留自有 `REMINDER_SMTP_*` 不动——两个限界上下文各自拥有配置，
部署模板中两处填同一套值。不为 Identity 引入 `*_ADAPTER` 开关：生产一律真实
发送、非生产一律 fakeoutbox，选择由 `APP_ENV` 决定，避免无用的配置面。

## 5. 提醒通道：`REMINDER_SMS_ADAPTER=disabled`

生产禁用 fake 适配器，而用户尚未开通短信服务，新增枚举值解除死锁：

- `REMINDER_SMS_ADAPTER` 枚举：`fake` | `aliyun` | `disabled`；`disabled` 在任何
  环境合法，`fake` 仍仅限非生产；
- `disabled` 语义：提醒计划扇出时跳过短信通道（即使用户存在已验证手机联系通道，
  也不产生短信投递行）；Worker 不注册短信 River 队列；`fake` 在生产仍被禁止；
- 邮件通道照常：`REMINDER_EMAIL_ADAPTER=smtp` + `REMINDER_SMTP_*`；
- 短信回执 webhook 保持注册但不再有匹配的短信投递（无操作）；
- 将来开通短信：改回 `aliyun` 并填 `REMINDER_ALIYUN_*`，零代码改动。

## 6. 大模型接入（纯配置）

`openai_compatible` 适配器已实现并装配，本节零代码改动：

| 变量 | 生产取值 |
| --- | --- |
| `MODEL_ADAPTER` | `openai_compatible` |
| `MODEL_BASE_URL` | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| `MODEL_NAME` | `qwen-max` |
| `MODEL_API_KEY` | 百炼 API Key（仅 `.env`，不进 git） |
| `MODEL_TIMEOUT` | `30s`（qwen-max 延迟偏高，自默认 15s 放宽） |

- ECS 与百炼同地域时，runbook 注明可改用 VPC 端点（`dashscope-vpc.<region>.aliyuncs.com`）省公网流量；
- 模型可随时改环境变量切换（如先 `qwen-turbo` 跑通链路再切回）。

## 7. 部署（ECS + Docker Compose）

### 7.1 交付物

1. 新增 `docs/runbooks/cloud-ecs.md`：ECS 上线 runbook（初始化、安全组、`.env`、
   首启、验收、更新、备份、故障诊断）；
2. 更新 `deploy/private/env.template`：新增 `PRIVATE_ADMIN_EMAIL`、`SMTP_*` 段、
   `REMINDER_SMS_ADAPTER=disabled` 示例；`WEB_PORT` 保留 `127.0.0.1:3000`
   默认值（D8 主机本地语义不变），注释明确：云上 IP 直连场景在 `.env` 覆盖为
   `WEB_PORT=3000`；
3. 更新 `deploy/private/README.md`：双标识与邮箱管理员说明；
4. `compose.yaml` 仅**增加环境变量传递**（api 服务：`PRIVATE_ADMIN_EMAIL`、
   `SMTP_*`），不改服务拓扑、端口与依赖顺序；
5. 根 `README.md` 环境变量表补新变量。

### 7.2 ECS 初始化（runbook 要点）

1. 探测 `docker --version && docker compose version`；缺失则按发行版安装
   （Alibaba Cloud Linux：`dnf install docker docker-compose-plugin`；
   Ubuntu：`docker-ce` + `docker-compose-plugin`）；已装则跳过；
2. 安全组：`22/tcp` 仅管理 IP；`3000/tcp` 仅常用出口 IP；
   `8080/8081/5432` 不开；
3. `git clone` → 按模板填 `.env`：
   - `DEPLOYMENT_MODE=private`、`APP_ENV=production`、`PRIVATE_ADMIN_EMAIL`；
   - `POSTGRES_PASSWORD`、`REMINDER_RECEIPT_SECRET`（`openssl rand -hex 32`）；
   - `SMTP_*` 与 `REMINDER_SMTP_*`（个人邮箱授权码，两处同值）；`MODEL_*`；
   - `REMINDER_EMAIL_ADAPTER=smtp`、`REMINDER_SMS_ADAPTER=disabled`；
   - `WEB_PORT=3000`、`API_PORT=127.0.0.1:8080`；
4. `docker compose up -d --build`：migrate 一次性跑完 → api/worker 转 healthy →
   首启 provisioning 管理员；
5. 端到端验收清单：
   - `curl :3000/health/live`、`curl 127.0.0.1:8080/health/ready`、
     `/api/v1/system/health` 全绿；
   - 浏览器打开 `http://<ECS公网IP>:3000/`，邮箱验证码登录；
   - 对话创建一条待办（验证 qwen-max 意图解析）；
   - 创建带到期时间的待办并等到投递（验证提醒邮件 + River + SMTP 全链路）。

### 7.3 更新与备份

- 更新：`make backup` → `git pull` → `docker compose build` →
  `docker compose up -d`（migrate 一次性容器自动补迁移）；
- 备份：沿用 `make backup`（pg_dump + sha256 sidecar → `deploy/private/backups/`）；
  runbook 附每日 cron 示例与可选的 OSS 异地命令（仅文档，不做脚本）；
- 恢复/升级：复用 `make restore`、`docs/runbooks/backup-restore.md`、
  `docs/runbooks/upgrade.md`，仅补 ECS 语境说明。

## 8. 错误处理汇总

| 场景 | 行为 |
| --- | --- |
| 登录标识零个/两个 | 422 `identifier_invalid` |
| 邮箱/手机号非法 | 422 `invalid_email` / `invalid_phone` |
| 生产手机号请求验证码 | 503 `sms_unavailable` |
| SMTP 瞬时失败（4xx/超时/拨号） | 502 `verification_send_failed`，可重试 |
| SMTP 永久失败（5xx） | 502 `verification_send_failed`，日志记录永久错误 |
| 生产缺 SMTP / 管理员标识双缺 / 非法枚举 | `config.Load` 失败，进程不启动 |
| 模型调用失败 | 沿用现有会话错误包语义，不新增 |

## 9. 测试与验收

- **单元**：域双标识校验；双标识登录/验证/管理员 provisioning 的 command 测试；
  `smtpoutbox` 注入拨号测试（照提醒 SMTP 适配器测试样式）；`config.Load`
  fail-closed 新规则；提醒扇出跳过短信（`disabled`）；
- **迁移**：`make migration-test` 从空库跑到 009，门禁版本号更新为 v9；
- **冒烟**：开发模式新增邮箱登录块（验证码走 dev inbox）。**CI 不碰真实供应商**
  （红线）；生产形态验收按 §7.2 清单在 ECS 上人工执行；
- **门禁**：`make verify` / `make migration-test` / `make smoke-test` 全过才可合并。

## 10. 黄区/红区影响登记（ITER 规则）

黄区（刻意变更，逐项列明）：

1. 新增迁移 `009_email_identity.sql`，schema 版本门禁 v8 → v9；
2. `compose.yaml`：api 服务新增环境变量传递（不改拓扑）；
3. `backend/internal/platform/config`：新增变量与校验规则；
4. `deploy/private/env.template` 与 `deploy/private/README.md` 扩展；
5. 根 `README.md` 环境变量表。

红区（遵守）：

- CI 不调用真实供应商（冒烟用开发模式 fakes）；
- 不提交任何凭证（`.env` 保持 gitignored，模板只留占位符）;
- 不降低 CI 门禁；迁移 001–007 不改动。

## 11. 明确不做（YAGNI）

- 限流/验证码（单管理员 + 安全组锁源足够）；
- TLS/域名（已选 IP 直连）；
- 手机号真实短信发送（缝已留，待短信服务开通）；
- ACR/CI 推镜像（单机场景 ECS build 足够）;
- 多用户运营能力（private 模式）；
- RDS MySQL 的任何利用（与系统不兼容，闲置）。
