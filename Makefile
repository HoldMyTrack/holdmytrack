# Thin wrapper around docker compose. The front door now that the repository spans two
# languages: nothing here is logic, and anything that grows logic belongs in a script
# under the package it serves, not in this file.

COMPOSE := docker compose
TEST    := $(COMPOSE) --profile test run --rm test

.DEFAULT_GOAL := help
.PHONY: help up down logs shell build typecheck basemap verify verify-build test clean

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

verify: ## Headless map checks against a dev server (test profile)
	$(TEST) npm run verify:map

verify-build: ## Build, then ask the same questions of the production bundle
	$(TEST) sh -c "npm run build && npm run verify:build"

test: verify verify-build ## Both verification suites

clean: ## Stop and drop the node_modules volume; next up re-seeds it
	$(COMPOSE) down -v
