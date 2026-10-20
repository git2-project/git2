#!/usr/bin/env bash
#
# run.sh — `make run`: the Go binary on :8080. Ctrl-C stops it.
#
# Port is overridable: API_PORT=9000 make run
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

. "$ROOT/helpers/dev/env.sh"

API_PORT="${API_PORT:-8080}"

# --- preflight -------------------------------------------------------------
# The binary reads .env itself; these are only the preflight checks.
if [ -z "$(dotenv POSTGRES_DSN)" ]; then
	echo "!! POSTGRES_DSN is not set. Copy env.example to .env and fill it in." >&2
	exit 1
fi
redis_addr="$(dotenv REDIS_ADDR)"
redis_addr="${redis_addr:-localhost:6379}"
if command -v redis-cli >/dev/null 2>&1 &&
	! redis-cli -h "${redis_addr%:*}" -p "${redis_addr##*:}" ping >/dev/null 2>&1; then
	echo "!! Redis not reachable at ${redis_addr}. Start it first: make deps" >&2
	exit 1
fi

echo "==> api http://localhost:$API_PORT   (Ctrl-C stops it)"
exec go run ./cmd/git2 --port "$API_PORT"
