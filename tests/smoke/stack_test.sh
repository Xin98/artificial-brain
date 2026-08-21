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
