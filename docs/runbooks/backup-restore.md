# Runbook: backup and restore

The stack's state lives entirely in PostgreSQL. Backup and restore are
operator scripts (`deploy/private/backup.sh`, `deploy/private/restore.sh`)
wrapped by Make targets; both are parameterized by environment so they also
work against non-default compose projects (the smoke test uses this).

## Backup

```sh
make backup
```

- Runs `pg_dump --format=custom` inside the project's `postgres` container.
- Writes `deploy/private/backups/backup-<UTC timestamp>.dump` plus a
  `<archive>.sha256` sidecar containing the archive digest, and prints the
  archive path.
- The archive directory is gitignored; copy archives off-host if you need
  off-site retention.

Overrides (all optional except the project name for direct script use):

| Variable             | Default                     | Purpose                          |
| -------------------- | --------------------------- | -------------------------------- |
| `COMPOSE_PROJECT_NAME` | compose default project   | project that runs the stack      |
| `POSTGRES_USER`      | `artificial_brain`          | database role used by `pg_dump`  |
| `POSTGRES_DB`        | `artificial_brain`          | database to dump                 |
| `OUTPUT_DIR`         | `deploy/private/backups`    | archive directory                |

Direct script use (e.g. against a named project):

```sh
COMPOSE_PROJECT_NAME=my-stack sh deploy/private/backup.sh
```

### What the archive contains

A full single-database `pg_dump` (custom format) of the application
database: every schema and table owned by the application role — identity,
todos, conversation, reminder (including delivery history), portability
state, and the migration bookkeeping table — with data. It does not contain
cluster-level objects (roles, tablespaces) or `.env` secrets; keep the
`.env` alongside your archives if you need a full disaster-recovery set.

## Restore

```sh
make restore BACKUP=deploy/private/backups/backup-....dump CONFIRM=restore
```

Restore semantics:

1. Refuses without `CONFIRM=restore` (same guard style as
   `make clean-local-data`), a missing `BACKUP`, or a missing archive file.
2. If a `<archive>.sha256` sidecar exists, the archive digest must match.
3. Stops `api`, `worker`, and `web` via compose.
4. Runs `pg_restore --clean --if-exists` into the database — existing
   objects are dropped and replaced; this is a full overwrite of application
   data.
5. Restarts the stopped services. If `pg_restore` fails, the services are
   still restarted and the script exits non-zero.

Verify afterwards:

```sh
curl http://127.0.0.1:8080/health/ready
```

and spot-check data (see `docs/runbooks/upgrade.md` for the verification
list used after upgrades).

### Restoring onto another host or project

Copy the archive and its `.sha256` sidecar to the target host, start the
stack (migrations create the schema if the database is empty), then run the
script directly with the target project's parameters:

```sh
COMPOSE_PROJECT_NAME=target-stack BACKUP=/path/backup-....dump CONFIRM=restore \
  sh deploy/private/restore.sh
```

`--clean --if-exists` makes this work both onto a fresh database and onto an
existing one.
