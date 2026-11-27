# Changelog

All notable changes to the `git2` repo. The format follows [Keep a Changelog 1.1](https://keepachangelog.com/en/1.1.0/) and versions follow [SemVer](https://semver.org/).

Every PR adds its lines under `## [Unreleased]`: one line per change, starting with the service, ending with the PR number. Groups: `Added`, `Changed`, `Fixed`, `Removed`, `Security`, and `Database` for each new migration file. A release renames `Unreleased` to `## [x.y.z] - YYYY-MM-DD` and is tagged `vx.y.z`.

## [Unreleased]

### Added
- docs: repository access v0 design (`docs/design/repo-access-v0.md`): `read` / `write` / `maintain` levels from the owner, direct and team grants, `private` / `internal` visibility stored in `dash_api`, one check shared by dash-api and the gateway's Git route
- ci: `ci` check on every pull request: gofmt, vet, build, migrations applied to a throwaway Postgres, `go test -race` with the integration tests (#20)
- ci: `web` check: typecheck, lint, test and build of `web/` (#20)
- ci: `migrations-check`: names, numbering, first-line comments and frozen files (`helpers/ci/check-migrations.sh`), then all migrations applied from scratch (#20)
- ci: `changelog` check: the PR touches `CHANGELOG.md` unless it has the `no-changelog` label (#20)
- auth-svc: `POST /api/auth/register`, `/login`, `/refresh`, `/logout` and `GET /api/auth/me`; tokens in `X-Access-Token` / `X-Refresh-Token` headers, refresh sessions rotated in Redis, per-IP rate limits on the public routes
- auth-svc: `RequireAuth` middleware and `UserID(ctx)` for other services
- auth-svc: instance role on every user (`admin`, `user`; new accounts get `user`), returned as `role` by register, login and `/me`
- auth-svc: role → permission table (`roles.go`) and `RequirePermission` middleware for admin routes; the role is read from the database on every request
- auth-svc: `DisableUser` / `EnableUser` for admin-api: disabling ends every session of the user and makes `RequireAuth` reject their access tokens at once (`users.tokens_valid_after`, the refresh token's `sat` claim, a per-user session set and a `disabled:<uid>` marker in Redis); the last active admin cannot be disabled
- auth-svc: first-admin setup: `GET /api/setup/status` and `POST /api/setup/admin` with a one-time setup token, printed once at startup or set with `GIT2_SETUP_TOKEN`, valid for one hour; a wrong token answers 403, a request after setup 409
- admin-api: `GET`/`POST /api/admin/users` and `GET`/`PATCH /api/admin/users/{user_id}`: list users (prefix search on username or email, role and active filters, paging), create accounts, change email or role, enable and disable. Admin role only; an admin cannot demote or disable their own account, and the last active admin cannot be removed
- auth-svc: `NewUser`, `CheckEmail`, `ParseInstanceRole` and the store's `ListUsers`, `UpdateUser`, `UserTeams` for admin-api (`directory.go`)
- admin-api: `GET /api/admin/roles`; teams (`GET`/`POST /api/admin/teams`, `GET`/`PATCH`/`DELETE /api/admin/teams/{team_id}`) and their members (`GET .../members`, `PUT`/`DELETE .../members/{user_id}` with the team role `maintainer` or `member`). Deleting a team deletes its memberships
- auth-svc: the store's roles, teams and team members for admin-api (`teams.go`)
- internal: shared config, Postgres pool, Redis client, JWT (HS256), bcrypt, HTTP helpers and rate limiting
- dev: devcontainer, `make run` / `tests` / `deps` / `build`, migration runner
- gateway: one entry point (`cmd/gateway`): `/api` routed to git2 core after auth-svc's token check (`/api/auth/*` and `/api/setup/*` public), Dash and Admin served as static apps, client `X-Git2-*` / `X-Real-IP` / `X-Forwarded-*` headers replaced, `X-Request-Id`, `nosniff`, 30 s per API request (git2-deploy gateway contract v0.1)
- gateway: Git over HTTPS: `/<owner>/<repo>.git/*` streamed to git-svc's Smart HTTP port by repository ID after an HTTP Basic sign-in, owner-only in v0, the password never forwarded; failed sign-ins limited per IP and per username; `GET /readyz` checks git2 core and git-svc (#24)
- gateway: TLS with `GATEWAY_TLS_CERT_FILE` / `GATEWAY_TLS_KEY_FILE` (TLS 1.2+, HSTS), and an optional plain-HTTP port that 308-redirects to `GIT2_EXTERNAL_URL` (#25)
- auth-svc: `PostgresStore.FindByUsername` and `Credentials.Check`, the username and password check the gateway uses for Git, with Login's rules (#24)
- contracts: OpenAPI v0 for auth-svc (`contracts/auth-svc/`) and admin-api users, roles and teams (`contracts/admin-api/`)
- contracts: OpenAPI v0 for dash-api (`contracts/dash-api/`): repositories and code browsing, drafts for merge requests, issues, notifications and pipelines (#17)
- contracts: references to the `backup.v1` contract and the gateway contract v0.1, both kept in git2-deploy (`contracts/backup/`, `contracts/gateway/`) (#19)
- contracts: `git.v1` reference pinned to git2-codeplane `v0.2.0`: `RefService` and `CommitService` for dash-api's code-browsing endpoints, push events over smart HTTP (#15)
- contracts: `events.v1` reference pinned to git2-codeplane `v0.3.0` (`contracts/events/`): `EventSink.Deliver`, through which a relay pushes its outbox events to the subscribed services; git-svc's relay is the first (#38)
- contracts: `git.v1` at git2-codeplane `v0.4.0`: `CommitService.GetArchive`, a commit's files as a tar archive for CI jobs, and `BundleService` (`ListAllRepositories`, `CreateBundle`, `RestoreBundle`), whole repositories as Git bundles for backup-svc and `git2ctl restore` (#52)
- docs: Dash requirements in the Requirements Specification: code browser, repository list and empty repositories (FR-30, FR-31, FR-97, FR-98), merge requests, issues and pipelines (#16)
- docs: Platform and Backup requirements in the Requirements Specification: gateway header trust, Git route and client IP (FR-02, FR-03, FR-05, FR-99), container hardening (NFR-35), backup settings from `git2.yaml`, retention and snapshot deletion (FR-65, FR-76, FR-100) (#23)
- docs: identity v0 design (roles, teams, first-admin setup) in `docs/design/identity-v0.md`
- docs: terminal UI v0 design (screens, seam with the CLI's API client, token store, work breakdown) in `docs/design/tui-v0.md`
- docs: README, project charter, project plan, team map, roadmap, this changelog
- contracts: examples for dash-api's repository and code-browsing answers (one repository, its tree, a README, branches, tags, history and a commit with a diff), so `prism mock` serves data Dash can show (#32)
- web: dash-api types generated from `contracts/dash-api/openapi.yaml` into `@git2/api` (`pnpm --filter @git2/api gen`); the `web` check fails when they are out of date (#32)
- web: `@git2/api` client for dash-api repositories and code browsing (repositories, branches, tags, tree, blob, raw download, commits); `request()` takes an `AbortSignal` (#32)
- dash: code browser (WBS-6.1): repository list, repository page with clone URL, branch and tag picker, directory tree with breadcrumb and last commit, file view highlighted with Shiki (binary, LFS, cut and empty files, download through `/raw`), commit history by ref and path, commit detail with its diff (#32)
- dash-api: first routes (`dash-api/`, package `dashapi`, mounted under `/api`): `GET /api/repos/{owner}/{repo}` and code browsing (`branches`, `tags`, `tree`, `blob`, `raw`, `commits`, `commits/{sha}`) backed by git-svc's `RepositoryService`, `RefService` and `CommitService`. `Blob.language` comes from the file name, `Signature.user` from the commit email (one lookup per distinct email), `Branch.protected` is false. `/raw` refuses files over 10 MiB with 413 and is always served as a download (`attachment`, `nosniff`, `sandbox` CSP, `Cache-Control: private`) (#57)
- dash-api: every `/repos/{owner}/{repo}` route resolves the owner and the repository and checks the caller's level first (`read` < `write` < `maintain`); a missing repository and one the caller cannot read both answer 404, faults in auth-svc, git-svc or the check answer 503, never 401. Access is behind `AccessChecker`, owner-only for now like the gateway's Git routes (#57)
- dash-api: one mapping from git-svc's gRPC codes to HTTP statuses; git-svc refusing dash-api itself (`UNAUTHENTICATED`, `PERMISSION_DENIED`) is a 503. Each unary call to git-svc gets 25 s, so `GetTree` keeps its own 10 s for `last_commit` and a long `ListCommits` with a path does not answer 503 (#57)
- dash-api: `GET /api/repos` (one owner's repositories the caller can read, the caller's by default, sorted by name, 50 per page; an unknown owner and one with nothing readable both give an empty list), `POST /api/repos` (under the caller's own name only, else 403; git-svc's name rules answer 400, a taken name 409) and `DELETE /api/repos/{owner}/{repo}` (`maintain`, owner-only for now). A valid token for an account that is gone or disabled answers 401 (#61)
- dash: "New repository" page (`/new`) that opens the new repository on its push instructions, and a repository Settings tab whose delete needs the name typed back (FR-26); `createRepository` and `deleteRepository` in `@git2/api` (#61)
- ui: the top bar's "git2 Dash" / "git2 Admin" links to the plane's home page (#61)
- config: `GIT_SVC_GRPC_ADDR` (default `localhost:9090`) and `GIT2_EXTERNAL_URL` (clone URLs; default `http://localhost:8000` outside prod, required in prod) for git2 core (#57)

### Changed
- deps: git2-codeplane `v0.1.0` → `v0.4.0` in `go.mod`, for the code-browsing RPCs; the gateway is unchanged (#57)
- contracts: `git.v1` and `events.v1` references pinned to git2-codeplane `v0.4.0`; `events.v1` is unchanged (#52)
- contracts: `git.v1` reference pinned to git2-codeplane `v0.3.0`; git-svc's events are now delivered over `events.v1.EventSink` (#38)
- auth-svc: `RequireAuth` checks revocation and the disabled marker in one Redis round trip; it still fails open on a Redis fault
- internal/jwt: token times (`iat`, `exp`, `nbf`) carry milliseconds, so a token issued in the same second as a disable is told apart
- contracts: dash-api `FileDiff.patch` is an empty string, not null, when only the mode or the name changed (git2-codeplane ADR 0013 review) (#32)
- docs: Project Plan v1.1: repository creation and deletion now in D-01, D-02, T-05, T-06, WBS-5.1, WBS-6.1 and WBS-17.2; effort distribution table in Section 3 (development, testing, documentation, project management)
- docs: Requirements Specification replaced with the shortened, reviewed text from #33: FRs reworded as "The system shall … when …", one pipeline per pushed branch or tag (FR-51, US-03, UC-21), `runner_system_failure` (FR-61), paused runners take no new jobs (FR-67), backup encryption and container scope restored (NFR-05, NFR-35) (#55)

### Fixed
- git2 core: per-IP rate limits see the client behind the gateway: `X-Real-IP` is used only on connections from `TRUSTED_PROXY_CIDRS` (`internal/realip`), never from anyone else (git2-deploy B-02)
- auth-svc: `password_hash` stored as `text`; email and username enforced lowercase
- auth-svc: `GET /api/auth/me` answers 503 on a database fault instead of 404
- docs: Requirements Specification, Git Core: a push's `repo.pushed` event is written right after Git updates the refs, not in the same transaction (FR-04, US-03, UC-11); repository name and default-branch rules (FR-25, UC-10); Git sign-in with the password until PATs arrive in W6 (FR-27 note)
- docs: Requirements Specification review: repository creation in SC-03 matches the Maintain rule of FR-25, persona introduction, FR-97 wording and numbering notes, dev-only `/api/auth/register` note (FR-20), bcrypt vs Argon2id deviation note (NFR-02), UC-12 flow order, missing actor links in the use case diagrams; Project Plan: student ID typo

### Database
- `001_auth_users.sql`: `auth_svc` schema and `auth_svc.users`
- `004_auth_roles_teams.sql`: `auth_svc.roles` (fixed list: admin, user, maintainer, member), `users.role` (default `user`), `auth_svc.teams`, `auth_svc.team_members`
- `005_auth_setup_token.sql`: `auth_svc.setup_token`, the hash of the one-time first-admin setup token
- `006_ci_inbox_and_attempts.sql`: job attempts in `ci_svc.jobs` (`attempt`, `retry_of`, `idempotency_key`, `triggered_by`) for ci-svc's `RetryJob`, and `ci_svc.inbox` for events delivered to ci-svc's `EventSink` (git2-runner #16, ADR 0010) (#34)
