#!/bin/sh
# One-shot redeploy for the single-host stack (docs/runbooks/upgrade.md).
#
# Automates the mechanical steps — backup, source update, image build,
# recreate, migrate exit-0 gate, health probes — and stops with rollback
# hints on the first failure. Human-judgement steps stay manual by design:
# the post-deploy data spot-check in the web UI, and any restore decision.
#
# Run from the repository root on the deployment host:
#
#   make deploy                     # backup -> git pull --ff-only -> rebuild
#   make deploy DEPLOY_OFFLINE=1    # offline host: skip git pull (docker load first)
#
# Optional environment:
#   DEPLOY_SKIP_BACKUP=1        skip the pre-deploy backup (loud warning)
#   DEPLOY_MIGRATE_TIMEOUT      seconds to wait for migrate to exit (default 300)
#   DEPLOY_HEALTH_TIMEOUT       seconds to wait for health probes (default 120)

set -eu

step_name=preflight
archive=""
previous_ref=""

fail() {
	printf 'deploy: %s\n' "$*" >&2
	printf 'deploy: FAILED during: %s\n' "$step_name" >&2
	printf 'deploy: rollback hints (docs/runbooks/upgrade.md "On failure"):\n' >&2
	if [ -n "$archive" ]; then
		printf 'deploy:   make restore BACKUP=%s CONFIRM=restore\n' "$archive" >&2
	fi
	if [ -n "$previous_ref" ]; then
		printf 'deploy:   git checkout %s && docker compose up -d --build\n' \
			"$previous_ref" >&2
	fi
	exit 1
}

step() {
	step_name=$1
	printf 'deploy: %s\n' "$1" >&2
}

# --- preflight --------------------------------------------------------------

step "preflight"
[ -f compose.yaml ] || fail "run from the repository root (compose.yaml not found)"
for tool in docker curl jq make; do
	command -v "$tool" >/dev/null 2>&1 || fail "$tool is required"
done
docker compose version >/dev/null 2>&1 || fail "docker compose v2 is required"

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	previous_ref=$(git rev-parse --short HEAD)
fi

# --- backup ------------------------------------------------------------------

step "backup"
if [ "${DEPLOY_SKIP_BACKUP:-0}" = "1" ]; then
	printf 'deploy: WARNING: DEPLOY_SKIP_BACKUP=1 — deploying without a backup\n' >&2
else
	postgres_id=$(docker compose ps -q postgres | head -n 1)
	postgres_running=false
	if [ -n "$postgres_id" ]; then
		postgres_running=$(docker inspect -f '{{.State.Running}}' "$postgres_id")
	fi
	if [ "$postgres_running" = "true" ]; then
		# -s (silent) also implies --no-print-directory, so the nested make
		# adds no Entering/Leaving lines to stdout — the last line stays the
		# archive path printed by backup.sh.
		archive=$(make -s backup) || fail "backup failed; refusing to deploy without one"
		archive=$(printf '%s\n' "$archive" | tail -n 1)
		[ -n "$archive" ] && [ -f "$archive" ] || fail "backup produced no archive"
	else
		printf 'deploy: postgres is not running — treating this as a fresh install, no backup taken\n' >&2
	fi
fi

# --- source update ------------------------------------------------------------

step "source update"
if [ "${DEPLOY_OFFLINE:-0}" = "1" ]; then
	printf 'deploy: DEPLOY_OFFLINE=1 — skipping git pull (transfer the offline bundle manually)\n' >&2
elif [ -z "$previous_ref" ]; then
	printf 'deploy: not a git work tree — skipping git pull\n' >&2
else
	git symbolic-ref -q HEAD >/dev/null || fail "detached HEAD; check out a branch first"
	git pull --ff-only || fail "git pull --ff-only failed; resolve the working tree first"
fi

# --- build and recreate ---------------------------------------------------------

step "build"
docker compose build || fail "image build failed"

step "recreate"
docker compose up -d || fail "docker compose up -d failed"

# --- migrate gate -----------------------------------------------------------------

step "migrate gate"
migrate_timeout=${DEPLOY_MIGRATE_TIMEOUT:-300}
elapsed=0
migrate_done=0
while [ "$elapsed" -lt "$migrate_timeout" ]; do
	# -a: migrate exits 0 by design, and `docker compose ps` hides stopped
	# containers, so without it the gate never sees the finished job.
	migrate_id=$(docker compose ps -aq migrate | head -n 1)
	if [ -n "$migrate_id" ]; then
		state=$(docker inspect -f '{{.State.Status}}' "$migrate_id")
		case "$state" in
		exited)
			code=$(docker inspect -f '{{.State.ExitCode}}' "$migrate_id")
			[ "$code" = "0" ] || fail "migrate exited with code $code"
			migrate_done=1
			break
			;;
		created | running | restarting) ;;
		*) fail "migrate is in unexpected state: $state" ;;
		esac
	fi
	sleep 2
	elapsed=$((elapsed + 2))
done
[ "$migrate_done" = "1" ] || fail "migrate did not finish within ${migrate_timeout}s"
printf 'deploy: migrate exited 0\n' >&2

# --- health probes ------------------------------------------------------------------

step "health probes"
health_timeout=${DEPLOY_HEALTH_TIMEOUT:-120}

http_address() {
	addr=$(docker compose port "$1" "$2" | head -n 1)
	case "$addr" in
	0.0.0.0:*) addr="127.0.0.1:${addr#0.0.0.0:}" ;;
	\[*) addr="127.0.0.1:${addr##*:}" ;;
	esac
	printf '%s\n' "$addr"
}

wait_for_http() {
	url=$1
	elapsed=0
	while [ "$elapsed" -lt "$health_timeout" ]; do
		if curl --fail --silent --output /dev/null "$url"; then
			return 0
		fi
		sleep 2
		elapsed=$((elapsed + 2))
	done
	return 1
}

api_addr=$(http_address api 8080)
web_addr=$(http_address web 3000)
wait_for_http "http://${api_addr}/health/ready" ||
	fail "api did not become ready within ${health_timeout}s: http://${api_addr}/health/ready"
wait_for_http "http://${web_addr}/health/live" ||
	fail "web did not become live within ${health_timeout}s: http://${web_addr}/health/live"

# -a: a crashed (exited) worker must fail the probe, not be silently skipped.
worker_id=$(docker compose ps -aq worker | head -n 1)
if [ -n "$worker_id" ]; then
	status=unknown
	elapsed=0
	while [ "$elapsed" -lt "$health_timeout" ]; do
		status=$(docker inspect -f '{{.State.Health.Status}}' "$worker_id" 2>/dev/null || printf 'unknown')
		[ "$status" = "healthy" ] && break
		sleep 2
		elapsed=$((elapsed + 2))
	done
	[ "$status" = "healthy" ] || fail "worker did not become healthy within ${health_timeout}s"
fi

# --- done -----------------------------------------------------------------------------

step "done"
printf 'deploy: stack is up and healthy\n' >&2
printf 'deploy: finish the runbook manually — open http://%s/, log in as the administrator,\n' \
	"$web_addr" >&2
printf 'deploy: and spot-check that pre-deploy data survived (todos, reminders, counters)\n' >&2
