# Identity v0: roles, teams and the first admin

Owner: Beste. Scope: WBS-1.1 (contracts) and WBS-3.1 (users, teams, roles). The wire shapes are in [`contracts/auth-svc/openapi.yaml`](../../contracts/auth-svc/openapi.yaml) and [`contracts/admin-api/openapi.yaml`](../../contracts/admin-api/openapi.yaml).

## Roles

Roles are a **fixed list shipped with git2**, not something an admin defines at runtime. Custom roles need a permission editor, a migration path for every new permission and much more testing; none of that fits nine weeks, and the plan only asks for role assignment.

- `auth_svc.roles` holds the list (`name`, `scope`, `description`), seeded by a migration. Assignments point at it with a foreign key, and the console reads it to fill its dropdowns.
- **What a role may do lives in Go code**, in one table in `auth-svc`. A new permission is a code change with tests, not a row someone edits.

| Role | Scope | Can |
|---|---|---|
| `admin` | instance | everything in the admin plane |
| `user` | instance | the Dash plane; the default for new accounts |
| `maintainer` | team | rename the team, add and remove members |
| `member` | team | belong to the team; repository access comes through it |

Every user has exactly one instance role (`auth_svc.users.role`). Team roles live on the membership row. In v0 teams are managed only from the admin plane; what a `maintainer` can do from Dash comes when Dash has team pages.

**The role is not put into the JWT.** Admin routes read it from the database on every request, so a demotion takes effect at once instead of after the access token's 15 minutes. Admin traffic is small, so the extra query costs nothing.

## Disabling a user

Disabling is how accounts are removed: repositories, merge requests and audit entries keep pointing at the user. A disabled user's tokens must stop working **at once**:

- the moment of disabling is stored in `auth_svc.users.tokens_valid_after` (database clock). Refresh refuses every session that began before it, using the `sat` (session start) claim refresh tokens carry across rotation. This also ends sessions Redis does not list, and keeps old sessions dead after the user is enabled again;
- all their listed refresh sessions are deleted (auth-svc keeps a per-user set of session ids for this);
- `RequireAuth` rejects their access tokens through a short-lived Redis marker that holds the same moment and lasts as long as an access token can live.

Both checks treat anything issued up to `DisableGrace` (10 s) after the moment as "before". That covers a sign-in that read the account as active just before the disable and issued its tokens just after, and clock differences between git2 processes. The cost: an account enabled again within those 10 seconds signs in once more after them.

The last active admin cannot be disabled (`ErrLastAdmin`, 409 in admin-api): the active admin rows are locked while checking, so two concurrent disables cannot both pass.

In code: `authsvc.DisableUser` / `authsvc.EnableUser`, called by admin-api.

**A Redis fault in that check fails open**, the same way the revoked-token check already did. Failing closed would turn a short Redis outage into 503s on every authenticated route of every service that mounts `RequireAuth`. The trade-off is small:

- admin routes do not rely on the marker: their middleware reads the role and `is_active` from the database on every request, so a disabled user is locked out of the admin plane even while Redis is down;
- refresh also reads `is_active` from the database, so no new access token is issued;
- what is left: during a Redis outage a disabled user can keep using Dash with the access token they already hold, for at most its 15 minutes.

A Redis fault never answers 401 either way: clients drop their tokens on a 401.

## Teams

Teams live in `auth_svc` (`teams`, `team_members`) because every service asks "is this user in that team?" when checking access. Team names are lowercase slugs, like usernames, since they will appear in URLs.

Organizations (T-03) come in WBS-3.1 but are not in the contract yet. They decide where repositories live (`/<owner>/<repo>`): users and organizations share one name space, and `git_svc.repositories.owner_id` can point at either. The shape is agreed with Abdullah (Git core) and Bilge (Dash) before it lands.

## The first admin

A fresh install has no admin, and "the first account to register becomes admin" lets whoever reaches the server first take it over. git2 uses a **one-time setup token** instead:

1. On startup, if no active `admin` exists, auth-svc creates a random setup token, stores only its SHA-256 in `auth_svc.setup_token` (one row at most), and prints the token once in the log. The token works for one hour; until then a restart or a second git2 process keeps it instead of replacing it, so the printed token stays valid. After the hour, the next start prints a new one: that is the way back for an operator who lost the log. `git2ctl install` can pass its own token (at least 32 characters) through `GIT2_SETUP_TOKEN`, which is how scripted installs work.
2. `GET /api/setup/status` tells the console whether setup is still needed.
3. `POST /api/setup/admin` with the token creates the first `admin` and burns the token, in one transaction that locks the token row, so two requests with the right token cannot both win. A wrong token answers 403, not 401. The W6 setup wizard (WBS-11.1) is a UI over these two endpoints.

Self-registration (`POST /api/auth/register`) is **off by default** after setup. An admin creates accounts, or turns sign-up on in the instance settings (WBS-11.1). Until those settings exist, sign-up stays open in dev.

## What changes in auth-svc

The current sign-in, refresh and logout flow stays. The changes are additions:

| Change | Why |
|---|---|
| `role` on users and in `UserResponse` | the console and Dash need to know who is admin |
| `GET /api/auth/me` answers 503 on a database fault (today 404) | team rule: a fault is 5xx, never 401/404 |
| Disabled users lose their tokens at once | admin "disable" must have an effect |
| Setup endpoints and setup token | first admin without a race |
| `RequirePermission(users, perm)` middleware next to `RequireAuth`, over one role → permission table in `auth-svc/roles.go` | admin routes; reads the role from the database |
| Migration `004_auth_roles_teams.sql` | `roles`, `users.role`, `teams`, `team_members` |
