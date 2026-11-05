# Gateway: public HTTP contract (reference)

The gateway is the one HTTPS entry point of a git2 instance (WBS-1.2). It has no gRPC API, so its contract is a document: the routes, the sign-in checks, the headers upstreams receive, the limits and the errors. It lives in git2-deploy with the rest of the Platform & Backup lane's docs, and is referenced from here.

| | |
|---|---|
| Source | [`docs/api/gateway.md`](https://github.com/git2-project/git2-deploy/blob/v0.1.0/docs/api/gateway.md) in git2-deploy |
| Version | v0 (pinned with git2-deploy `v0.1.0`) |
| Owner | Platform & Backup lane (Ceyda Akın) |

## What upstream services must know

- Requests reach you only through the gateway. Behind a route that needs sign-in, `X-Git2-User-Id` carries the user's ID.
- The gateway removes every client-sent `X-Git2-*` and `X-Real-IP` header before adding its own. Trust these headers only on connections from the gateway.
- `X-Real-IP` is the client's IP address. Use it, not `RemoteAddr`, for per-IP rate limits.
- A Redis or upstream fault is a 5xx, never a 401.

## Changing it

Open a PR in git2-deploy that changes `docs/api/gateway.md`. Every lane whose routes are affected approves it.
