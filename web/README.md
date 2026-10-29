# git2 web

The two browser apps of git2, in one pnpm workspace:

| Path | What it is |
| :--- | :--- |
| `apps/dash/` | **Dash**, the developer plane. Served at `/` |
| `apps/admin/` | **Admin**, the IT plane. Served at `/admin/` |
| `packages/ui/` | Shared UI kit: theme, sign-in and registration pages, `AuthProvider`, `RequireAuth`, `AppShell` |
| `packages/api/` | Client for the git2 API: auth calls, token storage, refresh on 401 |

React 18 + TypeScript + Vite. The apps compile to static files that the gateway serves; there is no Node in production. Packages are consumed as TypeScript source, so there is no build step for them.

## Development

Needs Node 22 and pnpm 9 (`corepack enable` picks the pinned pnpm), plus the Go API running on `:8080` (`make run` in the repo root).

```bash
cd web
pnpm install
pnpm dev          # Dash on http://localhost:5173, Admin on http://localhost:5173/admin/
pnpm test         # Vitest, every package
pnpm typecheck
pnpm lint
pnpm build        # apps/*/dist
```

Always open both apps through **port 5173**. The Dash dev server proxies `/api/` to the Go server (`GIT2_API_URL`, default `http://localhost:8080`) and `/admin/` to the Admin dev server on 5174. One origin means one session: signing in to Dash also signs you in to Admin, as it will behind the gateway.

## Sign-in

- auth-svc returns tokens in the `X-Access-Token` / `X-Refresh-Token` response headers. `packages/api` stores both in `localStorage` (key `git2.tokens`) and sends `Authorization: Bearer`.
- On a 401 the client refreshes once and retries. auth-svc ends the session when a refresh token is replayed, so a token is never sent twice: requests in one page share one refresh, and tabs take turns through the `git2.refresh` Web Lock and reuse the pair another tab already stored.
- When the server ends the session, the sign-in page says it expired; signing out in another tab does not.
- Only a 401 signs the user out. A 5xx or network error keeps the tokens and shows *Try again*.
- Sign-in and sign-out in one tab or plane reach the others through the `storage` event.
- Accounts are created at Dash `/register` for now; Admin user management (WBS-3.1) replaces it.
