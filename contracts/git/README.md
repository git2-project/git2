# git.v1: git-svc contract (reference)

This directory holds no `.proto` files on purpose. The contract of git-svc lives in the [git2-codeplane](https://github.com/git2-project/git2-codeplane) repo, next to its implementation, and is referenced from here so it cannot drift (git2-codeplane ADR 0004).

| | |
|---|---|
| Source | [`proto/git/v1/`](https://github.com/git2-project/git2-codeplane/tree/v0.4.0/proto/git/v1) in git2-codeplane |
| Pinned version | `v0.4.0` |
| Protobuf package | `git.v1` |
| Owner | Git Core lane (Abdullah Akcan) |

## What it contains

- `repository.proto`: `RepositoryService` with `CreateRepository`, `GetRepository`, `FindRepository`, `ListRepositories`, `DeleteRepository`.
- `ref.proto`: `RefService` with `ListBranches` and `ListTags`.
- `commit.proto`: `CommitService` with `GetTree`, `GetBlob`, `GetRawBlob` (a server stream), `ListCommits`, `GetCommit` and `GetArchive` (a server stream): directories, files, history, single commits with their diffs, and a commit's files as a tar archive.
- `bundle.proto`: `BundleService` with `ListAllRepositories`, `CreateBundle` (a server stream) and `RestoreBundle` (a client stream): whole repositories as Git bundles, for backups.
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

The decisions behind these RPCs (ref rules, limits, ordering, and what is left out) are in [git2-codeplane ADR 0013](https://github.com/git2-project/git2-codeplane/blob/v0.4.0/docs/decisions/0013-reading-repository-content.md).

## Source for CI jobs

`CommitService.GetArchive` gives a CI job the files of the pushed commit. ci-svc calls it with `ref` set to the pushed SHA, and the runner copies the tar into the job's container with Docker's API (git2-runner #19).

- The files are exactly as committed: `.gitattributes` is not applied, there is no `.git` directory, submodules are empty and Git LFS files are their pointers.
- Only a stream that ends with `OK` holds the whole archive.
- The content is whatever was pushed, so it is never extracted on the runner's host.

The decisions are in [git2-codeplane ADR 0016](https://github.com/git2-project/git2-codeplane/blob/v0.4.0/docs/decisions/0016-commit-archives-for-ci-jobs.md).

## Backups

`BundleService` copies every repository out of git-svc as a Git bundle, and back in on restore. It produces the `repositories/<repo-id>.bundle` files of git2-deploy's snapshot format ([`contracts/backup/`](../backup/README.md)).

- Only backup-svc and `git2ctl restore` may call it. It reads repositories whatever their owner, so the gateway and dash-api must never call it.
- A backup runs `pg_dump` first, then `ListAllRepositories` and one `CreateBundle` per repository. A restore loads the database first, then calls `RestoreBundle` per bundle.
- For now it shares git-svc's gRPC port with the other services. A port of its own and a size limit for restores are open items.

The decisions are in [git2-codeplane ADR 0015](https://github.com/git2-project/git2-codeplane/blob/v0.4.0/docs/decisions/0015-backups-as-git-bundles.md).

## Using it

**Go services** import the generated code, which is committed in git2-codeplane:

```sh
go get github.com/git2-project/git2-codeplane@v0.4.0
```

```go
import gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"

repos := gitv1.NewRepositoryServiceClient(conn)
refs := gitv1.NewRefServiceClient(conn)
commits := gitv1.NewCommitServiceClient(conn)
bundles := gitv1.NewBundleServiceClient(conn) // backup-svc and git2ctl restore only
```

**Mocks and other languages** generate straight from the pinned tag with buf:

```sh
buf generate "https://github.com/git2-project/git2-codeplane.git#tag=v0.4.0,subdir=proto"
```

**Events** are written to git-svc's outbox, and its relay delivers each one to the services subscribed to its type through `events.v1.EventSink` ([`contracts/events/`](../events/README.md)):
- The event type is the full message name, e.g. `git.v1.RepositoryCreated`.
- The payload is canonical protobuf JSON; decode it with `protojson.Unmarshal` into the matching message.
- Delivery is at least once, so consumers must be idempotent.

## Changing it

1. Open a PR in git2-codeplane that changes `proto/git/v1/` together with the implementation.
2. Every affected owner approves it.
3. After merge, a new tag is cut and the version above is bumped in a PR to this repo.

Do not add `.proto` files for `git.v1` to this directory.
