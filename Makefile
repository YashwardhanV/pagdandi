# Pagdandi v2. Run from a POSIX shell: Linux/macOS, WSL2, or Git Bash on Windows.

SHELL := bash
.ONESHELL:
.DEFAULT_GOAL := help
API := services/api
WEB := apps/web

.PHONY: help bootstrap data dev stop gh api web test test-go test-web build lint fmt tunnel deploy icons

help: ## list targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-12s %s\n",$$1,$$2}'

bootstrap: ## install tools (GraphHopper, osmium, pmtiles), create .env, install web deps
	./scripts/bootstrap.sh

data: ## build everything for the region: graph, tiles, feature layers (~5 min first time)
	. scripts/env.sh
	cd $(API) && go run ./cmd/datactl all

dev: ## run GraphHopper, the API and the web app (http://localhost:5173)
	./scripts/dev.sh

stop: ## stop everything started by make dev
	./scripts/dev.sh stop

gh: ## serve the GraphHopper graph on :8989
	./scripts/gh-serve.sh

api: ## run the API on :8080
	. scripts/env.sh
	cd $(API) && go run ./cmd/api

web: ## run the web dev server on :5173
	cd $(WEB) && npm run dev

test: test-go test-web ## all tests

test-go: ## Go unit tests (fake GraphHopper; no data needed)
	cd $(API) && go test ./...

test-web: ## web unit tests (Vitest)
	cd $(WEB) && npm run test

lint: ## go vet + gofmt check + TypeScript
	cd $(API) && go vet ./... && test -z "$$(gofmt -l .)"
	cd $(CURDIR)/$(WEB) && npm run typecheck

fmt: ## format Go code
	cd $(API) && gofmt -w .

build: ## build the Go binaries and the web app
	cd $(API) && go build -o bin/ ./cmd/...
	cd $(CURDIR)/$(WEB) && npm run build

icons: ## render PWA icons from public/favicon.svg
	cd $(WEB) && node scripts/make-icons.mjs

tunnel: ## HTTPS tunnel to the dev server so GPS works on your phone
	./scripts/tunnel.sh

deploy: ## production deploy (skips if DEPLOY_* are unset)
	./scripts/deploy.sh
