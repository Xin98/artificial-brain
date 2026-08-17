# Artificial Brain

ITER-0001 is a runnable, deliberately narrow system skeleton: a server-rendered Next.js health page, a Go API, a Go Worker, a one-shot migration command, and PostgreSQL. It proves startup ordering, health propagation, lifecycle handling, and repository verification without adding business-domain behavior.

## Prerequisites

- Go 1.26.5 (or a newer 1.26 patch)
- Node.js 24.18.0, Corepack, and pnpm 11.19.0
- Docker with Docker Compose v2
- `curl`, `jq`, and Ruby (used by bounded migration and smoke tests)

Check the local toolchain with `make toolchain-check`, then install JavaScript dependencies with `corepack pnpm install --frozen-lockfile`.

## Run locally

Start the complete stack in the foreground:

```sh
make dev
```

Compose waits for PostgreSQL, runs the migration process to completion, then starts API and Worker, and finally starts Web after API readiness succeeds. The default host URLs are:

- Web status page: `http://localhost:3000/`
- Web liveness: `http://localhost:3000/health/live`
- API liveness: `http://localhost:8080/health/live`
- API readiness: `http://localhost:8080/health/ready`
- API system health: `http://localhost:8080/api/v1/system/health`

The Worker health port and PostgreSQL are private to the Compose network. Browser code never receives their Compose service names.

Copy `.env.example` to an ignored `.env` only when local overrides are needed. Never commit real credentials.

| Variable | Default in Compose | Purpose |
| --- | --- | --- |
| `POSTGRES_DB` | `artificial_brain` | Local database name |
| `POSTGRES_USER` | `artificial_brain` | Local database user |
| `POSTGRES_PASSWORD` | `local-development-only` | Local-only PostgreSQL password; replace outside local development |
| `SERVICE_VERSION` | `dev` | Version attached to service logs and Worker leases |
| `API_HTTP_ADDRESS` | `:8080` | API listen address inside its container |
| `WORKER_HEALTH_ADDRESS` | `:8081` | Worker-only health listen address |
| `WORKER_HEARTBEAT_INTERVAL` | `2s` | Interval between Worker lease heartbeats |
| `WORKER_LEASE_TTL` | `6s` | Maximum Worker heartbeat age before degradation; must be at least twice the interval |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful API and Worker shutdown budget |
| `API_PORT` | `8080` | API host port; set to `0` for Docker assignment |
| `WEB_PORT` | `3000` | Web host port; set to `0` for Docker assignment |
| `DATABASE_URL` | supplied by Compose | Required PostgreSQL URL for direct Go process execution |
| `MIGRATIONS_DIR` | `/migrations` | Migration directory used only by the migrate command |
| `API_INTERNAL_URL` | `http://api:8080` | Server-only API base URL for Web |

## Health semantics

Liveness proves only that the process HTTP loop is responding. Readiness verifies dependencies needed to serve correctly and returns a stable non-2xx response when they are unavailable. API system health reports fixed API, database, and Worker components. A database failure or expired Worker heartbeat makes the aggregate `degraded`; the Web page still renders actionable component state. If Web cannot obtain or validate the API report, it renders `unavailable` instead of leaking the raw failure.

The API reads the Worker heartbeat lease from PostgreSQL; Web never calls Worker directly. Health checks are read-only and do not run migrations or create schema.

## Migration ownership

`backend/cmd/migrate` is the only schema owner. It uses tern as a one-shot adapter over the immutable migrations in `deploy/migrations`. API and Worker only verify schema compatibility and fail safely when the schema is absent or incompatible. Compose blocks API and Worker until migration succeeds.

## Verification

Human and CI verification use the same Make entry points:

```sh
make toolchain-check   # verify pinned local tools
make harness-test     # repository, Make graph, secret, and workflow policy
make format           # mutate Go, Web, and repository files into canonical format
make format-check     # read-only formatting check
make lint             # go vet plus Web lint
make architecture-test
make test             # Go race tests plus Web tests
make build            # production Go and Web builds
make verify           # all Docker-free, read-only pre-commit gates
make migration-test   # isolated empty-schema and migration ownership proof
make smoke-test       # complete healthy/degraded/recovered Compose proof
```

`make verify` never calls `make format`, Docker, migration tests, or smoke tests. Run the complete local acceptance sequence as CI does:

```sh
corepack pnpm install --frozen-lockfile
make verify
make migration-test
make smoke-test
```

## Shutdown and local data

Press Ctrl-C during `make dev` to stop the foreground Compose process; API and Worker use their configured graceful shutdown budget. Run `make down` to stop and remove this project's containers and network. **`make down` preserves data.**

Permanent local cleanup is separately guarded:

```sh
make clean-local-data CONFIRM=delete
```

This destroys only this Compose project's local named volume. The command refuses to run without the exact confirmation value.

## Diagnostics

- Tool mismatch: run `make toolchain-check`, then install the exact Node.js/pnpm pins or a compatible Go 1.26 patch.
- Port collision: set `API_PORT` or `WEB_PORT` in the local environment; smoke tests use Docker-assigned ports automatically.
- Startup ordering: run `docker compose ps --all`. A failed `migrate` container intentionally prevents API and Worker startup.
- Readiness failure: query the API readiness and system-health URLs above; responses are stable and credential-free.
- Recent service output: run `docker compose logs --no-color --tail 200`. Do not publish raw local logs without checking them for environment-specific sensitive values; automated smoke failure diagnostics redact supported credential forms.
- Resetting a disposable local schema: use only the guarded `make clean-local-data CONFIRM=delete`, then `make dev`.
