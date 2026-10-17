# Thin wrapper around docker compose, the front door for every language in the repository.
# Nothing here is logic; anything that grows logic belongs in a script under the package it
# serves. `make help` is the command reference docs/DEVELOPMENT.md points to.

COMPOSE := docker compose
TEST    := $(COMPOSE) --profile test run --rm test
# A throwaway Go toolchain for services/server's unit tests, so `make test` needs Docker and
# nothing else, like every other target here. Named volumes keep the module and build
# caches between runs. It shares compose's db container's network namespace, so the
# database tests reach their holdmytrack_test database at localhost:5432.
GO      := docker run --rm -v $(CURDIR)/services/server:/src -w /src \
             -v holdmytrack-go-mod:/go/pkg/mod -v holdmytrack-go-build:/root/.cache/go-build \
             --network container:$$($(COMPOSE) ps -q db) \
             -e TEST_DATABASE_URL=postgres://holdmytrack:holdmytrack@localhost:5432/holdmytrack_test \
             golang:1.25-bookworm

.DEFAULT_GOAL := help
.PHONY: help up down logs shell build typecheck basemap test-go test-unit verify verify-build test clean

help: ## List targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-14s %s\n", $$1, $$2}'

up: ## Start the whole stack (web on :5173, api on :8080, db, rustfs, migrate, worker)
	$(COMPOSE) up

down: ## Stop the stack; keeps every volume
	$(COMPOSE) down

logs: ## Follow the web (Vite) log; docker compose logs -f worker for ingest jobs
	$(COMPOSE) logs -f web

shell: ## Interactive shell in the web container
	$(COMPOSE) run --rm web bash

build: ## Typecheck and build apps/web to dist/ (run before verify:build outside make)
	$(COMPOSE) run --rm web npm run build

typecheck: ## Typecheck apps/web (tsc --noEmit)
	$(COMPOSE) run --rm web npm run typecheck

basemap: ## Re-cut the test basemap extract (404s once build-basemap.sh's BUILD ages out)
	$(COMPOSE) run --rm web npm run basemap

test-go: ## Go tests (services/server), one package at a time, against compose's holdmytrack_test db
	$(COMPOSE) up -d --wait db
	$(COMPOSE) exec -T db sh -c 'createdb -U holdmytrack holdmytrack_test 2>/dev/null || true'
	$(GO) go test -p 1 ./...

# verify:style compares against services/server/internal/mapstyle/styles, and verify:api-types
# against the API contract in services/server/api, and the map rule cases read testdata/; the web
# container sees none of these otherwise, so each is mounted where the relative paths land.
test-unit: ## Web unit tests and drift checks (fix: npm run build:style / gen:api-types)
	$(COMPOSE) run --rm -v $(CURDIR)/services/server/internal/mapstyle/styles:/services/server/internal/mapstyle/styles:ro \
	  -v $(CURDIR)/services/server/api:/services/server/api:ro \
	  -v $(CURDIR)/testdata:/testdata:ro \
	  web sh -c "npm run test:unit && npm run verify:style && npm run verify:api-types"

verify: ## Headless Playwright map checks against a dev server (test profile)
	$(TEST) npm run verify:map

verify-build: ## Build, then run the same checks against the production bundle
	$(TEST) sh -c "npm run build && npm run verify:build"

test: test-go test-unit verify verify-build ## Every CI suite except the doc-refs check and Android tests

clean: ## Stop and drop every volume (database, object store, node_modules)
	$(COMPOSE) down -v
