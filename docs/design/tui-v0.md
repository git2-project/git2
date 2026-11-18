# git2 TUI: design proposal (draft v0.1, 18 Nov 2026)

Owner: Ceyda (the TUI half of WBS-14.2, agreed in the team on 17 Nov and pulled forward from W7-W8). Status: draft for team review, nothing built. Lines marked **[A]** are assumptions I could not verify.

## 1. Goal and scope

Running `git2` with no arguments opens a full-screen, keyboard-driven terminal UI that works like GitHub Desktop (UC-27 steps 5-7, SC-08, US-36).

| In scope | Requirement |
|---|---|
| Changed files on the left, diff on the right; stage, unstage, commit | FR-86, FR-84 |
| Switch and create branches, push, pull; show the server's rejection text on a refused push | FR-86, FR-84, FR-35, UC-27 A2/E2 |
| Sign in to an instance and keep the session | FR-83, FR-10, UC-27 E1 |
| MR list, MR detail with diff and pipeline status, comment, approve | FR-85, UC-27 A1 (UC-16, UC-17) |
| Repository list and clone (convenience, not required by FR-86) | FR-84 |

Out of scope: issues, notifications, the code browser, merging an MR, inline (line-level) review comments, admin functions, SSH transport (FR-29, optional), mouse-first design, Windows-specific work, and the Typer commands themselves (WBS-14.1, Abdullah).

Plan note: `ProjectPlan.md` lists Abdullah as owner of WBS-14.2 (line 357) and schedules it W7-W8 (line 594). The team agreed on 17 Nov that Ceyda takes the TUI half and starts it early; Abdullah keeps the MR commands. This needs a plan revision entry (owner, weeks, size).

## 2. What the API offers today

Sources: `contracts/auth-svc/openapi.yaml` and `contracts/dash-api/openapi.yaml` on `dev/main` (ee35982, both `0.1.0-draft`), and `git2-deploy/docs/api/gateway.md` (v0.1). All paths are under `/api` behind the gateway.

| Group | Endpoints | State |
|---|---|---|
| Sign-in | `POST /auth/login` (email + password), `POST /auth/refresh`, `POST /auth/logout`, `GET /auth/me` | Implemented in auth-svc. Tokens come back in `X-Access-Token`, `X-Refresh-Token`, `X-Access-Expires-In` response headers, never in the body |
| Repositories | `GET/POST /repos`, `GET/DELETE /repos/{owner}/{repo}` (`Repository.clone_url`) | Contract complete for v0. No dash-api Go code on `dev/main` or any remote branch in my clone, so only the Prism mock answers |
| Code reads | `branches`, `tags`, `tree`, `blob`, `raw`, `commits`, `commits/{sha}` | Same: contract complete, server not found |
| Merge requests | `GET/POST .../merge-requests`, `GET .../{number}`, `.../diff`, `GET/POST .../comments`, `POST/DELETE .../approvals`, `POST .../merge`, `GET .../compare` | In the contract as `x-status: draft` (WBS-8.2, W4). Shapes only: no 400/401/403/404 responses yet, "will change" per the README |
| Pipelines | `GET .../pipelines`, `.../pipelines/{id}`, `.../jobs/{id}/log` (SSE, `offset` resumes), `.../jobs/{id}/artifacts`; `MergeRequest.pipeline` is a `PipelineSummary` | Draft, to align with `ci.v1` (WBS-10.5, W6) |
| Git transport | `/<owner>/<repo>.git/*`, HTTP Basic with username + account password | Gateway route exists (`gateway/git.go`). PATs replace passwords in W6 (WBS-3.2) |

Missing for the TUI:

1. **Personal access tokens.** FR-83 and UC-27 say "sign in with the instance URL and a PAT". No PAT endpoint exists in any contract, and the gateway accepts only the auth-svc JWT as bearer on `/api/*`. How a PAT is presented on `/api` is undefined.
2. **A dash-api server.** Every repository, MR and pipeline screen can only be built against the mock until WBS-6.1 / 8.2 / 10.5 land.
3. **Error shapes on draft operations.** A generated client has no typed 401/404/409 for MR calls.
4. **Request changes / review verdicts.** Only approve and withdraw exist; "review" in the TUI means comment plus approve, which is all FR-85 asks.
5. **Repositories across owners.** `GET /repos` lists one owner (default: the caller), so the list needs an owner field.
6. **Username on Git routes.** Login takes an email, Git takes the username; the TUI reads it from `GET /auth/me` **[A]** (field not checked).

So merge requests are not missing from the contract, but they are draft and unimplemented.

## 3. Where the package lives

Proposal: a new org repo `git2-cli` holding one Python package (D-07 says "one Python terminal app").

- For: D-07 is its own deliverable with its own acceptance (`pipx install`); the org already splits by deliverable (git2-runner, git2-deploy); Python CI (ruff, pytest) stays out of the Go pipeline; pipx can install straight from the repo URL.
- Against: the client is generated from `contracts/` in git2, so the spec must be pinned (a git2 tag, as `contracts/git/README.md` pins `git.v1` to v0.3.0), and one more repo needs board and CI setup.
- Alternative: a `cli/` directory in git2 (no pin, contract and client change in one PR). Abdullah decides, since he owns WBS-14.1.

Layout and the seam with Abdullah's client:

```
src/git2cli/
  api/      generated OpenAPI client (Abdullah, WBS-14.1). The only code that builds URLs.
  auth/     token store + Session (login, refresh, logout). One per machine, used by CLI, TUI and later git2-mcp.
  gitops/   local Git through GitPython (ReqSpec 3.3.11): status, stage, commit, branch, push, pull (Abdullah)
  cli/      Typer commands (Abdullah)
  tui/      Textual app, screens, widgets (Ceyda)
tests/      all tests (course rule), mirroring the package
```

Rules: the TUI never imports httpx or GitPython directly. It calls `api`, `auth` and `gitops`, the same functions the Typer commands call, so `git2 commit` and the TUI commit cannot differ. Until WBS-14.1 exists, the TUI codes against two small `typing.Protocol` ports (`ForgeApi`, `LocalRepo`) listing only the calls it needs; Abdullah's modules then satisfy them and no second client is ever written. Ceyda writes `auth/` now (auth-svc is implemented), with Abdullah as reviewer.

## 4. Screens

One `App`, one `Screen` per area, modals for short input. Footer shows the active bindings.

Sign-in (shown when no valid session exists):
```
+---------------- git2: sign in -----------------+
| Instance URL  [https://git2.example.com      ] |
| Email         [ada@example.com               ] |
| Password      [************                  ] |
|               [ Sign in ]      (PAT field: W6) |
| invalid credentials                            |
+------------------------------------------------+
```

Changes (home screen when started inside a Git repository):
```
 ada/billing-service   branch: feat/invoice   ↑2 ↓0        ada@git2.example.com
+- Changes (3) ----------+- internal/invoice/total.go ---------------------------+
| [x] M invoice/total.go | @@ -12,7 +12,9 @@ func Total(items []Item) int {      |
| [ ] M README.md        | -    sum := 0                                         |
| [ ] A invoice/tax.go   | +    sum := tax(items)                                |
+- Commit ---------------+                                                       |
| Summary [fix: add tax] |                                                       |
| [ Commit 1 file ]      |                                                       |
+------------------------+-------------------------------------------------------+
 space Stage  a All  c Commit  b Branches  p Push  l Pull  m MRs  r Repos  ? Help
```

Branches (modal over Changes):
```
+- Branches ------------- / filter -+
| * feat/invoice           ↑2       |
|   main                   default  |
|   fix/rounding                    |
+- enter Switch  n New  esc Close --+
```

Repositories (home screen outside a Git repository):
```
 Repositories of [ada      ]                        / filter
 billing-service    main   Invoices, payments and the monthly run
 infra              main
 enter Open local clone   C Clone to...   R Refresh
```

Merge requests, list then detail:
```
 ada/billing-service   Merge requests: open (2)        s State  n New from branch
 #12  Add tax to totals     feat/invoice -> main   ada    ✓1   pipeline: success
 #11  Fix rounding          fix/rounding -> main   deniz  ✓0   pipeline: failed

 #12 Add tax to totals   open   feat/invoice -> main   mergeable   approvals: selin
+- Files (2) ------+- diff --------------------------+- Pipeline #88: success --+
| M total.go +9 -1 | @@ -12,7 +12,9 @@               | build   ✓ compile        |
| A tax.go  +30 -0 | +    sum := tax(items)          | test    ✓ unit  ✓ lint   |
+- Comments (1) ---+---------------------------------+--------------------------+
| selin: looks right, one question on rounding                                  |
 c Comment   A Approve   U Withdraw approval   o Copy link   L Job log   esc Back
```

Pipeline status is possible with the draft API: the summary rides on `MergeRequest.pipeline`, stages and jobs come from `GET .../pipelines/{id}`, and `L` tails `streamJobLog`. A stand-alone pipelines screen is left out.

## 5. Key bindings

| Key | Action | Where |
|---|---|---|
| `up`/`down`, `j`/`k` | Move in the focused list | lists |
| `tab` / `shift+tab` | Next / previous pane | all |
| `space` | Stage or unstage the file | Changes |
| `a` | Stage all / unstage all | Changes |
| `c` | Focus commit summary (Changes), write a comment (MR) | Changes, MR |
| `ctrl+s` | Commit, or send the comment | inputs |
| `b` | Branch switcher; `n` inside it creates a branch | Changes |
| `p` / `l` | Push / pull | Changes |
| `m` / `r` | Merge requests / repositories | all screens |
| `A` / `U` | Approve / withdraw approval (asks to confirm, names the MR) | MR detail |
| `R` | Refresh | all |
| `/` | Filter the list | lists |
| `?` | Help screen with all bindings | all |
| `esc` | Close modal or go back | all |
| `ctrl+q` | Quit (Textual's default) **[A]** not re-checked in the docs | all |

Single-letter bindings are declared on screens, not on the App, so they do not fire while an `Input` or `TextArea` has focus **[A]** (focused widgets take keys first unless a binding is `priority=True`, per the Binding API; to be proven by a Pilot test).

## 6. Architecture

- **App and screens.** `Git2App(App)` holds the Session and the two ports. Screens are pushed with `push_screen`; modals return a value through `dismiss` and the caller awaits `push_screen_wait` inside a worker (Textual API: `App.push_screen`, `App.push_screen_wait`).
- **Bindings.** `BINDINGS` per screen with `Binding(key, action, description, show, priority)`; the footer is generated from them.
- **Workers.** No network or Git call runs in a message handler. HTTP uses `@work(exclusive=True, group=...)` coroutines on one shared `httpx.AsyncClient`; `exclusive` cancels the previous load when the user moves on, so a slow response cannot overwrite a newer one. GitPython is blocking, so `gitops` calls run with `@work(thread=True)`. Workers use `exit_on_error=False` and report through one error handler (section 7).
- **HTTP.** Base URL = instance URL + `/api`. Timeout 30 s, matching the gateway's per-request limit. Every request sends `X-Request-Id`; the id is shown with an error so a teammate can find it in the logs.
- **Session and refresh.** `Session.request()` adds the bearer header. It refreshes when the stored expiry (`X-Access-Expires-In`, 15 min by default) is under 60 s away, or once after a 401, then retries the original call once. Refresh tokens are single use and a replay ends the whole session (FR-10), so refresh is single-flight: an `asyncio.Lock` in the process, and an exclusive `flock` on the credential file across processes (a `git2` command and the TUI can run at the same time). After taking the lock, the file is read again and the refresh is skipped if another process already rotated it.
- **Credential file.** `$XDG_CONFIG_HOME/git2/credentials.json`, default `~/.config/git2/credentials.json` (FR-83 "local configuration"). Directory 0700, file created with `os.open(..., O_CREAT | O_EXCL, 0o600)`, updated by writing a 0600 temp file and `os.replace`. On load, a file readable by group or others is refused with a message naming the `chmod 600` fix. Content: instance URL, username, access token, refresh token, expiry, and `kind: session | pat` so the PAT lands without a format change. The account password is never stored.
- **Git credentials.** Before W6, push and pull need the account password: the TUI asks in a modal and hands it to Git for that one operation through `GIT_ASKPASS` with `GIT_TERMINAL_PROMPT=0`, so Git never prompts on the full-screen terminal. From W6 the stored PAT is used. **[A]** `gitops` exposes a credentials hook.
- **Job log.** `streamJobLog` is SSE and the gateway cuts it after 30 s: the reader reconnects with `offset` until `event: end`.

## 7. Error handling

Errors are classified by status code and exception type, never by message text. Server text is shown only where the contract defines it (`{"error": "..."}`).

| Case | Behaviour |
|---|---|
| 401 on `/api/*` | One refresh, one retry. If refresh answers 401: drop both tokens, show "not signed in or token revoked", open Sign-in (UC-27 E1). Local changes untouched |
| 429 | Read `Retry-After`, show a countdown in the status bar, retry once for GETs. Never auto-retry POST (comment, approve, create MR) |
| 503 / 502 | "git2 is not available, try again". Tokens are kept (NFR-22). `R` retries. No automatic loop |
| 404 | "not found or no access" (the API hides forbidden resources as 404) |
| 409 / 413 | Show the server's `error` text; comments are capped client-side below the 64 KiB body limit |
| Connect error, timeout | "cannot reach <instance URL>" (UC-27 E3). The Changes screen keeps working, since it is local |
| Git push or pull refused | Show Git's stderr unchanged in a scrollable modal (FR-35, UC-27 E2) |

## 8. Testing

All tests live in `tests/` (course rule): `tests/tui/`, `tests/auth/`.

- **Pilot tests.** `async with app.run_test(size=(120, 40)) as pilot:` then `await pilot.press(...)`, `await pilot.pause()` and assertions on widgets and on the fakes (Textual API: `App.run_test`, headless, default size 80x24). Needs pytest-asyncio **[A]** (from the Textual testing guide, which I did not fetch).
- **Fake API server.** `httpx.MockTransport` **[A]** (not re-checked in the httpx docs) injected into the Session: in-process, no sockets, scripted per test. Cases that must refuse: expired access token then refresh then retry; refresh 401 opens Sign-in; 429 with `Retry-After` on a POST is not retried; 503 keeps the tokens; two concurrent 401s cause exactly one refresh call.
- **Contract check.** One CI job runs the same client against `prism mock` of the pinned `openapi.yaml`, so drift in the draft MR shapes fails loudly.
- **Local Git.** Real repositories in `tmp_path` with a bare repository as remote; no network. Stage, commit, branch, and a push refused by a `pre-receive` hook to prove the rejection text is shown.
- **Token store.** Seams for home directory and clock. Tests: file mode is 0600, a 0644 file is refused, two processes refreshing rotate once.
- **Snapshots** (optional): `pytest-textual-snapshot` for the Changes and MR detail layouts.

## 9. Packaging

`pyproject.toml` with `[project.scripts] git2 = "git2cli.cli:main"`; the Typer callback starts the TUI when no subcommand is given (UC-27 step 5). Dependencies: typer, textual, httpx, GitPython, pinned with upper bounds. **[A]** Textual 8.x is current (Context7 lists v8.2.8; check PyPI before pinning) and the minimum Python is 3.11. Install: `pipx install git+https://github.com/git2-project/git2-cli@v1.0.0` **[A]**, or a wheel attached to the release; PyPI is not planned. CI smoke test on a clean runner: `pipx install .` then `git2 --version` (D-07 acceptance).

## 10. Work breakdown

Sprint 3 = 23.11-06.12, Sprint 4 = 07.12-20.12. The plan sized all of WBS-14.2 as S (6.3 h); the sum below is larger and should go into the plan revision.

| # | Title | Size | Sprint | Can start |
|---|---|---|---|---|
| 1 | `chore(cli): scaffold git2 package with Textual app shell, Pilot harness and pipx smoke test` | S | 3 | Now, once the repo question (section 3) is answered |
| 2 | `feat(auth): add 0600 token store and session with single-flight refresh` | S | 3 | Now (auth-svc is implemented). Abdullah reviews, since his client uses it |
| 3 | `feat(tui): add sign-in screen and signed-out handling` | XS | 3 | After 2. PAT field waits for WBS-3.2 (W6) |
| 4 | `feat(tui): add changes view with stage, unstage, diff and commit` | M | 3 | Now, behind the `LocalRepo` port. Final wiring needs Abdullah's `gitops` (WBS-14.1) |
| 5 | `feat(tui): add branch switcher, push and pull` | S | 3-4 | Local part now. Push and pull need `gitops` plus the gateway Git route on a dev instance |
| 6 | `feat(tui): add repository list and clone` | S | 4 | Mock only today. Needs Abdullah's client and a dash-api `/repos` server (missing) |
| 7 | `feat(tui): add merge request list, detail, comment, approve and pipeline status` | L | 4 | Mock only today. Needs the draft MR endpoints (WBS-8.2), pipeline endpoints (WBS-10.5) and Abdullah's client |

Risk: item 7 depends on three other lanes and lands in the last sprint before the 25.12 codebase deadline. If the MR API slips, FR-85 is still met by Abdullah's CLI commands and the TUI ships items 1-6 (FR-86 complete).

## 11. Open questions

For Abdullah (Git core and CLI):
1. New repo `git2-cli` or `cli/` in git2? Package name `git2cli`, command `git2`?
2. Which OpenAPI generator, and does it give an async httpx client? (openapi-python-client is one candidate **[A]**.) The TUI needs async or it must wrap every call in a thread worker.
3. Do you accept `auth/` (token store + Session) as the single owner of tokens, written now by Ceyda, and the two Protocol ports as the TUI seam?
4. Does `gitops` use GitPython as the spec says, and can it expose a credentials hook and return Git's stderr unchanged?
5. Does `git2 mr create` live only in the CLI, with the TUI's `n` calling the same function?

For Bilge (Dash, dash-api contract):
1. When will dash-api serve `/repos` and the MR endpoints on a dev instance? Is the Prism mock the agreed target until then?
2. When do the draft MR and pipeline operations get their 4xx responses, and may the TUI rely on `MergeRequest.pipeline` and `mergeable` as drafted?
3. Is `/api/repos` final or will it become `/api/v1/repos` (contract open question 1)?
4. A list of repositories across owners (personal plus organizations): planned?

For Beste (Identity), found while reading: how is a PAT sent on `/api/*` (same `Authorization: Bearer`?), and which endpoint checks it at sign-in (`GET /auth/me`?).

## 12. Sources

- Textual docs through Context7 (`/websites/textual_textualize_io`): `App.push_screen`, `App.push_screen_wait`, `App.run_test` (https://textual.textualize.io/api/app); `BindingsMap.bind` and binding priority (https://textual.textualize.io/api/binding); the `work` decorator with `exclusive`, `thread`, `exit_on_error` (returned under https://textual.textualize.io/api/logger). The Screens, Workers and Testing guide pages were not fetched; `pilot.press` and `pilot.pause` are from memory **[A]**.
- git2 `dev/main`: `contracts/auth-svc/`, `contracts/dash-api/`, `contracts/git/README.md`; Requirements Specification FR-10, FR-35, FR-83 to FR-86, NFR-22, UC-27, SC-08, section 3.3.11.
- git2 `main`: `docs/ProjectPlan.md` D-07, WBS-14.1, WBS-14.2. git2-deploy: `docs/api/gateway.md` v0.1.
