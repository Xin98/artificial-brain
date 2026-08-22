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

api_env = services.dig("api", "environment")
worker_env = services.dig("worker", "environment")
assert(api_env.fetch("DEPLOYMENT_MODE") == "${DEPLOYMENT_MODE:-cloud}", "api DEPLOYMENT_MODE passthrough does not default to cloud")
assert(worker_env.fetch("DEPLOYMENT_MODE") == "${DEPLOYMENT_MODE:-cloud}", "worker DEPLOYMENT_MODE passthrough does not default to cloud")
assert(api_env.key?("PRIVATE_ADMIN_PHONE"), "api does not receive PRIVATE_ADMIN_PHONE")
assert(worker_env.key?("PRIVATE_ADMIN_PHONE"), "worker does not receive PRIVATE_ADMIN_PHONE")
assert(api_env.key?("PORTABILITY_MAX_BUNDLE_BYTES"), "api does not receive PORTABILITY_MAX_BUNDLE_BYTES")

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
  "APP_ENV" => "development",
  "DEV_INBOX_ENABLED" => "true",
  "MODEL_ADAPTER" => "deterministic",
  "REMINDER_EMAIL_ADAPTER" => "fake",
  "REMINDER_SMS_ADAPTER" => "fake",
  "REMINDER_RECEIPT_SECRET" => "local-development-only",
  "REMINDER_DEV_OUTBOX_ENABLED" => "true",
  "REMINDER_QUEUE_EMAIL_CONCURRENCY" => "2",
  "REMINDER_QUEUE_SMS_CONCURRENCY" => "2",
  "REMINDER_JOB_MAX_ATTEMPTS" => "5",
  "DEPLOYMENT_MODE" => "cloud",
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
	page_path=$2
	page=$(curl --fail --silent --show-error --max-time 5 "http://127.0.0.1:${WEB_PORT}${page_path}")
	printf '%s\n' "$page" | grep -F "data-system-status=\"${wanted_status}\"" >/dev/null || \
		fail "Web page ${page_path} did not render ${wanted_status}"
	for label in Web API PostgreSQL Worker; do
		printf '%s\n' "$page" | grep -F ">${label}<" >/dev/null || \
			fail "Web page ${page_path} did not render the ${label} label"
	done
}

# verify_contact_channel registers a contact channel for the authenticated
# user, reads the verification code from the gated dev inbox, and verifies
# the channel. It reuses the session cookie and ports set by full_stack_test.
verify_contact_channel() {
	channel_kind=$1
	channel_address=$2
	channel_address_uri=$3
	channel_response=$(curl --silent --show-error --max-time 5 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--data "{\"kind\":\"${channel_kind}\",\"address\":\"${channel_address}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/settings/contact-channels")
	channel_status=$(printf '%s\n' "$channel_response" | tail -n 1)
	channel_body=$(printf '%s\n' "$channel_response" | sed '$d')
	[ "$channel_status" = 201 ] || \
		fail "contact channel create (${channel_kind}) status ${channel_status}, want 201: ${channel_body}"
	channel_id=$(printf '%s\n' "$channel_body" | jq -r '.id')
	[ -n "$channel_id" ] && [ "$channel_id" != null ] || \
		fail "contact channel create (${channel_kind}) did not return an id: ${channel_body}"
	channel_inbox=$(curl --fail --silent --show-error --max-time 5 \
		"http://127.0.0.1:${WEB_PORT}/api/v1/dev/sms-inbox?address=${channel_address_uri}")
	channel_code=$(printf '%s\n' "$channel_inbox" | jq -r '.messages[0].code')
	case "$channel_code" in '' | *[!0-9]*) fail "dev inbox (${channel_kind}) did not return a numeric code" ;; esac
	channel_verified=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--data "{\"code\":\"${channel_code}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/settings/contact-channels/${channel_id}/verify")
	printf '%s\n' "$channel_verified" | jq -e '.verified == true' >/dev/null || \
		fail "contact channel verify (${channel_kind}) did not verify: ${channel_verified}"
}

full_stack_test() {
	config_test

	project=$(unique_project_name)
	API_PORT=0
	WEB_PORT=0
	# Smoke caps the bundle size at the configuration floor (1 MiB) so the
	# oversized-upload rejection can be exercised with a small dd-padded file
	# instead of padding past the 32 MiB default.
	PORTABILITY_MAX_BUNDLE_BYTES=1048576
	export API_PORT WEB_PORT PORTABILITY_MAX_BUNDLE_BYTES

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
		docker compose --project-name "${project}-private" down \
			--volumes --remove-orphans >/dev/null 2>&1 || true
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
	assert_page healthy /status

	# ITER-0002 authenticated end-to-end loop through the web /api/v1 rewrite.
	e2e_phone="+8613800137001"
	e2e_request=$(curl --fail --silent --show-error --max-time 5 \
		--output /dev/null --write-out '%{http_code}' \
		--header 'Content-Type: application/json' \
		--data "{\"phone\":\"${e2e_phone}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/auth/login/request")
	[ "$e2e_request" = 202 ] || fail "login request status ${e2e_request}, want 202"

	e2e_inbox=$(curl --fail --silent --show-error --max-time 5 \
		"http://127.0.0.1:${WEB_PORT}/api/v1/dev/sms-inbox?address=$(printf '%s' "$e2e_phone" | jq -sRr @uri)")
	e2e_code=$(printf '%s\n' "$e2e_inbox" | jq -r '.messages[0].code')
	case "$e2e_code" in '' | *[!0-9]*) fail "dev inbox did not return a numeric code" ;; esac

	e2e_verify_headers=$(curl --fail --silent --show-error --max-time 5 \
		--dump-header - --output /dev/null \
		--header 'Content-Type: application/json' \
		--data "{\"phone\":\"${e2e_phone}\",\"code\":\"${e2e_code}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/auth/login/verify")
	e2e_session=$(printf '%s\n' "$e2e_verify_headers" |
		sed -n 's/^[Ss]et-[Cc]ookie: ab_session=\([^;]*\).*$/\1/p' | sed -n '1p')
	[ -n "$e2e_session" ] || fail "verify did not set the ab_session cookie"
	e2e_auth="Cookie: ab_session=${e2e_session}"

	e2e_home=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" "http://127.0.0.1:${WEB_PORT}/")
	printf '%s\n' "$e2e_home" | grep -F 'data-page="dashboard"' >/dev/null || \
		fail "authenticated home did not render the dashboard page"

	e2e_due=$(date -u -d '+1 hour' +%Y-%m-%dT%H:%M:%SZ)
	e2e_create=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--data "{\"title\":\"冒烟待办\",\"dueAtUtc\":\"${e2e_due}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/todos")
	e2e_todo_id=$(printf '%s\n' "$e2e_create" | jq -r '.id')
	[ -n "$e2e_todo_id" ] && [ "$e2e_todo_id" != null ] || \
		fail "todo create did not return an id: ${e2e_create}"

	e2e_dashboard=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/dashboard/summary?timezone=UTC")
	printf '%s\n' "$e2e_dashboard" | jq -e '.pendingTotal >= 1' >/dev/null || \
		fail "dashboard pendingTotal is not at least 1: ${e2e_dashboard}"

	e2e_message=$(curl --fail --silent --show-error --max-time 10 \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--data '{"text":"明天下午三点提醒我提交周报","timezone":"Asia/Shanghai"}' \
		"http://127.0.0.1:${WEB_PORT}/api/v1/conversation/messages")
	printf '%s\n' "$e2e_message" | jq -e '.kind == "todo_created"' >/dev/null || \
		fail "conversation did not create the todo: ${e2e_message}"

	plan_count=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from reminder.reminder_plans where status = 'planned'")
	[ "$plan_count" -ge 1 ] || \
		fail "expected at least one planned reminder plan, got ${plan_count}"

	e2e_confirmation=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--data "{\"intent\":\"todo.delete\",\"todoId\":\"${e2e_todo_id}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/confirmations")
	e2e_confirmation_id=$(printf '%s\n' "$e2e_confirmation" | jq -r '.confirmationId')
	[ -n "$e2e_confirmation_id" ] && [ "$e2e_confirmation_id" != null ] || \
		fail "confirmation create did not return an id: ${e2e_confirmation}"

	e2e_confirmed=$(curl --fail --silent --show-error --max-time 5 \
		--output /dev/null --write-out '%{http_code}' \
		--header "$e2e_auth" --header 'Content-Type: application/json' --data '{}' \
		"http://127.0.0.1:${WEB_PORT}/api/v1/confirmations/${e2e_confirmation_id}/confirm")
	[ "$e2e_confirmed" = 200 ] || fail "confirm status ${e2e_confirmed}, want 200"

	e2e_remaining=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" --get --data-urlencode "keyword=冒烟待办" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/todos")
	printf '%s\n' "$e2e_remaining" | jq -e '.todos | length == 0' >/dev/null || \
		fail "deleted todo still listed: ${e2e_remaining}"

	compose stop --timeout 15 worker
	wait_for_system_state degraded unavailable "$degradation_deadline"
	sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${WEB_PORT}/health/live" 10
	assert_page degraded /status

	compose start worker
	wait_for_system_state healthy healthy "$degradation_deadline"
	assert_page healthy /status

	# ITER-0003 reminder delivery end-to-end loop, reusing the authenticated
	# session from the ITER-0002 block: verified channels deliver a due todo
	# through the fake adapters into the gated dev outbox, a todo completed
	# before its due instant never delivers, a signed provider receipt is
	# recorded, the ops snapshot reflects both queues, and delivery recovers
	# after a worker restart.
	e2e_email="smoke@example.com"
	e2e_sms="+8613800137002"
	e2e_email_uri=$(printf '%s' "$e2e_email" | jq -sRr @uri)
	e2e_sms_uri=$(printf '%s' "$e2e_sms" | jq -sRr @uri)

	verify_contact_channel email "$e2e_email" "$e2e_email_uri"
	verify_contact_channel sms "$e2e_sms" "$e2e_sms_uri"

	e2e_rem_due=$(date -u -d '+5 seconds' +%Y-%m-%dT%H:%M:%SZ)
	e2e_rem_response=$(curl --silent --show-error --max-time 5 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--data "{\"title\":\"冒烟提醒\",\"dueAtUtc\":\"${e2e_rem_due}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/todos")
	e2e_rem_status=$(printf '%s\n' "$e2e_rem_response" | tail -n 1)
	e2e_rem_create=$(printf '%s\n' "$e2e_rem_response" | sed '$d')
	[ "$e2e_rem_status" = 201 ] || \
		fail "reminder todo create status ${e2e_rem_status}, want 201: ${e2e_rem_create}"
	e2e_rem_todo_id=$(printf '%s\n' "$e2e_rem_create" | jq -r '.id')
	[ -n "$e2e_rem_todo_id" ] && [ "$e2e_rem_todo_id" != null ] || \
		fail "reminder todo create did not return an id: ${e2e_rem_create}"

	e2e_deadline=$(( $(date +%s) + 30 ))
	while :; do
		e2e_rem_outbox=$(curl --fail --silent --show-error --max-time 5 \
			"http://127.0.0.1:${WEB_PORT}/api/v1/dev/reminder-outbox?address=${e2e_email_uri}")
		if printf '%s\n' "$e2e_rem_outbox" | jq -e --arg todoId "$e2e_rem_todo_id" \
			'.messages[] | select(.todoId == $todoId and (.body | contains("冒烟提醒")))' \
			>/dev/null 2>&1; then
			break
		fi
		[ "$(date +%s)" -lt "$e2e_deadline" ] || \
			fail "fake outbox did not receive 冒烟提醒 within 30s"
		sleep 1
	done

	e2e_deadline=$(( $(date +%s) + 30 ))
	while :; do
		e2e_rem_list=$(curl --fail --silent --show-error --max-time 5 \
			--header "$e2e_auth" "http://127.0.0.1:${WEB_PORT}/api/v1/reminders")
		if printf '%s\n' "$e2e_rem_list" | jq -e --arg todoId "$e2e_rem_todo_id" '
			[.deliveries[] | select(.todoId == $todoId)] as $rows |
			($rows | length == 2) and
			([$rows[] | select(.state == "succeeded")] | length == 2) and
			(([$rows[] | .channel] | sort) == ["email", "sms"])
		' >/dev/null 2>&1; then
			break
		fi
		[ "$(date +%s)" -lt "$e2e_deadline" ] || \
			fail "reminder deliveries did not reach two succeeded rows within 30s: ${e2e_rem_list}"
		sleep 1
	done
	e2e_rem_provider_id=$(printf '%s\n' "$e2e_rem_list" | jq -r --arg todoId "$e2e_rem_todo_id" \
		'.deliveries[] | select(.todoId == $todoId and .channel == "sms") | .providerMessageId')
	[ -n "$e2e_rem_provider_id" ] && [ "$e2e_rem_provider_id" != null ] || \
		fail "succeeded sms delivery has no providerMessageId: ${e2e_rem_list}"

	e2e_suppress_due_epoch=$(( $(date +%s) + 10 ))
	e2e_suppress_due=$(date -u -d "@${e2e_suppress_due_epoch}" +%Y-%m-%dT%H:%M:%SZ)
	e2e_suppress_response=$(curl --silent --show-error --max-time 5 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--data "{\"title\":\"冒烟抑制\",\"dueAtUtc\":\"${e2e_suppress_due}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/todos")
	e2e_suppress_status=$(printf '%s\n' "$e2e_suppress_response" | tail -n 1)
	e2e_suppress_create=$(printf '%s\n' "$e2e_suppress_response" | sed '$d')
	[ "$e2e_suppress_status" = 201 ] || \
		fail "suppression todo create status ${e2e_suppress_status}, want 201: ${e2e_suppress_create}"
	e2e_suppress_id=$(printf '%s\n' "$e2e_suppress_create" | jq -r '.id')
	[ -n "$e2e_suppress_id" ] && [ "$e2e_suppress_id" != null ] || \
		fail "suppression todo create did not return an id: ${e2e_suppress_create}"
	e2e_suppress_complete=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--data '{"version":1}' \
		"http://127.0.0.1:${WEB_PORT}/api/v1/todos/${e2e_suppress_id}/complete")
	printf '%s\n' "$e2e_suppress_complete" | jq -e '.status == "completed"' >/dev/null || \
		fail "suppression todo complete did not report completed: ${e2e_suppress_complete}"

	# A todo completed before its due instant must never deliver. Revoke (D9)
	# finalizes every still-scheduled delivery as suppressed with the
	# todo_completed reason inside the caller's transaction, so every row for
	# the completed todo must settle at suppressed(todo_completed); the due
	# instant then passes without any fake outbox message.
	e2e_deadline=$(( $(date +%s) + 30 ))
	while :; do
		e2e_suppress_unfinalized=$(compose exec -T postgres psql \
			--username "${POSTGRES_USER:-artificial_brain}" \
			--dbname "${POSTGRES_DB:-artificial_brain}" \
			--tuples-only --no-align \
			--command "select count(*) from reminder.reminder_deliveries where todo_id='${e2e_suppress_id}' and not (state = 'suppressed' and suppression_reason = 'todo_completed')")
		if [ "$e2e_suppress_unfinalized" = 0 ] && [ "$(date +%s)" -ge $((e2e_suppress_due_epoch + 3)) ]; then
			break
		fi
		[ "$(date +%s)" -lt "$e2e_deadline" ] || \
			fail "completed todo 冒烟抑制 did not finalize every delivery as suppressed(todo_completed): ${e2e_suppress_unfinalized}"
		sleep 1
	done
	e2e_suppress_rows=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from reminder.reminder_deliveries where todo_id='${e2e_suppress_id}'")
	[ "$e2e_suppress_rows" = 2 ] || \
		fail "expected two delivery rows for the completed todo, got ${e2e_suppress_rows}"
	for e2e_address_uri in "$e2e_email_uri" "$e2e_sms_uri"; do
		e2e_rem_outbox=$(curl --fail --silent --show-error --max-time 5 \
			"http://127.0.0.1:${WEB_PORT}/api/v1/dev/reminder-outbox?address=${e2e_address_uri}")
		if printf '%s\n' "$e2e_rem_outbox" | jq -e \
			'.messages[] | select(.body | contains("冒烟抑制"))' >/dev/null 2>&1; then
			fail "completed todo 冒烟抑制 reached the fake outbox"
		fi
	done

	e2e_receipt_body="{\"providerMessageId\":\"${e2e_rem_provider_id}\",\"delivered\":true}"
	e2e_receipt_signature=$(printf '%s' "$e2e_receipt_body" | \
		openssl dgst -sha256 -hmac 'local-development-only' | awk '{print $NF}')
	e2e_receipt_status=$(curl --silent --show-error --max-time 5 \
		--output /dev/null --write-out '%{http_code}' \
		--header "X-Receipt-Signature: ${e2e_receipt_signature}" \
		--header 'Content-Type: application/json' \
		--data "$e2e_receipt_body" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/webhooks/receipts/sms")
	[ "$e2e_receipt_status" = 200 ] || fail "receipt webhook status ${e2e_receipt_status}, want 200"
	e2e_rem_list=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" "http://127.0.0.1:${WEB_PORT}/api/v1/reminders")
	printf '%s\n' "$e2e_rem_list" | jq -e --arg todoId "$e2e_rem_todo_id" '
		.deliveries[] | select(.todoId == $todoId and .channel == "sms") |
		.receiptState == "received_ok"
	' >/dev/null || fail "sms delivery receipt was not recorded: ${e2e_rem_list}"

	e2e_rem_ops=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" "http://127.0.0.1:${WEB_PORT}/api/v1/ops/reminder")
	printf '%s\n' "$e2e_rem_ops" | jq -e '.queues | length == 2' >/dev/null || \
		fail "reminder ops does not list two queues: ${e2e_rem_ops}"
	printf '%s\n' "$e2e_rem_ops" | jq -e '.deliveries.succeeded >= 2' >/dev/null || \
		fail "reminder ops succeeded count is below 2: ${e2e_rem_ops}"

	compose restart worker
	wait_for_system_state healthy healthy 30
	assert_page healthy /status

	e2e_recovery_due=$(date -u -d '+5 seconds' +%Y-%m-%dT%H:%M:%SZ)
	e2e_recovery_response=$(curl --silent --show-error --max-time 5 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--data "{\"title\":\"冒烟恢复\",\"dueAtUtc\":\"${e2e_recovery_due}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/todos")
	e2e_recovery_status=$(printf '%s\n' "$e2e_recovery_response" | tail -n 1)
	e2e_recovery_create=$(printf '%s\n' "$e2e_recovery_response" | sed '$d')
	[ "$e2e_recovery_status" = 201 ] || \
		fail "recovery todo create status ${e2e_recovery_status}, want 201: ${e2e_recovery_create}"
	e2e_recovery_id=$(printf '%s\n' "$e2e_recovery_create" | jq -r '.id')
	[ -n "$e2e_recovery_id" ] && [ "$e2e_recovery_id" != null ] || \
		fail "recovery todo create did not return an id: ${e2e_recovery_create}"

	e2e_deadline=$(( $(date +%s) + 30 ))
	while :; do
		e2e_rem_outbox=$(curl --fail --silent --show-error --max-time 5 \
			"http://127.0.0.1:${WEB_PORT}/api/v1/dev/reminder-outbox?address=${e2e_email_uri}")
		if printf '%s\n' "$e2e_rem_outbox" | jq -e --arg todoId "$e2e_recovery_id" \
			'.messages[] | select(.todoId == $todoId and (.body | contains("冒烟恢复")))' \
			>/dev/null 2>&1; then
			break
		fi
		[ "$(date +%s)" -lt "$e2e_deadline" ] || \
			fail "fake outbox did not receive 冒烟恢复 within 30s after the worker restart"
		sleep 1
	done

	# ITER-0004 data portability: export the workspace's full history as a
	# zip bundle, then import the same bundle back. Locally created rows
	# carry no source identity, so a self-import creates copies on the first
	# confirm and is idempotent (every record skipped) from the second run on
	# (assumption A3). Imports never plan reminders. The exporting user's
	# channels already exist, so every channel record downgrades to skipped
	# and its source record registers against the existing row (T9). The
	# stack above runs with the bundle cap lowered to the configuration
	# floor (PORTABILITY_MAX_BUNDLE_BYTES=1048576) so the oversized-upload
	# rejection only needs a small dd-padded file.
	port_bundle=$(mktemp) || fail "cannot create a temporary bundle file"
	port_export_status=$(curl --silent --show-error --max-time 10 \
		--output "$port_bundle" --write-out '%{http_code}' \
		--header "$e2e_auth" --request POST \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/export")
	[ "$port_export_status" = 200 ] || \
		fail "portability export status ${port_export_status}, want 200"

	port_entries=$(unzip -Z1 "$port_bundle" | sort)
	port_expected_entries=$(printf '%s\n' \
		manifest.json preferences.json reminder-deliveries.json todos.csv todos.json)
	[ "$port_entries" = "$port_expected_entries" ] || \
		fail "export bundle entries are not the expected five: $(printf '%s' "$port_entries" | tr '\n' ' ')"

	port_manifest=$(unzip -p "$port_bundle" manifest.json) || \
		fail "unzip cannot read manifest.json from the export bundle"
	printf '%s\n' "$port_manifest" | jq -e '.schemaVersion == "1"' >/dev/null || \
		fail "export manifest schemaVersion is not 1: ${port_manifest}"
	printf '%s\n' "$port_manifest" | jq -e '.counts.todos >= 1' >/dev/null || \
		fail "export manifest does not report at least one todo: ${port_manifest}"
	port_todos=$(printf '%s\n' "$port_manifest" | jq -r '.counts.todos')
	port_deliveries=$(printf '%s\n' "$port_manifest" | jq -r '.counts.deliveries')
	port_channels=$(printf '%s\n' "$port_manifest" | jq -r '.counts.channels')
	port_total=$((port_todos + port_deliveries + port_channels))

	port_upload=$(curl --silent --show-error --max-time 10 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" \
		--form "bundle=@${port_bundle}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/imports")
	port_upload_status=$(printf '%s\n' "$port_upload" | tail -n 1)
	port_upload_body=$(printf '%s\n' "$port_upload" | sed '$d')
	[ "$port_upload_status" = 201 ] || \
		fail "portability upload status ${port_upload_status}, want 201: ${port_upload_body}"
	port_import_id=$(printf '%s\n' "$port_upload_body" | jq -r '.importId')
	[ -n "$port_import_id" ] && [ "$port_import_id" != null ] || \
		fail "portability upload did not return an importId: ${port_upload_body}"
	printf '%s\n' "$port_upload_body" | jq -e --argjson total "$port_total" '
		.preview.new == $total and
		.preview.skipped == 0 and
		.preview.conflicts == 0 and
		.preview.invalid == 0
	' >/dev/null || \
		fail "first upload preview does not classify every record as new: ${port_upload_body}"

	port_todos_before=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from todo.todos")
	port_plans_before=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from reminder.reminder_plans")
	port_channels_list=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/settings/contact-channels")
	port_existing_channel_id=$(printf '%s\n' "$port_channels_list" | \
		jq -r --arg address "$e2e_email" '.channels[] | select(.address == $address) | .id')
	[ -n "$port_existing_channel_id" ] && [ "$port_existing_channel_id" != null ] || \
		fail "settings listing carries no channel for ${e2e_email}: ${port_channels_list}"

	port_confirm=$(curl --silent --show-error --max-time 10 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" --request POST \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/imports/${port_import_id}/confirm")
	port_confirm_status=$(printf '%s\n' "$port_confirm" | tail -n 1)
	port_report=$(printf '%s\n' "$port_confirm" | sed '$d')
	[ "$port_confirm_status" = 200 ] || \
		fail "portability confirm status ${port_confirm_status}, want 200: ${port_report}"
	# Self-import executes against a user whose channels already exist: each
	# duplicate (user, kind, address) channel downgrades to skipped and
	# registers against the existing row (T9), so the executed report copies
	# every todo and delivery but skips every channel.
	port_new_expected=$((port_todos + port_deliveries))
	printf '%s\n' "$port_report" | jq -e \
		--argjson new "$port_new_expected" --argjson skipped "$port_channels" '
		.new == $new and
		.skipped == $skipped and
		.conflicts == 0 and
		.invalid == 0
	' >/dev/null || \
		fail "first confirm report does not match the self-import contract: ${port_report}"

	port_todos_after=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from todo.todos")
	[ $((port_todos_after - port_todos_before)) -eq "$port_todos" ] || \
		fail "todo count grew by $((port_todos_after - port_todos_before)), want ${port_todos}"
	port_plans_after=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from reminder.reminder_plans")
	[ "$port_plans_after" = "$port_plans_before" ] || \
		fail "import changed the reminder plan count: ${port_plans_before} -> ${port_plans_after}"
	port_imported_rows=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from reminder.reminder_deliveries where origin = 'imported'")
	[ "$port_imported_rows" = "$port_deliveries" ] || \
		fail "imported delivery rows are ${port_imported_rows}, want ${port_deliveries}"

	# Downgraded channels register their source record against the EXISTING
	# channel row and never create a copy: resolve one bundle channel id and
	# prove the mapping lands on the pre-existing channel, and that the
	# settings listing did not gain an unverified duplicate.
	port_bundle_channel_id=$(unzip -p "$port_bundle" preferences.json | \
		jq -r --arg address "$e2e_email" '.[] | select(.address == $address) | .id')
	[ -n "$port_bundle_channel_id" ] && [ "$port_bundle_channel_id" != null ] || \
		fail "bundle preferences.json carries no channel for ${e2e_email}"
	port_channel_target=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select target_id from portability.portability_source_records where source_record_id = '${port_bundle_channel_id}' and target_kind = 'channel'")
	[ "$port_channel_target" = "$port_existing_channel_id" ] || \
		fail "downgraded channel ${port_bundle_channel_id} is not registered against ${port_existing_channel_id}: ${port_channel_target}"
	port_channels_after=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/settings/contact-channels")
	printf '%s\n' "$port_channels_after" | jq -e --arg address "$e2e_email" \
		'[.channels[] | select(.address == $address)] | length == 1' \
		>/dev/null || \
		fail "self-import duplicated the channel ${e2e_email}: ${port_channels_after}"

	port_reminder_copy=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select target_id from portability.portability_source_records where source_record_id = '${e2e_rem_todo_id}' and target_kind = 'todo'")
	[ -n "$port_reminder_copy" ] || \
		fail "source records do not map the imported copy of todo ${e2e_rem_todo_id}"
	port_rem_list=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" "http://127.0.0.1:${WEB_PORT}/api/v1/reminders")
	printf '%s\n' "$port_rem_list" | jq -e --arg todoId "$port_reminder_copy" '
		[.deliveries[] | select(.todoId == $todoId)] as $rows |
		($rows | length == 2) and
		([$rows[] | select(.state == "succeeded")] | length == 2) and
		(([$rows[] | .channel] | sort) == ["email", "sms"])
	' >/dev/null || \
		fail "imported reminder history is not preserved for ${port_reminder_copy}: ${port_rem_list}"

	port_view=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/imports/${port_import_id}")
	printf '%s\n' "$port_view" | jq -e --argjson new "$port_new_expected" '
		.state == "committed" and .report.new == $new
	' >/dev/null || \
		fail "import view does not report the committed report: ${port_view}"

	port_reupload=$(curl --silent --show-error --max-time 10 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" \
		--form "bundle=@${port_bundle}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/imports")
	port_reupload_status=$(printf '%s\n' "$port_reupload" | tail -n 1)
	port_reupload_body=$(printf '%s\n' "$port_reupload" | sed '$d')
	[ "$port_reupload_status" = 201 ] || \
		fail "portability re-upload status ${port_reupload_status}, want 201: ${port_reupload_body}"
	port_reimport_id=$(printf '%s\n' "$port_reupload_body" | jq -r '.importId')
	[ -n "$port_reimport_id" ] && [ "$port_reimport_id" != null ] || \
		fail "portability re-upload did not return an importId: ${port_reupload_body}"
	printf '%s\n' "$port_reupload_body" | jq -e --argjson total "$port_total" '
		.preview.new == 0 and
		.preview.skipped == $total and
		.preview.conflicts == 0 and
		.preview.invalid == 0
	' >/dev/null || \
		fail "re-upload preview does not classify every record as skipped: ${port_reupload_body}"

	port_todos_rerun=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from todo.todos")
	port_reconfirm=$(curl --silent --show-error --max-time 10 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" --request POST \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/imports/${port_reimport_id}/confirm")
	port_reconfirm_status=$(printf '%s\n' "$port_reconfirm" | tail -n 1)
	port_rerun_report=$(printf '%s\n' "$port_reconfirm" | sed '$d')
	[ "$port_reconfirm_status" = 200 ] || \
		fail "portability re-confirm status ${port_reconfirm_status}, want 200: ${port_rerun_report}"
	printf '%s\n' "$port_rerun_report" | jq -e --argjson total "$port_total" '
		.new == 0 and
		.skipped == $total and
		.conflicts == 0 and
		.invalid == 0
	' >/dev/null || \
		fail "re-confirm report is not fully skipped: ${port_rerun_report}"
	port_todos_rerun_after=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from todo.todos")
	[ "$port_todos_rerun_after" = "$port_todos_rerun" ] || \
		fail "idempotent re-import changed the todo count: ${port_todos_rerun} -> ${port_todos_rerun_after}"

	# Conflict on change: mutate the SOURCE todo after it was imported so its
	# content fingerprint changes, then re-export and import the fresh bundle.
	# The registered source record classifies as conflict in the preview and
	# in the confirmed report, and the previously imported copy keeps the
	# original title — a conflict is skipped and reported, never applied. The
	# fresh bundle also carries the first import's copies, which carry no
	# source identity and therefore classify as new.
	port_coc_source=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/todos/${e2e_rem_todo_id}")
	port_coc_version=$(printf '%s\n' "$port_coc_source" | jq -r '.version')
	case "$port_coc_version" in '' | *[!0-9]*) fail "source todo ${e2e_rem_todo_id} has no numeric version: ${port_coc_source}" ;; esac
	port_coc_title="冒烟提醒-冲突演练"
	port_coc_patch=$(curl --silent --show-error --max-time 5 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--request PATCH \
		--data "{\"version\":${port_coc_version},\"title\":\"${port_coc_title}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/todos/${e2e_rem_todo_id}")
	port_coc_patch_status=$(printf '%s\n' "$port_coc_patch" | tail -n 1)
	port_coc_patch_body=$(printf '%s\n' "$port_coc_patch" | sed '$d')
	[ "$port_coc_patch_status" = 200 ] || \
		fail "source todo PATCH status ${port_coc_patch_status}, want 200: ${port_coc_patch_body}"
	printf '%s\n' "$port_coc_patch_body" | jq -e --arg title "$port_coc_title" \
		'.title == $title' >/dev/null || \
		fail "source todo PATCH did not apply the mutated title: ${port_coc_patch_body}"

	port_coc_bundle=$(mktemp) || fail "cannot create a temporary conflict bundle file"
	port_coc_export_status=$(curl --silent --show-error --max-time 10 \
		--output "$port_coc_bundle" --write-out '%{http_code}' \
		--header "$e2e_auth" --request POST \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/export")
	[ "$port_coc_export_status" = 200 ] || \
		fail "conflict re-export status ${port_coc_export_status}, want 200"

	port_coc_upload=$(curl --silent --show-error --max-time 10 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" \
		--form "bundle=@${port_coc_bundle}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/imports")
	port_coc_upload_status=$(printf '%s\n' "$port_coc_upload" | tail -n 1)
	port_coc_upload_body=$(printf '%s\n' "$port_coc_upload" | sed '$d')
	[ "$port_coc_upload_status" = 201 ] || \
		fail "conflict bundle upload status ${port_coc_upload_status}, want 201: ${port_coc_upload_body}"
	port_coc_import_id=$(printf '%s\n' "$port_coc_upload_body" | jq -r '.importId')
	[ -n "$port_coc_import_id" ] && [ "$port_coc_import_id" != null ] || \
		fail "conflict bundle upload did not return an importId: ${port_coc_upload_body}"
	printf '%s\n' "$port_coc_upload_body" | jq -e \
		--arg sourceRecordId "$e2e_rem_todo_id" \
		--argjson new "$port_new_expected" --argjson skipped "$((port_total - 1))" '
		.preview.new == $new and
		.preview.skipped == $skipped and
		.preview.conflicts >= 1 and
		.preview.invalid == 0 and
		([.preview.details[] | select(.kind == "todo" and .sourceRecordId == $sourceRecordId and .outcome == "conflict")] | length == 1)
	' >/dev/null || \
		fail "conflict bundle preview does not classify the mutated todo as conflict: ${port_coc_upload_body}"

	port_coc_confirm=$(curl --silent --show-error --max-time 10 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" --request POST \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/imports/${port_coc_import_id}/confirm")
	port_coc_confirm_status=$(printf '%s\n' "$port_coc_confirm" | tail -n 1)
	port_coc_report=$(printf '%s\n' "$port_coc_confirm" | sed '$d')
	[ "$port_coc_confirm_status" = 200 ] || \
		fail "conflict bundle confirm status ${port_coc_confirm_status}, want 200: ${port_coc_report}"
	printf '%s\n' "$port_coc_report" | jq -e \
		--arg sourceRecordId "$e2e_rem_todo_id" \
		--argjson new "$port_new_expected" --argjson skipped "$((port_total - 1))" '
		.new == $new and
		.skipped == $skipped and
		.conflicts >= 1 and
		.invalid == 0 and
		([.details[] | select(.kind == "todo" and .sourceRecordId == $sourceRecordId and .outcome == "conflict")] | length == 1)
	' >/dev/null || \
		fail "conflict bundle report does not report the mutated todo as conflict: ${port_coc_report}"

	port_coc_copy_title=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select title from todo.todos where id = '${port_reminder_copy}'")
	[ "$port_coc_copy_title" = 冒烟提醒 ] || \
		fail "conflict import overwrote the copied todo ${port_reminder_copy}: ${port_coc_copy_title}"
	port_coc_mutated_rows=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from todo.todos where title = '${port_coc_title}'")
	[ "$port_coc_mutated_rows" = 1 ] || \
		fail "mutated title appears in ${port_coc_mutated_rows} todo rows, want exactly 1 (the source row only)"
	rm -f "$port_coc_bundle"

	port_conflict=$(curl --silent --show-error --max-time 5 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" --request POST \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/imports/${port_import_id}/confirm")
	port_conflict_status=$(printf '%s\n' "$port_conflict" | tail -n 1)
	port_conflict_body=$(printf '%s\n' "$port_conflict" | sed '$d')
	[ "$port_conflict_status" = 409 ] || \
		fail "committed re-confirm status ${port_conflict_status}, want 409: ${port_conflict_body}"
	printf '%s\n' "$port_conflict_body" | jq -e '.code == "import_conflict"' >/dev/null || \
		fail "committed re-confirm code is not import_conflict: ${port_conflict_body}"

	port_garbage=$(mktemp) || fail "cannot create a temporary garbage file"
	printf 'not a bundle' >"$port_garbage"
	port_invalid=$(curl --silent --show-error --max-time 5 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" \
		--form "bundle=@${port_garbage}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/imports")
	port_invalid_status=$(printf '%s\n' "$port_invalid" | tail -n 1)
	port_invalid_body=$(printf '%s\n' "$port_invalid" | sed '$d')
	[ "$port_invalid_status" = 422 ] || \
		fail "non-zip upload status ${port_invalid_status}, want 422: ${port_invalid_body}"
	printf '%s\n' "$port_invalid_body" | jq -e '.code == "bundle_invalid"' >/dev/null || \
		fail "non-zip upload code is not bundle_invalid: ${port_invalid_body}"

	port_oversized=$(mktemp) || fail "cannot create a temporary oversized file"
	dd if=/dev/zero of="$port_oversized" bs=1024 count=1025 2>/dev/null || \
		fail "cannot pad the oversized bundle file"
	port_large=$(curl --silent --show-error --max-time 10 \
		--write-out '\n%{http_code}' \
		--header "$e2e_auth" \
		--form "bundle=@${port_oversized}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/portability/imports")
	port_large_status=$(printf '%s\n' "$port_large" | tail -n 1)
	port_large_body=$(printf '%s\n' "$port_large" | sed '$d')
	[ "$port_large_status" = 422 ] || \
		fail "oversized upload status ${port_large_status}, want 422: ${port_large_body}"
	printf '%s\n' "$port_large_body" | jq -e '.code == "bundle_too_large"' >/dev/null || \
		fail "oversized upload code is not bundle_too_large: ${port_large_body}"

	rm -f "$port_bundle" "$port_garbage" "$port_oversized"

	# ITER-0004 private mode: a second Compose project boots the same stack
	# with DEPLOYMENT_MODE=private. APP_ENV=development and
	# DEV_INBOX_ENABLED=true keep the fake adapters and the dev inbox, so CI
	# never calls a real provider (assumption A7). The fixed admin phone logs
	# in through the dev inbox; every other phone number is rejected with
	# registration_closed.
	private_project="${project}-private"
	DEPLOYMENT_MODE=private \
	PRIVATE_ADMIN_PHONE="+8613800137999" \
	APP_ENV=development \
	DEV_INBOX_ENABLED=true \
	API_PORT=0 \
	WEB_PORT=0 \
	docker compose --project-name "$private_project" up --build --detach --wait \
		--wait-timeout "${STACK_WAIT_SECONDS:-180}"
	private_web_mapping=$(docker compose --project-name "$private_project" port web 3000 | sed -n '1p')
	private_web_port=${private_web_mapping##*:}
	case "$private_web_port" in '' | *[!0-9]*) fail "private Web has no Docker-assigned host port" ;; esac

	private_admin_phone="+8613800137999"
	private_request_status=$(curl --fail --silent --show-error --max-time 5 \
		--output /dev/null --write-out '%{http_code}' \
		--header 'Content-Type: application/json' \
		--data "{\"phone\":\"${private_admin_phone}\"}" \
		"http://127.0.0.1:${private_web_port}/api/v1/auth/login/request")
	[ "$private_request_status" = 202 ] || \
		fail "private admin login request status ${private_request_status}, want 202"

	private_inbox=$(curl --fail --silent --show-error --max-time 5 \
		"http://127.0.0.1:${private_web_port}/api/v1/dev/sms-inbox?address=$(printf '%s' "$private_admin_phone" | jq -sRr @uri)")
	private_code=$(printf '%s\n' "$private_inbox" | jq -r '.messages[0].code')
	case "$private_code" in '' | *[!0-9]*) fail "private dev inbox did not return a numeric code" ;; esac

	private_verify_headers=$(curl --fail --silent --show-error --max-time 5 \
		--dump-header - --output /dev/null \
		--header 'Content-Type: application/json' \
		--data "{\"phone\":\"${private_admin_phone}\",\"code\":\"${private_code}\"}" \
		"http://127.0.0.1:${private_web_port}/api/v1/auth/login/verify")
	private_session=$(printf '%s\n' "$private_verify_headers" |
		sed -n 's/^[Ss]et-[Cc]ookie: ab_session=\([^;]*\).*$/\1/p' | sed -n '1p')
	[ -n "$private_session" ] || fail "private verify did not set the ab_session cookie"

	private_home=$(curl --fail --silent --show-error --max-time 5 \
		--header "Cookie: ab_session=${private_session}" \
		"http://127.0.0.1:${private_web_port}/")
	printf '%s\n' "$private_home" | grep -F 'data-page="dashboard"' >/dev/null || \
		fail "private admin home did not render the dashboard page"

	private_stranger_phone="+8613800137998"
	private_stranger=$(curl --silent --show-error --max-time 5 \
		--write-out '\n%{http_code}' \
		--header 'Content-Type: application/json' \
		--data "{\"phone\":\"${private_stranger_phone}\"}" \
		"http://127.0.0.1:${private_web_port}/api/v1/auth/login/request")
	private_stranger_status=$(printf '%s\n' "$private_stranger" | tail -n 1)
	private_stranger_body=$(printf '%s\n' "$private_stranger" | sed '$d')
	[ "$private_stranger_status" = 403 ] || \
		fail "private stranger login request status ${private_stranger_status}, want 403: ${private_stranger_body}"
	printf '%s\n' "$private_stranger_body" | jq -e '.code == "registration_closed"' >/dev/null || \
		fail "private stranger login code is not registration_closed: ${private_stranger_body}"

	docker compose --project-name "$private_project" down --volumes --remove-orphans

	# ITER-0004 backup/restore drill: archive the live database with
	# deploy/private/backup.sh, soft-delete the portability-imported copy todo
	# through the confirmation flow, restore the archive with
	# deploy/private/restore.sh, and prove the deleted row is back. A wrong
	# CONFIRM value must refuse before touching the database.
	port_backup_dir=$(mktemp -d) || fail "cannot create a backup directory"
	port_backup_archive=$(COMPOSE_PROJECT_NAME="$project" OUTPUT_DIR="$port_backup_dir" \
		sh deploy/private/backup.sh) || fail "backup.sh failed"
	[ -f "$port_backup_archive" ] || \
		fail "backup archive ${port_backup_archive} is missing"

	port_delete_confirmation=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" --header 'Content-Type: application/json' \
		--data "{\"intent\":\"todo.delete\",\"todoId\":\"${port_reminder_copy}\"}" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/confirmations")
	port_delete_confirmation_id=$(printf '%s\n' "$port_delete_confirmation" | jq -r '.confirmationId')
	[ -n "$port_delete_confirmation_id" ] && [ "$port_delete_confirmation_id" != null ] || \
		fail "backup delete confirmation create did not return an id: ${port_delete_confirmation}"
	port_deleted_status=$(curl --fail --silent --show-error --max-time 5 \
		--output /dev/null --write-out '%{http_code}' \
		--header "$e2e_auth" --header 'Content-Type: application/json' --data '{}' \
		"http://127.0.0.1:${WEB_PORT}/api/v1/confirmations/${port_delete_confirmation_id}/confirm")
	[ "$port_deleted_status" = 200 ] || \
		fail "backup delete confirm status ${port_deleted_status}, want 200"
	port_deleted_rows=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from todo.todos where id = '${port_reminder_copy}' and deleted_at is null")
	[ "$port_deleted_rows" = 0 ] || \
		fail "deleted import copy todo ${port_reminder_copy} is still active"

	COMPOSE_PROJECT_NAME="$project" BACKUP="$port_backup_archive" CONFIRM=restore \
		sh deploy/private/restore.sh || fail "restore.sh failed"
	# Compose restarts the stopped services (re-running the completed one-shot
	# migrate alongside them) and assigns FRESH ephemeral host ports, so both
	# ports must be re-resolved before waiting on them.
	api_mapping=$(compose port api 8080 | sed -n '1p')
	web_mapping=$(compose port web 3000 | sed -n '1p')
	API_PORT=${api_mapping##*:}
	WEB_PORT=${web_mapping##*:}
	case "$API_PORT" in '' | *[!0-9]*) fail "API has no Docker-assigned host port after the restore" ;; esac
	case "$WEB_PORT" in '' | *[!0-9]*) fail "Web has no Docker-assigned host port after the restore" ;; esac
	sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${API_PORT}/health/ready" 30
	wait_for_system_state healthy healthy 30
	sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${WEB_PORT}/health/live" 30
	port_restored_rows=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from todo.todos where id = '${port_reminder_copy}' and deleted_at is null")
	[ "$port_restored_rows" = 1 ] || \
		fail "restore did not bring back the deleted todo ${port_reminder_copy}"
	port_dashboard=$(curl --fail --silent --show-error --max-time 5 \
		--header "$e2e_auth" \
		"http://127.0.0.1:${WEB_PORT}/api/v1/dashboard/summary?timezone=UTC")
	printf '%s\n' "$port_dashboard" | jq -e '.pendingTotal >= 1' >/dev/null || \
		fail "dashboard after restore does not report pending todos: ${port_dashboard}"
	assert_page healthy /status

	if COMPOSE_PROJECT_NAME="$project" BACKUP="$port_backup_archive" CONFIRM=wrong \
		sh deploy/private/restore.sh >/dev/null 2>&1; then
		fail "restore.sh ran with CONFIRM=wrong"
	fi
	port_untouched_rows=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from todo.todos where id = '${port_reminder_copy}' and deleted_at is null")
	[ "$port_untouched_rows" = 1 ] || \
		fail "refused restore touched the database"
	rm -rf "$port_backup_dir"

	# ITER-0004 upgrade drill: rebuild and force-recreate the stack against
	# the existing database volume; the one-shot migrate re-runs idempotently
	# at the current schema version and no business row is lost.
	upgrade_todo_count=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from todo.todos")
	upgrade_delivery_count=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from reminder.reminder_deliveries")

	compose up --build --detach --wait --wait-timeout "${STACK_WAIT_SECONDS:-180}" --force-recreate
	migrate_container=$(compose ps --all --quiet migrate)
	[ -n "$migrate_container" ] || fail "migrate container is missing after the upgrade"
	migrate_exit=$(docker inspect --format '{{.State.ExitCode}}' "$migrate_container")
	[ "$migrate_exit" = 0 ] || fail "migrate exited with status ${migrate_exit} during the upgrade"

	api_mapping=$(compose port api 8080 | sed -n '1p')
	web_mapping=$(compose port web 3000 | sed -n '1p')
	API_PORT=${api_mapping##*:}
	WEB_PORT=${web_mapping##*:}
	case "$API_PORT" in '' | *[!0-9]*) fail "API has no Docker-assigned host port after the upgrade" ;; esac
	case "$WEB_PORT" in '' | *[!0-9]*) fail "Web has no Docker-assigned host port after the upgrade" ;; esac
	sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${API_PORT}/health/live" 30
	sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${API_PORT}/health/ready" 30
	sh tests/smoke/wait_for_url.sh "http://127.0.0.1:${WEB_PORT}/health/live" 30
	wait_for_system_state healthy healthy 30

	upgrade_schema_version=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select version from public.schema_version limit 1")
	[ "$upgrade_schema_version" = 8 ] || \
		fail "schema version after the upgrade is ${upgrade_schema_version}, want 8"
	upgrade_todo_after=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from todo.todos")
	upgrade_delivery_after=$(compose exec -T postgres psql \
		--username "${POSTGRES_USER:-artificial_brain}" \
		--dbname "${POSTGRES_DB:-artificial_brain}" \
		--tuples-only --no-align \
		--command "select count(*) from reminder.reminder_deliveries")
	[ "$upgrade_todo_after" = "$upgrade_todo_count" ] || \
		fail "upgrade changed the todo count: ${upgrade_todo_count} -> ${upgrade_todo_after}"
	[ "$upgrade_delivery_after" = "$upgrade_delivery_count" ] || \
		fail "upgrade changed the delivery count: ${upgrade_delivery_count} -> ${upgrade_delivery_after}"
	assert_page healthy /status
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
