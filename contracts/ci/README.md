# ci.v1: ci-svc contract (reference)

This directory holds no `.proto` files on purpose. The contract of ci-svc lives in the [git2-runner](https://github.com/git2-project/git2-runner) repo, next to its implementation, and is referenced from here so it cannot drift (the same rule as `contracts/git/`).

| | |
|---|---|
| Source | [`app/proto/ci/v1/`](https://github.com/git2-project/git2-runner/tree/main/app/proto/ci/v1) in git2-runner |
| Pinned version | none yet; the first tag will be `app/v0.1.0` |
| Protobuf package | `ci.v1` |
| Owner | Pipelines lane (Osman Akıncı) |
| Reference for humans | [`docs/api/`](https://github.com/git2-project/git2-runner/tree/main/docs/api) in git2-runner |

## What it contains

- `pipeline.proto`: `PipelineService` (`TriggerPipeline`, `GetPipeline`, `ListPipelines`, `CancelPipeline`, `GetJob`, `StreamJobLog`, `ValidateConfig`), for dash-api and the event relay.
- `runner_admin.proto`: `RunnerAdminService` (registration tokens, list, pause, delete runners), for admin-api.
- `secret.proto`: `SecretService` (write-only CI secrets per instance or repository), for admin-api and dash-api.
- `runner.proto`: `RunnerService`, the protocol between ci-svc and its runners. Nothing in this repo calls it.
- `events.proto`: `PipelineCreated`, `PipelineFinished`, `JobFinished`, written to `ci_svc.outbox`.

ci-svc does not check user permissions: dash-api and admin-api check them before calling, as they do for git-svc.

## Using it

**Go services** import the generated code, which is committed in git2-runner:

```sh
go get github.com/git2-project/git2-runner/app@<tag or commit>
```

```go
import civ1 "github.com/git2-project/git2-runner/app/gen/go/ci/v1"

client := civ1.NewPipelineServiceClient(conn) // ci-svc's internal port, :9090 by default
resp, err := client.ListPipelines(ctx, &civ1.ListPipelinesRequest{RepositoryId: repoID})
```

## Database

ci-svc's tables are in the `ci_svc` schema, created by [`migrations/003_ci_pipelines.sql`](../../migrations/003_ci_pipelines.sql) in this repo. git2-runner keeps a byte-identical copy for its tests.
