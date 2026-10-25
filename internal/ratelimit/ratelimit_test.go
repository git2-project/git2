package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func probe() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func call(h http.Handler, remote string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestByIPBlocksOverLimitPerIP(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	h := ByIP(rdb, "login", 2, time.Minute)(probe())

	for i := 0; i < 2; i++ {
		if got := call(h, "203.0.113.9:1000"); got != http.StatusNoContent {
			t.Fatalf("call %d: status %d, want 204", i, got)
		}
	}
	if got := call(h, "203.0.113.9:2000"); got != http.StatusTooManyRequests {
		t.Errorf("third call: status %d, want 429", got)
	}
	if got := call(h, "198.51.100.1:1000"); got != http.StatusNoContent {
		t.Errorf("other IP: status %d, want 204", got)
	}

	mr.FastForward(time.Minute)
	if got := call(h, "203.0.113.9:1000"); got != http.StatusNoContent {
		t.Errorf("after window: status %d, want 204", got)
	}
}

func TestByIPFailsOpen(t *testing.T) {
	down := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = down.Close() })

	for name, rdb := range map[string]*redis.Client{"nil client": nil, "redis down": down} {
		t.Run(name, func(t *testing.T) {
			h := ByIP(rdb, "login", 1, time.Minute)(probe())
			for i := 0; i < 3; i++ {
				if got := call(h, "203.0.113.9:1000"); got != http.StatusNoContent {
					t.Fatalf("call %d: status %d, want 204", i, got)
				}
			}
		})
	}
}
