# Changelog

All notable changes to the `git2` repo. The format follows [Keep a Changelog 1.1](https://keepachangelog.com/en/1.1.0/) and versions follow [SemVer](https://semver.org/).

Every PR adds its lines under `## [Unreleased]`: one line per change, starting with the service, ending with the PR number. Groups: `Added`, `Changed`, `Fixed`, `Removed`, `Security`, and `Database` for each new migration file. A release renames `Unreleased` to `## [x.y.z] - YYYY-MM-DD` and is tagged `vx.y.z`.

## [Unreleased]

### Added
- ci: `ci` check on every pull request: gofmt, vet, build, migrations applied to a throwaway Postgres, `go test -race` with the integration tests (#20)
- ci: `web` check: typecheck, lint, test and build of `web/` (#20)
- ci: `migrations-check`: names, numbering, first-line comments and frozen files (`helpers/ci/check-migrations.sh`), then all migrations applied from scratch (#20)
- ci: `changelog` check: the PR touches `CHANGELOG.md` unless it has the `no-changelog` label (#20)
- auth-svc: `POST /api/auth/register`, `/login`, `/refresh`, `/logout` and `GET /api/auth/me`; tokens in `X-Access-Token` / `X-Refresh-Token` headers, refresh sessions rotated in Redis, per-IP rate limits on the public routes
- auth-svc: `RequireAuth` middleware and `UserID(ctx)` for other services
- internal: shared config, Postgres pool, Redis client, JWT (HS256), bcrypt, HTTP helpers and rate limiting
- dev: devcontainer, `make run` / `tests` / `deps` / `build`, migration runner
- gateway: one entry point (`cmd/gateway`): `/api` routed to git2 core after auth-svc's token check (`/api/auth/*` and `/api/setup/*` public), Dash and Admin served as static apps, client `X-Git2-*` / `X-Real-IP` / `X-Forwarded-*` headers replaced, `X-Request-Id`, `nosniff`, 30 s per API request (git2-deploy gateway contract v0.1)
- gateway: Git over HTTPS: `/<owner>/<repo>.git/*` streamed to git-svc's Smart HTTP port by repository ID after an HTTP Basic sign-in, owner-only in v0, the password never forwarded; failed sign-ins limited per IP and per username; `GET /readyz` checks git2 core and git-svc (#24)
- gateway: TLS with `GATEWAY_TLS_CERT_FILE` / `GATEWAY_TLS_KEY_FILE` (TLS 1.2+, HSTS), and an optional plain-HTTP port that 308-redirects to `GIT2_EXTERNAL_URL` (#25)
- auth-svc: `PostgresStore.FindByUsername` and `Credentials.Check`, the username and password check the gateway uses for Git, with Login's rules (#24)
- contracts: OpenAPI v0 for auth-svc (`contracts/auth-svc/`) and admin-api users, roles and teams (`contracts/admin-api/`)
- contracts: OpenAPI v0 for dash-api (`contracts/dash-api/`): repositories and code browsing, drafts for merge requests, issues, notifications and pipelines (#17)
- contracts: references to the `backup.v1` contract and the gateway contract v0.1, both kept in git2-deploy (`contracts/backup/`, `contracts/gateway/`) (#19)
- docs: Dash requirements in the Requirements Specification: code browser, repository list and empty repositories (FR-30, FR-31, FR-97, FR-98), merge requests, issues and pipelines (#16)
- docs: Platform and Backup requirements in the Requirements Specification: gateway header trust, Git route and client IP (FR-02, FR-03, FR-05, FR-99), container hardening (NFR-35), backup settings from `git2.yaml`, retention and snapshot deletion (FR-65, FR-76, FR-100) (#23)
- docs: identity v0 design (roles, teams, first-admin setup) in `docs/design/identity-v0.md`
- docs: README, project charter, project plan, team map, roadmap, this changelog
- docs: Changelog section in the Requirements Specification: the changes to the Project Plan v1.0 it records (bcrypt, push events, token revocation during a Redis fault, Git sign-in before PATs, a student ID typo); submission date set to 2026-11-08

### Fixed
- git2 core: per-IP rate limits see the client behind the gateway: `X-Real-IP` is used only on connections from `TRUSTED_PROXY_CIDRS` (`internal/realip`), never from anyone else (git2-deploy B-02)
- auth-svc: `password_hash` stored as `text`; email and username enforced lowercase
- docs: Requirements Specification, Git Core: a push's `repo.pushed` event is written right after Git updates the refs, not in the same transaction (FR-04, US-03, UC-11); repository name and default-branch rules (FR-25, UC-10); Git sign-in with the password until PATs arrive in W6 (FR-27 note)
- docs: Requirements Specification review: repository creation in SC-03 matches the Maintain rule of FR-25, persona introduction, FR-97 wording and numbering notes, dev-only `/api/auth/register` note (FR-20), bcrypt vs Argon2id deviation note (NFR-02), UC-12 flow order, missing actor links in the use case diagrams; Project Plan: student ID typo

### Database
- `001_auth_users.sql`: `auth_svc` schema and `auth_svc.users`
