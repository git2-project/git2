// Package ratelimit provides a Redis-backed fixed-window limiter keyed on the
// client IP, for the unauthenticated endpoints where there is no user to key on.
package ratelimit

import (
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/git2-project/git2/internal/httpx"
	"github.com/redis/go-redis/v9"
)

// ByIP allows limit requests per window per client IP into the named bucket.
// It fails open: a nil client disables the layer, and a Redis error lets the
// request through rather than locking every user out during a cache blip.
// The credential check behind it is the real brute-force boundary.
func ByIP(rdb *redis.Client, bucket string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rdb == nil {
				next.ServeHTTP(w, r)
				return
			}
			key := "rl:" + bucket + ":" + clientIP(r)
			pipe := rdb.TxPipeline()
			incr := pipe.Incr(r.Context(), key)
			pipe.ExpireNX(r.Context(), key, window)
			if _, err := pipe.Exec(r.Context()); err != nil {
				slog.Warn("rate limit: redis", "bucket", bucket, "err", err)
				next.ServeHTTP(w, r)
				return
			}
			if incr.Val() > int64(limit) {
				w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
				httpx.Error(w, http.StatusTooManyRequests, "too many requests, try again later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP strips the port from RemoteAddr. The gateway in front of the
// services is responsible for rewriting RemoteAddr from X-Forwarded-For.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
