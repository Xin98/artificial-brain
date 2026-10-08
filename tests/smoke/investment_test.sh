#!/bin/sh
# Runs against the existing fixture stack. Never prints login codes or cookies.
set -eu
fail() { printf 'investment smoke: %s\n' "$1" >&2; exit 1; }
WEB_PORT=${1:-}
API_PORT=${2:-}
for port in "$WEB_PORT" "$API_PORT"; do
	case "$port" in '' | *[!0-9]*) fail 'two integer ports required' ;; esac
	[ "$port" -gt 0 ] && [ "$port" -le 65535 ] || fail 'port outside range'
done
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
origin="http://127.0.0.1:${WEB_PORT}"
base="$origin/api/v1/investment"
curl --fail --silent --show-error --max-time 5 "http://127.0.0.1:${API_PORT}/health/ready" >/dev/null
request() {
	request_jar=$1 request_method=$2 request_path=$3 request_key=$4 request_body=$5 request_expected=$6
	request_status=$(curl --silent --show-error --max-time 30 --cookie "$request_jar" \
		--request "$request_method" --header 'Content-Type: application/json' \
		--header "Idempotency-Key: $request_key" --data "$request_body" \
		--output "$scratch/response.json" --write-out '%{http_code}' "$base$request_path")
	[ "$request_status" = "$request_expected" ] || fail "$request_method $request_path returned $request_status, want $request_expected"
	cat "$scratch/response.json"
}
login() {
	phone=$1 jar=$2
	status=$(curl --silent --show-error --max-time 5 --output /dev/null --write-out '%{http_code}' \
		--header 'Content-Type: application/json' --data "{\"phone\":\"$phone\"}" "$origin/api/v1/auth/login/request")
	[ "$status" = 202 ] || fail 'login request failed'
	code=$(curl --fail --silent --show-error --max-time 5 "$origin/api/v1/dev/sms-inbox?address=$(printf '%s' "$phone" | jq -sRr @uri)" | jq -r '.messages[0].code')
	case "$code" in '' | *[!0-9]*) fail 'fixture code missing' ;; esac
	status=$(curl --silent --show-error --max-time 5 --cookie-jar "$jar" --output /dev/null --write-out '%{http_code}' \
		--header 'Content-Type: application/json' --data "{\"phone\":\"$phone\",\"code\":\"$code\"}" "$origin/api/v1/auth/login/verify")
	[ "$status" = 200 ] || fail 'login verification failed'
}
owner="$scratch/owner.cookies"
other="$scratch/other.cookies"
login '+8613900020031' "$owner"
login '+8613900020032' "$other"
data=$(request "$owner" GET /data-status '' '' 200)
printf '%s' "$data" | jq -e '.mode == "fixture" and .feed == "synthetic" and (.datasetVersion | contains("fixture")) and .market.state == "available"' >/dev/null || fail 'fixture source status incorrect'
instruments=$(request "$owner" GET '/instruments?limit=100' '' '' 200)
printf '%s' "$instruments" | jq -e '.items | length >= 30' >/dev/null || fail 'instrument coverage missing'
members=$(printf '%s' "$instruments" | jq -c '[.items[].instrument | select(.kind == "stock") | .id]')
poolbody=$(jq -nc --argjson ids "$members" '{name:"smoke pool",mode:"fixture",instrumentIds:$ids}')
pool=$(request "$owner" POST /universes smoke-pool "$poolbody" 201)
otherpool=$(request "$other" POST /universes smoke-pool "$poolbody" 201)
poolid=$(printf '%s' "$pool" | jq -r '.id')
otherpoolid=$(printf '%s' "$otherpool" | jq -r '.id')
body=$(jq -nc --arg u "$poolid" '{name:"smoke account",initialCash:"100000.00",universeVersionId:$u}')
account=$(request "$owner" POST /accounts smoke-account "$body" 201)
otherbody=$(jq -nc --arg u "$otherpoolid" '{name:"other account",initialCash:"100000.00",universeVersionId:$u}')
otheraccount=$(request "$other" POST /accounts smoke-account "$otherbody" 201)
id=$(printf '%s' "$account" | jq -r '.id')
otherid=$(printf '%s' "$otheraccount" | jq -r '.id')
printf '%s' "$account" | jq -e '.automationEnabled == false and .cash.available == "100000.00" and .cash.reserved == "0.00" and (.initialCash | type == "string")' >/dev/null || fail 'account defaults incorrect'
for suffix in '' /orders /positions /ledger /evaluations /performance; do
	request "$other" GET "/accounts/$id$suffix" '' '' 404 >/dev/null
done
request "$owner" GET "/accounts/$otherid" '' '' 404 >/dev/null
retry=$(request "$owner" POST /accounts smoke-account "$body" 201)
[ "$(printf '%s' "$retry" | jq -r '.id')" = "$id" ] || fail 'account retry duplicated funding'
request "$owner" POST /accounts smoke-account '{"name":"changed","initialCash":"100000.00"}' 409 >/dev/null
analysis=$(request "$owner" GET /instruments/fixture-26/analysis '' '' 200)
printf '%s' "$analysis" | jq -e '(.metrics.pe.value == null or (.metrics.pe.value | type == "string")) and (.risk.level | type == "string") and (.recommendation.potential | type == "string")' >/dev/null || fail 'risk/potential or decimal metric contract incorrect'
orderbody=$(printf '%s' "$account" | jq -c '{instrumentId:"fixture-26",side:"buy",quantity:"2",expectedVersion:.version}')
order=$(request "$owner" POST "/accounts/$id/orders" smoke-order "$orderbody" 201)
retry=$(request "$owner" POST "/accounts/$id/orders" smoke-order "$orderbody" 201)
[ "$(printf '%s' "$order" | jq -r '.id')" = "$(printf '%s' "$retry" | jq -r '.id')" ] || fail 'order retry duplicated reservation'
printf '%s' "$order" | jq -e '.state == "pending" and (.reservedCash | type == "string") and (.quantity | type == "string")' >/dev/null || fail 'pending order contract incorrect'
account=$(request "$owner" GET "/accounts/$id" '' '' 200)
toggle=$(printf '%s' "$account" | jq -c '{enabled:true,expectedVersion:.version,strategyVersionId,universeVersionId,policy}')
enabled=$(request "$owner" PUT "/accounts/$id/automation" smoke-enable "$toggle" 200)
printf '%s' "$enabled" | jq -e '.automationEnabled == true' >/dev/null || fail 'enable failed'
toggle=$(printf '%s' "$enabled" | jq -c '{enabled:false,expectedVersion:.version,strategyVersionId,universeVersionId,policy}')
paused=$(request "$owner" PUT "/accounts/$id/automation" smoke-pause "$toggle" 200)
printf '%s' "$paused" | jq -e '.automationEnabled == false' >/dev/null || fail 'pause failed'
backbody=$(printf '%s' "$account" | jq -c '{strategyVersionId,universeVersionId,from:"2026-07-01T00:00:00Z",to:"2026-07-31T00:00:00Z",initialCash:"100000.00"}')
backtest=$(request "$owner" POST /backtests smoke-backtest "$backbody" 202)
runid=$(printf '%s' "$backtest" | jq -r '.runId')
request "$other" GET "/backtests/$runid" '' '' 404 >/dev/null
retry=$(request "$owner" POST /backtests smoke-backtest "$backbody" 202)
[ "$(printf '%s' "$retry" | jq -r '.runId')" = "$runid" ] || fail 'backtest retry duplicated job'
deadline=$(($(date +%s) + 300))
while :; do
	result=$(request "$owner" GET "/backtests/$runid" '' '' 200)
	state=$(printf '%s' "$result" | jq -r '.status')
	case "$state" in completed) break ;; failed) fail 'backtest failed' ;; queued | running) ;; *) fail 'unknown backtest state' ;; esac
	[ "$(date +%s)" -lt "$deadline" ] || fail 'backtest completion deadline exceeded'
	sleep 2
done
printf '%s' "$result" | jq -e '(.curve | length > 0) and (.curve[0].nav | type == "string") and (.benchmarkCurve | length > 0)' >/dev/null || fail 'backtest result/benchmark missing'
printf 'investment smoke: two owners, source labels, exact decimals, idempotency, automation and completed backtest passed\n'
