# Thin wrapper around docker compose. The front door now that the repository spans two
# languages: nothing here is logic, and anything that grows logic belongs in a script
# under the package it serves, not in this file.

COMPOSE := docker compose
TEST    := $(COMPOSE) --profile test run --rm test
# A throwaway Go toolchain for services/server's unit tests, so `make test` needs Docker and
# nothing else, like every other target here. Named volumes keep the module and build
# caches between runs.
GO      := docker run --rm -v $(CURDIR)/services/server:/src -w /src \
             -v holdmytrack-go-mod:/go/pkg/mod -v holdmytrack-go-build:/root/.cache/go-build \
             golang:1.25-bookworm

.DEFAULT_GOAL := help
.PHONY: help up down logs shell build typecheck basemap test-go test-unit verify verify-build test clean

help: ## List targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-14s %s\n", $$1, $$2}'

up: ## Start the web client on http://localhost:5173
	$(COMPOSE) up

down: ## Stop everything; keeps the node_modules volume
	$(COMPOSE) down

logs: ## Follow the web service log
	$(COMPOSE) logs -f web

shell: ## Interactive shell in the web container
	$(COMPOSE) run --rm web bash

build: ## Typecheck and build apps/web to dist/
	$(COMPOSE) run --rm web npm run build

typecheck: ## tsc --noEmit
	$(COMPOSE) run --rm web npm run typecheck

basemap: ## Re-cut the .pmtiles extract (pmtiles is baked into the dev image)
	$(COMPOSE) run --rm web npm run basemap

test-go: ## Go unit tests (services/server)
	$(GO) go test ./...

# verify:style compares against services/server/internal/mapstyle/styles, which the web
# container does not otherwise see; mounted where the script's own relative path lands.
test-unit: ## Web unit tests, plus the served style documents' drift check
	$(COMPOSE) run --rm -v $(CURDIR)/services/server/internal/mapstyle/styles:/services/server/internal/mapstyle/styles:ro \
	  web sh -c "npm run test:unit && npm run verify:style"

verify: ## Headless map checks against a dev server (test profile)
	$(TEST) npm run verify:map

verify-build: ## Build, then ask the same questions of the production bundle
	$(TEST) sh -c "npm run build && npm run verify:build"

test: test-go test-unit verify verify-build ## Every suite: Go, web unit, and both verification suites

clean: ## Stop and drop the node_modules volume; next up re-seeds it
	$(COMPOSE) down -v
