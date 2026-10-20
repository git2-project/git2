# env.sh — sourced by the helpers. dotenv KEY prints KEY's value: the shell
# environment wins, then the repo-root .env. Read per key rather than by
# sourcing .env, because a DSN's '&' or '$' would be run as shell syntax.
# A trailing " # comment" is dropped; a '#' inside a value (passwords) is kept.
dotenv() {
	local key="$1"
	if [ -n "${!key:-}" ]; then
		printf '%s' "${!key}"
		return
	fi
	[ -f "$ROOT/.env" ] || return 0
	sed -n "s/^${key}=//p" "$ROOT/.env" | tail -1 | sed -e 's/[[:space:]][[:space:]]*#.*$//' -e 's/[[:space:]]*$//'
}
