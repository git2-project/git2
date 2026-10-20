#!/usr/bin/env bash
#
# migrate.sh — apply (or, with --status, list) the SQL files in migrations/.
#
# Applied files are recorded by name in public.schema_migrations. Each file
# runs in its own transaction together with its INSERT there, so a failing
# migration leaves no row and no partial schema behind.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

. "$ROOT/helpers/dev/env.sh"
POSTGRES_DSN="$(dotenv POSTGRES_DSN)"
if [ -z "$POSTGRES_DSN" ]; then
	echo "POSTGRES_DSN is not set (env or .env)." >&2
	exit 1
fi

psql "$POSTGRES_DSN" -v ON_ERROR_STOP=1 -q -c \
	"SET client_min_messages = warning;
	 CREATE TABLE IF NOT EXISTS schema_migrations (
	   filename   TEXT        PRIMARY KEY,
	   applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	 );" >/dev/null
applied="$(psql "$POSTGRES_DSN" -At -c "SELECT filename FROM schema_migrations")"

if [ "${1:-}" = "--status" ]; then
	for f in migrations/*.sql; do
		bn="$(basename "$f")"
		intent="$(awk 'NR==1 && /^--/ {sub(/^-- ?/,""); print; exit}' "$f")"
		mark=" "
		printf '%s\n' "$applied" | grep -qxF "$bn" && mark="X"
		printf '  [%s] %s  %s\n' "$mark" "$bn" "$intent"
	done
	exit 0
fi

pending=0
for f in migrations/*.sql; do
	bn="$(basename "$f")"
	printf '%s\n' "$applied" | grep -qxF "$bn" && continue
	pending=$((pending + 1))
	echo "==> applying $bn"
	psql "$POSTGRES_DSN" -v ON_ERROR_STOP=1 --single-transaction -q \
		-f "$f" \
		-c "INSERT INTO schema_migrations (filename) VALUES ('$bn')" ||
		{ echo "migration $bn failed; rolled back." >&2; exit 1; }
done
if [ "$pending" -eq 0 ]; then echo "no pending migrations."; else echo "applied $pending migration(s)."; fi
