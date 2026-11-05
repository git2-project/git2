#!/usr/bin/env bash
#
# check-migrations.sh BASE_REF — the static half of the migrations-check job
# (docs/project_docs/to_team/Roadmap.md, A3). Fails when a file in migrations/:
#
#   - is not named NNN_<service>_<what>.sql (lowercase, digits, underscores);
#   - does not start with a "-- " comment line (make show-migrations prints it);
#   - shares its NNN with another file, or leaves a gap after 001;
#   - already exists on BASE_REF and was modified, renamed or deleted here.
#     Applied migrations are frozen; fix forward with a new file.
#
# Run it locally the same way: helpers/ci/check-migrations.sh origin/dev/main
set -euo pipefail

base="${1:?usage: check-migrations.sh BASE_REF}"
cd "$(git rev-parse --show-toplevel)"

fail=0
err() { echo "::error file=$1::$2"; fail=1; }

shopt -s nullglob
files=(migrations/*)
want=1
prev=""
for f in "${files[@]}"; do
	bn="$(basename "$f")"
	if [[ ! "$bn" =~ ^([0-9]{3})_[a-z0-9]+_[a-z0-9_]+\.sql$ ]]; then
		err "$f" "name must be NNN_<service>_<what>.sql (lowercase, digits, underscores): $bn"
		continue
	fi
	num=$((10#${BASH_REMATCH[1]}))
	if [ "$num" -lt "$want" ]; then
		err "$f" "number $(printf %03d "$num") is used twice: $prev and $bn"
	elif [ "$num" -gt "$want" ]; then
		err "$f" "number $(printf %03d "$want") is missing before $bn"
		want=$num
	fi
	want=$((num + 1))
	prev="$bn"
	if ! head -n 1 "$f" | grep -q '^-- [^[:space:]]'; then
		err "$f" "the first line must be a '-- ' comment saying what the migration does"
	fi
done

# Files that exist on the base and were touched here. Three-dot: only what
# this branch changed since it left the base, not what the base did since.
while IFS=$'\t' read -r status path rest; do
	[ -z "$status" ] && continue
	case "$status" in
		M*) err "$path" "an existing migration was modified; add a new file instead" ;;
		D*) err "$path" "an existing migration was deleted" ;;
		R*) err "$path" "an existing migration was renamed to ${rest}" ;;
	esac
done < <(git diff --name-status --diff-filter=MDR "$base...HEAD" -- migrations/)

if [ "$fail" -ne 0 ]; then
	echo "migrations-check: failed" >&2
	exit 1
fi
echo "migrations-check: ${#files[@]} file(s) ok"
