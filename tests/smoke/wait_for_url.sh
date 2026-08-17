#!/bin/sh

set -eu

if [ "$#" -ne 2 ]; then
	printf 'usage: %s URL SECONDS\n' "$0" >&2
	exit 2
fi

url=$1
seconds=$2
case "$seconds" in
	'' | *[!0-9]*)
		printf 'wait_for_url: SECONDS must be a positive integer\n' >&2
		exit 2
		;;
esac
[ "$seconds" -gt 0 ] || {
	printf 'wait_for_url: SECONDS must be a positive integer\n' >&2
	exit 2
}

deadline=$(($(date +%s) + seconds))
while :; do
	if curl --fail --silent --show-error --max-time 2 "$url" >/dev/null 2>&1; then
		exit 0
	fi
	if [ "$(date +%s)" -ge "$deadline" ]; then
		printf 'wait_for_url: timed out after %ss waiting for %s\n' "$seconds" "$url" >&2
		exit 1
	fi
	sleep 1
done
