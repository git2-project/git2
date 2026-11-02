# git.v1: git-svc contract (reference)

This directory holds no `.proto` files on purpose. The contract of git-svc lives in the [git2-codeplane](https://github.com/git2-project/git2-codeplane) repo, next to its implementation, and is referenced from here so it cannot drift (git2-codeplane ADR 0004).

| | |
|---|---|
| Source | [`proto/git/v1/`](https://github.com/git2-project/git2-codeplane/tree/v0.1.0/proto/git/v1) in git2-codeplane |
| Pinned version | `v0.1.0` |
| Protobuf package | `git.v1` |
| Owner | Git Core lane (Abdullah Akcan) |

## What it contains

- `repository.proto`: `RepositoryService` with `CreateRepository`, `GetRepository`, `FindRepository`, `ListRepositories`, `DeleteRepository`.
- `events.proto`: the domain events `RepositoryCreated`, `RepositoryDeleted` and `RepositoryPushed` (defined, not emitted yet).

Every RPC, message and field is documented in the `.proto` files, including validation rules and error codes.

## Using it

**Go services** import the generated code, which is committed in git2-codeplane:

```sh
go get github.com/git2-project/git2-codeplane@v0.1.0
```

```go
import gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"

client := gitv1.NewRepositoryServiceClient(conn)
```

**Mocks and other languages** generate straight from the pinned tag with buf:

```sh
buf generate "https://github.com/git2-project/git2-codeplane.git#tag=v0.1.0,subdir=proto"
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
