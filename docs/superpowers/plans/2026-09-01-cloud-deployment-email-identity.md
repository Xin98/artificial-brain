# 云上部署与邮箱身份接入实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Artificial Brain 以 private 形态部署到阿里云 ECS（Docker Compose + Docker PostgreSQL），登录支持邮箱/手机号双标识（邮箱经个人 SMTP 生产可用，手机号留缝），并接入百炼 qwen-max。

**Architecture:** 沿用五服务 compose 栈与六边形模块边界；Identity 增加 `LoginIdentifier` 域值对象与 `smtpoutbox` 出站适配器，装配按 `APP_ENV` 在 fakeoutbox/生产路由间切换；提醒短信适配器新增 `disabled` 枚举；大模型为纯配置接入。设计依据：`docs/superpowers/specs/2026-09-01-cloud-deployment-email-identity-design.md`。

**Tech Stack:** Go 1.26.5（net/smtp 标准库，零新依赖）、Next.js / pnpm 11.19.0、PostgreSQL 迁移（tern）、Docker Compose、shell 冒烟测试。

## Global Constraints

- 工具链锁定：Go 1.26.5、Node 24.18.0、pnpm 11.19.0（`make toolchain-check` 验证）；改代码前先跑它。
- 迁移 append-only：001–008 只读；本次只新增 `009`；schema 版本门禁 8 → 9（`backend/internal/platform/database/schema.go`、`tests/smoke/migration_test.sh`）。
- 依赖方向：inbound adapter → application → domain；platform 不得 import 业务模块——config 里的邮箱校验**复制**正则（与既有 `e164PhonePattern` 同样的刻意复制，ITER-0004 假设 A1）。
- 错误包保持 `{code, message, correlationId}`；**无效输入沿用单一 422 `validation_error` 码**（代码库既有约定），只新增两个错误码：`sms_unavailable`（503）、`verification_send_failed`（502）。
- CI 永不触碰真实供应商（红线）：所有新冒烟块用开发模式 + dev inbox；生产验收在 ECS 人工执行。
- 凭证只进 gitignored `.env`；模板与文档只留占位符。
- 提交仪式（每个提交都执行，来自用户全局指令）：
  1. `git diff --cached --numstat` 统计；测试文件（`*_test.go`、`*.test.tsx`、`tests/` 下）计 UT，其余计 Feature；
  2. 提交前写标记文件：`echo '{"tool":"claude-code","model":"fable-5","files":[<本次修改文件列表>]}' > "$(git rev-parse --git-dir)/.cr-ai-session"`；
  3. 提交信息尾部依次为：`Co-Authored-By: Claude Code <noreply@anthropic.com>`、`AI-Model: fable-5`、`AI-Contributed/Feature: <ai>/<total>`、`AI-Contributed/UT: <ai>/<total>`（UT 行必须最后；无测试写 `0/0`）。
- 每个任务提交前跑 `make format`（整理 Go/Web/仓库文件格式）。
- 分支：`feat/260901_cloud_deploy`（已检出）。

## 任务总览

| # | 任务 | 主要产物 |
| --- | --- | --- |
| 1 | 迁移 009 + schema 版本 | `009_email_identity.sql`、版本门禁 9 |
| 2 | config 新变量 | `PRIVATE_ADMIN_EMAIL`、`SMTP_*`、`disabled` 枚举 |
| 3 | Identity 域 | `LoginIdentifier`、`User.Email`、新哨兵错误 |
| 4 | Identity 存储 | users/challenges email 列与查询 |
| 5 | Identity 命令 | 双标识 request/verify/provision |
| 6 | smtpoutbox 适配器 | 生产验证码邮件投递 |
| 7 | HTTP + 契约 | 双标识请求体、503/502 映射、OpenAPI |
| 8 | API 装配 | 生产投递路由、通道过滤、compose 变量 |
| 9 | Worker 装配 | `disabled` 队列裁剪、失败关闭通知器 |
| 10 | Web 登录页 | 单输入框双标识 |
| 11 | 冒烟测试 | 邮箱登录块、私域邮箱管理员演练 |
| 12 | 部署工件 | env 模板、runbook、README |
| 13 | 全部门禁 | verify / migration-test / smoke-test |

---

### Task 1: 迁移 009 与 schema 版本

**Files:**
- Create: `deploy/migrations/009_email_identity.sql`
- Modify: `backend/internal/platform/database/schema.go:11`
- Modify: `tests/smoke/migration_test.sh:140-141`

**Interfaces:**
- Produces: `identity.users.email`（唯一、可空）、`identity.login_challenges.email`；`database.CurrentSchemaVersion = 9`

- [ ] **Step 1: 写迁移文件**

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

---- create above / drop below ----

drop index if exists identity.login_challenges_email_created_at_idx;
alter table identity.login_challenges drop constraint if exists login_challenges_one_identifier;
alter table identity.login_challenges drop column if exists email;
alter table identity.users drop constraint if exists users_identifier_present;
alter table identity.users drop constraint if exists users_email_unique;
alter table identity.users drop column if exists email;
alter table identity.users alter column phone set not null;
```

注意：存量数据 phone 全部非空，新 check 约束对存量行天然成立；Postgres 唯一索引对多个 NULL 不冲突。

- [ ] **Step 2: 提升 schema 版本常量**

`backend/internal/platform/database/schema.go`：

```go
const CurrentSchemaVersion int32 = 9
```

- [ ] **Step 3: 更新迁移测试门禁**

`tests/smoke/migration_test.sh` 约第 140–141 行：

```sh
[ "$schema_version" = 9 ] || {
	printf 'migration test: schema version is %s, want 9\n' "$schema_version" >&2
```

（原来是 `8` / `want 8`。）

- [ ] **Step 4: 跑迁移测试验证**

Run: `make migration-test`
Expected: PASS（空库跑到版本 9；重复跑 migrate 幂等）。若当前环境没有 Docker，记录到任务末尾统一在 Task 13 跑。

- [ ] **Step 5: 提交**

```bash
git add deploy/migrations/009_email_identity.sql backend/internal/platform/database/schema.go tests/smoke/migration_test.sh
# 按 Global Constraints 写标记文件并提交
git commit -m "feat(identity): migration 009 adds email identifiers and bumps schema to v9"
```

---

### Task 2: config 新变量（管理员邮箱、SMTP、disabled 枚举）

**Files:**
- Modify: `backend/internal/platform/config/config.go`
- Test: `backend/internal/platform/config/config_cloud_deploy_test.go`（新建）

**Interfaces:**
- Produces: `Config.PrivateAdminEmail string`；`Config.SmtpHost/SmtpPort/SmtpUsername/SmtpPassword/SmtpFrom/SmtpTimeout`；`config.ReminderSmsAdapterDisabled = "disabled"`
- Consumes: Task 5/7/8/9 读取这些字段

- [ ] **Step 1: 写失败测试**

新建 `backend/internal/platform/config/config_cloud_deploy_test.go`：

```go
package config

import (
	"strings"
	"testing"
)

// cloudDeployEnv builds a production private-mode environment that satisfies
// every fail-closed rule; individual tests override one variable at a time.
func cloudDeployEnv(overrides map[string]string) LookupEnv {
	base := map[string]string{
		"DATABASE_URL":            "postgresql://user:secret@localhost:5432/db",
		"APP_ENV":                 "production",
		"DEPLOYMENT_MODE":         "private",
		"PRIVATE_ADMIN_EMAIL":     "admin@example.com",
		"REMINDER_RECEIPT_SECRET": "receipt-secret",
		"REMINDER_EMAIL_ADAPTER":  "smtp",
		"REMINDER_SMS_ADAPTER":    "disabled",
		"REMINDER_SMTP_HOST":      "smtp.example.com",
		"REMINDER_SMTP_PORT":      "465",
		"REMINDER_SMTP_FROM":      "noreply@example.com",
		"SMTP_HOST":               "smtp.example.com",
		"SMTP_PORT":               "465",
		"SMTP_FROM":               "noreply@example.com",
	}
	for key, value := range overrides {
		base[key] = value
	}
	return func(key string) (string, bool) {
		value, ok := base[key]
		return value, ok
	}
}

func TestLoadProductionEmailAdmin(t *testing.T) {
	cfg, err := Load(RoleAPI, cloudDeployEnv(nil))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if cfg.PrivateAdminEmail != "admin@example.com" {
		t.Fatalf("PrivateAdminEmail = %q", cfg.PrivateAdminEmail)
	}
	if cfg.ReminderSmsAdapter != ReminderSmsAdapterDisabled {
		t.Fatalf("ReminderSmsAdapter = %q", cfg.ReminderSmsAdapter)
	}
	if cfg.SmtpHost != "smtp.example.com" || cfg.SmtpPort != 465 || cfg.SmtpFrom != "noreply@example.com" {
		t.Fatalf("SMTP = %q %d %q", cfg.SmtpHost, cfg.SmtpPort, cfg.SmtpFrom)
	}
}

func TestLoadPrivateRequiresSomeAdminIdentifier(t *testing.T) {
	_, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"PRIVATE_ADMIN_EMAIL": "",
	}))
	if err == nil || !strings.Contains(err.Error(), "PRIVATE_ADMIN") {
		t.Fatalf("Load = %v, want PRIVATE_ADMIN error", err)
	}
}

func TestLoadCloudRejectsAdminIdentifiers(t *testing.T) {
	_, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"DEPLOYMENT_MODE":     "cloud",
		"PRIVATE_ADMIN_PHONE": "",
	}))
	if err == nil || !strings.Contains(err.Error(), "DEPLOYMENT_MODE") {
		t.Fatalf("Load = %v, want DEPLOYMENT_MODE error", err)
	}
}

func TestLoadProductionRequiresIdentitySmtp(t *testing.T) {
	_, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"SMTP_HOST": "",
	}))
	if err == nil || !strings.Contains(err.Error(), "SMTP_HOST") {
		t.Fatalf("Load = %v, want SMTP_HOST error", err)
	}
}

func TestLoadSmtpUsernameRequiresPassword(t *testing.T) {
	_, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"SMTP_USERNAME": "noreply@example.com",
	}))
	if err == nil || !strings.Contains(err.Error(), "SMTP_PASSWORD") {
		t.Fatalf("Load = %v, want SMTP_PASSWORD error", err)
	}
}

func TestLoadInvalidAdminEmail(t *testing.T) {
	_, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"PRIVATE_ADMIN_EMAIL": "not-an-email",
	}))
	if err == nil || !strings.Contains(err.Error(), "PRIVATE_ADMIN_EMAIL") {
		t.Fatalf("Load = %v, want PRIVATE_ADMIN_EMAIL error", err)
	}
}

func TestLoadWorkerRoleSkipsIdentitySmtp(t *testing.T) {
	env := cloudDeployEnv(map[string]string{
		"SMTP_HOST": "",
		"SMTP_PORT": "",
		"SMTP_FROM": "",
	})
	cfg, err := Load(RoleWorker, env)
	if err != nil {
		t.Fatalf("Load(RoleWorker) = %v", err)
	}
	if cfg.Role != RoleWorker {
		t.Fatalf("Role = %q", cfg.Role)
	}
}

func TestLoadPrivatePhoneOnlyAdminStillWorks(t *testing.T) {
	cfg, err := Load(RoleAPI, cloudDeployEnv(map[string]string{
		"PRIVATE_ADMIN_EMAIL": "",
		"PRIVATE_ADMIN_PHONE": "+8613800137999",
	}))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if cfg.PrivateAdminPhone != "+8613800137999" {
		t.Fatalf("PrivateAdminPhone = %q", cfg.PrivateAdminPhone)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./backend/internal/platform/config/ -run 'CloudDeploy|AdminIdentifier|AdminIdentifiers|IdentitySmtp|SmtpUsername|AdminEmail|WorkerRoleSkips|PhoneOnlyAdmin' -v`
Expected: FAIL（`ReminderSmsAdapterDisabled`、`PrivateAdminEmail`、`SmtpHost` 等未定义，编译错误）

- [ ] **Step 3: 实现 config 变更**

`backend/internal/platform/config/config.go`，按下列位置修改：

3a. 常量（与现有常量块合并）：

```go
	defaultIdentitySmtpTimeout = 10 * time.Second
```

```go
	ReminderSmsAdapterFake     = "fake"
	ReminderSmsAdapterAliyun   = "aliyun"
	ReminderSmsAdapterDisabled = "disabled"
```

3b. `e164PhonePattern` 旁新增（注释风格照抄）：

```go
// adminEmailPattern duplicates the identity module's email validation
// deliberately: the platform package must not import business modules
// (ITER-0004 assumption A1).
var adminEmailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
```

3c. `Config` 结构体新增字段（`PrivateAdminPhone` 旁与结构体末尾）：

```go
	PrivateAdminEmail string

	SmtpHost     string
	SmtpPort     int
	SmtpUsername string
	SmtpPassword string
	SmtpFrom     string
	SmtpTimeout  time.Duration
```

3d. 短信适配器 switch 增加 disabled：

```go
	switch reminderSmsAdapter {
	case ReminderSmsAdapterFake, ReminderSmsAdapterAliyun, ReminderSmsAdapterDisabled:
	default:
		return Config{}, fmt.Errorf("config: invalid REMINDER_SMS_ADAPTER")
	}
```

3e. 在 REMINDER_SMTP 校验块之后、REMINDER_ALIYUN 解析之前，插入 identity SMTP 解析：

```go
	smtpHost := valueOrDefault(lookup, "SMTP_HOST", "")
	smtpPort, err := intValue(lookup, "SMTP_PORT", 0)
	if err != nil {
		return Config{}, err
	}
	smtpUsername := valueOrDefault(lookup, "SMTP_USERNAME", "")
	smtpPassword := valueOrDefault(lookup, "SMTP_PASSWORD", "")
	smtpFrom := valueOrDefault(lookup, "SMTP_FROM", "")
	smtpTimeout, err := duration(lookup, "SMTP_TIMEOUT", defaultIdentitySmtpTimeout)
	if err != nil {
		return Config{}, err
	}
	if smtpUsername != "" && smtpPassword == "" {
		return Config{}, fmt.Errorf("config: SMTP_USERNAME requires SMTP_PASSWORD")
	}
	if appEnv == AppEnvProduction && role == RoleAPI {
		if smtpHost == "" {
			return Config{}, fmt.Errorf("config: missing SMTP_HOST")
		}
		if smtpPort < 1 {
			return Config{}, fmt.Errorf("config: invalid SMTP_PORT")
		}
		if smtpFrom == "" {
			return Config{}, fmt.Errorf("config: missing SMTP_FROM")
		}
	}
```

3f. 替换私域管理员校验段（原 `privateAdminPhone := ...` 到 `missing PRIVATE_ADMIN_PHONE` 为止）：

```go
	privateAdminPhone := valueOrDefault(lookup, "PRIVATE_ADMIN_PHONE", "")
	privateAdminEmail := valueOrDefault(lookup, "PRIVATE_ADMIN_EMAIL", "")
	if deploymentMode == DeploymentModeCloud && (privateAdminPhone != "" || privateAdminEmail != "") {
		return Config{}, fmt.Errorf("config: PRIVATE_ADMIN_PHONE and PRIVATE_ADMIN_EMAIL require a private DEPLOYMENT_MODE")
	}
	if privateAdminPhone != "" && !e164PhonePattern.MatchString(privateAdminPhone) {
		return Config{}, fmt.Errorf("config: invalid PRIVATE_ADMIN_PHONE")
	}
	// The E.164 pattern allows an optional '+', but the admin phone gates
	// provisioning and login by exact string comparison: a '+'-less value
	// would provision the user under one string while the admin logs in with
	// the canonical '+' form, a permanent lockout. Require the canonical
	// form so both sides always compare identical strings.
	if privateAdminPhone != "" && !strings.HasPrefix(privateAdminPhone, "+") {
		return Config{}, fmt.Errorf("config: PRIVATE_ADMIN_PHONE must start with '+' (canonical E.164 form)")
	}
	if privateAdminEmail != "" && !adminEmailPattern.MatchString(privateAdminEmail) {
		return Config{}, fmt.Errorf("config: invalid PRIVATE_ADMIN_EMAIL")
	}
	if deploymentMode == DeploymentModePrivate && role == RoleAPI && privateAdminPhone == "" && privateAdminEmail == "" {
		return Config{}, fmt.Errorf("config: private DEPLOYMENT_MODE requires PRIVATE_ADMIN_PHONE or PRIVATE_ADMIN_EMAIL")
	}
```

3g. `Load` 返回的 `Config{...}` 字面量补上新字段：

```go
		PrivateAdminEmail: privateAdminEmail,
		SmtpHost:          smtpHost,
		SmtpPort:          smtpPort,
		SmtpUsername:      smtpUsername,
		SmtpPassword:      smtpPassword,
		SmtpFrom:          smtpFrom,
		SmtpTimeout:       smtpTimeout,
```

- [ ] **Step 4: 跑全部 config 测试**

Run: `go test ./backend/internal/platform/config/ -v`
Expected: PASS（新测试 + 既有 iter0002/iter0004 测试全过；若有既有用例因错误文案变化失败，按新文案修正既有用例的断言——不要放宽新规则）

- [ ] **Step 5: 提交**

```bash
git add backend/internal/platform/config/config.go backend/internal/platform/config/config_cloud_deploy_test.go
git commit -m "feat(config): add admin email, identity SMTP, and disabled sms adapter"
```

---

### Task 3: Identity 域——LoginIdentifier 与新哨兵错误

**Files:**
- Create: `backend/internal/modules/identity/domain/identifier.go`
- Create: `backend/internal/modules/identity/domain/identifier_test.go`
- Modify: `backend/internal/modules/identity/domain/errors.go`
- Modify: `backend/internal/modules/identity/domain/user.go`
- Modify: `backend/internal/modules/identity/domain/challenge.go`

**Interfaces:**
- Produces: `domain.LoginIdentifier{Phone, Email string}`、`NewLoginIdentifier(phone, email) (LoginIdentifier, error)`、`(LoginIdentifier).Value() string`、`(LoginIdentifier).Channel() string`；`ErrIdentifierInvalid`、`ErrSmsUnavailable`、`ErrCodeDeliveryFailed`；`User.Email`、`LoginChallenge.Email`
- Consumes: Task 4/5/6/7

- [ ] **Step 1: 写失败测试**

`backend/internal/modules/identity/domain/identifier_test.go`：

```go
package domain

import "testing"

func TestNewLoginIdentifier(t *testing.T) {
	cases := []struct {
		name    string
		phone   string
		email   string
		want    LoginIdentifier
		wantErr error
	}{
		{"phone only", "+8613800138000", "", LoginIdentifier{Phone: "+8613800138000"}, nil},
		{"email only", "", "admin@example.com", LoginIdentifier{Email: "admin@example.com"}, nil},
		{"both present", "+8613800138000", "admin@example.com", LoginIdentifier{}, ErrIdentifierInvalid},
		{"neither present", "", "", LoginIdentifier{}, ErrIdentifierInvalid},
		{"invalid phone", "abc", "", LoginIdentifier{}, ErrInvalidPhone},
		{"invalid email", "", "not-an-email", LoginIdentifier{}, ErrInvalidEmail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewLoginIdentifier(tc.phone, tc.email)
			if err != tc.wantErr {
				t.Fatalf("NewLoginIdentifier(%q, %q) error = %v, want %v", tc.phone, tc.email, err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("NewLoginIdentifier(%q, %q) = %#v, want %#v", tc.phone, tc.email, got, tc.want)
			}
		})
	}
}

func TestLoginIdentifierValueAndChannel(t *testing.T) {
	phone := LoginIdentifier{Phone: "+8613800138000"}
	if phone.Value() != "+8613800138000" || phone.Channel() != "sms" {
		t.Fatalf("phone identifier = %q/%q", phone.Value(), phone.Channel())
	}
	email := LoginIdentifier{Email: "admin@example.com"}
	if email.Value() != "admin@example.com" || email.Channel() != "email" {
		t.Fatalf("email identifier = %q/%q", email.Value(), email.Channel())
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./backend/internal/modules/identity/domain/ -run 'LoginIdentifier' -v`
Expected: FAIL（编译错误：未定义）

- [ ] **Step 3: 实现域变更**

`errors.go` 追加：

```go
	ErrIdentifierInvalid    = errors.New("identity: exactly one of phone or email is required")
	ErrSmsUnavailable       = errors.New("identity: sms delivery is unavailable")
	ErrCodeDeliveryFailed   = errors.New("identity: verification code delivery failed")
```

`identifier.go`：

```go
package domain

// LoginIdentifier carries exactly one of phone or email as a login identity.
// Phone holds the validated E.164 form, email the validated address; the
// other field stays empty.
type LoginIdentifier struct {
	Phone string
	Email string
}

// NewLoginIdentifier validates that exactly one identifier form is present
// and well-formed.
func NewLoginIdentifier(phone, email string) (LoginIdentifier, error) {
	if (phone != "") == (email != "") {
		return LoginIdentifier{}, ErrIdentifierInvalid
	}
	if phone != "" {
		p, err := NewPhone(phone)
		if err != nil {
			return LoginIdentifier{}, err
		}
		return LoginIdentifier{Phone: p.String()}, nil
	}
	e, err := NewEmail(email)
	if err != nil {
		return LoginIdentifier{}, err
	}
	return LoginIdentifier{Email: e.String()}, nil
}

// Value returns the single non-empty identifier string.
func (i LoginIdentifier) Value() string {
	if i.Phone != "" {
		return i.Phone
	}
	return i.Email
}

// Channel returns the delivery channel for this identifier's verification
// codes.
func (i LoginIdentifier) Channel() string {
	if i.Phone != "" {
		return "sms"
	}
	return "email"
}
```

`user.go`：

```go
// User is a person who logs in with a phone number and/or an email address
// and owns one personal workspace. Absent identifiers are empty strings.
type User struct {
	ID          string
	WorkspaceID string
	Phone       string
	Email       string
	CreatedAt   time.Time
}
```

`challenge.go` 的 `LoginChallenge` 在 `Phone` 之后加：

```go
	Email      string
```

（注释同步：challenge 现在验证 phone 或 email。）

- [ ] **Step 4: 跑域测试**

Run: `go test ./backend/internal/modules/identity/domain/ -v`
Expected: PASS（若 `domain_test.go` 既有构造因新字段失败，按字段名补齐）

- [ ] **Step 5: 提交**

```bash
git add backend/internal/modules/identity/domain/
git commit -m "feat(identity): add LoginIdentifier domain value and email-aware entities"
```

---

### Task 4: Identity Postgres 存储——email 列与查询

**Files:**
- Modify: `backend/internal/modules/identity/application/ports/ports.go`
- Modify: `backend/internal/modules/identity/adapters/outbound/postgres/users.go`
- Modify: `backend/internal/modules/identity/adapters/outbound/postgres/challenges.go`
- Test: `backend/internal/modules/identity/adapters/outbound/postgres/integration_test.go`（扩展）

**Interfaces:**
- Consumes: Task 1 的 009 迁移；Task 3 的 `User.Email`、`LoginChallenge.Email`
- Produces: `UserStore.ByEmail(ctx, email) (domain.User, error)`；`ChallengeStore.ActiveByEmail(ctx, email)`、`ChallengeStore.CountByEmailSince(ctx, email, since)`；Save 写入 email 列（空串渲染为 SQL NULL）

- [ ] **Step 1: 扩展端口**

`ports.go`：

```go
// UserStore persists users.
type UserStore interface {
	Save(ctx context.Context, user domain.User) error
	ByPhone(ctx context.Context, phone string) (domain.User, error)
	ByEmail(ctx context.Context, email string) (domain.User, error)
}

// ChallengeStore persists login challenges.
type ChallengeStore interface {
	Save(ctx context.Context, challenge domain.LoginChallenge) error
	Update(ctx context.Context, challenge domain.LoginChallenge) error
	ActiveByPhone(ctx context.Context, phone string) (domain.LoginChallenge, error)
	ActiveByEmail(ctx context.Context, email string) (domain.LoginChallenge, error)
	CountByPhoneSince(ctx context.Context, phone string, since time.Time) (int, error)
	CountByEmailSince(ctx context.Context, email string, since time.Time) (int, error)
}
```

- [ ] **Step 2: users.go 适配可空标识**

`users.go` 全量替换为：

```go
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// UserStore persists users in PostgreSQL.
type UserStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) *UserStore { return &UserStore{pool: pool} }

func (s *UserStore) Save(ctx context.Context, user domain.User) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into identity.users (id, workspace_id, phone, email, created_at)
		values ($1, $2, $3, $4, $5)
	`, user.ID, user.WorkspaceID, nullIfEmpty(user.Phone), nullIfEmpty(user.Email), user.CreatedAt)
	return err
}

func (s *UserStore) ByPhone(ctx context.Context, phone string) (domain.User, error) {
	return s.byIdentifier(ctx, "where phone = $1", phone)
}

func (s *UserStore) ByEmail(ctx context.Context, email string) (domain.User, error) {
	return s.byIdentifier(ctx, "where email = $1", email)
}

func (s *UserStore) byIdentifier(ctx context.Context, where, value string) (domain.User, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var user domain.User
	var phone, email *string
	err := exec.QueryRow(ctx, `
		select id, workspace_id, phone, email, created_at
		from identity.users
		`+where, value).Scan(&user.ID, &user.WorkspaceID, &phone, &email, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	user.Phone = stringValue(phone)
	user.Email = stringValue(email)
	return user, nil
}

// nullIfEmpty renders an absent identifier as SQL NULL so unique constraints
// and the "at least one identifier" check behave correctly.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
```

- [ ] **Step 3: challenges.go 适配可空标识并新增邮箱查询**

`challenges.go` 全量替换为：

```go
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// ChallengeStore persists login challenges in PostgreSQL.
type ChallengeStore struct {
	pool *pgxpool.Pool
}

func NewChallengeStore(pool *pgxpool.Pool) *ChallengeStore { return &ChallengeStore{pool: pool} }

func (s *ChallengeStore) Save(ctx context.Context, challenge domain.LoginChallenge) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into identity.login_challenges
			(id, phone, email, code_hash, created_at, expires_at, consumed_at, attempts)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
	`, challenge.ID, nullIfEmpty(challenge.Phone), nullIfEmpty(challenge.Email), challenge.CodeHash,
		challenge.CreatedAt, challenge.ExpiresAt, challenge.ConsumedAt, challenge.Attempts)
	return err
}

func (s *ChallengeStore) Update(ctx context.Context, challenge domain.LoginChallenge) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		update identity.login_challenges
		set code_hash = $2, expires_at = $3, consumed_at = $4, attempts = $5
		where id = $1
	`, challenge.ID, challenge.CodeHash, challenge.ExpiresAt, challenge.ConsumedAt, challenge.Attempts)
	return err
}

// ActiveByPhone returns the most recent unconsumed challenge for the phone,
// falling back to the most recent challenge overall so callers can
// distinguish consumed and expired states.
func (s *ChallengeStore) ActiveByPhone(ctx context.Context, phone string) (domain.LoginChallenge, error) {
	return s.activeByIdentifier(ctx, "where phone = $1", phone)
}

// ActiveByEmail mirrors ActiveByPhone for email identifiers.
func (s *ChallengeStore) ActiveByEmail(ctx context.Context, email string) (domain.LoginChallenge, error) {
	return s.activeByIdentifier(ctx, "where email = $1", email)
}

func (s *ChallengeStore) activeByIdentifier(ctx context.Context, where, value string) (domain.LoginChallenge, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var challenge domain.LoginChallenge
	var phone, email *string
	err := exec.QueryRow(ctx, `
		select id, phone, email, code_hash, created_at, expires_at, consumed_at, attempts
		from identity.login_challenges
		`+where+`
		order by (consumed_at is null) desc, created_at desc
		limit 1
	`, value).Scan(
		&challenge.ID, &phone, &email, &challenge.CodeHash, &challenge.CreatedAt,
		&challenge.ExpiresAt, &challenge.ConsumedAt, &challenge.Attempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LoginChallenge{}, domain.ErrChallengeNotFound
	}
	if err != nil {
		return domain.LoginChallenge{}, err
	}
	challenge.Phone = stringValue(phone)
	challenge.Email = stringValue(email)
	return challenge, nil
}

func (s *ChallengeStore) CountByPhoneSince(ctx context.Context, phone string, since time.Time) (int, error) {
	return s.countByIdentifierSince(ctx, "where phone = $1 and created_at >= $2", phone, since)
}

func (s *ChallengeStore) CountByEmailSince(ctx context.Context, email string, since time.Time) (int, error) {
	return s.countByIdentifierSince(ctx, "where email = $1 and created_at >= $2", email, since)
}

func (s *ChallengeStore) countByIdentifierSince(ctx context.Context, where, value string, since time.Time) (int, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var count int
	if err := exec.QueryRow(ctx, `
		select count(*)
		from identity.login_challenges
		`+where, value, since).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
```

- [ ] **Step 4: 扩展集成测试**

先读 `integration_test.go` 现有的池夹具与用例风格，然后在同一文件中追加以下用例（沿用文件内既有的夹具名与跳过逻辑——`TEST_DATABASE_URL` 未设置时跳过）：

```go
func TestUserStoreEmailIdentifier(t *testing.T) {
	pool := testPool(t) // 使用本文件既有的池夹具；若名称不同，替换为实际名称
	ctx := context.Background()

	workspace := domain.PersonalWorkspace{ID: "ws-email", CreatedAt: time.Now().UTC()}
	if err := NewWorkspaceStore(pool).Save(ctx, workspace); err != nil {
		t.Fatalf("workspace save: %v", err)
	}
	user := domain.User{ID: "user-email", WorkspaceID: workspace.ID, Email: "admin@example.com", CreatedAt: time.Now().UTC()}
	store := NewUserStore(pool)
	if err := store.Save(ctx, user); err != nil {
		t.Fatalf("user save: %v", err)
	}

	got, err := store.ByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("ByEmail: %v", err)
	}
	if got.Email != "admin@example.com" || got.Phone != "" {
		t.Fatalf("ByEmail = %#v", got)
	}
	if _, err := store.ByPhone(ctx, "+8613800138000"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("ByPhone(missing) = %v, want ErrUserNotFound", err)
	}
}

func TestChallengeStoreEmailRoundTrip(t *testing.T) {
	pool := testPool(t) // 同上：替换为本文件实际夹具名
	ctx := context.Background()
	store := NewChallengeStore(pool)

	challenge := domain.LoginChallenge{
		ID:        "challenge-email",
		Email:     "admin@example.com",
		CodeHash:  domain.HashCode("123456"),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}
	if err := store.Save(ctx, challenge); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.ActiveByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("ActiveByEmail: %v", err)
	}
	if got.Email != "admin@example.com" || got.Phone != "" || !got.Matches(domain.HashCode("123456")) {
		t.Fatalf("ActiveByEmail = %#v", got)
	}

	count, err := store.CountByEmailSince(ctx, "admin@example.com", challenge.CreatedAt.Add(-time.Minute))
	if err != nil || count != 1 {
		t.Fatalf("CountByEmailSince = %d, %v", count, err)
	}
}
```

- [ ] **Step 5: 跑测试**

Run: `go test ./backend/internal/modules/identity/... -v 2>&1 | tail -40`
Expected: 域与存储编译通过；集成用例在设置 `TEST_DATABASE_URL` 时通过，未设置时跳过。此时 `command` 包会因接口变更编译失败——这是预期的，Task 5 修复。若希望本任务独立编译通过，可先跳到 Task 5 再一起提交；推荐做法：Task 4 与 Task 5 连续完成，各自提交前先确保 `go build ./backend/...` 通过。

- [ ] **Step 6: 提交**

```bash
git add backend/internal/modules/identity/application/ports/ports.go backend/internal/modules/identity/adapters/outbound/postgres/
git commit -m "feat(identity): persist email identifiers in users and challenges"
```

---

### Task 5: Identity 命令——双标识登录与 provisioning

**Files:**
- Modify: `backend/internal/modules/identity/application/command/login.go`
- Modify: `backend/internal/modules/identity/application/command/provision.go`
- Modify: `backend/internal/modules/identity/application/command/fakes_test.go`
- Modify: `backend/internal/modules/identity/application/command/login_test.go`
- Modify: `backend/internal/modules/identity/application/command/provision_test.go`

**Interfaces:**
- Consumes: Task 2 `Config.PrivateAdminEmail`（经装配传入）、Task 3 `LoginIdentifier`、Task 4 新端口
- Produces: `RequestLoginChallengeHandler.Handle(ctx, identifier domain.LoginIdentifier) error`（新签名，多 `PrivateAdminEmail` 字段）；`VerifyLoginChallengeHandler.Handle(ctx, identifier domain.LoginIdentifier, code string)`；`ProvisionAdminHandler.Handle(ctx, phone, email string)`

- [ ] **Step 1: 更新测试假对象**

`fakes_test.go` 的 `fakeChallengeStore` 追加（镜像既有 phone 实现的 email 版本）：

```go
func (s *fakeChallengeStore) ActiveByEmail(_ context.Context, email string) (domain.LoginChallenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latestUnconsumed *domain.LoginChallenge
	var latestAny *domain.LoginChallenge
	for i := range s.challenges {
		c := s.challenges[i]
		if c.Email != email {
			continue
		}
		if latestAny == nil || !c.CreatedAt.Before(latestAny.CreatedAt) {
			cc := c
			latestAny = &cc
		}
		if !c.IsConsumed() && (latestUnconsumed == nil || !c.CreatedAt.Before(latestUnconsumed.CreatedAt)) {
			cc := c
			latestUnconsumed = &cc
		}
	}
	if latestUnconsumed != nil {
		return *latestUnconsumed, nil
	}
	if latestAny != nil {
		return *latestAny, nil
	}
	return domain.LoginChallenge{}, domain.ErrChallengeNotFound
}

func (s *fakeChallengeStore) CountByEmailSince(_ context.Context, email string, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for i := range s.challenges {
		c := s.challenges[i]
		if c.Email == email && !c.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}
```

`fakeUserStore`（同文件，按现有结构）：保存时记录用户；新增：

```go
func (s *fakeUserStore) ByEmail(_ context.Context, email string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.users {
		if user.Email != "" && user.Email == email {
			return user, nil
		}
	}
	return domain.User{}, domain.ErrUserNotFound
}
```

（若现有 `fakeUserStore` 字段/方法名不同，照其结构镜像；`ByPhone` 同理保持按 `user.Phone` 精确匹配。）

- [ ] **Step 2: 写新的命令层失败测试**

在 `login_test.go` 追加（既有用例在 Step 3 后按新签名机械更新）：

```go
func TestRequestLoginChallengeEmail(t *testing.T) {
	challenges := newFakeChallengeStore()
	outbox := newFakeOutbox() // 沿用本包既有假 outbox 名
	handler := &RequestLoginChallengeHandler{
		Challenges: challenges, Outbox: outbox,
		NewCode: fixedCode("123456"), NewID: fixedID("challenge-1"), // 沿用既有测试辅助函数名
		Now: fixedNow, ChallengeTTL: 5 * time.Minute,
	}
	err := handler.Handle(context.Background(), domain.LoginIdentifier{Email: "admin@example.com"})
	if err != nil {
		t.Fatalf("Handle = %v", err)
	}
	if len(outbox.messages) != 1 {
		t.Fatalf("outbox messages = %d, want 1", len(outbox.messages))
	}
	message := outbox.messages[0]
	if message.Address != "admin@example.com" || message.Channel != "email" || message.Purpose != "login" {
		t.Fatalf("outbox message = %#v", message)
	}
}

func TestRequestLoginChallengePrivateEmailGate(t *testing.T) {
	handler := &RequestLoginChallengeHandler{
		Challenges: newFakeChallengeStore(), Outbox: newFakeOutbox(),
		NewCode: fixedCode("123456"), NewID: fixedID("challenge-1"),
		Now: fixedNow, ChallengeTTL: 5 * time.Minute,
		PrivateAdminEmail: "admin@example.com",
	}
	if err := handler.Handle(context.Background(), domain.LoginIdentifier{Email: "stranger@example.com"}); !errors.Is(err, domain.ErrRegistrationClosed) {
		t.Fatalf("stranger email = %v, want ErrRegistrationClosed", err)
	}
	if err := handler.Handle(context.Background(), domain.LoginIdentifier{Phone: "+8613800138000"}); !errors.Is(err, domain.ErrRegistrationClosed) {
		t.Fatalf("stranger phone = %v, want ErrRegistrationClosed", err)
	}
	if err := handler.Handle(context.Background(), domain.LoginIdentifier{Email: "admin@example.com"}); err != nil {
		t.Fatalf("admin email = %v", err)
	}
}

func TestVerifyLoginChallengeEmailFirstLogin(t *testing.T) {
	challenges := newFakeChallengeStore()
	users := newFakeUserStore()
	workspaces := newFakeWorkspaceStore()
	sessions := newFakeSessionStore()
	handler := &VerifyLoginChallengeHandler{
		Challenges: challenges, Users: users, Workspaces: workspaces, Sessions: sessions,
		NewID: sequentialIDs("ws-1", "user-1", "session-1"), NewToken: fixedToken("token-1"), // 沿用既有辅助名
		Now: fixedNow, SessionTTL: 24 * time.Hour,
	}
	request := &RequestLoginChallengeHandler{
		Challenges: challenges, Outbox: newFakeOutbox(),
		NewCode: fixedCode("123456"), NewID: fixedID("challenge-1"),
		Now: fixedNow, ChallengeTTL: 5 * time.Minute,
	}
	identifier := domain.LoginIdentifier{Email: "admin@example.com"}
	if err := request.Handle(context.Background(), identifier); err != nil {
		t.Fatalf("request: %v", err)
	}
	result, err := handler.Handle(context.Background(), identifier, "123456")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Principal.UserID == "" || result.Principal.WorkspaceID == "" {
		t.Fatalf("result = %#v", result)
	}
}
```

注意：`fixedCode`/`fixedID`/`fixedNow`/`newFakeOutbox` 等辅助名以 `fakes_test.go`/`login_test.go` 中实际存在的为准，读文件后对齐；断言不变。

- [ ] **Step 3: 重写 login.go 两个 Handler**

`login.go` 中 `RequestLoginChallengeHandler` 与 `VerifyLoginChallengeHandler` 全量替换为：

```go
// RequestLoginChallengeHandler creates a login challenge and sends its code
// via the outbound message port.
type RequestLoginChallengeHandler struct {
	Challenges   ports.ChallengeStore
	Outbox       ports.MessageOutbox
	NewCode      func() (string, error)
	NewID        func() string
	Now          func() time.Time
	ChallengeTTL time.Duration
	// PrivateAdminPhone and PrivateAdminEmail restrict login to the fixed
	// private-deployment admin identifiers; any other identifier is rejected
	// before any store or outbox interaction. Both empty keeps public-cloud
	// behavior.
	PrivateAdminPhone string
	PrivateAdminEmail string
}

func (h *RequestLoginChallengeHandler) Handle(ctx context.Context, identifier domain.LoginIdentifier) error {
	if h.privateBlocked(identifier) {
		return domain.ErrRegistrationClosed
	}
	now := h.Now()
	count, err := h.countRecent(ctx, identifier, now)
	if err != nil {
		return err
	}
	if count >= MaxChallengesPerPhonePerHour {
		return domain.ErrRateLimited
	}
	code, err := h.NewCode()
	if err != nil {
		return err
	}
	challenge := domain.LoginChallenge{
		ID:        h.NewID(),
		Phone:     identifier.Phone,
		Email:     identifier.Email,
		CodeHash:  domain.HashCode(code),
		CreatedAt: now,
		ExpiresAt: now.Add(h.ChallengeTTL),
	}
	if err := h.Challenges.Save(ctx, challenge); err != nil {
		return err
	}
	return h.Outbox.Write(ctx, ports.OutboxMessage{
		Address: identifier.Value(),
		Channel: identifier.Channel(),
		Purpose: "login",
		Code:    code,
	})
}

func (h *RequestLoginChallengeHandler) privateBlocked(identifier domain.LoginIdentifier) bool {
	if h.PrivateAdminPhone == "" && h.PrivateAdminEmail == "" {
		return false
	}
	if identifier.Phone != "" && identifier.Phone == h.PrivateAdminPhone {
		return false
	}
	if identifier.Email != "" && identifier.Email == h.PrivateAdminEmail {
		return false
	}
	return true
}

func (h *RequestLoginChallengeHandler) countRecent(ctx context.Context, identifier domain.LoginIdentifier, now time.Time) (int, error) {
	if identifier.Phone != "" {
		return h.Challenges.CountByPhoneSince(ctx, identifier.Phone, now.Add(-time.Hour))
	}
	return h.Challenges.CountByEmailSince(ctx, identifier.Email, now.Add(-time.Hour))
}

// VerifyLoginChallengeHandler validates a login code, registers the user and
// workspace on first login, and issues a session.
type VerifyLoginChallengeHandler struct {
	Challenges ports.ChallengeStore
	Users      ports.UserStore
	Workspaces ports.WorkspaceStore
	Sessions   ports.SessionStore
	NewID      func() string
	NewToken   func() (string, error)
	Now        func() time.Time
	SessionTTL time.Duration
	// PrivateAdminPhone and PrivateAdminEmail restrict login to the fixed
	// private-deployment admin identifiers. Both empty keeps public-cloud
	// behavior.
	PrivateAdminPhone string
	PrivateAdminEmail string
}

func (h *VerifyLoginChallengeHandler) Handle(ctx context.Context, identifier domain.LoginIdentifier, code string) (dto.VerifyLoginChallengeResult, error) {
	if h.privateBlocked(identifier) {
		return dto.VerifyLoginChallengeResult{}, domain.ErrRegistrationClosed
	}
	if _, err := domain.NewCode(code); err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}

	challenge, err := h.activeChallenge(ctx, identifier)
	if err != nil {
		return dto.VerifyLoginChallengeResult{}, domain.ErrChallengeNotFound
	}
	now := h.Now()
	if challenge.IsExpired(now) {
		return dto.VerifyLoginChallengeResult{}, domain.ErrChallengeExpired
	}
	if challenge.IsConsumed() {
		return dto.VerifyLoginChallengeResult{}, domain.ErrChallengeConsumed
	}
	if challenge.Attempts >= domain.MaxVerifyAttempts {
		return dto.VerifyLoginChallengeResult{}, domain.ErrTooManyAttempts
	}
	if !challenge.Matches(domain.HashCode(code)) {
		exhausted := challenge.RegisterFailedAttempt()
		if updateErr := h.Challenges.Update(ctx, challenge); updateErr != nil {
			return dto.VerifyLoginChallengeResult{}, updateErr
		}
		if exhausted {
			return dto.VerifyLoginChallengeResult{}, domain.ErrTooManyAttempts
		}
		return dto.VerifyLoginChallengeResult{}, domain.ErrInvalidCode
	}
	if err := challenge.Consume(now); err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}
	if err := h.Challenges.Update(ctx, challenge); err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}

	user, err := h.existingUser(ctx, identifier)
	if errors.Is(err, domain.ErrUserNotFound) {
		workspace := domain.PersonalWorkspace{ID: h.NewID(), CreatedAt: now}
		if err := h.Workspaces.Save(ctx, workspace); err != nil {
			return dto.VerifyLoginChallengeResult{}, err
		}
		user = domain.User{ID: h.NewID(), WorkspaceID: workspace.ID, Phone: identifier.Phone, Email: identifier.Email, CreatedAt: now}
		if err := h.Users.Save(ctx, user); err != nil {
			return dto.VerifyLoginChallengeResult{}, err
		}
	} else if err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}

	token, err := h.NewToken()
	if err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}
	session := domain.Session{
		ID:          h.NewID(),
		UserID:      user.ID,
		WorkspaceID: user.WorkspaceID,
		TokenHash:   domain.HashCode(token),
		CreatedAt:   now,
		ExpiresAt:   now.Add(h.SessionTTL),
	}
	if err := h.Sessions.Save(ctx, session); err != nil {
		return dto.VerifyLoginChallengeResult{}, err
	}
	return dto.VerifyLoginChallengeResult{
		Token: token,
		Principal: dto.Principal{
			UserID:      user.ID,
			WorkspaceID: user.WorkspaceID,
			SessionID:   session.ID,
		},
		ExpiresAt: session.ExpiresAt,
	}, nil
}

func (h *VerifyLoginChallengeHandler) privateBlocked(identifier domain.LoginIdentifier) bool {
	if h.PrivateAdminPhone == "" && h.PrivateAdminEmail == "" {
		return false
	}
	if identifier.Phone != "" && identifier.Phone == h.PrivateAdminPhone {
		return false
	}
	if identifier.Email != "" && identifier.Email == h.PrivateAdminEmail {
		return false
	}
	return true
}

func (h *VerifyLoginChallengeHandler) activeChallenge(ctx context.Context, identifier domain.LoginIdentifier) (domain.LoginChallenge, error) {
	if identifier.Phone != "" {
		return h.Challenges.ActiveByPhone(ctx, identifier.Phone)
	}
	return h.Challenges.ActiveByEmail(ctx, identifier.Email)
}

func (h *VerifyLoginChallengeHandler) existingUser(ctx context.Context, identifier domain.LoginIdentifier) (domain.User, error) {
	if identifier.Phone != "" {
		return h.Users.ByPhone(ctx, identifier.Phone)
	}
	return h.Users.ByEmail(ctx, identifier.Email)
}
```

（`MaxChallengesPerPhonePerHour` 常量名保留：它约束每个标识每小时的挑战数，语义未变。）

- [ ] **Step 4: 重写 provision.go**

```go
// Handle idempotently provisions the fixed private admin. Either identifier
// may be empty; when both are configured they belong to the same admin user.
// An existing user matching any configured identifier is a no-op; otherwise a
// fresh personal workspace + user is saved (workspace first, mirroring the
// first-login registration).
func (h *ProvisionAdminHandler) Handle(ctx context.Context, phone, email string) error {
	if phone == "" && email == "" {
		return domain.ErrIdentifierInvalid
	}
	if phone != "" {
		if _, err := domain.NewPhone(phone); err != nil {
			return err
		}
		if _, err := h.Users.ByPhone(ctx, phone); err == nil {
			return nil
		} else if !errors.Is(err, domain.ErrUserNotFound) {
			return err
		}
	}
	if email != "" {
		if _, err := domain.NewEmail(email); err != nil {
			return err
		}
		if _, err := h.Users.ByEmail(ctx, email); err == nil {
			return nil
		} else if !errors.Is(err, domain.ErrUserNotFound) {
			return err
		}
	}
	now := h.Now()
	workspace := domain.PersonalWorkspace{ID: h.NewID(), CreatedAt: now}
	if err := h.Workspaces.Save(ctx, workspace); err != nil {
		return err
	}
	user := domain.User{ID: h.NewID(), WorkspaceID: workspace.ID, Phone: phone, Email: email, CreatedAt: now}
	return h.Users.Save(ctx, user)
}
```

`provision_test.go` 更新调用点（`Handle(ctx, phone)` → `Handle(ctx, phone, "")` 等），并追加双标识用例：

```go
func TestProvisionAdminBothIdentifiers(t *testing.T) {
	users := newFakeUserStore()
	workspaces := newFakeWorkspaceStore()
	handler := &ProvisionAdminHandler{Users: users, Workspaces: workspaces, NewID: sequentialIDs("ws-1", "user-1"), Now: fixedNow}
	if err := handler.Handle(context.Background(), "+8613800137999", "admin@example.com"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	user, err := users.ByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("ByEmail: %v", err)
	}
	if user.Phone != "+8613800137999" {
		t.Fatalf("user = %#v, want both identifiers", user)
	}
	// Idempotent on either identifier.
	if err := handler.Handle(context.Background(), "+8613800137999", ""); err != nil {
		t.Fatalf("re-provision phone: %v", err)
	}
	if err := handler.Handle(context.Background(), "", "admin@example.com"); err != nil {
		t.Fatalf("re-provision email: %v", err)
	}
}

func TestProvisionAdminNoIdentifier(t *testing.T) {
	handler := &ProvisionAdminHandler{Users: newFakeUserStore(), Workspaces: newFakeWorkspaceStore(), NewID: sequentialIDs("ws-1"), Now: fixedNow}
	if err := handler.Handle(context.Background(), "", ""); !errors.Is(err, domain.ErrIdentifierInvalid) {
		t.Fatalf("Handle = %v, want ErrIdentifierInvalid", err)
	}
}
```

- [ ] **Step 5: 更新既有 login_test.go 调用点**

把所有 `handler.Handle(ctx, "+86...")` 改为 `handler.Handle(ctx, domain.LoginIdentifier{Phone: "+86..."})`、`handler.Handle(ctx, phone, code)` 改为 `handler.Handle(ctx, domain.LoginIdentifier{Phone: phone}, code)`；结构体字段保持不变。

- [ ] **Step 6: 跑测试**

Run: `go test ./backend/internal/modules/identity/... -v 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add backend/internal/modules/identity/application/command/
git commit -m "feat(identity): dual-identifier login, verify, and admin provisioning"
```

---

### Task 6: smtpoutbox 适配器（生产验证码邮件投递）

**Files:**
- Create: `backend/internal/modules/identity/adapters/outbound/smtpoutbox/outbox.go`
- Create: `backend/internal/modules/identity/adapters/outbound/smtpoutbox/outbox_test.go`

**Interfaces:**
- Consumes: Task 3 `domain.ErrSmsUnavailable`、`domain.ErrCodeDeliveryFailed`；`ports.MessageOutbox`
- Produces: `smtpoutbox.New(smtpoutbox.Config{Host, Port, Username, Password, From, Timeout}) *Outbox`，实现 `ports.MessageOutbox`

- [ ] **Step 1: 写失败测试**

先读 `backend/internal/modules/reminder/adapters/outbound/smtp/notifier_test.go`，复用其中假 `net.Conn` 的脚本化手法（逐行喂 SMTP 应答、记录写出内容）。`outbox_test.go`：

```go
package smtpoutbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// scriptedConn 的实现从提醒模块的 smtp notifier_test.go 移植：按脚本
// 逐条返回服务端应答，并把客户端写出的字节累积到缓冲供断言。
// （照抄该文件的假连接类型；若其名为别的，保持同名移植。）

func TestWriteSendsLoginCodeEmail(t *testing.T) {
	conn := newScriptedConn([]string{
		"220 smtp.example.com ESMTP\r\n",
		"250 smtp.example.com\r\n", // EHLO 应答（无 AUTH 能力）
		"250 OK\r\n",                // MAIL FROM
		"250 OK\r\n",                // RCPT TO
		"354 End data with <CR LF>.<CR LF>\r\n",
		"250 queued\r\n",
		"221 bye\r\n",
	})
	outbox := New(Config{Host: "smtp.example.com", Port: 465, From: "noreply@example.com", Timeout: 5 * time.Second})
	outbox.dial = func(_, _ string, _ time.Duration) (net.Conn, error) { return conn, nil }

	err := outbox.Write(context.Background(), ports.OutboxMessage{
		Address: "admin@example.com", Channel: "email", Purpose: "login", Code: "123456",
	})
	if err != nil {
		t.Fatalf("Write = %v", err)
	}
	transcript := conn.written()
	for _, want := range []string{"MAIL FROM:<noreply@example.com>", "RCPT TO:<admin@example.com>", "Subject: 登录验证码", "123456"} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("transcript missing %q:\n%s", want, transcript)
		}
	}
}

func TestWriteRejectsNonEmailMessage(t *testing.T) {
	outbox := New(Config{Host: "smtp.example.com", Port: 465, From: "noreply@example.com"})
	err := outbox.Write(context.Background(), ports.OutboxMessage{
		Address: "+8613800138000", Channel: "sms", Purpose: "login", Code: "123456",
	})
	if !errors.Is(err, domain.ErrSmsUnavailable) {
		t.Fatalf("Write = %v, want ErrSmsUnavailable", err)
	}
}

func TestWritePermanentRefusalWrapsDeliveryFailed(t *testing.T) {
	conn := newScriptedConn([]string{
		"220 smtp.example.com ESMTP\r\n",
		"250 smtp.example.com\r\n",
		"550 mailbox unavailable\r\n", // MAIL FROM 被拒
	})
	outbox := New(Config{Host: "smtp.example.com", Port: 465, From: "noreply@example.com", Timeout: 5 * time.Second})
	outbox.dial = func(_, _ string, _ time.Duration) (net.Conn, error) { return conn, nil }

	err := outbox.Write(context.Background(), ports.OutboxMessage{
		Address: "admin@example.com", Channel: "email", Purpose: "login", Code: "123456",
	})
	if !errors.Is(err, domain.ErrCodeDeliveryFailed) {
		t.Fatalf("Write = %v, want ErrCodeDeliveryFailed", err)
	}
}
```

（`net` import 与 `newScriptedConn` 的签名以移植后的实际实现为准；需要时把提醒测试里的假连接类型整体复制到本测试文件。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./backend/internal/modules/identity/adapters/outbound/smtpoutbox/ -v`
Expected: FAIL（包不存在/编译错误）

- [ ] **Step 3: 实现 outbox.go**

```go
// Package smtpoutbox is the production identity code-delivery adapter: it
// sends login and contact-channel verification codes to email addresses
// through a plain SMTP server using the standard library. It mirrors the
// reminder module's SMTP notifier; each bounded context owns its adapter.
package smtpoutbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// fallbackTimeout bounds a send when the config carries no timeout.
const fallbackTimeout = 30 * time.Second

// Config carries the SMTP endpoint and credentials. Auth is PLAIN and only
// attempted when Username is set.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	Timeout  time.Duration
}

// Outbox implements ports.MessageOutbox over SMTP for email messages.
type Outbox struct {
	cfg  Config
	dial func(network, addr string, timeout time.Duration) (net.Conn, error)
}

var _ ports.MessageOutbox = (*Outbox)(nil)

// New returns an SMTP outbox for cfg.
func New(cfg Config) *Outbox { return &Outbox{cfg: cfg, dial: net.DialTimeout} }

// Write delivers one verification code email. A non-email message fails
// closed with ErrSmsUnavailable; every SMTP or transport failure wraps
// domain.ErrCodeDeliveryFailed so the HTTP layer reports one stable code.
func (o *Outbox) Write(ctx context.Context, message ports.OutboxMessage) error {
	if message.Channel != "email" {
		return domain.ErrSmsUnavailable
	}
	err := o.send(ctx, message)
	if err == nil || errors.Is(err, domain.ErrCodeDeliveryFailed) {
		return err
	}
	return fmt.Errorf("%w: %v", domain.ErrCodeDeliveryFailed, err)
}

func (o *Outbox) send(ctx context.Context, message ports.OutboxMessage) error {
	timeout := o.cfg.Timeout
	if timeout <= 0 {
		timeout = fallbackTimeout
	}
	addr := net.JoinHostPort(o.cfg.Host, strconv.Itoa(o.cfg.Port))
	conn, err := o.dial("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	// Bound the whole conversation by both the configured timeout and the
	// caller's context deadline, whichever ends first.
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set deadline: %v", err)
	}

	client, err := smtp.NewClient(conn, o.cfg.Host)
	if err != nil {
		return fmt.Errorf("handshake %s: %v", addr, err)
	}
	defer client.Close()
	if o.cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", o.cfg.Username, o.cfg.Password, o.cfg.Host)); err != nil {
			return fmt.Errorf("auth: %v", err)
		}
	}
	if err := client.Mail(o.cfg.From); err != nil {
		return fmt.Errorf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt(message.Address); err != nil {
		return fmt.Errorf("RCPT TO: %v", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %v", err)
	}
	if _, err := writer.Write(renderCodeMessage(o.cfg.From, message)); err != nil {
		writer.Close()
		return fmt.Errorf("write message: %v", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("end of DATA: %v", err)
	}
	// The message is queued; a failed QUIT must not fail the send.
	_ = client.Quit()
	return nil
}

// renderCodeMessage renders the RFC 5322 message; the subject reflects the
// code purpose so recipients can tell login codes from channel codes.
func renderCodeMessage(from string, message ports.OutboxMessage) []byte {
	subject := "验证码"
	if message.Purpose == "login" {
		subject = "登录验证码"
	}
	return []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n\r\n验证码：%s\r\n如非本人操作，请忽略本邮件。\r\n",
		from, message.Address, subject, time.Now().UTC().Format(time.RFC1123Z), message.Code,
	))
}

// IsPermanent reports whether the underlying SMTP refusal was a 5xx, for
// logging; delivery semantics stay uniform for the caller.
func IsPermanent(err error) bool {
	var protoErr *textproto.Error
	return errors.As(err, &protoErr) && protoErr.Code >= 500 && protoErr.Code <= 599
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./backend/internal/modules/identity/adapters/outbound/smtpoutbox/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/modules/identity/adapters/outbound/smtpoutbox/
git commit -m "feat(identity): add SMTP outbox adapter for production verification codes"
```

---

### Task 7: HTTP 双标识请求体 + 错误映射 + OpenAPI 契约

**Files:**
- Modify: `backend/internal/modules/identity/adapters/inbound/http/handler.go`
- Modify: `backend/internal/modules/identity/adapters/inbound/http/handler_test.go`
- Modify: `contracts/openapi/identity.yaml`
- Modify: `tests/contract/identity_contract_test.go`
- Modify: `docs/superpowers/specs/2026-09-01-cloud-deployment-email-identity-design.md`（§3.2/§8 错误码表对齐）

**Interfaces:**
- Consumes: Task 3/5/6
- Produces: `POST /auth/login/request|verify` 接受 `{"phone"}` 或 `{"email"}`；503 `sms_unavailable`、502 `verification_send_failed`

- [ ] **Step 1: 更新 HTTP 接缝与处理器**

`handler.go`：

1a. 接缝接口改为：

```go
	loginRequester interface {
		Handle(ctx context.Context, identifier domain.LoginIdentifier) error
	}
	loginVerifier interface {
		Handle(ctx context.Context, identifier domain.LoginIdentifier, code string) (dto.VerifyLoginChallengeResult, error)
	}
```

1b. `requestLogin` 全量替换：

```go
func (h *Handler) requestLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	identifier, err := domain.NewLoginIdentifier(body.Phone, body.Email)
	if err != nil {
		writeValidationError(w, r)
		return
	}
	if err := h.RequestLoginChallenge.Handle(r.Context(), identifier); err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidPhone), errors.Is(err, domain.ErrInvalidEmail), errors.Is(err, domain.ErrIdentifierInvalid):
			writeValidationError(w, r)
		case errors.Is(err, domain.ErrRateLimited):
			writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many requests")
		case errors.Is(err, domain.ErrRegistrationClosed):
			writeError(w, r, http.StatusForbidden, "registration_closed", "registration is closed")
		case errors.Is(err, domain.ErrSmsUnavailable):
			writeError(w, r, http.StatusServiceUnavailable, "sms_unavailable", "sms delivery is unavailable")
		case errors.Is(err, domain.ErrCodeDeliveryFailed):
			writeError(w, r, http.StatusBadGateway, "verification_send_failed", "verification code delivery failed")
		default:
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{})
}
```

1c. `verifyLogin` 全量替换：

```go
func (h *Handler) verifyLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	identifier, err := domain.NewLoginIdentifier(body.Phone, body.Email)
	if err != nil {
		writeValidationError(w, r)
		return
	}
	result, err := h.VerifyLoginChallenge.Handle(r.Context(), identifier, body.Code)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidPhone), errors.Is(err, domain.ErrInvalidEmail), errors.Is(err, domain.ErrIdentifierInvalid):
			writeValidationError(w, r)
		case errors.Is(err, domain.ErrRegistrationClosed):
			writeError(w, r, http.StatusForbidden, "registration_closed", "registration is closed")
		default:
			writeError(w, r, http.StatusUnauthorized, "unauthenticated", "login failed")
		}
		return
	}
	h.setSessionCookie(w, result.Token)
	writeJSON(w, http.StatusOK, map[string]string{
		"userId":      result.Principal.UserID,
		"workspaceId": result.Principal.WorkspaceID,
		"expiresAt":   result.ExpiresAt.UTC().Format(time.RFC3339),
	})
}
```

1d. `addChannel` 错误 switch 追加两条（在 `ErrChannelExists` 分支前）：

```go
		case errors.Is(err, domain.ErrSmsUnavailable):
			writeError(w, r, http.StatusServiceUnavailable, "sms_unavailable", "sms delivery is unavailable")
		case errors.Is(err, domain.ErrCodeDeliveryFailed):
			writeError(w, r, http.StatusBadGateway, "verification_send_failed", "verification code delivery failed")
```

- [ ] **Step 2: 更新处理器测试**

读 `handler_test.go` 既有的假命令风格，然后：把所有假 `loginRequester`/`loginVerifier` 的 `Handle` 签名改为接受 `domain.LoginIdentifier`；既有用例保留语义，追加：

```go
func TestRequestLoginRejectsDualIdentifiers(t *testing.T) {
	handler := &Handler{RequestLoginChallenge: /* 既有假对象 */, SessionTTL: time.Hour}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request",
		strings.NewReader(`{"phone":"+8613800138000","email":"admin@example.com"}`))
	recorder := httptest.NewRecorder()
	handler.requestLogin(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
}

func TestRequestLoginMapsSmsUnavailable(t *testing.T) {
	// 假 requester 返回 domain.ErrSmsUnavailable
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request",
		strings.NewReader(`{"phone":"+8613800138000"}`))
	/* 组装带 failing requester（返回 domain.ErrSmsUnavailable）的 Handler */
	handler.requestLogin(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"sms_unavailable"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestRequestLoginMapsDeliveryFailure(t *testing.T) {
	// 假 requester 返回 fmt.Errorf("%w: dial", domain.ErrCodeDeliveryFailed)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request",
		strings.NewReader(`{"email":"admin@example.com"}`))
	handler.requestLogin(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"verification_send_failed"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}
```

（假对象的具体构造沿用该测试文件既有模式；注释处以文件内现成的 fake 类型实例化。）

- [ ] **Step 3: 更新 OpenAPI 契约**

`contracts/openapi/identity.yaml`：

3a. `LoginRequest` 替换为：

```yaml
    LoginRequest:
      type: object
      additionalProperties: false
      properties:
        phone:
          type: string
          maxLength: 16
        email:
          type: string
          maxLength: 254
```

3b. `LoginVerifyRequest` 替换为：

```yaml
    LoginVerifyRequest:
      type: object
      additionalProperties: false
      required: [code]
      properties:
        phone:
          type: string
          maxLength: 16
        email:
          type: string
          maxLength: 254
        code:
          type: string
          maxLength: 6
```

3c. `/api/v1/auth/login/request` 的 responses 追加：

```yaml
        '502':
          description: Verification code delivery failed.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorEnvelope'
        '503':
          description: SMS delivery unavailable.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorEnvelope'
```

3d. 422 描述改为 `Invalid or missing identifier.`。

- [ ] **Step 4: 更新契约测试**

`tests/contract/identity_contract_test.go`：

4a. `identityRoutes()` 中 login/request 一行的 schemas 改为：

```go
			map[string]string{"202": "EmptyObject", "422": "ErrorEnvelope", "429": "ErrorEnvelope", "502": "ErrorEnvelope", "503": "ErrorEnvelope"}, "LoginRequest"},
```

4b. 两处 `LoginRequest`/`LoginVerifyRequest` 断言（约第 57–62 行与第 164–168 行）改为：

```go
	login := schemas["LoginRequest"]
	if !docClosedObject(login, nil) ||
		!docIsString(login.Properties["phone"]) || !docMaxLength(login.Properties["phone"], 16) ||
		!docIsString(login.Properties["email"]) || !docMaxLength(login.Properties["email"], 254) {
		t.Fatalf("LoginRequest = %#v", login)
	}
	verify := schemas["LoginVerifyRequest"]
	if !docClosedObject(verify, []string{"code"}) ||
		!docIsString(verify.Properties["phone"]) || !docMaxLength(verify.Properties["phone"], 16) ||
		!docIsString(verify.Properties["email"]) || !docMaxLength(verify.Properties["email"], 254) ||
		!docMaxLength(verify.Properties["code"], 6) {
		t.Fatalf("LoginVerifyRequest = %#v", verify)
	}
```

（先读 `contract_types_test.go` 的 `docClosedObject` 确认 required 匹配语义；若其对 `nil` 的处理不同，按其实现调整传参。）

- [ ] **Step 5: 对齐 spec 错误码表**

编辑 `docs/superpowers/specs/2026-09-01-cloud-deployment-email-identity-design.md` §3.2 表格：把 `422 identifier_invalid`、`422 invalid_email`、`422 invalid_phone` 三行合并为一行：

```
| 标识缺失/重复/格式非法 | 422 `validation_error`（沿用代码库单一校验错误码约定） |
```

§8 表格同步（三行并一行）。

- [ ] **Step 6: 跑测试**

Run: `go test ./backend/internal/modules/identity/adapters/inbound/http/ ./tests/contract/ -v 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add backend/internal/modules/identity/adapters/inbound/http/ contracts/openapi/identity.yaml tests/contract/identity_contract_test.go docs/superpowers/specs/2026-09-01-cloud-deployment-email-identity-design.md
git commit -m "feat(identity): dual-identifier login API with sms-unavailable and send-failed codes"
```

---

### Task 8: API 装配——生产投递路由、通道过滤、compose 变量

**Files:**
- Modify: `backend/cmd/api/wiring.go`
- Modify: `backend/cmd/api/provision.go`
- Modify: `compose.yaml`（api 服务 environment）
- Test: `backend/cmd/api/composition_integration_test.go`（按需更新）

**Interfaces:**
- Consumes: Task 2/5/6 的全部产物
- Produces: `selectCodeOutbox(cfg, pool) ports.MessageOutbox`；`channelsProvider(cfg, channels)` 过滤停用通道；compose 向 api 传递 `PRIVATE_ADMIN_EMAIL`、`SMTP_*`

- [ ] **Step 1: wiring.go 新增导入**

在 `wiring.go` import 块追加（别名与既有可能冲突的包区分）：

```go
	identityports "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/adapters/outbound/smtpoutbox"
	identitydomain "github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
```

- [ ] **Step 2: 装配生产验证码投递**

`wiring.go` 追加：

```go
// selectCodeOutbox resolves the identity verification-code delivery adapter:
// the dev outbox outside production (the dev inbox keeps working), and a
// channel-routing production adapter inside it.
func selectCodeOutbox(cfg config.Config, pool *pgxpool.Pool) identityports.MessageOutbox {
	if cfg.AppEnv == config.AppEnvProduction {
		return productionCodeOutbox{email: smtpoutbox.New(smtpoutbox.Config{
			Host:     cfg.SmtpHost,
			Port:     cfg.SmtpPort,
			Username: cfg.SmtpUsername,
			Password: cfg.SmtpPassword,
			From:     cfg.SmtpFrom,
			Timeout:  cfg.SmtpTimeout,
		})}
	}
	return fakeoutbox.New(pool)
}

// productionCodeOutbox routes verification codes by channel: email reaches
// the real SMTP sender; phone has no real SMS provider yet and fails closed
// so a phone login attempt reports sms_unavailable instead of hanging.
type productionCodeOutbox struct {
	email identityports.MessageOutbox
}

func (o productionCodeOutbox) Write(ctx context.Context, message identityports.OutboxMessage) error {
	if message.Channel == "email" {
		return o.email.Write(ctx, message)
	}
	return identitydomain.ErrSmsUnavailable
}
```

`registerIdentityRoutes` 中两处修改：

```go
	outbox := selectCodeOutbox(cfg, pool)   // 替换 outbox := fakeoutbox.New(pool)
```

并在 `RequestLoginChallengeHandler` / `VerifyLoginChallengeHandler` 字面量中各加一行：

```go
			PrivateAdminEmail: cfg.PrivateAdminEmail,
```

- [ ] **Step 3: 提醒扇出过滤停用短信**

`wiring.go` 的 `channelsProvider` 全量替换为：

```go
// channelsProvider snapshots the owner's usable (verified+enabled) contact
// channel kinds, workspace+user scoped and deterministically sorted, so
// reminder plans carry a stable requested-channel snapshot. When the SMS
// reminder adapter is disabled, sms never joins the snapshot, so plans never
// fan out into SMS deliveries.
func channelsProvider(cfg config.Config, channels *identitypostgres.ChannelStore) todoports.ChannelsProvider {
	return func(ctx context.Context, workspaceID, ownerUserID string) ([]string, error) {
		rows, err := channels.ListByUser(ctx, workspaceID, ownerUserID)
		if err != nil {
			return nil, err
		}
		usable := make(map[string]bool)
		for _, row := range rows {
			if row.Usable() {
				usable[string(row.Kind)] = true
			}
		}
		snapshot := make([]string, 0, len(usable))
		for kind := range usable {
			if kind == "sms" && cfg.ReminderSmsAdapter == config.ReminderSmsAdapterDisabled {
				continue
			}
			snapshot = append(snapshot, kind)
		}
		sort.Strings(snapshot)
		return snapshot, nil
	}
}
```

（保留原函数尾部的排序逻辑；若原实现排序方式不同，沿用原方式。）

然后 `grep -n "channelsProvider(" backend/cmd/api/*.go` 找到**所有**调用点（`buildHandler` 内的 `buildTodoHandlers(...)` 以及 `buildPortabilityHandlers` 若有），统一改为传入 `cfg`：

```go
channelsProvider(cfg, identitypostgres.NewChannelStore(pool))
```

- [ ] **Step 4: provisioning 传邮箱**

`backend/cmd/api/provision.go` 的 `provisionPrivateAdmin`：

```go
	if err := handler.Handle(ctx, cfg.PrivateAdminPhone, cfg.PrivateAdminEmail); err != nil {
		return err
	}
```

- [ ] **Step 5: compose.yaml 传递新变量**

`compose.yaml` 的 `api.service.environment` 块追加（紧随 `PRIVATE_ADMIN_PHONE` 之后）：

```yaml
      PRIVATE_ADMIN_EMAIL: ${PRIVATE_ADMIN_EMAIL:-}
      SMTP_HOST: ${SMTP_HOST:-}
      SMTP_PORT: ${SMTP_PORT:-0}
      SMTP_USERNAME: ${SMTP_USERNAME:-}
      SMTP_PASSWORD: ${SMTP_PASSWORD:-}
      SMTP_FROM: ${SMTP_FROM:-}
      SMTP_TIMEOUT: ${SMTP_TIMEOUT:-10s}
```

- [ ] **Step 6: 跑装配与组合测试**

Run: `go build ./backend/... && go test ./backend/cmd/api/ -v 2>&1 | tail -30`
Expected: PASS。若 `composition_integration_test.go` 用环境变量构造 config，按其模式补齐新变量（生产断言场景才需要 SMTP 三件套）。

- [ ] **Step 7: 提交**

```bash
git add backend/cmd/api/ compose.yaml
git commit -m "feat(api): route production code delivery, filter disabled sms, pass new env"
```

---

### Task 9: Worker 装配——disabled 队列裁剪与失败关闭

**Files:**
- Modify: `backend/cmd/worker/main.go`
- Modify: `backend/cmd/worker/main_test.go`

**Interfaces:**
- Consumes: Task 2 `config.ReminderSmsAdapterDisabled`
- Produces: `reminderQueues(cfg) map[string]riverqueue.QueueConfig`；`disabledSmsNotifier`

- [ ] **Step 1: 写失败测试**

`main_test.go` 追加：

```go
func TestReminderQueuesDisabledSms(t *testing.T) {
	queues := reminderQueues(config.Config{
		ReminderSmsAdapter:            config.ReminderSmsAdapterDisabled,
		ReminderQueueEmailConcurrency: 2,
		ReminderQueueSmsConcurrency:   3,
	})
	if _, ok := queues["reminder_sms"]; ok {
		t.Fatalf("reminder_sms queue present while the sms adapter is disabled")
	}
	email, ok := queues["reminder_email"]
	if !ok || email.MaxWorkers != 2 {
		t.Fatalf("reminder_email queue = %#v", queues)
	}
}

func TestReminderQueuesDefaultSms(t *testing.T) {
	queues := reminderQueues(config.Config{
		ReminderSmsAdapter:            config.ReminderSmsAdapterFake,
		ReminderQueueEmailConcurrency: 2,
		ReminderQueueSmsConcurrency:   3,
	})
	sms, ok := queues["reminder_sms"]
	if !ok || sms.MaxWorkers != 3 {
		t.Fatalf("reminder_sms queue = %#v", queues)
	}
}
```

（import 需要 `"testing"` 与 config 包；若测试文件已有相同 import 块，合并。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./backend/cmd/worker/ -run 'ReminderQueues' -v`
Expected: FAIL（`reminderQueues` 未定义）

- [ ] **Step 3: 实现队列裁剪与失败关闭通知器**

`main.go` 追加：

```go
// reminderQueues selects the delivery queues this worker serves: the SMS
// queue disappears when the SMS adapter is disabled, so SMS jobs are never
// worked into a provider that does not exist.
func reminderQueues(cfg config.Config) map[string]riverqueue.QueueConfig {
	queues := map[string]riverqueue.QueueConfig{
		"reminder_email": {MaxWorkers: cfg.ReminderQueueEmailConcurrency},
	}
	if cfg.ReminderSmsAdapter != config.ReminderSmsAdapterDisabled {
		queues["reminder_sms"] = riverqueue.QueueConfig{MaxWorkers: cfg.ReminderQueueSmsConcurrency}
	}
	return queues
}
```

`buildRiverClient` 中 `Queues:` 行改为：

```go
		Queues: reminderQueues(cfg),
```

`selectSmsNotifier` 开头加分支：

```go
	if cfg.ReminderSmsAdapter == config.ReminderSmsAdapterDisabled {
		return disabledSmsNotifier{}
	}
```

并追加：

```go
// disabledSmsNotifier fails closed if an SMS delivery job ever executes while
// the adapter is disabled. channelsProvider never plans SMS in this state, so
// reaching this path means the job predates the switch; it dead-letters after
// its attempts exhaust instead of touching a nonexistent provider.
type disabledSmsNotifier struct{}

func (disabledSmsNotifier) Send(_ context.Context, _ reminderdto.ReminderMessage) (reminderdto.SendResult, error) {
	return reminderdto.SendResult{}, errors.New("reminder: sms adapter is disabled")
}
```

（`reminderdto` 别名已存在于 main.go 的 import；`errors` 也已导入。）

- [ ] **Step 4: 跑测试**

Run: `go test ./backend/cmd/worker/ -v`
Expected: PASS（既有 main_test 若断言默认双队列，保持通过——默认配置仍是双队列）

- [ ] **Step 5: 提交**

```bash
git add backend/cmd/worker/
git commit -m "feat(worker): drop sms queue and fail closed when sms adapter disabled"
```

---

### Task 10: Web 登录页——单输入框双标识

**Files:**
- Modify: `apps/web/src/features/auth/fetch-auth.ts`
- Modify: `apps/web/src/features/auth/login-form.tsx`
- Modify: `apps/web/src/features/auth/login-form.test.tsx`

**Interfaces:**
- Consumes: Task 7 的 API 形状
- Produces: `requestLoginChallenge(baseURL, fetcher, identifier: LoginIdentifier)`、`verifyLogin(baseURL, fetcher, identifier, code)`；`LoginIdentifier = { phone?: string; email?: string }`

- [ ] **Step 1: 更新 fetch-auth.ts**

`AuthErrorCode` 与各函数替换为：

```ts
export type AuthErrorCode =
  | "validation_error"
  | "rate_limited"
  | "unauthenticated"
  | "unavailable"
  | "sms_unavailable"
  | "verification_send_failed";

export interface LoginIdentifier {
  phone?: string;
  email?: string;
}

export async function requestLoginChallenge(
  baseURL: string,
  fetcher: typeof fetch,
  identifier: LoginIdentifier,
  timeoutMs = 5000,
): Promise<RequestChallengeOutcome> {
  try {
    const response = await postJSON(
      baseURL,
      fetcher,
      "/api/v1/auth/login/request",
      identifier,
      timeoutMs,
    );
    if (response.status === 202) {
      return { ok: true };
    }
    return { ok: false, error: await classifyStatus(response) };
  } catch {
    return { ok: false, error: "unavailable" };
  }
}

export async function verifyLogin(
  baseURL: string,
  fetcher: typeof fetch,
  identifier: LoginIdentifier,
  code: string,
  timeoutMs = 5000,
): Promise<VerifyOutcome> {
  try {
    const response = await postJSON(
      baseURL,
      fetcher,
      "/api/v1/auth/login/verify",
      { ...identifier, code },
      timeoutMs,
    );
    if (response.status === 200) {
      const payload: unknown = await response.json();
      if (
        isRecord(payload) &&
        hasExactKeys(payload, ["userId", "workspaceId", "expiresAt"]) &&
        isNonEmptyString(payload.userId) &&
        isNonEmptyString(payload.workspaceId) &&
        isRFC3339(payload.expiresAt)
      ) {
        return {
          ok: true,
          userId: payload.userId,
          workspaceId: payload.workspaceId,
          expiresAt: payload.expiresAt,
        };
      }
      return { ok: false, error: "unavailable" };
    }
    if (response.status === 401) {
      return { ok: false, error: "unauthenticated" };
    }
    return { ok: false, error: await classifyStatus(response) };
  } catch {
    return { ok: false, error: "unavailable" };
  }
}
```

`classifyStatus` 追加两个分支（`rate_limited` 之后）：

```ts
  if (classified.code === "sms_unavailable") {
    return "sms_unavailable";
  }
  if (classified.code === "verification_send_failed") {
    return "verification_send_failed";
  }
```

`logout` 不动。

- [ ] **Step 2: 更新 login-form.tsx**

全量替换为：

```tsx
"use client";

import { useState } from "react";

import { requestLoginChallenge, verifyLogin } from "./fetch-auth";
import type { AuthErrorCode, LoginIdentifier } from "./fetch-auth";

const errorMessages: Record<AuthErrorCode, string> = {
  validation_error: "输入格式不正确,请检查后重试。",
  rate_limited: "请求过于频繁,请稍后再试。",
  unauthenticated: "验证码不正确或已失效。",
  unavailable: "服务暂时不可用,请稍后再试。",
  sms_unavailable: "当前环境暂不支持手机号登录,请使用邮箱。",
  verification_send_failed: "验证码发送失败,请稍后重试。",
};

// identifierFrom classifies the single login input: an address containing
// '@' is an email identifier, everything else a phone number.
function identifierFrom(value: string): LoginIdentifier {
  if (value.includes("@")) {
    return { email: value };
  }
  return { phone: value };
}

// LoginForm is the two-step identifier + code login. fetcher and onNavigate
// are injected so tests can drive the flow without network or navigation.
export function LoginForm({
  fetcher = fetch,
  onNavigate = (path: string) => window.location.assign(path),
}: {
  fetcher?: typeof fetch;
  onNavigate?: (path: string) => void;
}): React.JSX.Element {
  const [step, setStep] = useState<"identifier" | "code">("identifier");
  const [identifier, setIdentifier] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submitIdentifier(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const outcome = await requestLoginChallenge("", fetcher, identifierFrom(identifier));
    setBusy(false);
    if (outcome.ok) {
      setStep("code");
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  async function submitCode(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const outcome = await verifyLogin("", fetcher, identifierFrom(identifier), code);
    setBusy(false);
    if (outcome.ok) {
      onNavigate("/");
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  return (
    <form
      aria-label="登录"
      className="login-form"
      onSubmit={step === "identifier" ? submitIdentifier : submitCode}
    >
      {step === "identifier" ? (
        <div className="login-step">
          <div className="field">
            <label htmlFor="login-identifier">手机号或邮箱</label>
            <input
              autoComplete="username"
              id="login-identifier"
              name="identifier"
              onChange={(event) => setIdentifier(event.target.value)}
              placeholder="+8613800138000 或 you@example.com"
              type="text"
              value={identifier}
            />
          </div>
          <button className="btn-primary" disabled={busy} type="submit">
            获取验证码
          </button>
        </div>
      ) : (
        <div className="login-step">
          <div className="field">
            <label htmlFor="login-code">验证码</label>
            <input
              autoComplete="one-time-code"
              id="login-code"
              name="code"
              onChange={(event) => setCode(event.target.value)}
              type="text"
              value={code}
            />
          </div>
          <button className="btn-primary" disabled={busy} type="submit">
            登录
          </button>
          <button
            className="btn-ghost"
            onClick={() => {
              setStep("identifier");
              setError(null);
            }}
            type="button"
          >
            返回
          </button>
          <p className="login-hint">
            本地开发可以从
            <a
              href={`/api/v1/dev/sms-inbox?address=${encodeURIComponent(identifier)}`}
            >
              开发收件箱
            </a>
            查看验证码。
          </p>
        </div>
      )}
      {error ? (
        <p aria-live="polite" className="login-error" role="alert">
          {error}
        </p>
      ) : null}
    </form>
  );
}
```

- [ ] **Step 3: 更新测试**

`login-form.test.tsx`：把 `screen.getByLabelText("手机号")` 全部改为 `screen.getByLabelText("手机号或邮箱")`；既有用例请求体断言保持 `{ phone: ... }`（不含 @ 的输入走 phone 分支）。追加：

```tsx
it("sends an email identifier when the input contains @", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(challengeResponse())
    .mockResolvedValueOnce(verifyResponse());
  const onNavigate = vi.fn();
  render(
    <LoginForm
      fetcher={fetcher as unknown as typeof fetch}
      onNavigate={onNavigate}
    />,
  );

  fireEvent.change(screen.getByLabelText("手机号或邮箱"), {
    target: { value: "admin@example.com" },
  });
  fireEvent.click(screen.getByRole("button", { name: "获取验证码" }));

  await waitFor(() =>
    expect(screen.getByLabelText("验证码")).toBeInTheDocument(),
  );
  const [challengeUrl, challengeInit] = fetcher.mock.calls[0];
  expect(challengeUrl).toBe("/api/v1/auth/login/request");
  expect(JSON.parse(String(challengeInit?.body))).toEqual({
    email: "admin@example.com",
  });

  fireEvent.change(screen.getByLabelText("验证码"), {
    target: { value: "123456" },
  });
  fireEvent.click(screen.getByRole("button", { name: "登录" }));

  await waitFor(() => expect(onNavigate).toHaveBeenCalledWith("/"));
  const [verifyUrl, verifyInit] = fetcher.mock.calls[1];
  expect(verifyUrl).toBe("/api/v1/auth/login/verify");
  expect(JSON.parse(String(verifyInit?.body))).toEqual({
    email: "admin@example.com",
    code: "123456",
  });
});

it("shows the sms-unavailable message when phone login is rejected", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ code: "sms_unavailable" }), { status: 503 }),
  );
  render(<LoginForm fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("手机号或邮箱"), {
    target: { value: "+8613800138000" },
  });
  fireEvent.click(screen.getByRole("button", { name: "获取验证码" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("暂不支持手机号登录"),
  );
});
```

注意：`classifyStatus` 对 503 的分类依赖 `readErrorPayload`/`classifyErrorPayload`（features/validation）——若它们对未知状态码返回空 code，`sms_unavailable` 分支不会命中；先读 `apps/web/src/features/validation/` 确认 `classifyErrorPayload` 会透传响应体中的 `code` 字段（既有用例已验证 `rate_limited` 透传），不必改。

- [ ] **Step 4: 检查其他调用点**

Run: `grep -rn "requestLoginChallenge\|verifyLogin" apps/web/src --include='*.ts*' | grep -v features/auth`
Expected: 无其他调用点；若有（如服务端会话工具），按新签名适配。

- [ ] **Step 5: 跑前端测试与 lint**

Run: `pnpm --filter @artificial-brain/web test && pnpm --filter @artificial-brain/web lint`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add apps/web/src/features/auth/
git commit -m "feat(web): single-input login accepting phone or email identifier"
```

---

### Task 11: 冒烟测试——邮箱登录与私域邮箱管理员

**Files:**
- Modify: `tests/smoke/stack_test.sh`

**Interfaces:**
- Consumes: Task 7 的 API、Task 8 的 compose 变量
- Produces: 开发栈邮箱登录端到端块、双标识拒绝断言、私域邮箱管理员演练项目

- [ ] **Step 1: compose 变量传递断言**

`stack_test.sh` 约第 70–71 行（断言 api/worker 收到 `PRIVATE_ADMIN_PHONE` 的块）旁，按同样语法追加：

```ruby
assert(api_env.key?("PRIVATE_ADMIN_EMAIL"), "api does not receive PRIVATE_ADMIN_EMAIL")
assert(api_env.key?("SMTP_HOST"), "api does not receive SMTP_HOST")
```

（先读该块确认宿主是 ruby 还是 shell 断言，语法照抄相邻行。）

- [ ] **Step 2: 开发栈邮箱登录端到端块**

在既有手机 e2e 登录块（`e2e_home` 断言 `data-page="dashboard"` 之后）插入：

```sh
	# Cloud-deploy iteration: email-identifier login through the same dev
	# inbox (the fake outbox records email codes by address).
	e2e_email="e2e@example.com"
	e2e_email_request=$(curl --fail --silent --show-error --max-time 5 \
		--output /dev/null --write-out '%{http_code}' \
		--header 'Content-Type: application/json' \
		--data "{\"email\":\"${e2e_email}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/auth/login/request")
	[ "$e2e_email_request" = 202 ] || fail "email login request status ${e2e_email_request}, want 202"

	e2e_email_inbox=$(curl --fail --silent --show-error --max-time 5 \
		"http://127.0.0.1:${WEB_PORT}/api/v1/dev/sms-inbox?address=$(printf '%s' "$e2e_email" | jq -sRr @uri)")
	e2e_email_code=$(printf '%s\n' "$e2e_email_inbox" | jq -r '.messages[0].code')
	case "$e2e_email_code" in '' | *[!0-9]*) fail "dev inbox did not return a numeric email code" ;; esac

	e2e_email_verify=$(curl --fail --silent --show-error --max-time 5 \
		--output /dev/null --write-out '%{http_code}' \
		--header 'Content-Type: application/json' \
		--data "{\"email\":\"${e2e_email}\",\"code\":\"${e2e_email_code}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/auth/login/verify")
	[ "$e2e_email_verify" = 200 ] || fail "email login verify status ${e2e_email_verify}, want 200"

	# A request carrying both identifiers is rejected as invalid.
	e2e_both=$(curl --silent --show-error --max-time 5 \
		--output /dev/null --write-out '%{http_code}' \
		--header 'Content-Type: application/json' \
		--data '{"phone":"+8613800137002","email":"both@example.com"}' \
		"http://127.0.0.1:${WEB_PORT}/api/v1/auth/login/request")
	[ "$e2e_both" = 422 ] || fail "dual-identifier login request status ${e2e_both}, want 422"
```

- [ ] **Step 3: 私域邮箱管理员演练**

在既有私域块的 `docker compose --project-name "$private_project" down ...` **之后**、备份演练之前插入：

```sh
	# Cloud-deploy iteration: private mode with an email administrator.
	private_email_project="${project}-private-email"
	DEPLOYMENT_MODE=private \
	PRIVATE_ADMIN_EMAIL="admin@example.com" \
	APP_ENV=development \
	DEV_INBOX_ENABLED=true \
	API_PORT=0 \
	WEB_PORT=0 \
	docker compose --project-name "$private_email_project" up --build --detach --wait \
		--wait-timeout "${STACK_WAIT_SECONDS:-180}"
	private_email_web_mapping=$(docker compose --project-name "$private_email_project" port web 3000 | sed -n '1p')
	private_email_web_port=${private_email_web_mapping##*:}
	case "$private_email_web_port" in '' | *[!0-9]*) fail "private email Web has no Docker-assigned host port" ;; esac

	private_email_admin="admin@example.com"
	private_email_request_status=$(curl --fail --silent --show-error --max-time 5 \
		--output /dev/null --write-out '%{http_code}' \
		--header 'Content-Type: application/json' \
		--data "{\"email\":\"${private_email_admin}\"}" \
		"http://127.0.0.1:${private_email_web_port}/api/v1/auth/login/request")
	[ "$private_email_request_status" = 202 ] || \
		fail "private email admin login request status ${private_email_request_status}, want 202"

	private_email_inbox=$(curl --fail --silent --show-error --max-time 5 \
		"http://127.0.0.1:${private_email_web_port}/api/v1/dev/sms-inbox?address=$(printf '%s' "$private_email_admin" | jq -sRr @uri)")
	private_email_code=$(printf '%s\n' "$private_email_inbox" | jq -r '.messages[0].code')
	case "$private_email_code" in '' | *[!0-9]*) fail "private email dev inbox did not return a numeric code" ;; esac

	private_email_verify_headers=$(curl --fail --silent --show-error --max-time 5 \
		--dump-header - --output /dev/null \
		--header 'Content-Type: application/json' \
		--data "{\"email\":\"${private_email_admin}\",\"code\":\"${private_email_code}\"}" \
		"http://127.0.0.1:${private_email_web_port}/api/v1/auth/login/verify")
	private_email_session=$(printf '%s\n' "$private_email_verify_headers" |
		sed -n 's/^[Ss]et-[Cc]ookie: ab_session=\([^;]*\).*$/\1/p' | sed -n '1p')
	[ -n "$private_email_session" ] || fail "private email verify did not set the ab_session cookie"

	private_email_home=$(curl --fail --silent --show-error --max-time 5 \
		--header "Cookie: ab_session=${private_email_session}" \
		"http://127.0.0.1:${private_email_web_port}/")
	printf '%s\n' "$private_email_home" | grep -F 'data-page="dashboard"' >/dev/null || \
		fail "private email admin home did not render the dashboard page"

	private_email_stranger=$(curl --silent --show-error --max-time 5 \
		--output /dev/null --write-out '%{http_code}' \
		--header 'Content-Type: application/json' \
		--data '{"email":"stranger@example.com"}' \
		"http://127.0.0.1:${private_email_web_port}/api/v1/auth/login/request")
	[ "$private_email_stranger" = 403 ] || \
		fail "private email stranger login request status ${private_email_stranger}, want 403"

	docker compose --project-name "$private_email_project" down --volumes --remove-orphans
```

- [ ] **Step 4: 跑冒烟**

Run: `make smoke-test`
Expected: PASS（含新块）。无 Docker 环境时顺延到 Task 13。

- [ ] **Step 5: 提交**

```bash
git add tests/smoke/stack_test.sh
git commit -m "test(smoke): cover email login, dual-identifier rejection, and email private admin"
```

---

### Task 12: 部署工件——env 模板、runbook、README

**Files:**
- Modify: `deploy/private/env.template`
- Modify: `deploy/private/README.md`
- Create: `docs/runbooks/cloud-ecs.md`
- Modify: `README.md`（环境变量表与登录段落）

**Interfaces:**
- Consumes: Task 2/8 的新变量；全部前序任务的行为
- Produces: 可照单执行的云上部署文档与模板

- [ ] **Step 1: 更新 env.template**

`deploy/private/env.template` 全量替换为：

```
# Artificial Brain — private deployment environment template
#
# Copy this file to `.env` in the repository root (next to compose.yaml) and
# fill in the values below; docker compose reads `.env` automatically.
# Never commit the real `.env` or real credentials (it is gitignored).
# See deploy/private/README.md and docs/runbooks/cloud-ecs.md.

# --- Deployment mode -------------------------------------------------------
# Private mode: exactly one administrator, identified by
# PRIVATE_ADMIN_PHONE and/or PRIVATE_ADMIN_EMAIL (at least one; when both are
# set they belong to the same admin user). Every other identifier is rejected
# with `registration_closed`.
DEPLOYMENT_MODE=private

# E.164 phone number of the fixed administrator, e.g. +8613800000000.
PRIVATE_ADMIN_PHONE=
# Email address of the fixed administrator, e.g. admin@example.com.
PRIVATE_ADMIN_EMAIL=

# Fail-closed production settings: the development inbox/outbox and the fake
# adapters are forbidden under APP_ENV=production, so keep these off and use
# the real adapters configured below.
APP_ENV=production
DEV_INBOX_ENABLED=false
REMINDER_DEV_OUTBOX_ENABLED=false

# HMAC secret used to verify provider delivery receipts. Use a long random
# value, e.g.: openssl rand -hex 32
REMINDER_RECEIPT_SECRET=

# --- Network ---------------------------------------------------------------
# Published ports bind to loopback by default (single host, no reverse proxy
# in the box). Cloud deployments that are reached by public IP override
# WEB_PORT=3000 in `.env` and rely on the ECS security group for source
# filtering — see docs/runbooks/cloud-ecs.md before doing this.
WEB_PORT=127.0.0.1:3000
API_PORT=127.0.0.1:8080

# --- PostgreSQL --------------------------------------------------------------
POSTGRES_DB=artificial_brain
POSTGRES_USER=artificial_brain
# Change before the first `docker compose up`; the database volume persists.
POSTGRES_PASSWORD=change-me

# --- Identity code delivery (SMTP) ------------------------------------------
# Production verification codes for email identifiers (login and contact
# channel verification) go through this SMTP endpoint. Required under
# APP_ENV=production; a personal mailbox with an authorization code works.
SMTP_HOST=
SMTP_PORT=465
SMTP_USERNAME=
SMTP_PASSWORD=
SMTP_FROM=
SMTP_TIMEOUT=10s

# --- Model adapter -----------------------------------------------------------
# Private deployments point at an OpenAI-compatible endpoint the operator
# controls. MODEL_BASE_URL, MODEL_NAME and MODEL_API_KEY are required when
# MODEL_ADAPTER=openai_compatible. Aliyun Bailian (DashScope) compatible
# endpoint shown; use the VPC endpoint when the ECS shares the region.
MODEL_ADAPTER=openai_compatible
MODEL_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
MODEL_API_KEY=
MODEL_NAME=qwen-max
MODEL_TIMEOUT=30s

# --- Email reminders (SMTP) ---------------------------------------------------
# Usually the same mailbox as the identity SMTP block above.
REMINDER_EMAIL_ADAPTER=smtp
REMINDER_SMTP_HOST=
REMINDER_SMTP_PORT=465
REMINDER_SMTP_USERNAME=
REMINDER_SMTP_PASSWORD=
REMINDER_SMTP_FROM=
REMINDER_SMTP_TIMEOUT=10s

# --- SMS reminders -------------------------------------------------------------
# disabled: reminder plans never fan out into SMS deliveries (no SMS provider
# purchased yet). aliyun: fill REMINDER_ALIYUN_* once the Aliyun SMS service
# (signature + template) is approved, then switch this value to `aliyun`.
REMINDER_SMS_ADAPTER=disabled
REMINDER_ALIYUN_ENDPOINT=https://dysmsapi.aliyuncs.com
REMINDER_ALIYUN_ACCESS_KEY_ID=
REMINDER_ALIYUN_ACCESS_KEY_SECRET=
REMINDER_ALIYUN_SIGN_NAME=
REMINDER_ALIYUN_TEMPLATE_CODE=
```

- [ ] **Step 2: 更新私域 README**

`deploy/private/README.md`：

2a. Quick start 的 Required edits 列表，把 `PRIVATE_ADMIN_PHONE` 条目替换为：

```
   - `PRIVATE_ADMIN_PHONE` and/or `PRIVATE_ADMIN_EMAIL` — the administrator's
     E.164 phone number and/or email address (at least one; when both are set
     they belong to the same admin user). These are the only identifiers that
     can ever log in, and the API refuses to start without at least one.
   - `POSTGRES_PASSWORD` and `REMINDER_RECEIPT_SECRET` — real random values.
   - Real model/SMTP configuration (`MODEL_*`, `SMTP_*`, `REMINDER_SMTP_*`);
     the fake adapters and dev inbox/outbox are forbidden under
     `APP_ENV=production`. SMS reminders stay `disabled` until an SMS
     provider is configured.
```

2b. 验证段（第 3 步）的登录句改为：

```
   Then open `http://127.0.0.1:3000/`, log in with the administrator phone
   number or email address, and enter the verification code delivered through
   your configured SMS/SMTP adapter. Every other identifier receives
   `registration_closed`.
```

2c. 文末追加一节：

```
## Cloud deployment

For the Alibaba Cloud ECS form of this private deployment (public-IP access
behind a locked-down security group, personal-mailbox SMTP, DashScope model),
follow [`docs/runbooks/cloud-ecs.md`](../../docs/runbooks/cloud-ecs.md).
```

- [ ] **Step 3: 新建云上部署 runbook**

`docs/runbooks/cloud-ecs.md` 全量内容：

````markdown
# Cloud deployment on Alibaba Cloud ECS (private mode)

Runs the standard compose stack on a single ECS host: PostgreSQL in Docker,
direct public-IP access on port 3000 protected by security-group source
filtering, verification codes over a personal-mailbox SMTP endpoint, and the
conversation model on Aliyun Bailian (DashScope). Assumes
`deploy/private/env.template` semantics — read
[`deploy/private/README.md`](../deploy/private/README.md) first.

## 1. Initialize the host

Probe first; install only what is missing:

```sh
docker --version && docker compose version
```

- Both present: skip installation.
- Missing Docker (Alibaba Cloud Linux 3):
  `sudo dnf install -y docker docker-compose-plugin && sudo systemctl enable --now docker`
- Missing Docker (Ubuntu):
  `sudo apt-get update && sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin`
- Docker present but `docker compose` missing: install `docker-compose-plugin`.

Docker is the only host dependency: Go, Node.js, and PostgreSQL all live
inside the stack images.

## 2. Security group (the access-control layer)

| Port | Direction | Source | Purpose |
| --- | --- | --- | --- |
| 22/tcp | inbound | your management IP only | SSH |
| 3000/tcp | inbound | your regular egress IPs only | web (the only app entry) |
| 8080, 8081, 5432 | never opened | — | api, worker health, postgres stay private to the compose network |

The browser only ever reaches the web service; `/api/*` is rewritten to the
API inside the compose network.

## 3. Build mirrors (restricted networks)

If the ECS cannot reach `proxy.golang.org` or `registry.npmjs.org`, create a
gitignored `compose.override.yaml` next to `compose.yaml` (same shape as the
development host):

```yaml
services:
  migrate:
    dns_search: []
    build:
      network: host
      args:
        GOPROXY: https://mirrors.aliyun.com/goproxy/,direct
  api:
    dns_search: []
    build:
      network: host
      args:
        GOPROXY: https://mirrors.aliyun.com/goproxy/,direct
  worker:
    dns_search: []
    build:
      network: host
      args:
        GOPROXY: https://mirrors.aliyun.com/goproxy/,direct
  web:
    dns_search: []
    build:
      network: host
      args:
        NPM_REGISTRY: https://registry.npmmirror.com/
```

## 4. Configure

```sh
git clone <repo-url> && cd artifical-brain
cp deploy/private/env.template .env
```

Required edits in `.env`:

- `PRIVATE_ADMIN_EMAIL` (and/or `PRIVATE_ADMIN_PHONE`).
- `WEB_PORT=3000` — the template default binds loopback only; public-IP
  access needs the open binding, protected by the security group above. Keep
  `API_PORT=127.0.0.1:8080`.
- `POSTGRES_PASSWORD` and `REMINDER_RECEIPT_SECRET`:
  `openssl rand -hex 32` each.
- `SMTP_*` and `REMINDER_SMTP_*`: your personal mailbox (host, port 465,
  username, authorization code, from address) — same values in both blocks.
- `MODEL_API_KEY`: the Bailian API key. `MODEL_BASE_URL` may switch to the
  VPC endpoint `https://dashscope-vpc.<region>.aliyuncs.com/compatible-mode/v1`
  when the ECS shares the region (no public egress needed).
- `REMINDER_EMAIL_ADAPTER=smtp`, `REMINDER_SMS_ADAPTER=disabled` (template
  defaults).

## 5. First boot

```sh
docker compose up -d --build
docker compose ps          # migrate exits 0; api/worker/web become healthy
```

The one-shot `migrate` service applies all migrations; the API provisions the
fixed administrator from the configured identifier(s) on first boot.

## 6. Acceptance checklist

```sh
curl http://127.0.0.1:3000/health/live          # web alive
curl http://127.0.0.1:8080/health/ready         # api ready (loopback only)
curl http://127.0.0.1:8080/api/v1/system/health # all components ok
```

Then in a browser from your whitelisted IP:

1. Open `http://<ECS-public-IP>:3000/` → login page.
2. Log in with the administrator email: the verification code arrives in the
   mailbox (SMTP block works, production gates hold).
3. Create a todo through the conversation page using natural language —
   verifies the DashScope `qwen-max` hookup.
4. Create a todo with a due time and a verified email contact channel; wait
   for the reminder email — verifies River + the reminder SMTP path.
5. A phone-number login attempt must answer `sms_unavailable` until an SMS
   provider is configured.

## 7. Updates

```sh
make backup                 # archive the database first
git pull
docker compose build
docker compose up -d        # migrate service re-runs, applies new migrations
```

Migrations are append-only; on failure restore the backup (below) and pin the
previous commit.

## 8. Backup and restore

```sh
make backup                                   # deploy/private/backups/*.dump + sha256
make restore BACKUP=<archive> CONFIRM=restore # destructive, gated
```

Schedule a daily copy, e.g. crontab:

```
17 3 * * * cd /path/to/artifical-brain && make backup >> /var/log/ab-backup.log 2>&1
```

Optional off-host copy to OSS (manual command, not scripted):

```sh
ossutil cp deploy/private/backups/<archive> oss://<bucket>/ab-backups/
```

Details and restore semantics: [`backup-restore.md`](./backup-restore.md);
upgrade checklist: [`upgrade.md`](./upgrade.md).

## 9. Diagnostics

- `docker compose ps --all` — a failed `migrate` container intentionally
  blocks api/worker startup.
- `docker compose logs --no-color --tail 200 api` — config validation errors
  appear here (missing SMTP/admin variables refuse startup).
- Readiness: `curl 127.0.0.1:8080/health/ready` and
  `curl 127.0.0.1:8080/api/v1/system/health` (loopback, credential-free).
- Login code never arrives: verify `SMTP_*` values, mailbox authorization
  code (not the mailbox password), and the provider's spam policy.
````

- [ ] **Step 4: 更新根 README**

`README.md` 环境变量表（`| Variable | Default in Compose | Purpose |`）追加：

```
| `PRIVATE_ADMIN_EMAIL` | unset | Email address of the fixed private-mode administrator; private mode requires at least one of this or `PRIVATE_ADMIN_PHONE`, and it must stay unset in cloud mode |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` | unset | Identity verification-code SMTP endpoint; required under `APP_ENV=production` |
| `SMTP_TIMEOUT` | `10s` | Timeout for identity verification-code SMTP sends |
```

并把 `REMINDER_SMS_ADAPTER` 行的 Purpose 改为：`Reminder SMS provider adapter: `fake` (dev outbox), `aliyun`, or `disabled` (no SMS deliveries); `fake` fails config.Load with `APP_ENV=production``。

"Logging in locally" 段第一句改为：

```
Compose runs in cloud mode with a fake outbox. Login is a two-step flow:
request a code for a phone number or email address, then read the code from
the double-gated dev inbox (`GET /api/v1/dev/sms-inbox?address=<identifier>`
— present only when `APP_ENV` is not `production` **and**
`DEV_INBOX_ENABLED=true`), and verify it to receive the `ab_session` cookie.
```

- [ ] **Step 5: 校验文档链接与格式**

Run: `make format && git diff --stat`
Expected: 仅格式变化；人工检查 runbook 相对链接（`../deploy/private/README.md`、`./backup-restore.md`、`./upgrade.md`）指向存在。

- [ ] **Step 6: 提交**

```bash
git add deploy/private/env.template deploy/private/README.md docs/runbooks/cloud-ecs.md README.md
git commit -m "docs: cloud ECS runbook, dual-identifier env template, and README updates"
```

---

### Task 13: 全部门禁与收尾

**Files:**
- 无新增（验证全部前序任务）

**Interfaces:**
- Consumes: Task 1–12 全部

- [ ] **Step 1: 安装依赖并跑 Docker-free 门禁**

```sh
make toolchain-check
corepack pnpm install --frozen-lockfile
make verify
```

Expected: PASS（harness-test、format-check、lint、architecture-test、Go race 测试、Web 测试、build 全绿）。失败时按错误定位回对应任务修复，不放宽门禁。

- [ ] **Step 2: 迁移门禁**

```sh
make migration-test
```

Expected: PASS，schema 版本 9。

- [ ] **Step 3: 冒烟门禁**

```sh
make smoke-test
```

Expected: PASS，包含新增的邮箱登录块、双标识拒绝块、私域邮箱管理员演练、既有全部块（提醒投递/抑制/回执/运维/可移植性/私域手机管理员/备份恢复/升级演练）。

- [ ] **Step 4: 本地生产配置演练（可选，验证 fail-closed）**

临时用生产形态起栈，确认无 SMTP/管理员配置时 API 拒绝启动、配置齐全时健康，随后 `docker compose down`：

```sh
APP_ENV=production DEPLOYMENT_MODE=private docker compose up -d --build 2>&1 | tail -5
docker compose logs api | grep -i "config:" || true
docker compose down
```

Expected: api 因缺失 `PRIVATE_ADMIN*`/`SMTP_*` 启动失败且日志给出具体缺失变量（验证生产闸门），然后恢复正常开发栈。

- [ ] **Step 5: 收尾提交（如有格式/文档修正）**

按 Global Constraints 提交仪式收尾；确认 `git status` 干净、分支 `feat/260901_cloud_deploy` 领先 master 的任务提交序列完整。

- [ ] **Step 6: 人工验收交接**

向用户报告：合并/推送后，按 `docs/runbooks/cloud-ecs.md` 在 ECS 执行第 1–6 节；需用户提供 `.env` 真实值（管理员邮箱、邮箱授权码、百炼 API Key、`openssl rand` 密钥）。CI 不触碰真实供应商，生产验收清单在 runbook §6。

---

## 计划自审记录

1. **Spec 覆盖**：D1–D8 决策全部落任务——D1（compose postgres 不变，无任务即正确）、D2（Task 2/12 private）、D3（Task 1/3/4/5/7/10/11）、D4（Task 6/8/12）、D5（Task 12 模板；零代码）、D6（Task 12 runbook §2）、D7（runbook §7）、D8（沿用）。`REMINDER_SMS_ADAPTER=disabled`（Task 2/8/9/12）。spec §3.2 错误码表已在 Task 7 Step 5 对齐为 `validation_error` 约定。
2. **占位符扫描**：测试辅助函数名（`fixedCode`/`newFakeOutbox` 等）与集成测试夹具名以任务内"先读既有文件对齐"指令消解——断言与用例体完整给出，仅夹具标识符随宿主文件。
3. **类型一致性**：`LoginIdentifier`、`Handle(ctx, identifier)`、`Handle(ctx, identifier, code)`、`Handle(ctx, phone, email)`、`ReminderSmsAdapterDisabled`、`ErrSmsUnavailable`/`ErrCodeDeliveryFailed`、`smtpoutbox.Config` 在各任务间签名一致。
