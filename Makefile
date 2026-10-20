# git2 — local development
#
# First time:  cp env.example .env   (fill in POSTGRES_DSN and JWT_SECRET)
#              make deps             (Redis; the devcontainer already has it)
#              make migrate
# Each cycle:  make run   /   make tests

SHELL := /bin/bash

.PHONY: run tests test-go test-web deps migrate show-migrations build

## Run the Go API (:8080) and the Vite dev server (:5173) together, with
## prefixed logs. Ctrl-C stops both. Needs Redis and POSTGRES_DSN.
run:
	@bash helpers/dev/run.sh

## Every test: Go (unit + the Postgres integration test when
## TEST_POSTGRES_DSN is set) and the web app's Vitest suite.
tests: test-go test-web

test-go:
	@echo "==> go test"
	@ROOT=$(CURDIR); . helpers/dev/env.sh; TEST_POSTGRES_DSN="$$(dotenv TEST_POSTGRES_DSN)" go test ./...

test-web:
	@echo "==> vitest"
	@[ -d web/node_modules ] || npm --prefix web ci
	@npm --prefix web test --silent

## Start the local Redis for host-side development.
deps:
	docker compose -f docker-compose.dev.yml up -d --wait

## Apply pending SQL migrations to POSTGRES_DSN.
migrate:
	@bash helpers/dev/migrate.sh

## List migrations with [X] applied / [ ] pending.
show-migrations:
	@bash helpers/dev/migrate.sh --status

## Build the git2 binary and the static web bundle.
build:
	go build -o bin/git2 ./cmd/git2
	npm --prefix web run build
