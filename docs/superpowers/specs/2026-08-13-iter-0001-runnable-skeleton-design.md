# ITER-0001 Harness 与可运行骨架设计

- 日期：2026-08-13
- 状态：已批准（2026-08-13）
- 上位设计：[AI Native 个人智能工作台：MVP 总体设计](./2026-08-13-ai-native-personal-workbench-mvp-design.md)
- 迭代目标：建立能在干净环境中启动、验证和持续演进的纵向技术骨架，不提前实现业务功能。

## 1. 背景与设计选择

仓库已经确定 Next.js Web、Go 模块化单体、独立 Go Worker 和 PostgreSQL 的总体架构，但尚无应用代码、构建入口、迁移体系或持续集成。ITER-0001 需要先证明整条交付链路可运行，并用自动化政策保护后续模块边界。

本迭代采用“纵向可运行骨架”：一次贯通 Web、API、Worker、PostgreSQL、迁移、健康检查、架构测试和 CI。未采用后端优先方案，因为它不能尽早验证 Web 到 API 的交付链路；未采用纯规范与 CI 方案，因为它可能形成检查通过但系统无法启动的空骨架。

## 2. 目标与非目标

### 2.1 目标

1. 开发者在安装 Docker、Docker Compose 和 Make 后，可用一条命令构建并启动完整本地栈。
2. Web、API、Worker 和 PostgreSQL 均有可自动验证的运行状态。
3. 数据库迁移由独立的一次性进程执行，API 和 Worker 不在启动时修改 Schema。
4. Go 代码从第一天遵守模块化单体的依赖方向和跨上下文边界。
5. 本地与 CI 复用同一组格式化、静态检查、测试、迁移、构建和冒烟验证入口。
6. 为后续迭代建立就近项目指令、迭代证据文件、结构化日志、关联 ID 和优雅停机基线。

### 2.2 非目标

- 不实现登录、Todo、Conversation、Reminder 或 Portability 的业务行为。
- 不引入 River、模型 SDK、短信、邮件或其他外部供应商。
- 不实现生产云基础设施、完整监控平台、私域离线包或数据导入导出。
- 不创建伪业务接口或虚构领域实体来填充目录。
- 不要求浏览器页面承担生产运维控制台职责；健康页只用于开发与骨架验收。

## 3. 仓库结构

本迭代创建以下最小结构：

```text
apps/
  web/
    src/app/
    src/features/system-health/
    src/shared/
backend/
  cmd/
    api/
    worker/
    migrate/
  internal/
    modules/
      identity/
      todo/
      conversation/
      reminder/
      portability/
    platform/
      config/
      database/
      observability/
      server/
contracts/
  openapi/
architecture/
  tests/
deploy/
  migrations/
tests/
  smoke/
docs/
  iterations/ITER-0001/
```

业务模块目录只保留说明文件、公开 package 边界或被测试需要的最小 package；不批量创建没有代码和职责的空层级。实际引入某个模块时，再在模块内部建立 `domain/application/adapters`。

根目录提供 `Makefile` 作为人和 CI 的统一入口，并提供 Docker Compose 配置、示例环境变量和清晰的本地启动说明。工具链与依赖版本必须锁定在仓库声明和容器构建文件中，禁止依赖开发机的隐式全局版本。

## 4. 运行时架构

```text
Browser
  |
  v
Next.js Web ----HTTP----> Go API ----SQL----> PostgreSQL
                            ^                    ^
                            | health summary     |
                            |                    |
                       Go Worker ----------------+

One-shot migrate ----------------------------SQL-> PostgreSQL
```

### 4.1 Web

Web 提供一个系统状态页面，展示 Web 自身、API、Worker 和数据库的聚合状态。浏览器只访问 Web；Web 的服务端逻辑使用内部网络地址查询 API 健康汇总，不向浏览器暴露 Compose 服务名。

状态页面必须支持 `healthy`、`degraded` 和 `unavailable` 三种呈现。下游失败或超时时页面仍可渲染，并显示稳定、可行动且不包含内部凭证的错误信息。

### 4.2 API

API 是 Go 模块化单体的 HTTP 组合入口。本迭代只提供：

- `GET /health/live`：只证明进程和 HTTP 循环存活，不访问外部依赖。
- `GET /health/ready`：验证必要配置和 PostgreSQL 连接，依赖不可用时返回非 2xx。
- `GET /api/v1/system/health`：返回 API、数据库和 Worker 的聚合状态，供 Web 使用。

所有响应均携带或回显 Correlation ID。错误响应使用稳定 JSON 包络，至少包含机器可读错误码、用户可读消息和 Correlation ID；内部堆栈、连接串和密钥不得返回给调用者。

### 4.3 Worker

Worker 与 API 共用配置、数据库和可观测性平台包，但拥有独立组合入口和生命周期。它不消费提醒任务，只完成数据库连接、运行状态登记、存活/就绪探针和优雅停机。

Worker 在仅供容器内部访问的独立健康端口提供 `GET /health/live` 和 `GET /health/ready`；该端口不由反向代理或 Web 对外暴露。存活探针只检查进程事件循环，就绪探针检查必要配置、数据库连接和心跳循环状态。Compose 与未来编排平台使用这两个探针，Web 聚合状态不直接访问 Worker，而是通过 API 读取心跳租约。

Worker 状态通过带租约的数据库心跳发布。状态记录包含实例标识、启动时间、最近心跳时间和进程版本；API 只在心跳未超过允许时限时把 Worker 判定为健康。进程正常退出时尽力注销，异常退出则依赖租约自然过期，因此不会永久显示假健康。

### 4.4 PostgreSQL 与迁移

PostgreSQL 保存当前迭代所需的 Schema 版本和 Worker 心跳。迁移文件按不可变、只追加的版本顺序保存于 `deploy/migrations`。

`backend/cmd/migrate` 是唯一迁移入口。Compose 必须在 PostgreSQL 健康后运行迁移，迁移成功退出后才启动 API 与 Worker。迁移失败会阻止依赖服务启动。API 和 Worker 只检查 Schema 兼容性，不执行建表或升级。

## 5. 启动与数据流

标准本地启动流程：

```text
make dev
-> docker compose up --build
-> PostgreSQL healthcheck 通过
-> migrate 执行全部待执行迁移并成功退出
-> API 与 Worker 启动
-> API /health/ready 通过
-> Web 启动并展示聚合健康状态
```

Web 聚合请求的数据流：

```text
Browser requests status page
-> Next.js server requests API system health with a short timeout
-> API checks its own state and PostgreSQL
-> API reads the latest Worker heartbeat lease
-> API returns deterministic component statuses
-> Web maps the result to healthy/degraded/unavailable UI
```

健康检查必须快速、只读且有超时，不能触发迁移、重试风暴或任何业务副作用。

## 6. 配置、日志与生命周期

- 配置来自环境变量；仓库提交不含密钥的 `.env.example`，实际 `.env` 被忽略。
- 配置在进程启动时一次性解析和校验；缺失必需项时进程以明确错误退出。
- 日志输出结构化 JSON，字段至少包括时间、级别、服务、版本、消息和 Correlation ID；敏感值默认不记录。
- 入站请求若带合法 Correlation ID 则透传，否则生成新值；后台 Worker 每次心跳和生命周期事件也带关联标识。
- API 与 Worker 响应终止信号：先停止接收新工作，再等待受控宽限期，最后关闭数据库和 HTTP 资源。
- 健康汇总使用短超时和确定性状态映射。单个依赖失败不导致 Web 页面崩溃，但会使相应就绪检查失败。

ITER-0001 只提供 OpenTelemetry 可接入的边界和统一字段，不要求部署 Collector、指标数据库或追踪后端。

## 7. 架构政策

Go 依赖方向为：

```text
inbound adapter -> application -> domain
application -> port <- outbound adapter
cmd -> concrete adapters
```

自动化架构测试至少验证：

1. `domain` 只能依赖 Go 标准库和本模块领域代码。
2. `application` 不得导入 HTTP、PostgreSQL、River、模型或供应商实现。
3. 一个业务上下文不得导入另一个上下文的 `domain`、`adapters` 或数据库实现。
4. `platform` 不得导入任何业务模块。
5. `cmd` 是了解具体适配器和完成依赖装配的位置。
6. Web 的 feature 代码不能反向依赖具体部署配置，服务端内部地址不能进入浏览器 bundle。

根目录和需要独立约束的主要目录提供 `AGENTS.md`，记录职责、依赖方向、允许修改范围和必跑验证命令。架构边界必须由测试和 CI 执行，不能只写成提示文本。

## 8. 开发命令与 CI

根 `Makefile` 提供稳定命令：

```text
make dev                构建并启动本地完整栈
make down               停止本地栈并保留数据库卷
make format             格式化 Go、TypeScript 和仓库配置
make format-check       只读检查格式，不修改工作区
make lint               执行 Go 与 TypeScript 静态检查
make architecture-test  验证依赖方向和目录政策
make test               执行单元与组件测试
make migration-test     在隔离数据库上从空库执行迁移验证
make build              构建 Web、API、Worker 和 migrate
make smoke-test         验证运行栈和健康页面
make verify             执行提交前全部只读、确定性门禁
```

`make down` 不删除卷；清理持久数据必须使用名称明确的独立命令，并要求显式确认，避免日常命令误删开发数据。

`make verify` 调用 `format-check` 而不是 `format`，验证过程不得改写源码。CI 依次执行：

```text
format check
-> lint / static analysis
-> architecture tests
-> unit and component tests
-> migration tests
-> production builds
-> Compose smoke test
```

Go 检查包含格式化、`go vet`、普通测试和竞态测试。Web 使用 TypeScript 严格模式、ESLint 和组件测试。容器冒烟测试必须有总超时，并在失败时输出服务状态和经脱敏的近期日志。

## 9. 测试设计

### 9.1 单元与组件测试

- 配置解析：缺失、非法和有效配置。
- Correlation ID：生成、合法透传、非法值替换和响应回显。
- 健康状态映射：健康、超时、数据库失败和 Worker 心跳过期。
- Worker 租约判定：有效、边界和过期；测试使用注入 Clock，不真实等待。
- Web 页面：所有组件健康、部分降级、API 不可用三类状态。

### 9.2 架构测试

架构测试包含允许样例和故意违规的测试夹具，证明规则既不会误报合法依赖，也确实能阻止反向依赖与跨上下文内部导入。

### 9.3 迁移测试

- 从空 PostgreSQL 执行全部迁移成功。
- 第二次执行无待处理迁移且不改变 Schema。
- API 与 Worker 在 Schema 不兼容时拒绝就绪，但不自动迁移。

### 9.4 冒烟测试

在干净 Compose 项目中验证：

1. migrate 成功退出。
2. PostgreSQL、API、Worker 和 Web 在限定时间内就绪。
3. API 存活和就绪探针返回预期状态。
4. Web 状态页可访问并显示全部组件健康。
5. 暂停 Worker 后，页面在租约到期后显示降级，其他服务仍可响应。
6. 终止栈后没有遗留测试容器；数据库卷按调用命令的语义保留或清理。

## 10. 迭代工件与修改边界

创建 `docs/iterations/ITER-0001`，包含：

- `brief.md`：迭代目的、范围和验收条件。
- `spec.md`：由本设计细化出的可执行规格。
- `plan.md`：按 TDD 小切片拆分的实施步骤。
- `progress.md`：实施状态和验证证据。
- `decisions.md`：仅记录本迭代新增且需要追溯的决策。
- `test-matrix.md`：需求到测试的映射。
- `handoff.md`：干净上下文可恢复的当前状态。

`regression-report.md` 只在实现完成后由独立干净上下文回归生成。任何对总体架构、公开契约、数据库迁移、根构建配置、CI 或 `AGENTS.md` 的修改都属于上位设计定义的黄色区，必须在计划中明确列出并由高级模型处理。

## 11. 验收标准

ITER-0001 在以下条件全部满足时完成：

1. 新开发者按仓库说明可用 `make dev` 启动完整栈，无需手工建表。
2. Web 状态页显示 Web、API、Worker 和 PostgreSQL；依赖失败时页面可降级呈现。
3. API、Worker 与迁移进程职责分离，应用启动不会隐式修改 Schema。
4. Worker 异常退出后，健康状态会在确定的租约时间内转为不可用。
5. `make verify` 在干净检出中通过，且覆盖格式、静态检查、架构、测试、迁移和构建。
6. Compose 冒烟测试在限定时间内通过，并能证明暂停 Worker 后的降级行为。
7. 架构测试能拒绝至少一例反向层依赖和一例跨上下文内部导入。
8. 仓库中没有真实密钥，日志和错误响应不泄漏数据库连接串或环境变量值。
9. `docs/iterations/ITER-0001` 的计划、测试矩阵、进度与交接证据足以让新 Agent 在不读取实施对话的情况下继续工作。
10. 独立干净上下文回归 Agent 根据本规格和提交差异执行回归并给出通过报告。

## 12. 风险与控制

- **骨架过度设计**：只创建健康链路实际需要的代码；业务模块不预建空实现。
- **健康检查产生耦合**：健康 DTO 属于系统交付接口，不放入任何业务上下文；业务模块不依赖它。
- **Worker 假健康**：使用带 Clock 的心跳租约和过期判定，不依赖正常退出事件。
- **Compose 与 CI 漂移**：本地和 CI 都调用根 Make 入口；CI 不复制另一套脚本逻辑。
- **迁移竞争**：只有一次性 migrate 进程执行迁移，API 和 Worker 只验证兼容性。
- **工具链漂移**：语言、包管理器、依赖和容器基础镜像版本在仓库中锁定，升级必须显式评审。
- **冒烟测试不稳定**：所有等待基于健康条件和总超时，不使用固定长时间睡眠。
