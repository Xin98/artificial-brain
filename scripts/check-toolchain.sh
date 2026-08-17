#!/bin/sh
set -eu

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "toolchain-check: $1 is required; install $2" >&2
    exit 1
  fi
}

require_command go "Go 1.26.5 or a newer 1.26 patch"
require_command node "Node.js 24.18.0"
require_command pnpm "pnpm 11.19.0"

go_output=$(go version)
case "$go_output" in
  *" go1.26."*) ;;
  *)
    echo "toolchain-check: Go 1.26.5 or a newer 1.26 patch is required (found: $go_output)" >&2
    exit 1
    ;;
esac
go_patch=${go_output#* go1.26.}
go_patch=${go_patch%%[!0-9]*}
if [ -z "$go_patch" ] || [ "$go_patch" -lt 5 ]; then
  echo "toolchain-check: Go 1.26.5 or a newer 1.26 patch is required (found: $go_output)" >&2
  exit 1
fi

node_output=$(node --version)
if [ "$node_output" != "v24.18.0" ]; then
  echo "toolchain-check: Node.js 24.18.0 is required (found: $node_output)" >&2
  exit 1
fi

pnpm_output=$(pnpm --version)
if [ "$pnpm_output" != "11.19.0" ]; then
  echo "toolchain-check: pnpm 11.19.0 is required (found: $pnpm_output)" >&2
  exit 1
fi

echo "toolchain-check: Go 1.26.$go_patch, Node.js ${node_output#v}, pnpm $pnpm_output"
