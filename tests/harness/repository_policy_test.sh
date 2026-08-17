#!/bin/sh
set -eu

fixture_dir=$(mktemp -d)
trap 'rm -rf "$fixture_dir"' EXIT

mkdir -p "$fixture_dir/bin"
for tool in go node pnpm; do
  cp "tests/harness/fixtures/$tool" "$fixture_dir/bin/$tool"
  chmod +x "$fixture_dir/bin/$tool"
done

toolchain_output=$(PATH="$fixture_dir/bin:$PATH" sh scripts/check-toolchain.sh)
if [ "$toolchain_output" != "toolchain-check: Go 1.26.5, Node.js 24.18.0, pnpm 11.19.0" ]; then
  echo "unexpected toolchain success output: $toolchain_output" >&2
  exit 1
fi

TOOLCHAIN_FAKE_GO_VERSION=go1.25.0 \
  PATH="$fixture_dir/bin:$PATH" \
  sh -c 'if sh scripts/check-toolchain.sh; then exit 1; fi'

node -e '
  const fs = require("node:fs");
  const pkg = JSON.parse(fs.readFileSync("package.json", "utf8"));
  if (pkg.packageManager !== "pnpm@11.19.0") process.exit(1);
  if (pkg.engines.node !== "24.18.0") process.exit(1);
'

corepack pnpm --silent list --depth=-1 >/dev/null

for target in format-check lint architecture-test test build verify; do
  make -n "$target" >/dev/null
done

verify_commands=$(make -n verify)
printf '%s\n' "$verify_commands" | grep -F 'sh scripts/check-format.sh' >/dev/null || {
  echo 'verify does not select the read-only format check' >&2
  exit 1
}
if printf '%s\n' "$verify_commands" | grep -E 'gofmt[[:space:]]+-w|corepack pnpm format([[:space:]]|$)' >/dev/null; then
  echo 'verify selects a mutating formatter' >&2
  exit 1
fi

repository_root=$(pwd)
secret_checker="$repository_root/scripts/check-secrets.sh"
secret_repo="$fixture_dir/secrets-repo"
mkdir -p "$secret_repo"
git -C "$secret_repo" init --quiet
postgres_scheme=postgresql
printf '%s\n' 'ordinary tracked content' >"$secret_repo/safe.txt"
printf 'DATABASE_URL=%s://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost/example\n' "$postgres_scheme" >"$secret_repo/.env.example"
printf 'const url = "%s://tester:secret@localhost/test";\n' "$postgres_scheme" >"$secret_repo/client.test.ts"
printf 'const url = "%s://tester:placeholder@localhost/test";\n' "$postgres_scheme" >"$secret_repo/client.test.tsx"
printf 'package fixture // %s://tester:test-fixture@localhost/test\n' "$postgres_scheme" >"$secret_repo/client_test.go"
printf '%s://reader:secret@localhost/example\n' "$postgres_scheme" >"$secret_repo/placeholders.md"
git -C "$secret_repo" add safe.txt .env.example client.test.ts client.test.tsx client_test.go placeholders.md
(
  cd "$secret_repo"
  sh "$secret_checker"
)

live_url="${postgres_scheme}://release-user:LiveCredential8427@database.example/release"

printf '%s\n' "$live_url" >"$secret_repo/.env.example"
printf '%s\n' "$live_url" >"$secret_repo/client_test.go"
printf '%s\n' "$live_url" >"$secret_repo/client.test.ts"
printf '%s\n' "$live_url" >"$secret_repo/client.test.tsx"
git -C "$secret_repo" add .env.example client_test.go client.test.ts client.test.tsx
set +e
secret_output=$(
  cd "$secret_repo"
  sh "$secret_checker" 2>&1
)
secret_status=$?
set -e
[ "$secret_status" -ne 0 ] || {
  echo 'secret checker accepted live credentials in fixture-capable paths' >&2
  exit 1
}
expected_secret_output=$(printf '%s\n' \
  '.env.example:1 credential-bearing PostgreSQL URL' \
  'client.test.ts:1 credential-bearing PostgreSQL URL' \
  'client.test.tsx:1 credential-bearing PostgreSQL URL' \
  'client_test.go:1 credential-bearing PostgreSQL URL')
[ "$secret_output" = "$expected_secret_output" ] || {
  echo "unexpected fixture-path credential diagnostics: $secret_output" >&2
  exit 1
}
case "$secret_output" in
  *LiveCredential8427*)
    echo 'secret checker echoed a fixture-path credential' >&2
    exit 1
    ;;
esac

printf 'DATABASE_URL=%s://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost/example\n' "$postgres_scheme" >"$secret_repo/.env.example"
printf 'const url = "%s://tester:secret@localhost/test";\n' "$postgres_scheme" >"$secret_repo/client.test.ts"
printf 'const url = "%s://tester:placeholder@localhost/test";\n' "$postgres_scheme" >"$secret_repo/client.test.tsx"
printf 'package fixture // %s://tester:test-fixture@localhost/test\n' "$postgres_scheme" >"$secret_repo/client_test.go"
git -C "$secret_repo" add .env.example client_test.go client.test.ts client.test.tsx

printf '%s\n' "$live_url" >"$secret_repo/database.txt"
git -C "$secret_repo" add database.txt
set +e
secret_output=$(
  cd "$secret_repo"
  sh "$secret_checker" 2>&1
)
secret_status=$?
set -e
[ "$secret_status" -ne 0 ] || {
  echo 'secret checker accepted a credential-bearing PostgreSQL URL' >&2
  exit 1
}
[ "$secret_output" = 'database.txt:1 credential-bearing PostgreSQL URL' ] || {
  echo "unexpected redacted credential diagnostic: $secret_output" >&2
  exit 1
}
case "$secret_output" in
  *LiveCredential8427*)
    echo 'secret checker echoed a credential' >&2
    exit 1
    ;;
esac

printf '%s\n' 'ordinary tracked content' >"$secret_repo/database.txt"
private_key_header='-----BEGIN PRIVATE'" KEY-----"
printf '%s\n' "$private_key_header" >"$secret_repo/private.pem"
git -C "$secret_repo" add database.txt private.pem
set +e
secret_output=$(
  cd "$secret_repo"
  sh "$secret_checker" 2>&1
)
secret_status=$?
set -e
[ "$secret_status" -ne 0 ] || {
  echo 'secret checker accepted a private-key header' >&2
  exit 1
}
[ "$secret_output" = 'private.pem:1 private-key header' ] || {
  echo "unexpected redacted private-key diagnostic: $secret_output" >&2
  exit 1
}

printf '%s\n' 'ordinary tracked content' >"$secret_repo/private.pem"
live_token='gh'"p_liveToken8427"
printf '%s\n' "$live_token" >"$secret_repo/token.txt"
git -C "$secret_repo" add private.pem token.txt
set +e
secret_output=$(
  cd "$secret_repo"
  sh "$secret_checker" 2>&1
)
secret_status=$?
set -e
[ "$secret_status" -ne 0 ] || {
  echo 'secret checker accepted a live-token prefix' >&2
  exit 1
}
[ "$secret_output" = 'token.txt:1 live-token prefix' ] || {
  echo "unexpected redacted token diagnostic: $secret_output" >&2
  exit 1
}

printf '%s\n' 'ordinary tracked content' >"$secret_repo/token.txt"
printf '%s\n' "$live_url" >"$secret_repo/staged.txt"
git -C "$secret_repo" add token.txt staged.txt
printf '%s\n' 'safe unstaged replacement' >"$secret_repo/staged.txt"
set +e
secret_output=$(
  cd "$secret_repo"
  sh "$secret_checker" 2>&1
)
secret_status=$?
set -e
[ "$secret_status" -ne 0 ] || {
  echo 'secret checker ignored a credential staged in the index' >&2
  exit 1
}
[ "$secret_output" = 'staged.txt:1 credential-bearing PostgreSQL URL' ] || {
  echo "unexpected staged credential diagnostic: $secret_output" >&2
  exit 1
}

git -C "$secret_repo" add staged.txt
printf '%s\n' "$live_url" >"$secret_repo/staged.txt"
set +e
secret_output=$(
  cd "$secret_repo"
  sh "$secret_checker" 2>&1
)
secret_status=$?
set -e
[ "$secret_status" -ne 0 ] || {
  echo 'secret checker ignored a tracked working-tree credential' >&2
  exit 1
}
[ "$secret_output" = 'staged.txt:1 credential-bearing PostgreSQL URL' ] || {
  echo "unexpected working-tree credential diagnostic: $secret_output" >&2
  exit 1
}

printf '%s\n' "$live_url" >"$secret_repo/untracked.txt"
printf '%s\n' 'safe tracked content' >"$secret_repo/staged.txt"
(
  cd "$secret_repo"
  sh "$secret_checker"
)

printf '%s\n' "$live_url" >"$secret_repo/payload=fixture"
printf '%s\n' "$live_url" >"$secret_repo/path with spaces.txt"
newline_path=$(printf 'path with\nnewline.txt')
printf '%s\n' "$live_url" >"$secret_repo/$newline_path"
git -C "$secret_repo" add -- payload=fixture 'path with spaces.txt' "$newline_path"
set +e
secret_output=$(
  cd "$secret_repo"
  sh "$secret_checker" 2>&1
)
secret_status=$?
set -e
[ "$secret_status" -ne 0 ] || {
  echo 'secret checker ignored special-path credentials' >&2
  exit 1
}
expected_secret_output=$(printf '%s\n' \
  'path with spaces.txt:1 credential-bearing PostgreSQL URL' \
  'path with\nnewline.txt:1 credential-bearing PostgreSQL URL' \
  'payload=fixture:1 credential-bearing PostgreSQL URL')
[ "$secret_output" = "$expected_secret_output" ] || {
  echo "unexpected escaped special-path diagnostics: $secret_output" >&2
  exit 1
}
case "$secret_output" in
  *LiveCredential8427*)
    echo 'secret checker echoed a special-path credential' >&2
    exit 1
    ;;
esac

printf '%s\n' safe >"$secret_repo/payload=fixture"
printf '%s\n' safe >"$secret_repo/path with spaces.txt"
printf '%s\n' safe >"$secret_repo/$newline_path"
git -C "$secret_repo" add -- payload=fixture 'path with spaces.txt' "$newline_path"

symlink_token='gh'"p_symlinkToken8427"
printf '%s\n' safe >"$secret_repo/safe-link-target"
ln -s "$symlink_token" "$secret_repo/staged-link"
git -C "$secret_repo" add safe-link-target staged-link
rm "$secret_repo/staged-link"
ln -s safe-link-target "$secret_repo/staged-link"
set +e
secret_output=$(
  cd "$secret_repo"
  sh "$secret_checker" 2>&1
)
secret_status=$?
set -e
[ "$secret_status" -ne 0 ] || {
  echo 'secret checker ignored a token in a staged symlink blob' >&2
  exit 1
}
[ "$secret_output" = 'staged-link:1 live-token prefix' ] || {
  echo "unexpected staged symlink diagnostic: $secret_output" >&2
  exit 1
}
case "$secret_output" in
  *symlinkToken8427*)
    echo 'secret checker echoed a staged symlink token' >&2
    exit 1
    ;;
esac

git -C "$secret_repo" add staged-link
rm "$secret_repo/staged-link"
ln -s "$symlink_token" "$secret_repo/staged-link"
set +e
secret_output=$(
  cd "$secret_repo"
  sh "$secret_checker" 2>&1
)
secret_status=$?
set -e
[ "$secret_status" -ne 0 ] || {
  echo 'secret checker ignored a token in a tracked worktree symlink' >&2
  exit 1
}
[ "$secret_output" = 'staged-link:1 live-token prefix' ] || {
  echo "unexpected worktree symlink diagnostic: $secret_output" >&2
  exit 1
}

rm "$secret_repo/staged-link"
ln -s safe-link-target "$secret_repo/staged-link"

real_git=$(command -v git)
mkdir -p "$fixture_dir/failing-bin"
printf '%s\n' \
  '#!/bin/sh' \
  'if [ "$1" = "$SECRET_TEST_GIT_FAILURE" ]; then exit 23; fi' \
  'exec "$SECRET_TEST_REAL_GIT" "$@"' >"$fixture_dir/failing-bin/git"
chmod +x "$fixture_dir/failing-bin/git"

for failed_operation in ls-files cat-file; do
  set +e
  secret_output=$(
    cd "$secret_repo"
    SECRET_TEST_GIT_FAILURE="$failed_operation" \
      SECRET_TEST_REAL_GIT="$real_git" \
      PATH="$fixture_dir/failing-bin:$PATH" \
      sh "$secret_checker" 2>&1
  )
  secret_status=$?
  set -e
  [ "$secret_status" -ne 0 ] || {
    echo "secret checker ignored $failed_operation failure" >&2
    exit 1
  }
  [ "$secret_output" = "check-secrets: $failed_operation failed" ] || {
    echo "unexpected $failed_operation failure diagnostic: $secret_output" >&2
    exit 1
  }
done

go test ./tests/harness -run TestWorkflow -count=1

if git ls-files | grep -E '(^|/)\.DS_Store$|(^|/)\.env$'; then
  echo 'tracked local or secret file detected' >&2
  exit 1
fi
