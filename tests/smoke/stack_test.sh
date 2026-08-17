#!/bin/sh

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT"

fail() {
	printf 'stack test: %s\n' "$*" >&2
	exit 1
}

unique_project_name() {
	printf 'artificial-brain-smoke-%s-%s\n' "$(date +%s)" "$$"
}

config_test() {
	command -v docker >/dev/null 2>&1 || fail "docker is required"
	docker compose version >/dev/null 2>&1 || fail "docker compose is required"
	command -v jq >/dev/null 2>&1 || fail "jq is required"
	docker compose config --quiet
	config=$(docker compose config --format json)
	test_config=$(docker compose --profile test config --format json)

	actual_runtime=$(printf '%s\n' "$config" | jq -r '.services | keys[]')
	expected_runtime=$(printf '%s\n' api migrate postgres web worker)
	[ "$actual_runtime" = "$expected_runtime" ] || fail "unexpected runtime services"

	printf '%s\n' "$config" | jq -e '
		.services.postgres.image == "postgres:18.4-alpine" and
		.services.migrate.depends_on.postgres.condition == "service_healthy" and
		.services.api.depends_on.migrate.condition == "service_completed_successfully" and
		.services.worker.depends_on.migrate.condition == "service_completed_successfully" and
		.services.web.depends_on.api.condition == "service_healthy" and
		((.services.worker.ports // []) | length == 0) and
		((.services.postgres.ports // []) | length == 0)
	' >/dev/null || fail "compose topology does not satisfy the runtime contract"
	printf '%s\n' "$test_config" | jq -e '
		(.services | keys | sort) == ["api", "backend-test", "migrate", "postgres", "web", "worker"] and
		.services["backend-test"].profiles == ["test"]
	' >/dev/null || fail "backend-test is not isolated behind the test profile"
}

static_test() {
	command -v ruby >/dev/null 2>&1 || fail "ruby is required for static validation"
	ruby -ryaml <<'RUBY'
def assert(condition, message)
  abort("stack static test: #{message}") unless condition
end

root = Dir.pwd
config = YAML.load_file(File.join(root, "compose.yaml"))
services = config.fetch("services")
assert(services.keys.sort == %w[api backend-test migrate postgres web worker], "unexpected service set")
assert(services.dig("postgres", "image") == "postgres:18.4-alpine", "PostgreSQL image is not pinned")
assert(services.dig("migrate", "depends_on", "postgres", "condition") == "service_healthy", "migrate ordering is invalid")
assert(services.dig("api", "depends_on", "migrate", "condition") == "service_completed_successfully", "API ordering is invalid")
assert(services.dig("worker", "depends_on", "migrate", "condition") == "service_completed_successfully", "Worker ordering is invalid")
assert(services.dig("web", "depends_on", "api", "condition") == "service_healthy", "Web ordering is invalid")
assert(!services.fetch("worker").key?("ports"), "Worker publishes a host port")
assert(!services.fetch("postgres").key?("ports"), "PostgreSQL publishes a host port")
assert(services.dig("backend-test", "profiles") == ["test"], "backend-test is not isolated behind the test profile")
assert(config.fetch("volumes").keys == ["postgres-data"], "unexpected named volumes")
assert(services.dig("migrate", "volumes").include?("./deploy/migrations:/migrations:ro"), "migrations are not mounted read-only")

backend = File.read(File.join(root, "backend/Dockerfile"))
assert(backend.include?("FROM golang:1.26.5-alpine AS build"), "Go build image is not pinned")
assert(backend.scan("CGO_ENABLED=0 go build").length == 3, "backend build does not produce three static binaries")
assert(backend.include?("FROM build AS test"), "backend test target is missing")
assert(backend.include?("FROM alpine:3.23.3 AS certificates"), "certificate image is not pinned")
assert(backend.include?("RUN apk add --no-cache ca-certificates"), "CA certificates are not installed")
assert(backend.include?("FROM alpine:3.23.3 AS runtime"), "backend runtime image is not pinned")
assert(backend.include?("COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt"), "CA bundle is not copied into the backend runtime")
assert(backend.include?("USER 10001:10001"), "backend runtime is not non-root")

web = File.read(File.join(root, "apps/web/Dockerfile"))
assert(web.include?("FROM node:24.18.0-alpine AS toolchain"), "Web build image is not pinned")
assert(web.include?("RUN pnpm install --frozen-lockfile"), "Web install is not frozen")
assert(web.include?("/src/apps/web/.next/standalone"), "Web standalone output is not copied")
assert(web.include?("/src/apps/web/.next/static"), "Web static output is not copied")
assert(web.include?("USER node"), "Web runtime is not non-root")

expected_env = {
  "POSTGRES_DB" => "artificial_brain",
  "POSTGRES_USER" => "artificial_brain",
  "POSTGRES_PASSWORD" => "local-development-only",
  "SERVICE_VERSION" => "dev",
  "API_HTTP_ADDRESS" => ":8080",
  "WORKER_HEALTH_ADDRESS" => ":8081",
  "WORKER_HEARTBEAT_INTERVAL" => "2s",
  "WORKER_LEASE_TTL" => "6s",
  "SHUTDOWN_TIMEOUT" => "10s",
}
actual_env = File.readlines(File.join(root, ".env.example"), chomp: true).to_h do |line|
  line.split("=", 2)
end
assert(actual_env == expected_env, ".env.example defaults are incomplete or unexpected")
RUBY
}

compose() {
	docker compose --project-name "$project" "$@"
}

redact_logs() {
	sh scripts/redact-logs.sh
}

redaction_test() {
	postgres_scheme=postgresql
	redacted=$(
		printf '%s\n' \
			"uri=${postgres_scheme}://redaction-user:uri-sentinel@postgres:5432/database" \
			'{"password":"json-password-sentinel","api_key": "json-api-key-sentinel","token":"json-token-sentinel","client_secret":"escaped-\"json-sentinel"}' \
			"DATABASE_URL=${postgres_scheme}://redaction-user:database-sentinel@postgres:5432/database" \
			'PASSWORD=plain-password-sentinel' | redact_logs
	)
	expected=$(
		printf '%s\n' \
			"uri=${postgres_scheme}://[REDACTED]@postgres:5432/database" \
			'{"password":"[REDACTED]","api_key": "[REDACTED]","token":"[REDACTED]","client_secret":"[REDACTED]"}' \
			'DATABASE_URL=[REDACTED]' \
			'PASSWORD=[REDACTED]'
	)
	[ "$redacted" = "$expected" ] || fail "log redaction contract failed"
}

wait_for_system_state() {
	wanted_overall=$1
	wanted_worker=$2
	seconds=$3
	deadline=$(($(date +%s) + seconds))
	while :; do
		report=$(curl --fail --silent --show-error --max-time 3 \
			"http://127.0.0.1:${API_PORT}/api/v1/system/health" 2>/dev/null || true)
		if printf '%s\n' "$report" | jq -e \
			--arg overall "$wanted_overall" \
			--arg worker "$wanted_worker" \
			'.status == $overall and .components.worker.status == $worker' >/dev/null 2>&1; then
			return 0
		fi
		if [ "$(date +%s)" -ge "$deadline" ]; then
			printf 'stack test: timed out waiting for overall=%s worker=%s\n' \
				"$wanted_overall" "$wanted_worker" >&2
			return 1
		fi
		sleep 1
	done
}

assert_page() {
	wanted_status=$1
	page=$(curl --fail --silent --show-error --max-time 5 "http://127.0.0.1:${WEB_PORT}/")
	printf '%s\n' "$page" | grep -F "data-system-status=\"${wanted_status}\"" >/dev/null || \
		fail "Web page did not render ${wanted_status}"
	for label in Web API PostgreSQL Worker; do
		printf '%s\n' "$page" | grep -F ">${label}<" >/dev/null || \
			fail "Web page did not render the ${label} label"
	done
}

full_stack_test() {
	config_test

	project=$(unique_project_name)
	API_PORT=0
	WEB_PORT=0
	export API_PORT WEB_PORT

	lease_ttl=${WORKER_LEASE_TTL:-6s}
	case "$lease_ttl" in
		*s)
			lease_seconds=${lease_ttl%s}
			case "$lease_seconds" in
				'' | *[!0-9]*) fail "WORKER_LEASE_TTL must be whole seconds for smoke testing" ;;
			esac
			;;
		*) fail "WORKER_LEASE_TTL must be whole seconds for smoke testing" ;;
	esac
	degradation_deadline=$((lease_seconds + 4))

	cleanup() {
		exit_code=$?
		trap - EXIT HUP INT TERM
		if [ "$exit_code" -ne 0 ]; then
			printf 'stack test: service state at failure\n' >&2
			compose ps --all >&2 || true
			printf 'stack test: redacted recent logs\n' >&2
			compose logs --no-color --tail 200 2>&1 | redact_logs >&2 || true
		fi
		compose down --volumes --remove-orphans >/dev/null 2>&1 || true
		exit "$exit_code"
	}
	trap cleanup EXIT HUP INT TERM

	compose up --build --detach --wait --wait-timeout "${STACK_WAIT_SECONDS:-180}"
	api_mapping=$(compose port api 8080 | sed -n '1p')
	web_mapping=$(compose port web 3000 | sed -n '1p')
	API_PORT=${api_mapping##*:}
	WEB_PORT=${web_mapping##*:}
	case "$API_PORT" in '' | *[!0-9]*) fail "API has no Docker-assigned host port" ;; esac
	case "$WEB_PORT" in '' | *[!0-9]*) fail "Web has no Docker-assigned host port" ;; esac
	[ "$API_PORT" -gt 0 ] || fail "API has no Docker-assigned host port"
	[ "$WEB_PORT" -gt 0 ] || fail "Web has no Docker-assigned host port"
	migrate_container=$(compose ps --all --quiet migrate)
	[ -n "$migrate_container" ] || fail "migrate container is missing"
	migrate_exit=$(docker inspect --format '{{.State.ExitCode}}' "$migrate_container")
	[ "$migrate_exit" = 0 ] || fail "migrate exited with status ${migrate_exit}"

	sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${API_PORT}/health/live" 30
	sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${API_PORT}/health/ready" 30
	sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${WEB_PORT}/health/live" 30
	wait_for_system_state healthy healthy 30
	assert_page healthy

	compose stop --timeout 15 worker
	wait_for_system_state degraded unavailable "$degradation_deadline"
	sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${WEB_PORT}/health/live" 10
	assert_page degraded

	compose start worker
	wait_for_system_state healthy healthy "$degradation_deadline"
	assert_page healthy
}

case "${1:-}" in
	--project-name-only)
		unique_project_name
		;;
	--config-only)
		config_test
		;;
	--static-only)
		static_test
		;;
	--redaction-test)
		redaction_test
		;;
	'')
		full_stack_test
		;;
	*)
		fail "unknown argument: $1"
		;;
esac
