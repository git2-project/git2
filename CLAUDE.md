# git2 — notes for Claude

Read `README.md` (layout, commands, auth API) and `docs/Charter.md` (scope, owners) first.

## Stack
- Go 1.26, one module at the root (`github.com/git2-project/git2`), one binary in `cmd/git2` that mounts every service under `/api`.
- chi router, pgx/v5 pool, go-redis/v9, golang-jwt/v5, bcrypt. Shared code lives in `internal/`; a service lives in its own top-level dir (`auth-svc/`, package `authsvc`).
- Postgres = the Supabase project in dev (`POSTGRES_DSN`). Redis runs locally (devcontainer service or `make deps`).

## Conventions
- One Postgres schema per service. auth-svc uses `auth_svc`, never `auth` (Supabase owns that schema).
- Migrations are plain SQL in `migrations/NNN_<service>_<what>.sql`; the first line is a `-- ` comment that `make show-migrations` prints. Never edit an applied migration; add a new one.
- Handlers decode with `httpx.DecodeJSON(w, r, &req)` (unknown fields, trailing data and bodies over 64 KiB rejected) and answer with `httpx.JSON` / `httpx.Error` (`{"error": "..."}`).
- Tokens travel in `X-Access-Token` / `X-Refresh-Token` response headers, never in bodies. Refresh sessions are a Redis whitelist (`sess:<sid>` = current JTI) rotated by one Lua compare-and-swap (`rotateSession`), never GET-then-SET; revoked access JTIs are `revoked:<jti>`.
- JWTs are HS256 only with `exp` required (`jwt.WithValidMethods`, `jwt.WithExpirationRequired`).
- A database or Redis fault answers 5xx, never 401: clients drop their tokens on a 401.
- Other services protect routes with `authsvc.RequireAuth(jwt, redis)` and read the caller with `authsvc.UserID(ctx)`.
- Tests: Redis-dependent code is tested against miniredis; Postgres code has `*_integration_test.go` files that skip unless `TEST_POSTGRES_DSN` is set.

## Web (`web/`)
- pnpm workspace: `apps/dash`, `apps/admin` (base `/admin/`), `packages/ui` (shared UI kit), `packages/api` (API client). Shared versions live in the `catalog:` of `web/pnpm-workspace.yaml`; React and react-router must stay a single copy.
- All API calls go through `request()` in `packages/api`; never call `fetch` for `/api` directly. Only a 401 clears the tokens.
- Open both apps through the Dash dev server (:5173) so they share one origin and one session.
- Commands (in `web/`): `pnpm dev` · `pnpm test` · `pnpm typecheck` · `pnpm lint` · `pnpm build`

## Docs lookup
Context7 is configured for the repo in `.mcp.json`. Check library APIs (chi, pgx, go-redis, golang-jwt) there before relying on memory.

## Commands
`make run` · `make tests` · `make migrate` · `make show-migrations` · `make deps` · `make build`
