package authsvc

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Refresh-token sessions live in Redis as a per-session whitelist: one key
// holds the single currently-valid refresh JTI for a session (one sign-in on
// one device). Rotation overwrites the key in place, so the keyspace is bounded
// by the number of live sessions rather than by rotations × token lifetime.
const sessionKeyPrefix = "sess:"

// userSessionsKeyPrefix holds, per user, the set of their session ids, so
// disabling a user can end every session at once. It lives as long as the
// user's newest session; a stale id in it only points at a key that is gone.
const userSessionsKeyPrefix = "user-sess:"

// revokedKeyPrefix marks access-token JTIs revoked at logout. Each entry lives
// only as long as the token it revokes would have.
const revokedKeyPrefix = "revoked:"

// disabledKeyPrefix holds, per disabled user, the moment of disabling in Unix
// milliseconds (database clock). Access tokens issued before it plus
// DisableGrace are rejected. It lives as long as such a token can.
const disabledKeyPrefix = "disabled:"

func sessionKey(sid string) string            { return sessionKeyPrefix + sid }
func userSessionsKey(userID uuid.UUID) string { return userSessionsKeyPrefix + userID.String() }
func disabledKey(userID uuid.UUID) string     { return disabledKeyPrefix + userID.String() }

// storeSession records a new session sid for userID with its first refresh
// JTI. Called at login.
func storeSession(ctx context.Context, rdb *redis.Client, userID uuid.UUID, sid, jti string, ttl time.Duration) error {
	_, err := rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, sessionKey(sid), jti, ttl)
		p.SAdd(ctx, userSessionsKey(userID), sid)
		p.PExpire(ctx, userSessionsKey(userID), ttl)
		return nil
	})
	return err
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
//
//	KEYS[1] sess:<sid>   KEYS[2] user-sess:<uid>
//	ARGV[1] old JTI      ARGV[2] new JTI   ARGV[3] TTL in ms   ARGV[4] sid
var rotateScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current then return 0 end
if current ~= ARGV[1] then
  redis.call('DEL', KEYS[1])
  redis.call('SREM', KEYS[2], ARGV[4])
  return -1
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
redis.call('SADD', KEYS[2], ARGV[4])
redis.call('PEXPIRE', KEYS[2], ARGV[3])
return 1
`)

// rotateSession replaces sid's current JTI oldJTI with newJTI. An error means
// a real Redis fault, so callers can tell "session gone" (reject) from "cache
// unavailable" (retry).
func rotateSession(ctx context.Context, rdb *redis.Client, userID uuid.UUID, sid, oldJTI, newJTI string, ttl time.Duration) (rotateResult, error) {
	keys := []string{sessionKey(sid), userSessionsKey(userID)}
	n, err := rotateScript.Run(ctx, rdb, keys, oldJTI, newJTI, ttl.Milliseconds(), sid).Int()
	if err != nil {
		return 0, err
	}
	return rotateResult(n), nil
}

// deleteSession ends sid: on logout.
func deleteSession(ctx context.Context, rdb *redis.Client, userID uuid.UUID, sid string) error {
	_, err := rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Del(ctx, sessionKey(sid))
		p.SRem(ctx, userSessionsKey(userID), sid)
		return nil
	})
	return err
}

// endUserScript ends every session of a user and marks them disabled, in one
// step, so no session id can be added between reading the set and deleting it.
//
//	KEYS[1] user-sess:<uid>   KEYS[2] disabled:<uid>
//	ARGV[1] session key prefix   ARGV[2] disabled-at (Unix ms)   ARGV[3] marker TTL in ms
var endUserScript = redis.NewScript(`
for _, sid in ipairs(redis.call('SMEMBERS', KEYS[1])) do
  redis.call('DEL', ARGV[1] .. sid)
end
redis.call('DEL', KEYS[1])
redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3])
return 1
`)

func endUserSessions(ctx context.Context, rdb *redis.Client, userID uuid.UUID, at time.Time, markerTTL time.Duration) error {
	keys := []string{userSessionsKey(userID), disabledKey(userID)}
	return endUserScript.Run(ctx, rdb, keys, sessionKeyPrefix, at.UnixMilli(), markerTTL.Milliseconds()).Err()
}

func revokeAccess(ctx context.Context, rdb *redis.Client, jti string, ttl time.Duration) error {
	if ttl <= 0 || jti == "" {
		return nil
	}
	return rdb.Set(ctx, revokedKeyPrefix+jti, "1", ttl).Err()
}

type rejection int

const (
	accepted rejection = iota
	tokenRevoked
	userDisabled
)

// checkAccess tells whether an otherwise valid access token was revoked at
// logout or belongs to a user disabled after it was issued, in one round
// trip. It fails open on a Redis error, like the revocation check always has:
// the access token is short-lived, and failing closed would turn a cache blip
// into 503s on every authenticated route. Admin routes and refresh read
// is_active from the database, so they do not depend on this check.
func checkAccess(ctx context.Context, rdb *redis.Client, claims *authjwt.Claims) rejection {
	vals, err := rdb.MGet(ctx, revokedKeyPrefix+claims.ID, disabledKey(claims.UserID)).Result()
	if err != nil {
		slog.Warn("access check failed open", "err", err)
		return accepted
	}
	if v, _ := vals[0].(string); v == "1" {
		return tokenRevoked
	}
	if v, ok := vals[1].(string); ok {
		disabledAt, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			// A marker we cannot read still means "disabled".
			slog.Warn("unreadable disabled marker", "user_id", claims.UserID, "value", v)
			return userDisabled
		}
		if claims.IssuedAt == nil || claims.IssuedAt.UnixMilli() <= disabledAt+DisableGrace.Milliseconds() {
			return userDisabled
		}
	}
	return accepted
}
