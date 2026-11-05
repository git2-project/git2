package authsvc

import (
	"context"
	"errors"
	"fmt"
	"time"

	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ErrSessionsNotEnded means the user is disabled in the database, but Redis
// failed. Refresh and admin routes already refuse them (both read the
// database); their access tokens keep working until they expire. Calling
// DisableUser again finishes the job.
var ErrSessionsNotEnded = errors.New("user disabled, but their sessions could not be ended")

// DisableGrace widens "issued before the disable" when checking tokens and
// sessions. It covers a sign-in that read the account as active just before
// the disable and issued its tokens just after, and clock differences
// between git2 processes. The cost: an account enabled again within this
// window has to sign in once more after it.
const DisableGrace = 10 * time.Second

// UserStatusStore is what DisableUser and EnableUser need from
// persistence. PostgresStore implements it.
type UserStatusStore interface {
	// Disable sets is_active = false and tokens_valid_after = now (database
	// clock), and returns that moment. ErrLastAdmin, ErrNotFound.
	Disable(ctx context.Context, id uuid.UUID) (time.Time, error)
	Enable(ctx context.Context, id uuid.UUID) error
}

// DisableUser turns an account off and makes its tokens stop working at once
// (docs/design/identity-v0.md, "Disabling a user"):
//
//  1. in the database: is_active = false, so sign-in and admin routes refuse
//     the user, and tokens_valid_after = now, so refresh refuses every
//     session that began before it, including ones Redis does not list;
//  2. in Redis: every listed refresh session is deleted, and a marker makes
//     RequireAuth reject access tokens issued before the disable.
//
// It returns ErrLastAdmin when id is the last active admin, ErrNotFound for
// an unknown user, and ErrSessionsNotEnded when step 1 worked but Redis
// failed. admin-api calls it; it is safe to repeat.
func DisableUser(ctx context.Context, users UserStatusStore, jwtMgr *authjwt.Manager, rdb *redis.Client, id uuid.UUID) error {
	at, err := users.Disable(ctx, id)
	if err != nil {
		return err
	}
	// The marker must outlive every access token issued up to at+grace. The
	// extra minute is slack for Redis expiry.
	markerTTL := jwtMgr.AccessExpiry() + DisableGrace + time.Minute
	if err := endUserSessions(ctx, rdb, id, at, markerTTL); err != nil {
		return fmt.Errorf("%w: %v", ErrSessionsNotEnded, err)
	}
	return nil
}

// EnableUser turns an account back on. Tokens and sessions from before the
// disable stay refused; the user signs in again (after DisableGrace).
func EnableUser(ctx context.Context, users UserStatusStore, id uuid.UUID) error {
	return users.Enable(ctx, id)
}
