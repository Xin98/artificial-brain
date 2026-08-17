#!/bin/sh

set -eu

unformatted=$(gofmt -l $(find backend architecture tests -name '*.go' -type f))
if [ -n "$unformatted" ]; then
	printf 'Go files require gofmt:\n%s\n' "$unformatted" >&2
	exit 1
fi

corepack pnpm format:check
