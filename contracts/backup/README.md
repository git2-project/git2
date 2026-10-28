# backup.v1: backup-svc contract (reference)

This directory holds no `.proto` files on purpose. The contract of backup-svc lives in the [git2-deploy](https://github.com/git2-project/git2-deploy) repo, next to its implementation and next to `git2ctl restore`, which reads the same snapshot format (git2-deploy ADR 0003). It is referenced from here so it cannot drift, as for `git.v1` (git2-codeplane ADR 0004).

| | |
|---|---|
| Source | [`proto/backup/v1/`](https://github.com/git2-project/git2-deploy/tree/main/proto/backup/v1) in git2-deploy |
| Pinned version | `v0.1.0`, cut when the contract PR merges in git2-deploy |
| Protobuf package | `backup.v1` |
| Reference | [`docs/api/backup.md`](https://github.com/git2-project/git2-deploy/blob/main/docs/api/backup.md), [`docs/api/snapshot-format.md`](https://github.com/git2-project/git2-deploy/blob/main/docs/api/snapshot-format.md) |
| Owner | Platform & Backup lane (Ceyda Akın) |

## What it contains

- `backup.proto`: `BackupService` with `CreateSnapshot`, `GetSnapshot`, `ListSnapshots`, `DeleteSnapshot`, `GetBackupSettings`, `UpdateBackupSettings`.
- `events.proto`: the domain events `SnapshotFinished`, `SnapshotDeleted` and `BackupSettingsChanged`.

Restore is not an RPC: `git2ctl restore` reads the snapshot straight from the bucket on a fresh server.

## Using it

**Go services** import the generated code, which is committed in git2-deploy:

```sh
go get github.com/git2-project/git2-deploy@v0.1.0
```

```go
import backupv1 "github.com/git2-project/git2-deploy/gen/go/backup/v1"

client := backupv1.NewBackupServiceClient(conn)
```

**Mocks and other languages** generate from the pinned tag with buf:

```sh
buf generate "https://github.com/git2-project/git2-deploy.git#tag=v0.1.0,subdir=proto"
```

**Events** arrive through backup-svc's outbox (`backup_svc.outbox`): the type is the full message name (`backup.v1.SnapshotFinished`), the payload is canonical protobuf JSON, and delivery is at least once.

## Changing it

1. Open a PR in git2-deploy that changes `proto/backup/v1/` together with the implementation.
2. Every affected owner approves it.
3. After merge, a new tag is cut and the version above is bumped in a PR to this repo.

Do not add `.proto` files for `backup.v1` to this directory.
