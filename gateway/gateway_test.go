package gateway

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const testSecret = "test-secret-at-least-32-characters-long"

// upstream records what git2 core would receive.
type upstream struct {
	mu   sync.Mutex
	got  []*http.Request
	srv  *httptest.Server
	wait time.Duration
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	u := &upstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.got = append(u.got, r.Clone(r.Context()))
		wait := u.wait
		u.mu.Unlock()
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *upstream) calls() []*http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]*http.Request(nil), u.got...)
}

type fixture struct {
	h   http.Handler
	jwt *authjwt.Manager
	mr  *miniredis.Miniredis
	up  *upstream
}

func newFixture(t *testing.T, opts ...func(*Deps)) *fixture {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1, DialTimeout: 200 * time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })
	up := newUpstream(t)
	core, _ := url.Parse(up.srv.URL)
	jwt := authjwt.NewManager(testSecret, 15*time.Minute, time.Hour)
	d := Deps{JWT: jwt, Redis: rdb, Core: core, WebDir: webDir(t)}
	for _, o := range opts {
		o(&d)
	}
	h, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{h: h, jwt: jwt, mr: mr, up: up}
}

// webDir builds a fake web install: dash/ and admin/ with an index and an asset.
func webDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for app, body := range map[string]string{"dash": "DASH", "admin": "ADMIN"} {
		must(t, os.MkdirAll(filepath.Join(dir, app, "assets"), 0o755))
		must(t, os.WriteFile(filepath.Join(dir, app, "index.html"), []byte(body+" INDEX"), 0o644))
		must(t, os.WriteFile(filepath.Join(dir, app, "assets", "app.js"), []byte(body+" JS"), 0o644))
	}
	// A file outside both apps that a traversal would try to reach.
	must(t, os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("SECRET"), 0o644))
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) do(t *testing.T, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "203.0.113.7:51234"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func (f *fixture) token(t *testing.T) (string, uuid.UUID) {
	t.Helper()
	uid := uuid.New()
	tok, err := f.jwt.IssueAccess(uid)
	must(t, err)
	return tok, uid
}

func TestAPIWithoutValidTokenNeverReachesCore(t *testing.T) {
	f := newFixture(t)
	other := authjwt.NewManager("another-secret-that-is-32-characters-x", time.Minute, time.Hour)
	forged, _ := other.IssueAccess(uuid.New())
	expired := authjwt.NewManager(testSecret, -time.Minute, time.Hour)
	old, _ := expired.IssueAccess(uuid.New())

	cases := map[string]string{
		"no header":     "",
		"not bearer":    "Basic dXNlcjpwYXNz",
		"garbage":       "Bearer not-a-jwt",
		"wrong secret":  "Bearer " + forged,
		"expired token": "Bearer " + old,
	}
	for name, auth := range cases {
		t.Run(name, func(t *testing.T) {
			hdr := map[string]string{}
			if auth != "" {
				hdr["Authorization"] = auth
			}
			rec := f.do(t, http.MethodGet, "/api/repos", hdr)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401", rec.Code)
			}
		})
	}
	if n := len(f.up.calls()); n != 0 {
		t.Fatalf("core received %d requests, want 0", n)
	}
}

func TestRevokedTokenIsRefused(t *testing.T) {
	f := newFixture(t)
	tok, _ := f.token(t)
	claims, err := f.jwt.Parse(tok, authjwt.PurposeAccess)
	must(t, err)
	must(t, f.mr.Set("revoked:"+claims.ID, "1"))

	rec := f.do(t, http.MethodGet, "/api/repos", map[string]string{"Authorization": "Bearer " + tok})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}

func TestValidTokenReachesCoreWithGatewayHeaders(t *testing.T) {
	f := newFixture(t)
	tok, _ := f.token(t)

	rec := f.do(t, http.MethodGet, "/api/repos?page_size=5", map[string]string{
		"Authorization":     "Bearer " + tok,
		"X-Git2-User-Id":    uuid.NewString(), // spoof attempt
		"X-Git2-Anything":   "x",
		"X-Real-IP":         "10.9.9.9",
		"X-Forwarded-For":   "10.8.8.8",
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Port":  "1",
		"X-Request-Id":      "client-id-1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}
	calls := f.up.calls()
	if len(calls) != 1 {
		t.Fatalf("core received %d requests, want 1", len(calls))
	}
	got := calls[0]
	checks := map[string]string{
		"Authorization":     "Bearer " + tok,
		"X-Git2-User-Id":    "", // never on /api (contract v0.1)
		"X-Git2-Anything":   "",
		"X-Real-IP":         "203.0.113.7",
		"X-Forwarded-For":   "203.0.113.7",
		"X-Forwarded-Proto": "http",
		"X-Forwarded-Port":  "",
		"X-Request-Id":      "client-id-1",
	}
	for k, want := range checks {
		if v := got.Header.Get(k); v != want {
			t.Errorf("upstream %s = %q, want %q", k, v, want)
		}
	}
	if got.URL.Path != "/api/repos" || got.URL.RawQuery != "page_size=5" {
		t.Errorf("upstream URL %s, want /api/repos?page_size=5", got.URL)
	}
	if v := rec.Header().Get("X-Request-Id"); v != "client-id-1" {
		t.Errorf("response X-Request-Id %q", v)
	}
}

// A client may list header names in Connection to have them removed as
// hop-by-hop. With Rewrite that happens before our headers are set.
func TestConnectionHeaderCannotDropGatewayHeaders(t *testing.T) {
	f := newFixture(t)
	tok, _ := f.token(t)
	f.do(t, http.MethodGet, "/api/repos", map[string]string{
		"Authorization": "Bearer " + tok,
		"Connection":    "X-Real-IP, X-Request-Id, X-Forwarded-For",
	})
	got := f.up.calls()[0]
	for _, k := range []string{"X-Real-IP", "X-Request-Id", "X-Forwarded-For"} {
		if got.Header.Get(k) == "" {
			t.Errorf("upstream lost %s", k)
		}
	}
}

func TestPublicAPIRoutesSkipTheTokenCheck(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{"/api/auth/login", "/api/auth/refresh", "/api/setup/status"} {
		if rec := f.do(t, http.MethodPost, p, nil); rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200 from core", p, rec.Code)
		}
	}
	// Only the exact prefixes are public.
	if rec := f.do(t, http.MethodGet, "/api/authx", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("/api/authx: status %d, want 401", rec.Code)
	}
}

func TestUncleanPathsAreRefused(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{
		"/api/auth/../repos",
		"/api/auth/./../repos",
		"/api//repos",
		"/api/auth/%2e%2e/repos",
		"/api/auth/x%2F..%2Frepos",
		"/../secret.txt",
	} {
		req := httptest.NewRequest(http.MethodGet, "http://gw"+p, nil)
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "SECRET") {
			t.Errorf("%s: served a file outside the web apps", p)
		}
	}
	if n := len(f.up.calls()); n != 0 {
		t.Fatalf("core received %d requests, want 0", n)
	}
}

// auth-svc fails open when Redis cannot answer (git2#14); the gateway runs
// the same check, so it does too. Signature and expiry still count.
func TestRedisDownFailsOpenOnlyForValidTokens(t *testing.T) {
	f := newFixture(t)
	tok, _ := f.token(t)
	f.mr.Close()

	if rec := f.do(t, http.MethodGet, "/api/repos", map[string]string{"Authorization": "Bearer " + tok}); rec.Code != http.StatusOK {
		t.Fatalf("valid token with Redis down: status %d, want 200", rec.Code)
	}
	if rec := f.do(t, http.MethodGet, "/api/repos", map[string]string{"Authorization": "Bearer junk"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("junk token with Redis down: status %d, want 401", rec.Code)
	}
}

func TestUpstreamFailures(t *testing.T) {
	t.Run("unreachable is 503", func(t *testing.T) {
		f := newFixture(t, func(d *Deps) {
			d.Core, _ = url.Parse("http://127.0.0.1:1") // nothing listens
		})
		rec := f.do(t, http.MethodPost, "/api/auth/login", nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d, want 503", rec.Code)
		}
	})
	t.Run("too slow is 504", func(t *testing.T) {
		f := newFixture(t, func(d *Deps) { d.APITimeout = 50 * time.Millisecond })
		f.up.wait = time.Second
		rec := f.do(t, http.MethodPost, "/api/auth/login", nil)
		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("status %d, want 504", rec.Code)
		}
	})
}

func TestNoSniffOnEveryResponseOnce(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{"/api/auth/login", "/", "/admin/", "/api/repos", "/healthz"} {
		rec := f.do(t, http.MethodGet, p, nil)
		if v := rec.Header().Values("X-Content-Type-Options"); len(v) != 1 || v[0] != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, want one nosniff", p, v)
		}
	}
	// Other upstream response headers pass through.
	if rec := f.do(t, http.MethodGet, "/api/auth/login", nil); rec.Header().Get("Cache-Control") != "private" {
		t.Errorf("upstream Cache-Control lost")
	}
}

func TestWebApps(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		path, want string
		code       int
	}{
		{"/", "DASH INDEX", 200},
		{"/alice/repo", "DASH INDEX", 200},
		{"/assets/app.js", "DASH JS", 200},
		{"/assets/missing.js", "", 404},
		{"/admin", "ADMIN INDEX", 200},
		{"/admin/", "ADMIN INDEX", 200},
		{"/admin/users/42", "ADMIN INDEX", 200},
		{"/admin/assets/app.js", "ADMIN JS", 200},
		{"/admin/assets/missing.js", "", 404},
		{"/api", "", 404},
	}
	for _, c := range cases {
		rec := f.do(t, http.MethodGet, c.path, nil)
		if rec.Code != c.code {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.code)
			continue
		}
		if c.want != "" && rec.Body.String() != c.want {
			t.Errorf("%s: body %q, want %q", c.path, rec.Body, c.want)
		}
	}
	if rec := f.do(t, http.MethodPost, "/", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /: status %d, want 405", rec.Code)
	}
	if len(f.up.calls()) != 0 {
		t.Errorf("web app requests reached core")
	}
}

func TestRequestID(t *testing.T) {
	f := newFixture(t)
	for in, keep := range map[string]bool{
		"abc-123_X.y":            true,
		"":                       false,
		"has space":              false,
		"<script>":               false,
		strings.Repeat("a", 129): false,
	} {
		rec := f.do(t, http.MethodGet, "/healthz", map[string]string{"X-Request-Id": in})
		got := rec.Header().Get("X-Request-Id")
		if keep && got != in {
			t.Errorf("%q: replaced with %q", in, got)
		}
		if !keep && (got == in || got == "") {
			t.Errorf("%q: kept or empty (%q)", in, got)
		}
	}
}
