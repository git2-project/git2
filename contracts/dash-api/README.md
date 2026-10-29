# dash-api: public REST contract of the Dash plane

| | |
|---|---|
| Spec | [`openapi.yaml`](openapi.yaml) (OpenAPI 3.1) |
| Version | `0.1.0-draft` (WBS-1.1, contracts v0) |
| Owner | Dash lane (Bilge Bostanbaşı) |
| Served at | `/api` behind the gateway, next to auth-svc's `/api/auth` |

## Status

| Group | State | Needed by |
|---|---|---|
| Repositories | Complete for v0 | WBS-5.1 / 6.1, W2 |
| Code browsing: branches, tags, tree, blob, raw, commits | Complete for v0 | WBS-6.1, W2–W3 |
| Merge requests, compare | Draft (`x-status: draft`) | WBS-8.2, W4 |
| Issues, labels, milestones, notifications | Draft | WBS-9.1, W5 |
| Pipelines, job logs, artifacts | Draft, to align with `ci.v1` in git2-runner | WBS-10.5, W6 |

Draft operations fix the shape only; they still lack error responses (the 21 `operation-4xx-response` lint warnings) and will change.

## What it depends on

- **auth-svc** issues the bearer token and owns users; organizations come with WBS-3.1.
- **git-svc** (`git.v1`, [git2-codeplane](https://github.com/git2-project/git2-codeplane)): today it has repository CRUD only. Code browsing needs RPCs for refs, trees, blobs, commits and diffs; merge requests need merge.
- **ci-svc** (`ci.v1`, [git2-runner](https://github.com/git2-project/git2-runner)): `ListPipelines`, `GetPipeline`, `StreamJobLog`, `ListArtifacts`.
- git-svc and ci-svc trust their callers, so **dash-api checks repository access on every call** (a repository the caller cannot read is a `404`).

## Lint and mock

From this directory, with Node 22 and pnpm:

```bash
pnpm dlx --package=@redocly/cli@1.25.15 redocly lint openapi.yaml
pnpm dlx --package=@stoplight/prism-cli@5.12.0 prism mock -p 4010 openapi.yaml       # fixed example data
pnpm dlx --package=@stoplight/prism-cli@5.12.0 prism mock -d -p 4010 openapi.yaml    # random data each call
```

The mock checks requests against the spec: no bearer token gives `401`, a missing `path` on `/blob` gives `400`.

To build Dash against the mock while signing in with the real auth-svc:

```bash
cd web
GIT2_DASH_MOCK_URL=http://127.0.0.1:4010 pnpm dev
```

The Dash dev server then sends `/api/auth/*` to the Go server and every other `/api/*` to the mock.

## Open questions for v0

1. **URL prefix and versioning**: `/api/repos` (like `/api/auth`) or `/api/v1/repos`?
2. **Owners**: users and organizations share one namespace (`/repos/{owner}/{repo}`); organizations depend on WBS-3.1.
3. **Repository permissions**: which service stores repository roles (read, write, admin) and team access: dash-api or admin-api?
4. **Visibility**: `git.v1` has no private/internal/public field; decide whether dash-api adds one.
5. **Pipeline statuses**: take the final list from `ci.v1`.

## Changing it

Contract changes go through a PR approved by every affected owner: Git core (Abdullah) for anything backed by git-svc, Pipelines (Osman) for pipelines, Admin & Identity (Zarif) for auth and owners, Platform (Ceyda) for gateway routing.
