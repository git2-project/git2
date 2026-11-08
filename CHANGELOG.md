# Changelog

All notable changes to the `git2` repo. The format follows [Keep a Changelog 1.1](https://keepachangelog.com/en/1.1.0/) and versions follow [SemVer](https://semver.org/).

Every PR adds its lines under `## [Unreleased]`: one line per change, starting with the service, ending with the PR number. Groups: `Added`, `Changed`, `Fixed`, `Removed`, `Security`, and `Database` for each new migration file. A release renames `Unreleased` to `## [x.y.z] - YYYY-MM-DD` and is tagged `vx.y.z`.

## [Unreleased]

### Added
- auth-svc: `POST /api/auth/register`, `/login`, `/refresh`, `/logout` and `GET /api/auth/me`; tokens in `X-Access-Token` / `X-Refresh-Token` headers, refresh sessions rotated in Redis, per-IP rate limits on the public routes
- auth-svc: `RequireAuth` middleware and `UserID(ctx)` for other services
- auth-svc: instance role on every user (`admin`, `user`; new accounts get `user`), returned as `role` by register, login and `/me`
- auth-svc: role → permission table (`roles.go`) and `RequirePermission` middleware for admin routes; the role is read from the database on every request
- auth-svc: `DisableUser` / `EnableUser` for admin-api: disabling ends every session of the user and makes `RequireAuth` reject their access tokens at once (`users.tokens_valid_after`, the refresh token's `sat` claim, a per-user session set and a `disabled:<uid>` marker in Redis); the last active admin cannot be disabled
- auth-svc: first-admin setup: `GET /api/setup/status` and `POST /api/setup/admin` with a one-time setup token, printed once at startup or set with `GIT2_SETUP_TOKEN`, valid for one hour; a wrong token answers 403, a request after setup 409
- internal: shared config, Postgres pool, Redis client, JWT (HS256), bcrypt, HTTP helpers and rate limiting
- dev: devcontainer, `make run` / `tests` / `deps` / `build`, migration runner
- contracts: OpenAPI v0 for auth-svc (`contracts/auth-svc/`) and admin-api users, roles and teams (`contracts/admin-api/`)
- docs: identity v0 design (roles, teams, first-admin setup) in `docs/design/identity-v0.md`
- docs: README, project charter, project plan, team map, roadmap, this changelog

### Changed
- auth-svc: `RequireAuth` checks revocation and the disabled marker in one Redis round trip; it still fails open on a Redis fault
- internal/jwt: token times (`iat`, `exp`, `nbf`) carry milliseconds, so a token issued in the same second as a disable is told apart

### Fixed
- auth-svc: `password_hash` stored as `text`; email and username enforced lowercase
- auth-svc: `GET /api/auth/me` answers 503 on a database fault instead of 404

### Database
- `001_auth_users.sql`: `auth_svc` schema and `auth_svc.users`
- `004_auth_roles_teams.sql`: `auth_svc.roles` (fixed list: admin, user, maintainer, member), `users.role` (default `user`), `auth_svc.teams`, `auth_svc.team_members`
- `005_auth_setup_token.sql`: `auth_svc.setup_token`, the hash of the one-time first-admin setup token
