#!/bin/sh

set -eu

exec sed -E \
	-e 's#([A-Za-z][A-Za-z0-9+.-]*://)[^/@[:space:]]+@#\1[REDACTED]@#g' \
	-e 's#("(database_url|postgres_password|password|secret|token|api_key|access_token|client_secret|secret_key|DATABASE_URL|POSTGRES_PASSWORD|PASSWORD|SECRET|TOKEN|API_KEY|ACCESS_TOKEN|CLIENT_SECRET|SECRET_KEY)"[[:space:]]*:[[:space:]]*)"([^"\\]|\\.)*"#\1"[REDACTED]"#g' \
	-e 's#((DATABASE_URL|POSTGRES_PASSWORD|PASSWORD|SECRET|TOKEN|API_KEY)[=:][[:space:]]*)[^[:space:],;]+#\1[REDACTED]#g' \
	-e 's#((database_url|postgres_password|password|secret|token|api_key)[=:][[:space:]]*)[^[:space:],;]+#\1[REDACTED]#g'
