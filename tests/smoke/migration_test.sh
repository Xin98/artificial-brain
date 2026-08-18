#!/bin/sh

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT"

unique_project_name() {
	printf 'artificial-brain-migration-%s-%s\n' "$(date +%s)" "$$"
}

run_with_deadline() {
	seconds=$1
	shift
	ruby -e '
seconds = Float(ARGV.shift)
abort "deadline must be positive" unless seconds.positive?
pid = Process.spawn(*ARGV, pgroup: true)
deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + seconds
loop do
  waited = Process.waitpid(pid, Process::WNOHANG)
  if waited
    status = $?
    exit(status.exitstatus || 128 + status.termsig)
  end
  if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline
    Process.kill("TERM", -pid) rescue Errno::ESRCH
    sleep 1
    Process.kill("KILL", -pid) rescue Errno::ESRCH
    Process.waitpid(pid) rescue Errno::ECHILD
    exit 124
  end
  sleep 0.1
end
' "$seconds" "$@"
}

case "${1:-}" in
	--project-name-only)
		unique_project_name
		exit 0
		;;
	--deadline-self-test)
		if run_with_deadline 1 sleep 30; then
			printf 'migration test: deadline self-test unexpectedly succeeded\n' >&2
			exit 1
		fi
		exit 0
		;;
esac

command -v docker >/dev/null 2>&1 || {
	printf 'migration test: docker is required\n' >&2
	exit 1
}
command -v ruby >/dev/null 2>&1 || {
	printf 'migration test: ruby is required for bounded process execution\n' >&2
	exit 1
}
docker compose version >/dev/null 2>&1 || {
	printf 'migration test: docker compose is required\n' >&2
	exit 1
}

project=$(unique_project_name)
database_name=${POSTGRES_DB:-artificial_brain}
database_user=${POSTGRES_USER:-artificial_brain}
api_container=

compose() {
	docker compose --project-name "$project" "$@"
}

cleanup() {
	exit_code=$?
	trap - EXIT HUP INT TERM
	if [ -n "$api_container" ]; then
		docker rm --force "$api_container" >/dev/null 2>&1 || true
	fi
	compose down --volumes --remove-orphans >/dev/null 2>&1 || true
	exit "$exit_code"
}
trap cleanup EXIT HUP INT TERM

compose up --detach --wait --wait-timeout 60 postgres

api_container="${project}-empty-api"
compose run --build --no-deps --detach \
	--name "$api_container" \
	--publish 127.0.0.1::8080 \
	api >/dev/null
api_port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "8080/tcp") 0).HostPort}}' "$api_container")
[ -n "$api_port" ] || {
	printf 'migration test: empty-schema API did not publish a test port\n' >&2
	exit 1
}
sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${api_port}/health/live" 30
readiness_status=$(curl --silent --output /dev/null --write-out '%{http_code}' \
	--max-time 3 "http://127.0.0.1:${api_port}/health/ready")
[ "$readiness_status" = 503 ] || {
	printf 'migration test: empty-schema API readiness returned %s, want 503\n' "$readiness_status" >&2
	exit 1
}
docker stop --time 15 "$api_container" >/dev/null
docker rm "$api_container" >/dev/null
api_container=

set +e
run_with_deadline 60 \
	docker compose --project-name "$project" run --build --no-deps --rm worker
worker_exit=$?
set -e
if [ "$worker_exit" -eq 0 ]; then
	printf 'migration test: Worker started successfully against an empty schema\n' >&2
	exit 1
fi
if [ "$worker_exit" -eq 124 ]; then
	printf 'migration test: Worker did not exit within the startup deadline\n' >&2
	exit 1
fi

schema_version_table=$(compose exec -T postgres psql \
	--username "$database_user" \
	--dbname "$database_name" \
	--tuples-only --no-align \
	--command "select coalesce(to_regclass('public.schema_version')::text, '')")
[ -z "$schema_version_table" ] || {
	printf 'migration test: API or Worker created public.schema_version\n' >&2
	exit 1
}

compose run --build --no-deps --rm migrate
compose run --build --no-deps --rm migrate

schema_version=$(compose exec -T postgres psql \
	--username "$database_user" \
	--dbname "$database_name" \
	--tuples-only --no-align \
	--command 'select version from public.schema_version limit 1')
[ "$schema_version" = 5 ] || {
	printf 'migration test: schema version is %s, want 5\n' "$schema_version" >&2
	exit 1
}

worker_table_count=$(compose exec -T postgres psql \
	--username "$database_user" \
	--dbname "$database_name" \
	--tuples-only --no-align \
	--command "select count(*) from information_schema.tables where table_schema = 'runtime' and table_name = 'worker_heartbeats'")
[ "$worker_table_count" = 1 ] || {
	printf 'migration test: runtime.worker_heartbeats count is %s, want 1\n' "$worker_table_count" >&2
	exit 1
}

compose --profile test run --build --no-deps --rm backend-test \
	go test -p=1 -race -v \
	./backend/internal/platform/database \
	./backend/internal/platform/workerstatus \
	./backend/internal/modules/... \
	./backend/cmd/api
