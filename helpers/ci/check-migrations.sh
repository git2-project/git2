#!/usr/bin/env bash
#
# check-migrations.sh BASE_REF — the static half of the migrations-check job
# (docs/project_docs/to_team/Roadmap.md, A3). Fails when BASE_REF does not
# resolve, or when a .sql file in migrations/:
#
#   - is not named NNN_<service>_<what>.sql (lowercase, digits, underscores);
#   - does not start with a "-- " comment line (make show-migrations prints it);
#   - shares its NNN with another file, or leaves a gap after 001;
#   - existed where this branch left BASE_REF and was modified, renamed,
#     deleted or changed type since, committed or not. Applied migrations
#     are frozen; fix forward with a new file.
#
# Run it locally the same way: helpers/ci/check-migrations.sh origin/dev/main
set -euo pipefail

base="${1:?usage: check-migrations.sh BASE_REF}"
cd "$(git rev-parse --show-toplevel)"

# A base that does not resolve must fail loudly: otherwise the frozen-file
# diff below would read nothing and report "ok".
if ! git rev-parse --verify --quiet "${base}^{commit}" >/dev/null; then
	echo "::error::base ref '$base' does not resolve to a commit (not fetched?)" >&2
	exit 1
fi
fork="$(git merge-base "$base" HEAD)"

fail=0
err() { echo "::error file=$1::$2"; fail=1; }

# Only .sql files are migrations; a README or similar in migrations/ is fine.
shopt -s nullglob
files=(migrations/*.sql)
want=1
prev=""
for f in "${files[@]}"; do
	bn="$(basename "$f")"
	if [[ ! "$bn" =~ ^([0-9]{3})_[a-z0-9]+_[a-z0-9_]+\.sql$ ]]; then
		err "$f" "name must be NNN_<service>_<what>.sql (lowercase, digits, underscores): $bn"
		# Still count a recognisable NNN_ prefix, so one bad name does not
		# show up as a gap before the next file.
		[[ "$bn" =~ ^([0-9]{3})_ ]] || continue
	fi
	num=$((10#${BASH_REMATCH[1]}))
	if [ "$num" -lt "$want" ]; then
		err "$f" "number $(printf %03d "$num") is used twice: $prev and $bn"
	elif [ "$num" -gt "$want" ]; then
		err "$f" "number $(printf %03d "$want") is missing before $bn"
	fi
	want=$((num + 1))
	prev="$bn"
	if ! head -n 1 "$f" | grep -q '^-- [^[:space:]]'; then
		err "$f" "the first line must be a '-- ' comment saying what the migration does"
	fi
done

# Files that exist where this branch left the base and were changed since,
# committed or not, so a local run catches what CI will. M modified,
# D deleted, R renamed, T type changed (for example replaced by a symlink).
# Captured first, so a git failure stops the script (set -e) instead of
# reading as "no changes".
changes="$(git diff --name-status --diff-filter=MDRT "$fork" -- migrations/)"
while IFS=$'\t' read -r status path rest; do
	[ -z "$status" ] && continue
	case "$status" in
		M*) err "$path" "an existing migration was modified; add a new file instead" ;;
		D*) err "$path" "an existing migration was deleted" ;;
		R*) err "$path" "an existing migration was renamed to ${rest}" ;;
		T*) err "$path" "an existing migration changed type (for example to a symlink)" ;;
	esac
done <<<"$changes"

if [ "$fail" -ne 0 ]; then
	echo "migrations-check: failed" >&2
	exit 1
fi
echo "migrations-check: ${#files[@]} file(s) ok"
