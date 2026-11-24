# Repository access v0: who may read, push and manage a repository

Owner: Bilge (Dash). Scope: WBS-6.1 and WBS-8.2 (dash-api), FR-16, FR-26, FR-97, NFR-01. Reviewers: Abdullah (Git core), Ceyda (gateway), Beste (identity), Osman (pipelines).

This note settles open questions 3 (repository permissions) and 4 (visibility) of [`contracts/dash-api/README.md`](../../contracts/dash-api/README.md). Until it lands, the gateway lets only a repository's owner use it (git2-deploy `docs/api/gateway.md`, "Who may use a repository (v0)"). This model replaces that rule.

## Why dash-api owns it

git-svc and ci-svc trust their callers and check nothing (git2-codeplane ADR 0012, A-10; git2-runner `docs/api/overview.md`). The gateway authenticates but does not authorize. Something has to answer "may this user do this to this repository?", and dash-api already checks every Dash call. So:

- **dash-api stores repository access** in its own schema, `dash_api`.
- **The rule lives in one Go function** in dash-api. The gateway calls the same function for `git clone`, `fetch` and `push`, so the web and Git always agree.
- **git.v1 stays free of permissions and visibility**, as Git core asked.
- **Teams stay in `auth_svc`**. dash-api reads memberships through auth-svc's exported `UserTeams`, never with SQL against `auth_svc`, the same way admin-api works today.

## Permission levels

Three ordered levels. A higher level includes everything below it.

| Level | Can |
|---|---|
| `read` | Browse code, branches, tags and commits; clone and fetch; view merge requests, issues and pipelines; download artifacts; open issues and comment |
| `write` | Everything in `read`, plus: push to unprotected branches; open, approve and merge merge requests into unprotected branches; set labels, milestones and assignees; cancel and retry pipelines |
| `maintain` | Everything in `write`, plus: change visibility, collaborators and team grants; protect branches; manage CI secrets; delete the repository |

This is the table in the Requirements Specification (§3.1). Pushing or merging into a protected branch follows the branch rules (FR-34–FR-36, WBS-7.1), whatever the level.

`maintain` is not the instance `admin` role. An instance admin gets no implicit access to repository contents (see "Decided with this note", 2).

## How a user gets a level

A user's level on a repository is the **highest** of:

1. **Owner.** The user who owns the repository (`owner_id` in git-svc) has `maintain`. It cannot be taken away, so a user-owned repository can never be locked out.
2. **Direct grant.** A row in `dash_api.repository_grants` for the user.
3. **Team grant.** A row for a team the user belongs to. The team role (`member` or `maintainer`) does not change the level; it only decides who manages the team.
4. **Visibility.** `internal` gives `read` to every signed-in user. `private` gives nothing.

No match means no access. Disabled users never get this far: `RequireAuth` and the gateway's sign-in reject them first. Their grants stay in place, so enabling them again restores their access.

Organization-owned repositories (WBS-3.1) add one more source: the user's role in the organization. Owners get `maintain`; members get access only through teams (see "Decided with this note", 1).

## Visibility

| Value | Who can read | |
|---|---|---|
| `private` | Owner and grants only | **Default** for new repositories |
| `internal` | Every signed-in user of the instance | Common in companies: everyone can find and clone, only the team can push |

`public` (readable without signing in) is left out of v0. It only differs from `internal` once anonymous clone and anonymous Dash pages exist, and today the gateway answers 401 to every anonymous request. Adding it later is one migration and one line in the rule (see "Decided with this note", 4).

## Storage

Migration `009_dash_api_repo_access.sql` (007 and 008 are taken by open PRs; renumbered if needed at merge):

```sql
CREATE SCHEMA IF NOT EXISTS dash_api;

-- One row per repository dash-api created. repo_id is git-svc's ID; there is
-- no foreign key into git_svc, because each service owns its own schema.
CREATE TABLE IF NOT EXISTS dash_api.repositories (
    repo_id    UUID        PRIMARY KEY,
    visibility TEXT        NOT NULL DEFAULT 'private',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT repositories_visibility_check CHECK (visibility IN ('private', 'internal'))
);

-- A grant goes either to one user or to one team, never both.
CREATE TABLE IF NOT EXISTS dash_api.repository_grants (
    id         UUID        PRIMARY KEY,
    repo_id    UUID        NOT NULL REFERENCES dash_api.repositories (repo_id) ON DELETE CASCADE,
    user_id    UUID,
    team_id    UUID,
    level      TEXT        NOT NULL,
    granted_by UUID        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT repository_grants_level_check   CHECK (level IN ('read', 'write', 'maintain')),
    CONSTRAINT repository_grants_subject_check CHECK ((user_id IS NULL) <> (team_id IS NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS repository_grants_user_uq
    ON dash_api.repository_grants (repo_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS repository_grants_team_uq
    ON dash_api.repository_grants (repo_id, team_id) WHERE team_id IS NOT NULL;
-- "Which repositories can this user or team see?" for the repository list.
CREATE INDEX IF NOT EXISTS repository_grants_user_idx ON dash_api.repository_grants (user_id);
CREATE INDEX IF NOT EXISTS repository_grants_team_idx ON dash_api.repository_grants (team_id);
```

**A repository with no `dash_api.repositories` row counts as `private`.** That covers repositories created before this migration or straight through git-svc, and a crash between the two steps of creating one (below). The missing row can only take access away, never give it.

Deleting a team in auth-svc leaves its grants behind. They match nobody, so they are harmless; dash-api skips them when listing and deletes them when it sees them.

## The check

In `dash-api/access.go` (package `dashapi`):

```go
type Level int // LevelNone < LevelRead < LevelWrite < LevelMaintain

// Access returns the user's level on the repository. ownerID comes from
// git-svc's FindRepository or GetRepository answer, which the caller already has.
func (a *Access) Level(ctx context.Context, userID, repoID, ownerID uuid.UUID) (Level, error)
```

- It costs two queries: the user's teams (`authsvc.UserTeams`) and one query over `dash_api.repositories` and `repository_grants`. No cache in v0, so a revoked grant takes effect on the next request.
- **A database fault returns an error, and callers answer `503`.** This check fails **closed**, unlike the Redis checks in `RequireAuth`: those only add to a token check that already passed, while this check is the only thing between a user and the code.

What callers answer:

| Situation | Answer |
|---|---|
| Unknown repository, or level below `read` | `404`, so private names do not leak (FR-97) |
| Can read, but the action needs more | `403` (FR-16) |
| Database, auth-svc or git-svc fault | `503`, never `401` or `404` |

## Who calls it

**dash-api, on every `/api/repos/{owner}/{repo}/…` call.** It resolves the owner name with auth-svc (`FindByUsername`), finds the repository with `FindRepository`, checks the level, and only then calls git-svc or ci-svc. One middleware on the `{owner}/{repo}` subrouter does this, and each route declares the level it needs.

**The gateway, on Git routes.** It replaces the owner-only rule in `gateway/git.go` with the same function:

| Git request | Level needed |
|---|---|
| `GET info/refs?service=git-upload-pack`, `POST git-upload-pack` (clone, fetch) | `read` |
| `GET info/refs?service=git-receive-pack`, `POST git-receive-pack` (push) | `write` |

Below `read` the gateway answers `404`, as it does today. A reader who pushes gets `403`. Protected branches are still checked by git-svc's pre-receive hook (WBS-7.1).

**ci-svc is never called for a repository the user cannot read.** Viewing pipelines needs `read`; cancel and retry need `write`.

**The repository list** (`GET /api/repos`) returns only repositories the caller can read: owned, granted directly or through a team, or `internal`.

## Creating and deleting

**Create** (`POST /api/repos`):

1. The caller creates under their own name. Organization owners come with organizations.
2. dash-api calls git-svc `CreateRepository`.
3. dash-api inserts the `dash_api.repositories` row with the chosen visibility (default `private`).

If step 3 fails, the repository exists without a row: it is private, and its owner can still use it and set its visibility again. Nothing becomes readable by accident.

**Delete** (`DELETE /api/repos/{owner}/{repo}`) needs `maintain` and the name typed back (FR-26). dash-api calls `DeleteRepository`, then deletes its row; the grants go with it (`ON DELETE CASCADE`). A new repository with the same name gets a new ID, so old grants never apply to it.

## Contract changes (a separate PR to `contracts/dash-api`)

Additions, so Dash can show the right buttons and manage access without a trip to the admin plane:

- `Repository.visibility`: `private` or `internal`.
- `Repository.permission`: the caller's level. Dash hides what the caller cannot do instead of showing buttons that fail with 403.
- `CreateRepositoryRequest.visibility`, defaulting to `private`.
- `PATCH /repos/{owner}/{repo}`: description, default branch, visibility (`maintain`).
- `GET /repos/{owner}/{repo}/access`: the owner, user grants and team grants (`read`).
- `PUT` / `DELETE /repos/{owner}/{repo}/access/users/{username}` and `…/access/teams/{team}` with `{level}` (`maintain`).

In Dash these become a **Settings → Access** page on the repository: one list of people and teams with a level dropdown, and a visibility switch.

## What this changes for each lane

| Lane | Change |
|---|---|
| Dash (Bilge) | dash-api, the migration, the middleware, the contract PR, the Settings → Access page |
| Gateway (Ceyda) | `gateway/git.go`: owner resolution by username for any owner, then `Level` instead of the owner-only rule; `docs/api/gateway.md` updated |
| Git core (Abdullah) | Nothing in git.v1. The pre-receive hook (WBS-7.1) stays the place for branch rules |
| Identity (Beste) | Nothing for user-owned repositories. Organizations add an organization-role source to `Level` |
| Pipelines (Osman) | Nothing in ci.v1. dash-api checks `read` or `write` before each call |

## Decided with this note

1. **Organizations.** The organization owner gets `maintain` on every repository of the organization. Organization members get nothing by default; their access comes through teams, as in GitHub. This is added to `Level` when organizations reach the contract (WBS-3.1, Beste).
2. **Instance admins.** No implicit access to repository contents. Recovering a repository whose owner was disabled comes later as an audited "transfer ownership" action in the admin plane.
3. **Personal access tokens (WBS-3.2).** A token's scopes cap the level: `Level` takes the token's scopes and returns the lower of the two, so a read-only token never pushes.
4. **`public` and anonymous clone.** Not before M3 (11.12). The W7 review decides whether v1.0.0 gets it; if it does, the gateway lets anonymous `upload-pack` through to `public` repositories and Dash gets signed-out pages.

## Open question

**The gateway's call (Ceyda).** ADR 0004 makes the gateway its own process, but it is in the same Go module. Proposal: the gateway imports `dashapi.Access` and reads `dash_api` with its own pool, like it already uses auth-svc's `Credentials`. The alternative is an internal dash-api endpoint, at the cost of one more network hop per Git request.
