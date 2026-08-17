.PHONY: toolchain-check harness-test architecture-test format format-check lint test verify dev down build migration-test smoke-test clean-local-data

toolchain-check:
	@sh scripts/check-toolchain.sh

harness-test:
	@sh tests/harness/repository_policy_test.sh

architecture-test:
	@go test ./architecture/... -v

format:
	@gofmt -w $$(find backend architecture tests -name '*.go' -type f)
	@corepack pnpm format

format-check:
	@sh scripts/check-format.sh

lint:
	@go vet ./...
	@corepack pnpm --filter @artificial-brain/web lint

test:
	@go test ./... -race
	@corepack pnpm --filter @artificial-brain/web test

verify: harness-test format-check lint architecture-test test build
	@sh scripts/check-secrets.sh

dev:
	@docker compose up --build

down:
	@docker compose down

build:
	@go build ./backend/cmd/api ./backend/cmd/worker ./backend/cmd/migrate
	@corepack pnpm --filter @artificial-brain/web build

migration-test:
	@sh tests/smoke/migration_test.sh

smoke-test:
	@sh tests/smoke/stack_test.sh

clean-local-data:
	@test "$(CONFIRM)" = "delete" || (echo 'Run make clean-local-data CONFIRM=delete' >&2; exit 1)
	@docker compose down --volumes
