# Changelog

All notable changes to the `git2` repo. The format follows [Keep a Changelog 1.1](https://keepachangelog.com/en/1.1.0/) and versions follow [SemVer](https://semver.org/).

Every PR adds its lines under `## [Unreleased]`: one line per change, starting with the service, ending with the PR number. Groups: `Added`, `Changed`, `Fixed`, `Removed`, `Security`, and `Database` for each new migration file. A release renames `Unreleased` to `## [x.y.z] - YYYY-MM-DD` and is tagged `vx.y.z`.

## [Unreleased]

### Added
- auth-svc: `POST /api/auth/register`, `/login`, `/refresh`, `/logout` and `GET /api/auth/me`; tokens in `X-Access-Token` / `X-Refresh-Token` headers, refresh sessions rotated in Redis, per-IP rate limits on the public routes
- auth-svc: `RequireAuth` middleware and `UserID(ctx)` for other services
- internal: shared config, Postgres pool, Redis client, JWT (HS256), bcrypt, HTTP helpers and rate limiting
- dev: devcontainer, `make run` / `tests` / `deps` / `build`, migration runner
- contracts: OpenAPI v0 for auth-svc (`contracts/auth-svc/`) and admin-api users, roles and teams (`contracts/admin-api/`)
- docs: identity v0 design (roles, teams, first-admin setup) in `docs/design/identity-v0.md`
- docs: README, project charter, project plan, team map, roadmap, this changelog

### Fixed
- auth-svc: `password_hash` stored as `text`; email and username enforced lowercase

### Database
- `001_auth_users.sql`: `auth_svc` schema and `auth_svc.users`
