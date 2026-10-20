# git2

> **Your company's Git server, on your own machines.**
> Open source · Self-hosted · One command to install · One bucket to back up and restore everything.

**Repos:** [git2](https://github.com/git2-project/git2) · [git2-codeplane](https://github.com/git2-project/git2-codeplane) · [git2-runner](https://github.com/git2-project/git2-runner) · [git2-deploy](https://github.com/git2-project/git2-deploy)

This is the core repo: the shared contracts, the web UI and every control-plane service except the Git engine and the CI runner.

## What lives here

| Path | What it is |
| :--- | :--- |
| `contracts/` | OpenAPI specs for public APIs and gRPC protos shared between services |
| `web/` | React UI for both planes: **Dash** for developers, **Admin** for IT. Compiled to static files and served by `gateway` (no Node in prod) |
| `dash-api/` | Repos, code browsing, merge requests, review, issues, search |
| `admin-api/` | Users, teams, instance settings, runner management, audit log |
| `auth-svc/` | Sign-in for both planes, tokens, SSO and LDAP |
| `ci-svc/` | `.git2-ci.yml` spec, pipeline scheduling, job queue, logs, artifacts |
| `gateway/` | TLS termination, routing, static web assets |
| `backup-svc/` | Encrypted (`age`) snapshots of the database, repos, `git2.yaml` and secrets into one bucket, plus full restore |
| `cmd/git2/` | Single binary entrypoint that runs the services above |
| `internal/` | Go packages shared by the services: config, Postgres, Redis, JWT, passwords, HTTP helpers, rate limiting |
| `migrations/` | SQL migrations, applied in filename order by `make migrate`. One schema per service (`auth_svc`, …) |

The Git engine (`git-svc`) lives in [git2-codeplane](https://github.com/git2-project/git2-codeplane), the CI job executor in [git2-runner](https://github.com/git2-project/git2-runner), and install/upgrade tooling in [git2-deploy](https://github.com/git2-project/git2-deploy).

## Architecture

```
            browser / git client
                    │
                ┌───▼────┐
                │gateway │  TLS, routing, static web/
                └───┬────┘
      ┌────────┬────┴────┬─────────┬──────────┐
  dash-api admin-api  auth-svc   ci-svc   backup-svc
      └────────┴────┬────┴─────────┴──────────┘
                    │ gRPC
        ┌───────────┼──────────────┐
   PostgreSQL   git-svc         runners
  (data + outbox) (codeplane)  (git2-runner)
```

- **Services:** Go, talking gRPC internally; OpenAPI for public APIs.
- **State and events:** PostgreSQL holds all data, and a transactional outbox carries events such as `repo.pushed` and `pipeline.finished`. Redis holds sessions, rate limits and caches. No Kafka or NATS.
- **Object storage:** `gocloud.dev/blob`: S3, GCS, Azure Blob, R2, SeaweedFS, Garage, Ceph or local disk.
- **Telemetry:** OpenTelemetry.

## Status

Early development, on the 9-week plan in the [org README](https://github.com/git2-project). `auth-svc` runs today (backend only) and `web/` is an empty React + Vite app; the other services are still to come.

## Development

Open the repo in the devcontainer (VS Code → *Reopen in Container*). It has Go 1.26, Node 22, `psql` and a Redis service. The database is the team's Supabase project (dev only), reached through `POSTGRES_DSN`.

```bash
cp env.example .env    # fill in POSTGRES_DSN and JWT_SECRET (openssl rand -hex 32)
make migrate           # apply migrations/ to POSTGRES_DSN (make show-migrations lists them)
make run               # Go API on :8080 + Vite on :5173 (proxies /api), Ctrl-C stops both
make tests             # go test ./... + Vitest
```

Outside the devcontainer, `make deps` starts Redis from `docker-compose.dev.yml` first. Set `TEST_POSTGRES_DSN` to a throwaway database to include the Postgres integration test in `make tests`.

### auth-svc API

| Method | Path | |
| :--- | :--- | :--- |
| `POST` | `/api/auth/register` | `{email, username, password}` → `201` user |
| `POST` | `/api/auth/login` | `{email, password}` → user; tokens in `X-Access-Token` / `X-Refresh-Token` headers |
| `POST` | `/api/auth/refresh` | `{refresh_token}` → new token pair in the same headers; a replayed old token ends the session |
| `POST` | `/api/auth/logout` | bearer access token + optional `{refresh_token}`; revokes both, always `200` |
| `GET` | `/api/auth/me` | bearer access token → current user |

## Contributing

- Work stays inside this repo's scope. Changes to shared contracts go through a PR approved by every affected owner.
- Every PR ships with tests and needs one human review before merge.
