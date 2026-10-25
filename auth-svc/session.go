package authsvc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Refresh-token sessions live in Redis as a per-session whitelist: one key
// holds the single currently-valid refresh JTI for a session (one sign-in on
// one device). Rotation overwrites the key in place, so the keyspace is bounded
// by the number of live sessions rather than by rotations × token lifetime.
const sessionKeyPrefix = "sess:"

// revokedKeyPrefix marks access-token JTIs revoked at logout. Each entry lives
// only as long as the token it revokes would have.
const revokedKeyPrefix = "revoked:"

func sessionKey(sid string) string { return sessionKeyPrefix + sid }

// storeSession records sid's currently-valid refresh JTI, overwriting any
// previous one. Called at login and on every rotation (which resets the TTL).
func storeSession(ctx context.Context, rdb *redis.Client, sid, jti string, ttl time.Duration) error {
	return rdb.Set(ctx, sessionKey(sid), jti, ttl).Err()
}

type rotateResult int

const (
	rotated        rotateResult = 1
	sessionMissing rotateResult = 0  // logged out or expired
	tokenReused    rotateResult = -1 // presented JTI is not the current one
)

// rotateScript swaps the session's JTI only if the presented one is still the
// current one, in a single step. A GET-then-SET would let two concurrent
// refreshes with the same token both succeed, which defeats reuse detection.
// On a mismatch it deletes the session: an old token was replayed.
var rotateScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current then return 0 end
if current ~= ARGV[1] then
  redis.call('DEL', KEYS[1])
  return -1
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
return 1
`)

// rotateSession replaces sid's current JTI oldJTI with newJTI. An error means
// a real Redis fault, so callers can tell "session gone" (reject) from "cache
// unavailable" (retry).
func rotateSession(ctx context.Context, rdb *redis.Client, sid, oldJTI, newJTI string, ttl time.Duration) (rotateResult, error) {
	n, err := rotateScript.Run(ctx, rdb, []string{sessionKey(sid)}, oldJTI, newJTI, ttl.Milliseconds()).Int()
	if err != nil {
		return 0, err
	}
	return rotateResult(n), nil
}

// deleteSession ends sid: on logout, and when a replayed old refresh token
// shows the session has been stolen.
func deleteSession(ctx context.Context, rdb *redis.Client, sid string) error {
	return rdb.Del(ctx, sessionKey(sid)).Err()
}

func revokeAccess(ctx context.Context, rdb *redis.Client, jti string, ttl time.Duration) error {
	if ttl <= 0 || jti == "" {
		return nil
	}
	return rdb.Set(ctx, revokedKeyPrefix+jti, "1", ttl).Err()
}

// isRevoked fails open on a Redis error: the access token is short-lived, and
// failing closed would sign every user out during a cache blip.
func isRevoked(ctx context.Context, rdb *redis.Client, jti string) bool {
	v, err := rdb.Get(ctx, revokedKeyPrefix+jti).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		slog.Warn("revocation check failed open", "err", err)
	}
	return v == "1"
}
