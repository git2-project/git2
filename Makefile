# git2 — local development
#
# First time:  cp env.example .env   (fill in POSTGRES_DSN and JWT_SECRET)
#              make deps             (Redis; the devcontainer already has it)
#              make migrate
# Each cycle:  make run   /   make tests

SHELL := /bin/bash

.PHONY: run tests deps migrate show-migrations build

## Run the Go API on :8080. Ctrl-C stops it. Needs Redis and POSTGRES_DSN.
run:
	@bash helpers/dev/run.sh

## Every Go test: unit + the Postgres integration test when
## TEST_POSTGRES_DSN is set. Only packages that have tests are listed.
tests:
	@ROOT=$(CURDIR); . helpers/dev/env.sh; TEST_POSTGRES_DSN="$$(dotenv TEST_POSTGRES_DSN)" \
		go test -count=1 $$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)

## Start the local Redis for host-side development.
deps:
	docker compose -f docker-compose.dev.yml up -d --wait

## Apply pending SQL migrations to POSTGRES_DSN.
migrate:
	@bash helpers/dev/migrate.sh

## List migrations with [X] applied / [ ] pending.
show-migrations:
	@bash helpers/dev/migrate.sh --status

## Build the git2 binary.
build:
	go build -o bin/git2 ./cmd/git2
