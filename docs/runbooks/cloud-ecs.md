# Cloud deployment on Alibaba Cloud ECS (private mode)

Runs the standard compose stack on a single ECS host: PostgreSQL in Docker,
direct public-IP access on port 3000 protected by security-group source
filtering, verification codes over a personal-mailbox SMTP endpoint, and the
conversation model on Aliyun Bailian (DashScope). Assumes
`deploy/private/env.template` semantics — read
[`deploy/private/README.md`](../../deploy/private/README.md) first.

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
