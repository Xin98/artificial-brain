.PHONY: toolchain-check harness-test architecture-test format format-check lint test verify dev down build migration-test smoke-test clean-local-data backup restore offline-bundle

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

backup:
	@COMPOSE_PROJECT_NAME="$${COMPOSE_PROJECT_NAME:-$$(docker compose config --format json | jq -r '.name')}" \
		sh deploy/private/backup.sh

restore:
	@test -n "$(BACKUP)" || (echo 'Run make restore BACKUP=<archive> CONFIRM=restore' >&2; exit 1)
	@test "$(CONFIRM)" = "restore" || (echo 'Run make restore BACKUP=<archive> CONFIRM=restore' >&2; exit 1)
	@COMPOSE_PROJECT_NAME="$${COMPOSE_PROJECT_NAME:-$$(docker compose config --format json | jq -r '.name')}" \
		BACKUP="$(BACKUP)" CONFIRM="$(CONFIRM)" sh deploy/private/restore.sh

offline-bundle:
	@mkdir -p .artifacts/offline
	@docker compose --profile test build
	@set -eu; \
	images=$$(docker compose --profile test config --images | sort -u); \
	docker save --output .artifacts/offline/artificial-brain-images.tar $$images; \
	printf '%s\n' \
		'# Artificial Brain offline image bundle' \
		'' \
		'Produced by `make offline-bundle`. Never commit this directory.' \
		'' \
		'- artificial-brain-images.tar: `docker save` archive of every stack' \
		'  image (postgres:18.4-alpine plus the built migrate/api/worker/web' \
		'  and backend-test images).' \
		'' \
		'Load recipe on the target host:' \
		'' \
		'    docker load --input artificial-brain-images.tar' \
		'' \
		'Copy this repository next to the loaded images, then start without' \
		'network pulls:' \
		'' \
		'    docker compose up -d' \
		'' \
		'Built images are tagged with the compose project name, so keep the' \
		'repository directory name (or COMPOSE_PROJECT_NAME) identical on the' \
		'target host, or rebuild the bundle there.' \
		> .artifacts/offline/README.md
	@printf 'offline-bundle: wrote .artifacts/offline/artificial-brain-images.tar\n'
