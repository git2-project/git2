# git2 repo — roadmap

Written in W1 (28.10.2026). Built from the team map (`git2-team-map.html`), `README.md`, `CLAUDE.md`, `Charter.md` and `ProjectPlan.md`. It covers the `git2` repo only; codeplane, runner and deploy keep their own plans.

## 0. Ground rules

Already agreed on the team map:

1. **PRs only.** Nobody pushes to `main`.
2. **One shared Supabase dev DB.** Nobody edits it by hand.
3. **Schema lives in `git2`.** Other repos ask for tables in an issue; the migration is written here.
4. **One Postgres schema per service** (`auth_svc`, `dash_api`, `admin_api`, `ci_svc`, …). Never edit an applied migration; add a new one.

New in this roadmap:

5. **No migrations from a laptop.** Only GitHub Actions writes the schema of the Supabase dev DB.
6. **PR to `main` → check, merge to `main` → apply.** A pull request validates its migrations against a throwaway Postgres inside the Action and never touches Supabase. When the PR merges (the push to `main`), the Action applies the pending migrations to Supabase.
   *Why not apply on PR open:* unreviewed schema would land in the shared DB, two open PRs would both apply, and a closed PR would leave orphan tables behind.
7. **Every PR updates `CHANGELOG.md`.** Releases are cut from it (section 2).

## 1. Phase A — pipeline and rules (W1–W2, before any new migration)

Status 28.10: CI is not assigned yet, and Supabase work waits until everyone has access to the project. `CHANGELOG.md` is in place.

| # | Task | Done when | Owner |
|---|---|---|---|
| A1 | Branch protection on `main` (GitHub settings) | PR required, 1 approving review, required checks `ci`, `migrations-check`, `changelog`; no force push; applies to admins too | repo admin |
| A2 | `.github/workflows/ci.yml` (on PR and push to `main`) | `go vet`, `go build ./...`, `go test` green; a `postgres` service container sets `TEST_POSTGRES_DSN`, so the integration tests run in CI for the first time | CI owner (unassigned) |
| A3 | `.github/workflows/migrations.yml`, job **`migrations-check`** (on PR to `main`) | Fails on: a name that is not `NNN_<service>_<what>.sql`, a missing `-- ` first line, a duplicate or skipped `NNN`, any modified, renamed or deleted file under `migrations/` that already exists on `main`. Then applies **all** migrations from scratch to a Postgres container on the same major version as Supabase | CI owner (unassigned) |
| A4 | Same workflow, job **`migrate`** (on push to `main` touching `migrations/**`, plus `workflow_dispatch`) | Runs in GitHub Environment `supabase-dev` with secret `SUPABASE_DB_URL`; `concurrency: supabase-dev-migrate` (no cancel), so two merges never race; writes the `--status` list to the job summary | deferred: Supabase access |
| A5 | Move `helpers/dev/migrate.sh` → `helpers/ci/migrate.sh` | Reads only `DATABASE_URL` from the environment (no `.env`); takes `pg_advisory_lock` for the run; stores a sha256 per file in `schema_migrations` and fails if an applied file's checksum changed | CI owner (unassigned) |
| A6 | Local workflow without Supabase | `make migrate` removed. `make show-migrations` stays (read-only). New `make migrate-scratch` applies migrations to a local Postgres in `docker-compose.dev.yml` and refuses any `*.supabase.co` / pooler host, so a migration can be tried before opening the PR | CI owner (unassigned) |
| A7 | Reconcile `001_auth_users.sql` | `001` was changed after its first commit (1d51a22). Check `auth_svc.users` on Supabase against the file. If they differ, either add `002_auth_users_fix.sql` that brings the DB in line, or — since there is no real data yet — one recorded reset of `auth_svc` before v0.1.0. From then on `001` is frozen | deferred: Supabase access |
| A8 | Separate DB roles *(recommended)* | `git2_migrator` owns the service schemas and lives only in the Action secret. `git2_app` gets DML only and is what everyone puts in `.env`. Rule 5 is then enforced by permissions, not by trust. Bootstrapped once in the Supabase SQL editor, with the script kept in `docs/ops/` | deferred: Supabase access |
| A9 | Docs | README *Development* section, `CLAUDE.md` *Commands* and *Conventions*, `env.example` (DSN = `git2_app`), team map label `db/migrations` → `migrations/` | with A2–A6 |
| A10 | `CHANGELOG.md` + `changelog` check | File seeded (section 2); the check fails a PR that does not touch it unless it has the `no-changelog` label (CI-only or typo PRs) | Beste (file done; check with CI owner) |

**Supabase connection from Actions:** use the **session pooler** connection string (port 5432 on the `pooler.supabase.com` host). GitHub-hosted runners have no IPv6 and the direct `db.<ref>.supabase.co` host is IPv6-only; the transaction pooler (6543) is a poor fit for DDL.

### Migration flow after Phase A

```
dev branch ── PR to main ──► migrations-check   (throwaway Postgres; never Supabase)
                               ci, changelog
                  │ review + merge
                  ▼
           push to main ─────► migrate          (environment supabase-dev, one at a time)
                                    │
                                    ▼
                             Supabase dev DB ── seen by Dash, console, codeplane, runner
```

Two PRs that both pick the same `NNN`: the second one to rebase fails `migrations-check` and renumbers.

## 2. Version notes — `CHANGELOG.md`

- At the repo root, in [Keep a Changelog 1.1](https://keepachangelog.com/en/1.1.0/) format, versions in [SemVer](https://semver.org/).
- Every PR adds lines under `## [Unreleased]`, in the groups `Added`, `Changed`, `Fixed`, `Removed`, `Security`, plus **`Database`**, which lists each new migration file by name.
- One line per change: service, what changed, PR number. Example:

```markdown
## [Unreleased]

### Added
- auth-svc: `POST /api/auth/register`, `/login`, `/refresh`, `/logout`, `GET /api/auth/me` (#3)
- ci: migrations are applied to the Supabase dev DB by GitHub Actions on merge to `main` (#7)

### Database
- `001_auth_users.sql` — `auth_svc.users`
```

- **Release:** the `Unreleased` block becomes `## [x.y.z] - YYYY-MM-DD`, the tag `vx.y.z` is pushed (Ceyda tags releases), and the GitHub release body is that section.
- Proposed versions, one per milestone:

| Version | Date | Milestone |
|---|---|---|
| v0.1.0 | 30.10 | M0 Foundation — auth-svc, CI, migration pipeline |
| v0.2.0 | 13.11 | M1 First Push |
| v0.3.0 | 27.11 | M2 First Merge |
| v0.4.0 | 11.12 | M3 Green Pipeline |
| v0.5.0 | 18.12 | M4 Restore |
| v1.0.0 | 25.12 | Codebase deadline |

## 3. Phase B — features in this repo (from ProjectPlan)

Each row that needs tables ships its migration in the same PR, under the service's own schema.

| Week | Beste — auth-svc, admin-api, console | Bilge — dash-api, Dash | Others in `git2` |
|---|---|---|---|
| W1 26.10 | 2.1 auth-svc sign-in ✓ · Phase A | 2.2 web shells + shared sign-in | 1.1 contracts v0 (all) |
| W2 02.11 | 3.1 users, teams, roles | 6.1 code browser | 1.2 gateway (Ceyda), 1.3 outbox (Abdullah) |
| W3 09.11 | 3.1 cont. | 6.1 cont. | 1.2 cont. |
| W4 16.11 | 4.1 OIDC SSO | 8.2 MR + review UI | 10.3 ci-svc + job queue (Osman), 12.1 backup snapshot (Ceyda) |
| W5 23.11 | 4.2 LDAP | 8.2 cont. · 9.1 issues | 1.4 cache + rate limit (Ceyda), 10.3 + 12.1 cont. |
| W6 30.11 | 3.2 PATs + SSH keys · 11.1 setup wizard | 9.1 cont. · 10.5 pipeline views | 10.4 logs + artifacts (Osman), 12.2 bucket drivers (Ceyda) |
| W7 07.12 | 11.1 cont. | 9.1 cont. · 10.5 cont. | 10.4 cont., 12.3 restore (Ceyda) |
| W8 14.12 | 11.2 audit log + health · 16.3 API reference | 17.2 E2E tests + UI polish | 12.3 cont., 16.x guides |
| W9 21.12 | 17.3 bug bash, v1.0.0 | 17.3 | 17.3 |

Expected schemas: `auth_svc` (teams, roles, PATs, SSH keys, OIDC/LDAP identities, audit log), `dash_api` (issues, labels, milestones, notifications, MR comments), `ci_svc` (pipelines, jobs, artifacts), outbox table(s).

## 4. Open questions

1. **Ownership in this repo.** The team map gives Ceyda `git2-deploy` and Apo `git2-codeplane`; the Charter and ProjectPlan also put the gateway (Ceyda), outbox (Abdullah) and ci-svc (Osman) inside `git2`. Who reviews PRs to `gateway/`, `ci-svc/` and the outbox?
2. **Customer installs have no GitHub Actions.** The Action covers the dev DB only. For `git2ctl install/upgrade` the binary will need to apply migrations itself (embedded with `go:embed`, same `schema_migrations` table). Plan it before W6 (13.2 / 13.3).
3. **Rollback.** Proposal: no down migrations; fix forward with a new file.
4. **A8 roles:** who holds the Supabase owner password and runs the one-time bootstrap?
