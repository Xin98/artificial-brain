#!/bin/sh
# Restore a pg_dump custom-format archive into the stack's PostgreSQL
# database. Destructive: every existing object in the database is replaced.
#
# Required environment:
#   COMPOSE_PROJECT_NAME  compose project that runs the stack
#   BACKUP                path to a backup-*.dump archive
#   CONFIRM               must be exactly "restore" or the script refuses
# Optional environment:
#   POSTGRES_USER         database role (default: artificial_brain)
#   POSTGRES_DB           database name (default: artificial_brain)
#
# Stops api, worker, and web, runs pg_restore --clean --if-exists, then
# restarts the stopped services.

set -eu

fail() {
	printf 'restore: %s\n' "$*" >&2
	exit 1
}

[ "${CONFIRM:-}" = "restore" ] || \
	fail "refusing to restore: run with CONFIRM=restore (and BACKUP=<archive>) to overwrite the database"
[ -n "${COMPOSE_PROJECT_NAME:-}" ] || fail "COMPOSE_PROJECT_NAME is required"
[ -n "${BACKUP:-}" ] || fail "BACKUP must point at a backup archive (e.g. deploy/private/backups/backup-....dump)"
[ -f "$BACKUP" ] || fail "backup archive not found: ${BACKUP}"

if [ -f "${BACKUP}.sha256" ]; then
	expected=$(tr -d '[:space:]' <"${BACKUP}.sha256")
	actual=$(sha256sum "$BACKUP" | awk '{print $1}')
	[ -n "$expected" ] && [ "$expected" = "$actual" ] || \
		fail "sha256 sidecar does not match ${BACKUP}"
fi

POSTGRES_USER="${POSTGRES_USER:-artificial_brain}"
POSTGRES_DB="${POSTGRES_DB:-artificial_brain}"

command -v docker >/dev/null 2>&1 || fail "docker is required"

compose() {
	docker compose --project-name "$COMPOSE_PROJECT_NAME" "$@"
}

compose stop api worker web
if ! compose exec -T postgres pg_restore \
	--username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
	--clean --if-exists <"$BACKUP"; then
	compose start api worker web || true
	fail "pg_restore failed; services restarted, database may be inconsistent"
fi
compose start api worker web

printf 'restore: restored %s into project %s\n' "$BACKUP" "$COMPOSE_PROJECT_NAME"
