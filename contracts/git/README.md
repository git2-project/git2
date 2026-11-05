# git.v1: git-svc contract (reference)

This directory holds no `.proto` files on purpose. The contract of git-svc lives in the [git2-codeplane](https://github.com/git2-project/git2-codeplane) repo, next to its implementation, and is referenced from here so it cannot drift (git2-codeplane ADR 0004).

| | |
|---|---|
| Source | [`proto/git/v1/`](https://github.com/git2-project/git2-codeplane/tree/v0.2.0/proto/git/v1) in git2-codeplane |
| Pinned version | `v0.2.0` |
| Protobuf package | `git.v1` |
| Owner | Git Core lane (Abdullah Akcan) |

## What it contains

- `repository.proto`: `RepositoryService` with `CreateRepository`, `GetRepository`, `FindRepository`, `ListRepositories`, `DeleteRepository`.
- `ref.proto`: `RefService` with `ListBranches` and `ListTags`.
- `commit.proto`: `CommitService` with `GetTree`, `GetBlob`, `GetRawBlob` (a server stream), `ListCommits` and `GetCommit`: directories, files, history, and single commits with their diffs.
- `events.proto`: the domain events `RepositoryCreated`, `RepositoryDeleted` and `RepositoryPushed` (written after every push that changes a ref).

Every RPC, message and field is documented in the `.proto` files, including validation rules, limits and error codes.

Git clients (clone, fetch, push) do not use gRPC: they reach git-svc's smart HTTP port through the gateway (git2-codeplane ADR 0012).

## Reading repository content

`RefService` and `CommitService` back dash-api's code-browsing endpoints, one RPC per endpoint under `/repos/{owner}/{repo}`:

| dash-api endpoint | git-svc RPC |
|---|---|
| `GET …/branches` | `RefService.ListBranches` |
| `GET …/tags` | `RefService.ListTags` |
| `GET …/tree` | `CommitService.GetTree` |
| `GET …/blob` | `CommitService.GetBlob` |
| `GET …/raw` | `CommitService.GetRawBlob` |
| `GET …/commits` | `CommitService.ListCommits` |
| `GET …/commits/{sha}` | `CommitService.GetCommit` |

dash-api resolves owner and name to a repository ID and checks access before calling, as for `RepositoryService`. It fills three fields of its own contract itself: `Blob.language` (from the file name), `Signature.user` (from the email) and `Branch.protected` (false until WBS-7.1).

ci-svc reads `.git2-ci.yml` with `CommitService.GetBlob`, with `ref` set to the pushed commit's SHA.

The decisions behind these RPCs (ref rules, limits, ordering, and what is left out) are in [git2-codeplane ADR 0013](https://github.com/git2-project/git2-codeplane/blob/v0.2.0/docs/decisions/0013-reading-repository-content.md).

## Using it

**Go services** import the generated code, which is committed in git2-codeplane:

```sh
go get github.com/git2-project/git2-codeplane@v0.2.0
```

```go
import gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"

repos := gitv1.NewRepositoryServiceClient(conn)
refs := gitv1.NewRefServiceClient(conn)
commits := gitv1.NewCommitServiceClient(conn)
```

**Mocks and other languages** generate straight from the pinned tag with buf:

```sh
buf generate "https://github.com/git2-project/git2-codeplane.git#tag=v0.2.0,subdir=proto"
```

**Events** arrive through git-svc's outbox:
- The event type is the full message name, e.g. `git.v1.RepositoryCreated`.
- The payload is canonical protobuf JSON; decode it with `protojson.Unmarshal` into the matching message.
- Delivery is at least once, so consumers must be idempotent.

## Changing it

1. Open a PR in git2-codeplane that changes `proto/git/v1/` together with the implementation.
2. Every affected owner approves it.
3. After merge, a new tag is cut and the version above is bumped in a PR to this repo.

Do not add `.proto` files for `git.v1` to this directory.
