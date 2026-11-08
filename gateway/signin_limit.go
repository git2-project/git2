package gateway

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Failed Git sign-ins are limited per client IP and per username (gateway
// contract, "Limits"). The numbers follow GitLab's Git HTTP defaults (30
// failures, then a ban) with a fixed window instead of a ban:
//   - per IP: 30 failures in 10 minutes stops a single guesser;
//   - per username: 100 in 10 minutes bounds guesses spread over many IPs,
//     high enough that a stranger cannot lock the owner out by mistake.
//
// Only failures count, so a busy CI host signing in correctly is never held.
const (
	signInWindow     = 10 * time.Minute
	signInFailsPerIP = 30
	signInFailsUser  = 100
	// maxUsernameKey bounds the username part of a Redis key. The username
	// comes from the client; auth-svc's are at most 39 characters, so a
	// longer one never signs in and its tail does not need its own counter.
	maxUsernameKey = 64
)

// SignInLimiter counts failed sign-ins in Redis. Like the other limits it
// fails open: when Redis cannot answer, sign-in still works and the
// password check stays the boundary.
type SignInLimiter struct {
	rdb       *redis.Client
	window    time.Duration
	perIP     int64
	perUser   int64
	keyPrefix string
}

func NewSignInLimiter(rdb *redis.Client) *SignInLimiter {
	return &SignInLimiter{rdb: rdb, window: signInWindow, perIP: signInFailsPerIP,
		perUser: signInFailsUser, keyPrefix: "rl:git-signin-fail:"}
}

func (l *SignInLimiter) keys(ip, username string) (ipKey, userKey string) {
	if len(username) > maxUsernameKey {
		username = username[:maxUsernameKey]
	}
	return l.keyPrefix + "ip:" + ip, l.keyPrefix + "user:" + strings.ToLower(username)
}

// Blocked reports whether ip or username has used up its failures, and when
// to try again.
func (l *SignInLimiter) Blocked(ctx context.Context, ip, username string) (time.Duration, bool) {
	ipKey, userKey := l.keys(ip, username)
	counts, err := l.rdb.MGet(ctx, ipKey, userKey).Result()
	if err != nil {
		slog.Warn("gateway: sign-in limit: redis", "err", err)
		return 0, false
	}
	if atLeast(counts[0], l.perIP) || atLeast(counts[1], l.perUser) {
		return l.window, true
	}
	return 0, false
}

// Failed counts one failed sign-in for ip and username. The window starts at
// the first failure.
func (l *SignInLimiter) Failed(ctx context.Context, ip, username string) {
	ipKey, userKey := l.keys(ip, username)
	pipe := l.rdb.TxPipeline()
	for _, k := range []string{ipKey, userKey} {
		pipe.Incr(ctx, k)
		pipe.ExpireNX(ctx, k, l.window)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Warn("gateway: sign-in limit: redis", "err", err)
	}
}

// atLeast reads an MGET value: a decimal string, or nil for a missing key.
func atLeast(v any, limit int64) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n >= limit
}
