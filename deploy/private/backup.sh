#!/bin/sh
# Dump the stack's PostgreSQL database into a timestamped archive.
#
# Required environment:
#   COMPOSE_PROJECT_NAME  compose project that runs the stack
# Optional environment:
#   POSTGRES_USER         database role (default: artificial_brain)
#   POSTGRES_DB           database name (default: artificial_brain)
#   OUTPUT_DIR            archive directory (default: deploy/private/backups)
#
# Produces ${OUTPUT_DIR}/backup-<UTC timestamp>.dump (pg_dump custom format)
# plus a .sha256 sidecar, and prints the archive path on the last line.

set -eu

fail() {
	printf 'backup: %s\n' "$*" >&2
	exit 1
}

[ -n "${COMPOSE_PROJECT_NAME:-}" ] || fail "COMPOSE_PROJECT_NAME is required"

POSTGRES_USER="${POSTGRES_USER:-artificial_brain}"
POSTGRES_DB="${POSTGRES_DB:-artificial_brain}"
OUTPUT_DIR="${OUTPUT_DIR:-deploy/private/backups}"

command -v docker >/dev/null 2>&1 || fail "docker is required"

mkdir -p "$OUTPUT_DIR" || fail "cannot create ${OUTPUT_DIR}"

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
archive="${OUTPUT_DIR}/backup-${timestamp}.dump"

docker compose --project-name "$COMPOSE_PROJECT_NAME" exec -T postgres \
	pg_dump --username "$POSTGRES_USER" --format=custom --dbname "$POSTGRES_DB" \
	>"$archive" || {
	rm -f "$archive"
	fail "pg_dump failed for project ${COMPOSE_PROJECT_NAME}"
}

sha256sum "$archive" | awk '{print $1}' >"${archive}.sha256" || \
	fail "cannot write sha256 sidecar for ${archive}"

printf 'backup: archive %s\n' "$archive" >&2
printf '%s\n' "$archive"
