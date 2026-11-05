# admin-api: users, roles and teams

| | |
|---|---|
| Spec | [`openapi.yaml`](openapi.yaml) (OpenAPI 3.1) |
| Version | `0.1.0-draft` (WBS-1.1, contracts v0) |
| Owner | Admin & Identity lane (Zarif Beste Süner) |
| Served at | `/api/admin` |
| Design notes | [`docs/design/identity-v0.md`](../../docs/design/identity-v0.md) |

## Status

| Group | State | Needed by |
|---|---|---|
| Roles, users, teams, team members | Draft (`x-status: draft`) | Admin console, WBS-3.1, W2–W3 |
| Organizations | Not written yet | dash-api owners (`/repos/{owner}/...`), WBS-3.1 |

## Rules

- Reads need the `admin` or `auditor` instance role, writes need `admin`. The role is read from the database on every request, so a change takes effect at once.
- Users are disabled, never deleted. Disabling ends their sessions at once.
- The last active admin cannot be demoted or disabled.
- Paging, errors and the bearer token work as in `contracts/dash-api/`.

## Lint

From this directory, with Node 22 and pnpm:

```bash
pnpm dlx --package=@redocly/cli@1.25.15 redocly lint openapi.yaml
```

## Open questions for v0

1. **Organizations**: users and organizations share one name space, and `git_svc.repositories.owner_id` can point at either. Shape to agree with Git core (Abdullah) and Dash (Bilge).
2. **Repository permissions**: proposal: team membership here, repository grants (user or team → read, write, admin) in dash-api.

## Changing it

Contract changes go through a PR approved by every affected owner: Dash (Bilge) for anything the web apps use.
