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

// ErrSessionsNotEnded means the user is disabled in the database, but their
// sessions could not be ended in Redis. Refresh and admin routes already
// refuse them (both read is_active); their access tokens keep working until
// they expire. Calling DisableUser again finishes the job.
var ErrSessionsNotEnded = errors.New("user disabled, but their sessions could not be ended")

// UserStatusStore is what DisableUser needs from persistence. PostgresStore
// implements it.
type UserStatusStore interface {
	SetActive(ctx context.Context, id uuid.UUID, active bool) error
}

// DisableUser turns an account off and makes its tokens stop working at once
// (docs/design/identity-v0.md, "Disabling a user"):
//
//  1. is_active = false in the database: sign-in, refresh and admin routes
//     refuse the user from here on;
//  2. every refresh session of the user is deleted;
//  3. a marker makes RequireAuth reject every access token issued until now.
//
// It returns ErrNotFound for an unknown user, and ErrSessionsNotEnded when
// step 1 worked but Redis failed. admin-api calls it; it is safe to repeat.
func DisableUser(ctx context.Context, users UserStatusStore, jwtMgr *authjwt.Manager, rdb *redis.Client, id uuid.UUID) error {
	if err := users.SetActive(ctx, id, false); err != nil {
		return err
	}
	// The marker must outlive every access token issued before now. The extra
	// minute covers clock skew between git2 processes.
	markerTTL := jwtMgr.AccessExpiry() + time.Minute
	if err := endUserSessions(ctx, rdb, id, time.Now(), markerTTL); err != nil {
		return fmt.Errorf("%w: %v", ErrSessionsNotEnded, err)
	}
	return nil
}

// EnableUser turns an account back on. Tokens issued while it was disabled
// do not exist (sign-in refused it), and tokens from before stay rejected,
// so the user simply signs in again.
func EnableUser(ctx context.Context, users UserStatusStore, id uuid.UUID) error {
	return users.SetActive(ctx, id, true)
}
