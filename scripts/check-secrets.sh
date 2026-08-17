#!/bin/sh

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || {
	printf 'check-secrets: script path resolution failed\n' >&2
	exit 2
}

exec node "$script_dir/check-secrets.mjs"
