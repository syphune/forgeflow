.PHONY: dev dev-local docker-dev dev-host dev-db test test-backend test-integration test-mcp mcp-bridge test-desktop test-e2e lint typecheck build generate migrate-up migrate-down security smoke

PNPM := $(shell if command -v pnpm >/dev/null 2>&1; then printf pnpm; else printf 'npm exec --yes --package=pnpm@10.14.0 -- pnpm'; fi)

dev:
	$(MAKE) docker-dev

dev-local:
	$(MAKE) docker-dev

docker-dev:
	docker compose --env-file .env.local -f infra/compose.dev.yaml up --build

dev-host:
	sh scripts/dev-local.sh

dev-db:
	docker compose -f infra/compose.dev.yaml up -d postgres

test: test-backend test-desktop
	$(PNPM) --dir apps/web test

test-desktop:
	$(PNPM) --dir apps/desktop test

test-e2e:
	$(PNPM) exec playwright test

test-backend:
	cd backend && go test -race -shuffle=on ./...

test-integration:
	cd backend && sh ../scripts/test-integration.sh

test-mcp:
	cd backend && go test -race ./internal/mcp

mcp-bridge:
	cd backend && mkdir -p ./bin && go build -trimpath -o ./bin/forgeflow-mcp-bridge ./cmd/mcp-bridge

lint:
	cd backend && go vet ./...
	$(PNPM) --dir apps/web lint

typecheck:
	$(PNPM) --dir apps/web typecheck
	$(PNPM) --dir apps/desktop typecheck

build:
	cd backend && go build ./cmd/api ./cmd/worker ./cmd/mcp ./cmd/mcp-bridge ./cmd/migrate ./cmd/runner
	$(PNPM) --dir apps/web build

generate:
	./scripts/generate.sh

migrate-up:
	cd backend && go run ./cmd/migrate up

migrate-down:
	cd backend && go run ./cmd/migrate down 1

security:
	cd backend && go vet ./...
	cd backend && go run golang.org/x/vuln/cmd/govulncheck@latest ./...

smoke:
	cd backend && go test ./... && go build ./cmd/api ./cmd/worker ./cmd/mcp ./cmd/mcp-bridge ./cmd/migrate ./cmd/runner
