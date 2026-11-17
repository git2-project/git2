# events.v1: event delivery contract (reference)

This directory holds no `.proto` files on purpose. The contract that carries domain events between services lives in the [git2-codeplane](https://github.com/git2-project/git2-codeplane) repo, next to the first relay that calls it, and is referenced from here so it cannot drift, as for `git.v1` (git2-codeplane ADR 0004).

| | |
|---|---|
| Source | [`proto/events/v1/`](https://github.com/git2-project/git2-codeplane/tree/v0.3.0/proto/events/v1) in git2-codeplane |
| Pinned version | `v0.3.0` |
| Protobuf package | `events.v1` |
| Decisions | [git2-codeplane ADR 0014](https://github.com/git2-project/git2-codeplane/blob/v0.3.0/docs/decisions/0014-outbox-relay.md), git2-runner ADR 0010 |
| Owner | Git Core lane (Abdullah Akcan), agreed with the Pipelines lane (Osman Akıncı) |

## What it contains

- `event_sink.proto`: `EventSink` with one RPC, `Deliver(DeliverRequest)`. The request is one outbox row: `event_id`, `event_type`, `aggregate_type`, `aggregate_id`, `occurred_at` and `payload`, the event message in protobuf JSON.

## How an event travels

1. A producer writes the event to its own outbox, in the same transaction as the change (git2-codeplane ADR 0007).
2. The producer's relay calls `EventSink.Deliver` on every consumer subscribed to the event's type. The list of consumers is the producer's configuration, set by Deploy on the instance network.
3. The consumer stores the event and answers at once; the work happens afterwards. The relay waits at most 10 seconds.

Delivery is at least once and in no guaranteed order. A consumer drops repeats by `event_id`.

| Producer | Events | Subscribers |
|---|---|---|
| git-svc (`GIT2_GITSVC_EVENT_SINKS`) | `git.v1.RepositoryCreated`, `RepositoryDeleted`, `RepositoryPushed` | ci-svc: `RepositoryPushed` |
| ci-svc (planned) | `ci.v1.PipelineCreated`, `PipelineFinished`, `JobFinished` | dash-api (planned) |

## What an answer means

| Answer | The relay |
|---|---|
| `OK` | counts the event as delivered to this consumer |
| `INVALID_ARGUMENT`, `FAILED_PRECONDITION` | logs it at ERROR and never sends that event to this consumer again |
| any other code, including `UNIMPLEMENTED` | tries again later with a growing pause, and never drops the event |

gRPC also answers `UNIMPLEMENTED` when a server does not serve `EventSink` at all, so that code is retried. A consumer that gets an event type it does not handle answers `INVALID_ARGUMENT`.

## Using it

**Go consumers** import the generated code, which is committed in git2-codeplane:

```sh
go get github.com/git2-project/git2-codeplane@v0.3.0
```

```go
import eventsv1 "github.com/git2-project/git2-codeplane/gen/go/events/v1"

type sink struct{ eventsv1.UnimplementedEventSinkServer }

func (s *sink) Deliver(ctx context.Context, req *eventsv1.DeliverRequest) (*eventsv1.DeliverResponse, error) {
	// Store the event under req.GetEventId(), ignoring a repeat, and answer.
	// Decode req.GetPayload() with protojson into the message named by
	// req.GetEventType() when the work runs.
	return &eventsv1.DeliverResponse{}, nil
}

eventsv1.RegisterEventSinkServer(grpcServer, &sink{}) // on the internal gRPC port
```

**Mocks and other languages** generate from the pinned tag with buf:

```sh
buf generate "https://github.com/git2-project/git2-codeplane.git#tag=v0.3.0,subdir=proto"
```

## Changing it

1. Open a PR in git2-codeplane that changes `proto/events/v1/`.
2. Every service that serves or calls `EventSink` approves it.
3. After merge, a new tag is cut and the version above is bumped in a PR to this repo.

Do not add `.proto` files for `events.v1` to this directory.
