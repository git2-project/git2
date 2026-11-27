package dashapi

import (
	"context"

	"github.com/google/uuid"
)

// Level is a user's permission level on a repository. A higher level
// includes everything below it (docs/design/repo-access-v0.md).
type Level int

const (
	LevelNone Level = iota
	LevelRead
	LevelWrite
	LevelMaintain
)

func (l Level) String() string {
	switch l {
	case LevelRead:
		return "read"
	case LevelWrite:
		return "write"
	case LevelMaintain:
		return "maintain"
	default:
		return "none"
	}
}

// AccessChecker returns a user's level on a repository. ownerID is the
// repository's owner as git-svc reports it. An error means the level could
// not be decided; callers answer 503 and never let the request through.
type AccessChecker interface {
	Level(ctx context.Context, userID, repoID, ownerID uuid.UUID) (Level, error)
}

// ownerOnly is the v0 rule until the access model lands: the owner has
// maintain, everyone else nothing. It matches the gateway's rule for Git
// routes (gateway/git.go).
type ownerOnly struct{}

func (ownerOnly) Level(_ context.Context, userID, _, ownerID uuid.UUID) (Level, error) {
	if userID != uuid.Nil && userID == ownerID {
		return LevelMaintain, nil
	}
	return LevelNone, nil
}
