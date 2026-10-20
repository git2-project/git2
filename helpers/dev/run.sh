#!/usr/bin/env bash
#
# run.sh — `make run`: the Go binary on :8080 and the Vite dev server on :5173,
# with prefixed logs. Ctrl-C stops both.
#
# The Go binary is built once and run directly rather than through `go run`,
# whose PID is the launcher rather than the server, so killing it would orphan
# the port. Kept portable to macOS bash 3.2 + BSD tools.
# Ports are overridable: API_PORT=9000 WEB_PORT=3000 make run
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

. "$ROOT/helpers/dev/env.sh"

API_PORT="${API_PORT:-8080}"
WEB_PORT="${WEB_PORT:-5173}"

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
if [ ! -d web/node_modules ]; then
	echo "==> installing web dependencies..."
	npm --prefix web ci
fi

echo "==> building git2 binary..."
BIN_DIR="$(mktemp -d)"
go build -o "$BIN_DIR/git2" ./cmd/git2

pids=()
cleanup() {
	trap - INT TERM EXIT
	echo "" >&2
	echo "==> stopping api/web..." >&2
	if [ "${#pids[@]}" -gt 0 ]; then
		kill "${pids[@]}" 2>/dev/null || true
	fi
	wait 2>/dev/null || true
	rm -rf "$BIN_DIR"
}
trap cleanup INT TERM EXIT

use_colour=0
[ -t 1 ] && use_colour=1

# run launches a process in the background with prefixed output. Output goes
# through awk in a process substitution so $! is the process's own PID, not
# the prefixer's. awk's fflush() line-buffers on both GNU and BSD.
run() {
	local name="$1" colour="$2"
	shift 2
	local prefix="[$name] "
	[ "$use_colour" = 1 ] && prefix="${colour}[$name]"$'\033[0m '
	"$@" > >(awk -v p="$prefix" '{ print p $0; fflush() }') 2>&1 &
	pids+=("$!")
}

echo "==> api http://localhost:$API_PORT   web http://localhost:$WEB_PORT   (Ctrl-C stops both)"
run api $'\033[36m' "$BIN_DIR/git2" --port "$API_PORT"
# exec so the tracked PID is vite's own node process, not an npm wrapper.
run web $'\033[35m' env API_URL="http://localhost:$API_PORT" WEB_PORT="$WEB_PORT" \
	sh -c 'cd web && exec ./node_modules/.bin/vite --strictPort'

# Block until either process exits or Ctrl-C; the EXIT trap stops the other.
wait -n 2>/dev/null || wait
