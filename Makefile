# Pagdandi — one-command dev (SPEC §13.2).
# Run from a POSIX shell: Linux/macOS, WSL2, or Git Bash on Windows.

SHELL := bash
.ONESHELL:
.DEFAULT_GOAL := help
REGION ?= indore
API := services/api
WEB := apps/web
TEST_DB ?= postgres://pagdandi:pagdandi@localhost:5432/pagdandi_test?sslmode=disable

.PHONY: help bootstrap data dev stop tunnel test test-go test-gh test-web e2e lint fmt build seed-dev admin refresh-graph deploy services-up services-down gh-in gh-se api worker web

help: ## list targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-16s %s\n",$$1,$$2}'

bootstrap: ## install deps, create .env, start Postgres+Redis, run migrations
	./scripts/bootstrap.sh

services-up: ## start Postgres and Redis
	./scripts/services-up.sh

services-down: ## stop Postgres and Redis
	./scripts/services-down.sh

data: ## build data for REGION (default indore; REGION=all for every region)
	. scripts/env.sh
	cd $(API) && go run ./cmd/datactl all --region $(REGION)

dev: ## run api, worker, graphhopper and web with hot reload
	./scripts/dev.sh

stop: ## stop everything started by make dev
	./scripts/dev.sh stop

gh-in: ## serve the India graph on :8989
	./scripts/gh-serve.sh in

gh-se: ## serve the Sweden graph on :8990
	./scripts/gh-serve.sh se

tunnel: ## HTTPS tunnel so you can test on your phone with GPS
	./scripts/tunnel.sh

test: test-go test-web ## Go + web unit/integration tests

test-go: ## Go unit tests, plus integration tests when Postgres is up
	. scripts/env.sh
	cd $(API) && TEST_DATABASE_URL="$${TEST_DATABASE_URL:-$(TEST_DB)}" TEST_REDIS_URL="$${TEST_REDIS_URL:-redis://localhost:6379/1}" go test ./...

test-gh: ## deterministic profile tests on the synthetic-town fixture
	./scripts/test-gh.sh

test-web: ## web unit tests (Vitest)
	cd $(WEB) && npm run test

e2e: ## Playwright on mobile viewports with mocked GPS
	cd $(WEB) && npm run e2e

lint: ## go vet, staticcheck, eslint, tsc, design-token check
	cd $(API) && go vet ./... && (command -v staticcheck >/dev/null && staticcheck ./... || go run honnef.co/go/tools/cmd/staticcheck@latest ./...)
	cd $(CURDIR)/$(WEB) && npm run lint && npm run typecheck && npm run check:tokens

fmt: ## format Go and web code
	cd $(API) && gofmt -w ./cmd ./internal
	cd $(CURDIR)/$(WEB) && npx prettier --write src

build: ## build Go binaries and the web app
	cd $(API) && go build -o bin/ ./cmd/...
	cd $(CURDIR)/$(WEB) && npm run build

seed-dev: ## demo shortcuts and reports around Bilawali, Indore
	. scripts/env.sh
	cd $(API) && go run ./cmd/datactl seed-dev

admin: ## make a user moderator/admin: make admin EMAIL=you@x.com [ROLE=admin]
	@test -n "$(EMAIL)" || (echo "usage: make admin EMAIL=you@example.com" && exit 1)
	. scripts/env.sh
	cd $(API) && go run ./cmd/datactl admin --email "$(EMAIL)" --role "$${ROLE:-admin}"

refresh-graph: ## weekly OSM refresh with blue/green swap
	./scripts/refresh-graph.sh $(REGION)

deploy: ## production deploy (skips if credentials are missing)
	./scripts/deploy.sh
