# Project Charter: git2

## 1. Project Purpose and Objectives
git2 is an open-source, self-hosted Git platform in the spirit of GitLab that a company can deploy on its own servers with a single command. It is built as Go microservices and split into two planes: **Dash**, where developers work with repositories, merge requests, issues and CI/CD pipelines, and **Admin**, where IT manages users, single sign-on, runners, settings and backups. Our objectives are easy deployment (one container or a Kubernetes cluster), a full-instance backup to any S3-compatible bucket that restores every repository, setting and secret with one command, and a user-friendly experience, extended by a Python CLI that works like GitHub Desktop and an MCP server that lets AI agents use git2 through its API. We will build, containerize and deploy a live reference instance of git2.

## 2. Stakeholders & Roles
* **Product Owner:** Osman Yahya Akıncı
* **Software Engineering Team:**
    * **Osman Yahya Akıncı** : Project Manager, Pipelines Owner  
      Plans the nine-week roadmap and runs the Friday integration. Owns ci-svc and runner: reads `.git2-ci.yml`, queues jobs, runs each job in its own container and streams logs and artifacts back.
    * **Abdullah Akcan** : Git Core Owner  
      Owns git-svc: repository storage, push and clone over HTTPS and SSH, branches and protected branches, diff, merge and Git LFS. Publishes the repo.pushed events that start pipelines.
    * **Bilge Bostanbaşı** : Dash Owner  
      Owns dash-web and dash-api, the developer plane: repository and code browsing, merge requests with diffs and code review, issues, search and pipeline status on merge requests.
    * **Ceyda Akın** : Platform & Backup Owner  
      Owns the gateway (TLS, sign-in check, serving both UIs), git2ctl install and upgrades, Docker and Helm packaging, and backup-svc for encrypted snapshots and one-command restore.
    * **Zarif Beste Süner** : Scrum Master, Admin & Identity Owner  
      Runs the sprints and daily updates. Owns admin-web, admin-api and auth-svc: users, teams, SSO and LDAP, access tokens, runner and instance settings, and the audit log.
* **GH Repo/Project URL:** https://github.com/git2-project/git2

## 3. Project Scope
### Project domain
Software development tooling and DevOps: source code hosting, code review and CI/CD. Possible customers are organizations that must keep their source code on their own infrastructure, such as banks, public institutions, defense and healthcare companies, universities, and small and mid-sized software companies that want a lighter alternative to self-hosted GitLab. Key users are software developers (Dash plane), IT and system administrators (Admin plane), team leads and code reviewers, and AI coding agents that connect through MCP.

### In-Scope 
* Repository hosting: create, browse and delete repositories; push and clone over HTTPS and SSH; branches, tags, protected branches, file and commit browser, Git LFS
* Merge requests and code review: diffs, inline comments, approvals, merge strategies and pipeline status on merge requests
* Issue tracking: issues, labels, milestones, assignees, mentions and in-app notifications
* CI/CD pipelines: `.git2-ci.yml` pipeline definitions, containerized jobs on self-hosted runners, live job logs, artifacts and caches
* Identity and access: user accounts, organizations and teams, role-based permissions, personal access tokens, SSH keys, OIDC single sign-on and LDAP
* Admin plane: first-run setup wizard, instance settings, user and team management, runner management, health dashboard and audit log
* Backup and restore: scheduled, encrypted full-instance snapshots (database, repositories, `git2.yaml` config, secrets) to any S3-compatible bucket or local disk, and one-command restore to a fresh server
* Containerization and deployment: a Docker image per service, an all-in-one image, Docker Compose for single-host installs, a Helm chart for Kubernetes, `git2ctl` for install and upgrade, and a live deployed reference instance
* git2 CLI: a Python terminal app that works like GitHub Desktop (clone, status, stage, commit, branch, push and pull, open and review merge requests)
* AI integration: a local MCP server that turns AI agent requests into git2 REST API calls

### Out-of-Scope 
* Multi-tenant SaaS hosting, public sign-up and billing
* High-availability Git storage clustering and geo-replication
* Built-in container, package and artifact registries (Docker, npm, Maven, PyPI)
* Wikis, static site hosting (Pages), kanban boards and time tracking
* Built-in AI features inside git2 (LLM code review, code generation, chat); AI access is only through MCP
* Security scanning (SAST, DAST, dependency and secret scanning)
* Importers from GitHub, GitLab or Bitbucket beyond plain git mirroring
* Windows and macOS CI runners (Linux container runners only)
* Native mobile apps and a graphical desktop client

## 4. Technology Stack

### Backend Stack
* Go for all services (gateway, auth-svc, git-svc, dash-api, admin-api, ci-svc, runner, backup-svc)
* gRPC with Protocol Buffers for service-to-service calls
* REST with OpenAPI 3 for the public API, exposed through the gateway
* PostgreSQL as the primary database (one schema per service, transactional outbox for domain events)
* Redis for sessions, caching, rate limiting, background job queues, and pub/sub for live job logs and notifications
* Git CLI and go-git for repository storage and the Smart HTTP and SSH protocols
* S3-compatible object storage through gocloud.dev/blob (AWS S3, Google Cloud Storage, Azure Blob, SeaweedFS, Garage, Ceph or local disk)
* age for encrypting backup snapshots
* Containerization:
    * Multi-stage Dockerfiles build each Go service into a small distroless image (amd64 and arm64)
    * An all-in-one image runs every service in one container for small teams
    * Docker Compose runs git2 with PostgreSQL and Redis on a single host; repository and database data live on named volumes
    * A Helm chart deploys each service as its own Kubernetes Deployment, so busy services scale independently
    * Runners start each CI job in its own short-lived container through the Docker Engine API
    * GitHub Actions builds, tests and publishes the images to GitHub Container Registry
* OpenTelemetry, Prometheus and Grafana for tracing, metrics and dashboards

### Frontend Stack
* React with TypeScript, built with Vite into static files that the gateway serves (no Node.js server in production)
* Redux Toolkit with RTK Query for state and API calls (API client generated from the OpenAPI spec)
* React Router for routing
* Two apps, dash-web and admin-web, sharing one UI kit in a pnpm workspace
* Tailwind CSS with Radix UI components
* Shiki for syntax highlighting and CodeMirror 6 for in-browser editing
* Vitest and React Testing Library for unit tests, Playwright for end-to-end tests
* git2 CLI: Python with Typer for commands, Textual for the GitHub Desktop-style terminal UI, GitPython for local git operations and httpx for API calls, distributed with pipx

### AI Stack
* Model Context Protocol (MCP) for connecting AI agents to git2
* git2-mcp: a local MCP server written in Python with the official MCP Python SDK (FastMCP); it runs on the developer's machine over stdio, turns each tool call into an authenticated git2 REST API request and returns the result
* A shared Python API client, generated from the OpenAPI spec, used by both git2-mcp and the git2 CLI
* Tools exposed to agents: `list_repositories`, `read_file`, `search_code`, `list_issues`, `create_issue`, `comment_on_issue`, `open_merge_request`, `review_merge_request`, `get_pipeline_status`, `retry_pipeline`
* Access control: a scoped personal access token, and a read-only mode that disables all write tools
* MCP client configuration (Claude Code, Claude Desktop or any MCP-compatible agent):
    ```json
    {
      "mcpServers": {
        "git2": {
          "command": "uvx",
          "args": ["git2-mcp"],
          "env": {
            "GIT2_URL": "https://git.example.com",
            "GIT2_TOKEN": "${GIT2_TOKEN}",
            "GIT2_READ_ONLY": "false"
          }
        }
      }
    }
    ```
* Claude (Claude Code) as the team's AI development assistant, with a CLAUDE.md file in each service

## 5. Project Milestones
| Deliverable                       | Deadline          |
|---                                |---                |
| Project Charter                   | 05.10.2026 23:59  |
| Project Plan                      | 19.10.2026 23:59  |
| Requirement Specification Report  | 02.11.2026 23:59  |
| Requirement Analysis Report       | 23.11.2026 23:59  |
| Software Design Report            | 07.12.2026 23:59  |
| Software Test Report              | 21.12.2026 23:59  |
| Codebase                          | 25.12.2026 23:59  |
| Demo                              | 26.12.2026 23:59  |
| Presentation                      | 28.12.2026 23:59  |
