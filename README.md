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
- **State and events:** PostgreSQL holds all data, and a transactional outbox carries events such as `repo.pushed` and `pipeline.finished`. No Redis, no Kafka or NATS.
- **Object storage:** `gocloud.dev/blob`: S3, GCS, Azure Blob, R2, SeaweedFS, Garage, Ceph or local disk.
- **Telemetry:** OpenTelemetry.

## Status

Early development, on the 9-week plan in the [org README](https://github.com/git2-project). Nothing here is ready to run yet; build and test commands will land in this file and in `CLAUDE.md` as the code does.

## Contributing

- Work stays inside this repo's scope. Changes to shared contracts go through a PR approved by every affected owner.
- Every PR ships with tests and needs one human review before merge.
