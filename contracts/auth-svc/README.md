# auth-svc: sign-in contract

| | |
|---|---|
| Spec | [`openapi.yaml`](openapi.yaml) (OpenAPI 3.1) |
| Version | `0.1.0-draft` (WBS-1.1, contracts v0) |
| Owner | Admin & Identity lane (Zarif Beste Süner) |
| Served at | `/api/auth` and `/api/setup` |
| Design notes | [`docs/design/identity-v0.md`](../../docs/design/identity-v0.md) |

## Status

| Group | State | Needed by |
|---|---|---|
| Register, login, refresh, logout, me | Implemented; the spec matches the running service | Dash and Admin sign-in (WBS-2.2), now |
| `role` on the user | Draft | Admin access check, WBS-3.1, W2 |
| First-admin setup (`/setup/status`, `/setup/admin`) | Draft (`x-status: draft`) | Setup wizard, WBS-11.1, W6 |

## Rules clients rely on

- Tokens travel in the `X-Access-Token` / `X-Refresh-Token` response headers, never in bodies.
- A refresh token works once. Sending an old one ends the whole session.
- Only a 401 means "signed out". A database or Redis fault answers 5xx, so the client keeps its tokens.

## Lint

From this directory, with Node 22 and pnpm (same tool as `contracts/dash-api/`):

```bash
pnpm dlx --package=@redocly/cli@1.25.15 redocly lint openapi.yaml
```

## Open questions for v0

1. **One name rule** for usernames, organization and team names. auth-svc allows `_` and needs 3 to 39 characters; `OwnerName` in dash-api does not allow `_` and accepts 1 character.
2. **Password hashing** (OI-05): bcrypt today, Argon2id in the Project Plan.
3. **Refresh token storage**: `localStorage` today; an `HttpOnly` cookie scoped to `/api/auth` would keep it away from scripts.

## Changing it

Contract changes go through a PR approved by every affected owner: Dash (Bilge) for anything the web apps use, Platform (Ceyda) for gateway routing.
