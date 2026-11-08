# Gateway: public HTTP contract (reference)

The gateway is the one HTTPS entry point of a git2 instance (WBS-1.2). It has no gRPC API, so its contract is a document: the routes, the sign-in checks, the headers upstreams receive, the limits and the errors. It lives in git2-deploy with the rest of the Platform & Backup lane's docs, and is referenced from here.

| | |
|---|---|
| Source | [`docs/api/gateway.md`](https://github.com/git2-project/git2-deploy/blob/5318ffa/docs/api/gateway.md) in git2-deploy |
| Version | v0.1, under review in git2-deploy#13 (pinned to its commit `5318ffa`; re-pinned to the release tag once #13 is merged) |
| Owner | Platform & Backup lane (Ceyda Akın) |

## What upstream services must know

- Requests reach you only through the gateway.
- **`/api/*`:** verify the access token yourself with `authsvc.RequireAuth` and read the caller with `authsvc.UserID(ctx)`. The gateway checks the token too, but forwards `Authorization` unchanged and does **not** set `X-Git2-User-Id` here.
- **Git routes (`/<owner>/<repo>.git/*`):** `X-Git2-User-Id` carries the signed-in user's ID (git-svc reads it).
- On every route the gateway removes every client-sent `X-Git2-*`, `X-Real-IP` and `X-Forwarded-*` header before writing its own.
- **Client IP:** keep using `RemoteAddr` (`internal/ratelimit`). `internal/realip` (git2#22) replaces it with `X-Real-IP` only for connections from `TRUSTED_PROXY_CIDRS`. Do not read `X-Real-IP` directly: that skips the trust check.
- **Redis fault:** the revoked and disabled checks on access tokens fail open, as in auth-svc; signature and expiry are still checked, so a revoked token works at most until it expires (`JWT_EXPIRY`). On Git routes a database or Redis fault is a `503`, never a `401`.

## Changing it

Open a PR in git2-deploy that changes `docs/api/gateway.md`. Every lane whose routes are affected approves it.
