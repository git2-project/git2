package realip

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/git2-project/git2/internal/ratelimit"
	"github.com/redis/go-redis/v9"
)

func seen(t *testing.T, trusted, remote, header string) string {
	t.Helper()
	prefixes, err := ParsePrefixes(trusted)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	h := FromTrusted(prefixes)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	if header != "" {
		req.Header.Set(Header, header)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestFromTrusted(t *testing.T) {
	cases := []struct {
		name, trusted, remote, header, want string
	}{
		{"gateway sends client IP", "172.30.0.0/24", "172.30.0.5:4000", "203.0.113.7", "203.0.113.7:0"},
		{"single trusted address", "172.30.0.5", "172.30.0.5:4000", "203.0.113.7", "203.0.113.7:0"},
		{"IPv6 client", "172.30.0.0/24", "172.30.0.5:4000", "2001:db8::1", "[2001:db8::1]:0"},
		{"spoof from an untrusted peer is ignored", "172.30.0.0/24", "198.51.100.9:4000", "203.0.113.7", "198.51.100.9:4000"},
		{"nothing trusted by default", "", "172.30.0.5:4000", "203.0.113.7", "172.30.0.5:4000"},
		{"trusted peer, no header", "172.30.0.0/24", "172.30.0.5:4000", "", "172.30.0.5:4000"},
		{"trusted peer, junk header", "172.30.0.0/24", "172.30.0.5:4000", "1.2.3.4, 5.6.7.8", "172.30.0.5:4000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := seen(t, c.trusted, c.remote, c.header); got != c.want {
				t.Fatalf("RemoteAddr = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParsePrefixes(t *testing.T) {
	if _, err := ParsePrefixes("10.0.0.0/8, 172.30.0.5 ,"); err != nil {
		t.Fatalf("valid list: %v", err)
	}
	if _, err := ParsePrefixes("10.0.0.0/8,gateway"); err == nil {
		t.Fatal("a host name must be refused: the list is addresses only")
	}
}

// What B-02 is about: behind the gateway, two clients must land in two
// rate-limit buckets, not in the gateway's one.
func TestRateLimitBucketsPerClientBehindGateway(t *testing.T) {
	prefixes, _ := ParsePrefixes("172.30.0.5")
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := FromTrusted(prefixes)(ratelimit.ByIP(newRedis(t), "t", 1, time.Minute)(ok))

	call := func(client string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "172.30.0.5:4000"
		req.Header.Set(Header, client)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := call("203.0.113.1"); c != http.StatusOK {
		t.Fatalf("client A first call: %d", c)
	}
	if c := call("203.0.113.2"); c != http.StatusOK {
		t.Fatalf("client B was limited by client A's bucket: %d", c)
	}
	if c := call("203.0.113.1"); c != http.StatusTooManyRequests {
		t.Fatalf("client A second call: %d, want 429", c)
	}
}

func newRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}
