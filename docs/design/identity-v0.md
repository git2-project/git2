# Identity v0: roles, teams and the first admin

Owner: Beste. Scope: WBS-1.1 (contracts) and WBS-3.1 (users, teams, roles). The wire shapes are in [`contracts/auth-svc/openapi.yaml`](../../contracts/auth-svc/openapi.yaml) and [`contracts/admin-api/openapi.yaml`](../../contracts/admin-api/openapi.yaml).

## Roles

Roles are a **fixed list shipped with git2**, not something an admin defines at runtime. Custom roles need a permission editor, a migration path for every new permission and much more testing; none of that fits nine weeks, and the plan only asks for role assignment.

- `auth_svc.roles` holds the list (`name`, `scope`, `description`), seeded by a migration. Assignments point at it with a foreign key, and the console reads it to fill its dropdowns.
- **What a role may do lives in Go code**, in one table in `auth-svc`. A new permission is a code change with tests, not a row someone edits.

| Role | Scope | Can |
|---|---|---|
| `admin` | instance | everything in the admin plane |
| `auditor` | instance | read-only admin plane: users, teams, settings, audit log |
| `user` | instance | the Dash plane; the default for new accounts |
| `maintainer` | team | rename the team, add and remove members |
| `member` | team | belong to the team; repository access comes through it |

Every user has exactly one instance role (`auth_svc.users.role`). Team roles live on the membership row. In v0 teams are managed only from the admin plane; what a `maintainer` can do from Dash comes when Dash has team pages.

**The role is not put into the JWT.** Admin routes read it from the database on every request, so a demotion takes effect at once instead of after the access token's 15 minutes. Admin traffic is small, so the extra query costs nothing.

## Disabling a user

Disabling is how accounts are removed: repositories, merge requests and audit entries keep pointing at the user. A disabled user's tokens must stop working **at once**:

- all their refresh sessions are deleted (auth-svc keeps a per-user set of session ids for this);
- `RequireAuth` rejects their access tokens through a short-lived Redis marker that lasts as long as an access token can live;
- a Redis fault in that check still answers 5xx, never 401.

## Teams

Teams live in `auth_svc` (`teams`, `team_members`) because every service asks "is this user in that team?" when checking access. Team names are lowercase slugs, like usernames, since they will appear in URLs.

Organizations (T-03) come in WBS-3.1 but are not in the contract yet. They decide where repositories live (`/<owner>/<repo>`): users and organizations share one name space, and `git_svc.repositories.owner_id` can point at either. The shape is agreed with Abdullah (Git core) and Bilge (Dash) before it lands.

## The first admin

A fresh install has no admin, and "the first account to register becomes admin" lets whoever reaches the server first take it over. git2 uses a **one-time setup token** instead:

1. On startup, if no `admin` exists, auth-svc creates a random setup token, stores only its hash, and prints the token once in the log. `git2ctl install` can pass its own token through `GIT2_SETUP_TOKEN`, which is how scripted installs work.
2. `GET /api/setup/status` tells the console whether setup is still needed.
3. `POST /api/setup/admin` with the token creates the first `admin` and burns the token. The W6 setup wizard (WBS-11.1) is a UI over these two endpoints.

Self-registration (`POST /api/auth/register`) is **off by default** after setup. An admin creates accounts, or turns sign-up on in the instance settings (WBS-11.1). Until those settings exist, sign-up stays open in dev.

## What changes in auth-svc

The current sign-in, refresh and logout flow stays. The changes are additions:

| Change | Why |
|---|---|
| `role` on users and in `UserResponse` | the console and Dash need to know who is admin |
| `GET /api/auth/me` answers 503 on a database fault (today 404) | team rule: a fault is 5xx, never 401/404 |
| Disabled users lose their tokens at once | admin "disable" must have an effect |
| Setup endpoints and setup token | first admin without a race |
| `RequireRole(...)` middleware next to `RequireAuth` | admin routes; reads the role from the database |
| Migrations `002_auth_roles_teams.sql` | `roles`, `users.role`, `teams`, `team_members` |
