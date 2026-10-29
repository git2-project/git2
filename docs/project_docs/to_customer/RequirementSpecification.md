# Software Engineering — Requirement Specification Report

## Cover Page

| Field | Details |
|---|---|
| **Course** | BLG411E — Software Engineering |
| **Report name** | Requirement Specification Report |
| **Project title** | git2 — A Self-Hosted Git Platform |
| **Group name / number** | feastLabs |
| **Group members** | Osman Yahya Akıncı — 150230106, Abdullah Akcan — 150220051, Bilge Bostanbaşı — 150230081, Ceyda Akın — 150220096, Zarif Beste Süner — 150220085 |
| **Instructor** | Tolga Ovatman, Ayşe Tosun Kühn |
| **Editor** | Bilge Bostanbaşı |
| **Submission date** | 2026-11-02 |
| **Document version** | 0.1 (draft) |

> **Draft status (remove before submission).** Shared sections 1, 2, 4, 5 and 7 and the Dash part of section 3 are drafted by the editor. Each lane owner writes their part of section 3 and adds their rows to section 6 **by Saturday 31.10, 20:00**. Use the story format in 3.0 exactly, so the report reads as one document.

---

## 1. Introduction

### 1.1 Purpose

This report specifies what git2 must do and how well it must do it. It turns the scope of the Project Plan (19.10.2026) into user stories with acceptance criteria and into measurable non-functional requirements. The Requirement Analysis Report (23.11) models these requirements; the Software Design Report (07.12) designs the system that meets them; the Software Test Report (21.12) tests against the acceptance criteria written here.

### 1.2 Product scope

git2 is an open-source, self-hosted Git platform that an organization installs on its own servers with one command (`git2ctl install`), on a single container host or a Kubernetes cluster. It has two planes:

- **Dash**, where software developers work with repositories, merge requests, issues and CI/CD pipelines;
- **Admin**, where IT and system administrators manage users, single sign-on, runners, settings and backups.

One encrypted backup to any S3-compatible bucket restores the whole instance on a new server. A Python CLI and an MCP server let developers and AI coding agents use git2 from the terminal.

Out of scope, as fixed in the Project Plan: multi-tenant SaaS hosting, public sign-up and billing; high-availability Git storage; built-in package registries; wikis, Pages, kanban boards and time tracking; built-in AI features; security scanning; importers beyond plain git mirroring; Windows and macOS runners; mobile and desktop apps.

### 1.3 Definitions

| Term | Meaning |
|---|---|
| Instance | One installation of git2 on an organization's servers |
| Plane | One of the two web applications: Dash (developers) or Admin (IT) |
| Lane | One team member's area of ownership (Section 4.1 of the Project Plan) |
| MR | Merge request: a proposal to merge one branch into another, with review |
| Pipeline, job, runner | A CI/CD run defined in `.git2-ci.yml`; one step of it; the machine that executes jobs |
| PAT | Personal access token, used by Git clients and the CLI instead of a password |
| MCP | Model Context Protocol, through which AI agents call git2 |
| MoSCoW | Priorities: **Must**, **Should**, **Could**, **Won't** (this release) |

### 1.4 References

- Project Charter (05.10.2026) and Project Plan v1.0 (19.10.2026)
- API contracts: `contracts/dash-api/openapi.yaml`, `git.v1` (git2-codeplane), `ci.v1` (git2-runner)

---

## 2. Stakeholders and Actors

| Actor | Who | Uses | Main goals |
|---|---|---|---|
| Developer | Software engineer in the customer organization | Dash, Git client, git2 CLI | Host code, review changes, track work, see pipeline results |
| Reviewer / team lead | Senior developer | Dash | Review and approve merge requests, keep branches protected |
| IT administrator | System administrator of the organization | Admin | Manage users, teams, SSO/LDAP, runners, settings, audit log |
| Server operator | Person with shell access to the host | `git2ctl` | Install, upgrade, back up and restore the instance |
| AI coding agent | e.g. Claude Code, on a developer's machine | git2-mcp | Read repositories, open MRs, check pipelines on the developer's behalf |
| Runner | `git2-runner` process on a Linux host | ci-svc | Execute CI jobs in containers |

Customer organizations are those that must keep source code on their own servers: banks, public institutions, defense and healthcare companies, universities and small and mid-sized software companies.

---

## 3. Functional Requirements

### 3.0 Story format (all lanes)

Each requirement is a user story:

> **US-‹LANE›-‹nn› — ‹short title›** · Priority: Must / Should / Could · Task: T-xx · WBS: x.y
>
> As a ‹actor›, I want ‹capability›, so that ‹benefit›.
>
> **Acceptance criteria**
> 1. Given ‹state›, when ‹action›, then ‹observable result›.
> 2. …

Lane prefixes: `DASH` (Bilge), `GIT` (Abdullah), `CI` (Osman), `ADM` (Zarif), `PLAT` (Ceyda), `CLI` and `MCP` (Abdullah, Osman). Acceptance criteria must be testable: the Software Test Report will cite them by story ID.

### 3.1 Identity and access — T-02, T-03, T-04 (Zarif)

> **To do (Zarif, Sat 31.10 20:00):** sign-in and sessions for both planes, users/teams/roles, PATs and SSH keys, OIDC and LDAP. Include how an account becomes an instance administrator (see open issue OI-01).

### 3.2 Repository hosting and Git — T-05, T-07, T-08 engine (Abdullah)

> **To do (Abdullah):** create/delete repositories, push and clone over HTTPS (SSH optional), protected branches, Git LFS, diff and merge engine.

### 3.3 Dash: code browsing, review, issues, pipeline views — T-06, T-08, T-09, T-10 (Bilge)

#### Code browsing (T-06, WBS-6.1)

**US-DASH-01 — See my repositories** · Must · T-06 · WBS-6.1
As a developer, I want a list of the repositories I can access, so that I can open one quickly.
1. Given I am signed in, when I open Dash, then I see the repositories I can read, newest first, with name, owner and description.
2. Given more than 50 repositories, when I scroll to the end, then the next page loads.
3. Given a repository I cannot read, then it does not appear and its URL shows "not found".

**US-DASH-02 — Browse files on any branch or tag** · Must · T-06 · WBS-6.1
As a developer, I want to browse a repository's directories on any branch or tag, so that I can read the code without cloning it.
1. Given a repository, when I open it, then I see the root directory of the default branch, directories first.
2. When I pick another branch or tag in the ref picker, then the listing shows that ref and the URL changes so it can be shared.
3. When I open a directory, then I see its entries and a breadcrumb back to the root.

**US-DASH-03 — Read a file with syntax highlighting** · Must · T-06 · WBS-6.1
As a developer, I want to read a file with syntax highlighting and line numbers, so that code is easy to follow and to point at.
1. Given a text file up to 1 MiB, when I open it, then it is highlighted for its language and lines are numbered.
2. When I click a line number, then the URL links to that line.
3. Given a binary file, a file over 1 MiB or a Git LFS file, then I see its size and a download link instead of broken text.

**US-DASH-04 — Commit history** · Must · T-06 · WBS-6.1
As a developer, I want the commit history of a branch, a directory or a file, so that I can see who changed what and when.
1. Given a branch, when I open its history, then I see commits newest first with title, author, date and short SHA.
2. Given a file, when I open its history, then I see only commits that changed it.

**US-DASH-05 — Commit details** · Must · T-06 · WBS-6.1
As a reviewer, I want to see what one commit changed, so that I can understand it without a local checkout.
1. When I open a commit, then I see its full message, author, parents and a diff per changed file with added and removed lines.
2. Given more than 300 changed files, then the page says the list is truncated.

**US-DASH-06 — First push of an empty repository** · Should · T-06 · WBS-6.1
As a developer, I want clear instructions in a new, empty repository, so that I can make the first push without help.
1. Given an empty repository, when I open it, then I see its HTTPS clone URL with a copy button and the commands for a first push.

**US-DASH-07 — Search code** · Could (optional scope) · T-06 · WBS-6.2
As a developer, I want to search a repository's default branch, so that I can find where something is defined.
1. When I search for a term, then I see matching files and lines, linking to the line.

#### Merge requests and code review (T-08, WBS-8.2)

**US-DASH-08 — Open a merge request** · Must · T-08 · WBS-8.2
As a developer, I want to open a merge request from my branch, so that my change is reviewed before it reaches the target branch.
1. When I choose source and target branches, then I see the commits and changes between them before creating the MR.
2. When I create it with a title and description, then it gets a number (`!n`) and appears in the open MR list.

**US-DASH-09 — Review the changes** · Must · T-08 · WBS-8.2
As a reviewer, I want to read an MR's changes file by file, so that I can review them.
1. When I open the MR's changes, then I see each file's diff with old and new line numbers.
2. When new commits are pushed to the source branch, then the diff shows the latest state.

**US-DASH-10 — Comment on a line** · Must · T-08 · WBS-8.2
As a reviewer, I want to comment on a specific line and reply to comments, so that feedback is precise.
1. When I comment on a line of the diff, then the comment appears at that line for everyone.
2. When the line later changes, then the comment is marked outdated but stays readable.

**US-DASH-11 — Approve** · Must · T-08 · WBS-8.2
As a reviewer, I want to approve an MR or withdraw my approval, so that the author knows it is ready.
1. When I approve, then my name appears in the MR's approvals; when I withdraw, it disappears.

**US-DASH-12 — Merge** · Must · T-08 · WBS-8.2
As a maintainer, I want to merge an MR with a chosen strategy, so that the history matches the team's convention.
1. Given an approved MR without conflicts, when I merge with merge commit, squash or fast-forward, then the target branch contains the change and the MR shows "merged".
2. Given conflicts, a failed pipeline or a branch protection rule that blocks it, then the merge button is disabled and the reason is shown.

#### Issues and notifications (T-09, WBS-9.1)

**US-DASH-13 — Track work in issues** · Must · T-09 · WBS-9.1
As a developer, I want to create issues with labels, a milestone and assignees, so that the team can track work.
1. When I create an issue, then it gets a number (`#n`) and appears in the open issue list.
2. When I change labels, milestone, assignees or close it, then the change is shown in the issue's history.

**US-DASH-14 — Find issues** · Should · T-09 · WBS-9.1
As a team lead, I want to filter issues by state, label, milestone and assignee, so that I can plan a sprint.
1. When I apply filters, then only matching issues are listed and the filters are kept in the URL.

**US-DASH-15 — Mentions notify people** · Must · T-09 · WBS-9.1
As a developer, I want to be notified when someone mentions me or assigns me, so that I do not miss work.
1. Given a comment containing `@ada`, then user ada gets an in-app notification linking to the comment.
2. When I open a notification, then it is marked as read.

#### Pipeline views (T-10, WBS-10.5)

**US-DASH-16 — See pipelines** · Must · T-10 · WBS-10.5
As a developer, I want the pipelines of a repository and their stages and jobs, so that I know whether my change builds.
1. When I open a repository's pipelines, then I see them newest first with status, ref and commit.
2. When I open a pipeline, then I see its stages and jobs with status and duration.

**US-DASH-17 — Watch a job's log live** · Must · T-10 · WBS-10.5
As a developer, I want to watch a running job's log, so that I can see failures as they happen.
1. Given a running job, when I open its log, then new lines appear within 2 s of the job printing them.
2. Given a finished job, then I see its whole log and can download its artifacts.

**US-DASH-18 — Pipeline status on a merge request** · Must · T-10 · WBS-10.5
As a reviewer, I want to see the latest pipeline's status on an MR, so that I do not merge a broken change.
1. Given an MR, then its page shows the status of the latest pipeline of the source branch, linking to it.

### 3.4 CI/CD backend and runners — T-10 (Osman)

> **To do (Osman):** `.git2-ci.yml`, pipeline creation on push, job queue, runners in containers, log streaming, artifacts.

### 3.5 Admin plane — T-11 (Zarif)

> **To do (Zarif):** setup wizard and first administrator, runner registration, instance settings, audit log, health dashboard.

### 3.6 Backup, restore and deployment — T-12, T-13 (Ceyda)

> **To do (Ceyda):** scheduled encrypted snapshots, bucket and local disk, one-command restore, Compose, Helm, `git2ctl install/upgrade`, gateway.

### 3.7 CLI and AI agent integration — T-14 (Abdullah), T-15 (Osman)

> **To do (Abdullah, Osman):** git2 CLI commands and TUI; git2-mcp tools and read-only mode.

---

## 4. Non-functional Requirements

Each requirement is measurable; the "Verified by" column is the test the Software Test Report will report on.

| ID | Quality | Requirement | Verified by |
|---|---|---|---|
| NFR-01 | Security | Every API except health checks requires authentication | Authorization test for every endpoint (allowed and denied roles) |
| NFR-02 | Security | Permissions are checked server-side on every call; a repository the caller cannot read answers "not found" | Same test suite |
| NFR-03 | Security | Passwords are hashed (bcrypt today; Argon2id in the Project Plan, see OI-05); PATs are scoped and revocable | Unit tests; revocation test |
| NFR-04 | Security | All external traffic uses TLS; backups are encrypted with `age` and unreadable without the key | Gateway configuration review; decrypt-without-key test |
| NFR-05 | Security | Administrator actions are written to the audit log | Admin E2E test |
| NFR-06 | Security | CI jobs run unprivileged with CPU and memory limits on a separate runner host | Runner configuration test |
| NFR-07 | Performance | p95 response time < 300 ms for page APIs with 20 users and 50 repositories on 4 vCPU / 8 GB | k6 load test, W8 |
| NFR-08 | Performance | HTTPS clone of a 100 MB repository takes < 2× plain `git http-backend` | Timed clone benchmark |
| NFR-09 | Performance | A queued job starts on an idle runner in < 5 s; log lines reach the browser in < 2 s | Timestamps in runner and browser logs |
| NFR-10 | Usability | `git2ctl install` finishes in < 10 minutes on a clean host | Timed install on a clean VM |
| NFR-11 | Usability | A new user creates a repository, pushes and opens an MR in < 10 minutes without help | Hallway test with 3 people, W8 |
| NFR-12 | Usability | Dash and Admin share one design system; keyboard navigation; WCAG 2.1 AA contrast; Lighthouse accessibility ≥ 90 on main pages | Lighthouse report |
| NFR-13 | Reliability | Events are delivered at least once; losing Redis loses no durable data | Outbox redelivery tests; fault test, W8 |
| NFR-14 | Reliability | A restore on a new server reproduces repositories, users, settings and secrets | Weekly restore test from W5 |
| NFR-15 | Portability | Runs on amd64 and arm64, with Compose and Helm, on any S3-compatible bucket or local disk | Multi-arch build; Helm install on k3d/kind; bucket driver tests |
| NFR-16 | Maintainability | Contract-first APIs; lint and format checks in CI; ≥ 60% unit-test coverage per Go service by W8 | CI gates and coverage report |

---

## 5. Constraints and Assumptions

- **Schedule:** 9 one-week sprints, 26.10–27.12.2026; codebase due 25.12, demo 26.12, presentation 28.12.
- **Team:** 5 members at about 15 h/week each (675 h in total); Claude Code assists every member but is not counted as a member, and every AI-generated change is reviewed by a human.
- **Technology:** Go services (gRPC inside, REST/OpenAPI outside), PostgreSQL, Redis (no durable state), React + TypeScript, Python for the CLI and MCP server, Docker, Compose and Helm.
- **Deployment:** self-hosted only; the product never needs to reach the internet to work.
- **Budget:** about $370 for the demo VM, bucket, domain and AI seats, paid by the team.

---

## 6. Traceability

| Story | Task | WBS | Deliverable | Milestone |
|---|---|---|---|---|
| US-DASH-01 – 06 | T-06 | 6.1 | D-02 | M1 First Push (13.11) |
| US-DASH-07 | T-06 | 6.2 (optional) | D-02 | — |
| US-DASH-08 – 12 | T-08 | 8.2 | D-02 | M2 First Merge (27.11) |
| US-DASH-13 – 15 | T-09 | 9.1 | D-02 | M3 (11.12) |
| US-DASH-16 – 18 | T-10 | 10.5 | D-02 | M3 Green Pipeline (11.12) |
| *Each lane adds its rows* | | | | |

---

## 7. Open Issues

| ID | Issue | Owner | Decide by |
|---|---|---|---|
| OI-01 | How an account becomes an instance administrator (`is_admin` flag checked on every Admin request) and how the first administrator is created | Zarif | W2 |
| OI-02 | Public sign-up is out of scope: keep `/register` for development only, or add an instance setting that enables it | Zarif, Osman (PO) | W2 |
| OI-03 | Serve Admin under `/admin/` or on its own host or port reachable only from the internal network; single sign-on across hosts | Ceyda, Zarif | W3 |
| OI-04 | Public API prefix and versioning (`/api/...` or `/api/v1/...`); who stores repository roles (dash-api or admin-api) | Bilge, Zarif | W2 |
| OI-05 | Password hashing: the Project Plan says Argon2id, auth-svc uses bcrypt | Zarif | W2 |
