package gateway

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const gitPassword = "right-password"

// fakeCreds knows one active user, "deniz".
type fakeCreds struct {
	user  authsvc.User
	fault error
	calls atomic.Int32
}

func (c *fakeCreds) Check(_ context.Context, username, password string) (authsvc.User, error) {
	c.calls.Add(1)
	if c.fault != nil {
		return authsvc.User{}, c.fault
	}
	if strings.EqualFold(username, c.user.Username) && password == gitPassword {
		return c.user, nil
	}
	return authsvc.User{}, authsvc.ErrBadCredentials
}

// fakeRepos knows deniz's repository "api".
type fakeRepos struct {
	owner, id uuid.UUID
	fault     error
}

func (r *fakeRepos) Find(_ context.Context, ownerID uuid.UUID, name string) (uuid.UUID, error) {
	if r.fault != nil {
		return uuid.Nil, r.fault
	}
	if ownerID == r.owner && strings.EqualFold(name, "api") {
		return r.id, nil
	}
	return uuid.Nil, errRepoNotFound
}

type gitFixture struct {
	*fixture
	creds  *fakeCreds
	repos  *fakeRepos
	gitSvc *upstream
}

func newGitFixture(t *testing.T) *gitFixture {
	t.Helper()
	user := authsvc.User{ID: uuid.New(), Username: "deniz", IsActive: true}
	g := &gitFixture{
		creds:  &fakeCreds{user: user},
		repos:  &fakeRepos{owner: user.ID, id: uuid.New()},
		gitSvc: newUpstream(t),
	}
	svc, _ := url.Parse(g.gitSvc.srv.URL)
	g.fixture = newFixture(t, func(d *Deps) {
		d.Git = &GitDeps{Creds: g.creds, Repos: g.repos, Limiter: NewSignInLimiter(d.Redis), GitSvc: svc}
	})
	return g
}

func basic(user, pass string) map[string]string {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth(user, pass)
	return map[string]string{"Authorization": req.Header.Get("Authorization")}
}

const refsPath = "/deniz/api.git/info/refs?service=git-upload-pack"

func TestGitSignInIsRequired(t *testing.T) {
	g := newGitFixture(t)
	cases := map[string]map[string]string{
		"no credentials": nil,
		"wrong password": basic("deniz", "wrong"),
		"unknown user":   basic("nobody", gitPassword),
		"bearer token":   {"Authorization": "Bearer something"},
	}
	for name, hdr := range cases {
		t.Run(name, func(t *testing.T) {
			rec := g.do(t, http.MethodGet, refsPath, hdr)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != gitChallenge {
				t.Fatalf("WWW-Authenticate = %q, want %q so git asks for credentials", got, gitChallenge)
			}
			if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
				t.Fatalf("Content-Type = %q, want plain text for the git client", rec.Header().Get("Content-Type"))
			}
		})
	}
	if n := len(g.gitSvc.calls()); n != 0 {
		t.Fatalf("git-svc got %d requests without a signed-in user", n)
	}
}

func TestGitForwardsByIDWithTheUserAndWithoutThePassword(t *testing.T) {
	g := newGitFixture(t)
	hdr := basic("Deniz", gitPassword)
	hdr[headerUserID] = uuid.NewString() // a client claiming another user
	hdr["X-Real-IP"] = "10.0.0.1"

	rec := g.do(t, http.MethodGet, "/Deniz/API.git/info/refs?service=git-upload-pack", hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}
	calls := g.gitSvc.calls()
	if len(calls) != 1 {
		t.Fatalf("git-svc got %d requests, want 1", len(calls))
	}
	got := calls[0]
	if want := "/" + g.repos.id.String() + ".git/info/refs"; got.URL.Path != want {
		t.Errorf("path = %q, want %q", got.URL.Path, want)
	}
	if got.URL.RawQuery != "service=git-upload-pack" {
		t.Errorf("query = %q, want the service kept", got.URL.RawQuery)
	}
	if got.Header.Get(headerUserID) != g.creds.user.ID.String() {
		t.Errorf("%s = %q, want the signed-in user %s", headerUserID, got.Header.Get(headerUserID), g.creds.user.ID)
	}
	if got.Header.Get("Authorization") != "" {
		t.Error("the client's password reached git-svc")
	}
	if got.Header.Get("X-Real-IP") != "203.0.113.7" {
		t.Errorf("X-Real-IP = %q, want the connection's address", got.Header.Get("X-Real-IP"))
	}
	if len(g.up.calls()) != 0 {
		t.Error("a Git request reached git2 core")
	}
}

func TestGitOnlyTheOwnerReachesARepository(t *testing.T) {
	g := newGitFixture(t)
	for _, p := range []string{
		"/murat/api.git/info/refs?service=git-upload-pack", // someone else's
		"/deniz/missing.git/info/refs?service=git-upload-pack",
	} {
		if rec := g.do(t, http.MethodGet, p, basic("deniz", gitPassword)); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, rec.Code)
		}
	}
	if n := len(g.gitSvc.calls()); n != 0 {
		t.Fatalf("git-svc got %d requests for repositories the user may not use", n)
	}
}

func TestGitServesOnlySmartHTTPPaths(t *testing.T) {
	g := newGitFixture(t)
	for _, p := range []string{"/deniz/api.git/config", "/deniz/api.git/objects/info/packs", "/deniz/api.git/HEAD"} {
		if rec := g.do(t, http.MethodGet, p, basic("deniz", gitPassword)); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, rec.Code)
		}
	}
	if n := g.creds.calls.Load(); n != 0 {
		t.Errorf("credentials checked %d times for paths that are never served", n)
	}
}

func TestGitPathsDoNotCatchOtherRoutes(t *testing.T) {
	g := newGitFixture(t)
	// A Dash page and an API path whose second segment is not "<name>.git".
	if rec := g.do(t, http.MethodGet, "/deniz/api", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "DASH INDEX") {
		t.Errorf("/deniz/api: status %d body %q, want the Dash app", rec.Code, rec.Body)
	}
	if rec := g.do(t, http.MethodGet, "/deniz/.git/info/refs", nil); strings.Contains(rec.Header().Get("WWW-Authenticate"), "Basic") {
		t.Error("/deniz/.git (empty repository name) was treated as a Git route")
	}
}

func TestGitFaultsAre503NotSignOut(t *testing.T) {
	t.Run("user store down", func(t *testing.T) {
		g := newGitFixture(t)
		g.creds.fault = errors.New("connection refused")
		if rec := g.do(t, http.MethodGet, refsPath, basic("deniz", gitPassword)); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d, want 503", rec.Code)
		}
	})
	t.Run("git-svc lookup down", func(t *testing.T) {
		g := newGitFixture(t)
		g.repos.fault = errors.New("unavailable")
		if rec := g.do(t, http.MethodGet, refsPath, basic("deniz", gitPassword)); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d, want 503", rec.Code)
		}
	})
	t.Run("git-svc Smart HTTP down", func(t *testing.T) {
		g := newGitFixture(t)
		g.gitSvc.srv.Close()
		if rec := g.do(t, http.MethodGet, refsPath, basic("deniz", gitPassword)); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d, want 503", rec.Code)
		}
	})
}

func TestGitFailedSignInsAreLimited(t *testing.T) {
	g := newGitFixture(t)
	for i := 0; i < signInFailsPerIP; i++ {
		if rec := g.do(t, http.MethodGet, refsPath, basic("deniz", "wrong")); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: status %d, want 401", i+1, rec.Code)
		}
	}
	checks := g.creds.calls.Load()
	rec := g.do(t, http.MethodGet, refsPath, basic("deniz", gitPassword))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("after %d failures: status %d Retry-After %q, want 429 with Retry-After",
			signInFailsPerIP, rec.Code, rec.Header().Get("Retry-After"))
	}
	if g.creds.calls.Load() != checks {
		t.Error("the password was still checked while blocked; a blocked guesser must not cost bcrypt time")
	}

	// A correct sign-in from the same IP does not count, and requests
	// without credentials (git's first try) never do.
	g2 := newGitFixture(t)
	for i := 0; i < signInFailsPerIP+5; i++ {
		g2.do(t, http.MethodGet, refsPath, nil)
		g2.do(t, http.MethodGet, refsPath, basic("deniz", gitPassword))
	}
	if rec := g2.do(t, http.MethodGet, refsPath, basic("deniz", gitPassword)); rec.Code != http.StatusOK {
		t.Fatalf("successful sign-ins were limited: status %d", rec.Code)
	}
}

func TestGitSignInLimitFailsOpenWhenRedisIsDown(t *testing.T) {
	g := newGitFixture(t)
	g.mr.Close()
	if rec := g.do(t, http.MethodGet, refsPath, basic("deniz", gitPassword)); rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: the password check stays the boundary", rec.Code)
	}
	if rec := g.do(t, http.MethodGet, refsPath, basic("deniz", "wrong")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password with Redis down: status %d, want 401", rec.Code)
	}
}

// A clone must stream: the client sees git-svc's first bytes before git-svc
// has finished, not after the whole pack is buffered in the gateway.
func TestGitResponsesStream(t *testing.T) {
	release := make(chan struct{})
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A known length: Go's proxy streams chunked responses on its own,
		// but buffers these unless told to flush every write.
		w.Header().Set("Content-Length", "11")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "first\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "rest\n")
	}))
	defer svc.Close()
	defer close(release)

	g := newGitFixture(t)
	target, _ := url.Parse(svc.URL)
	rdb := redis.NewClient(&redis.Options{Addr: g.mr.Addr()})
	defer rdb.Close()
	h, err := New(Deps{JWT: g.jwt, Redis: rdb, Core: target, WebDir: webDir(t),
		Git: &GitDeps{Creds: g.creds, Repos: g.repos, Limiter: NewSignInLimiter(rdb), GitSvc: target}})
	must(t, err)
	gw := httptest.NewServer(h)
	defer gw.Close()

	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/deniz/api.git/git-upload-pack", strings.NewReader("0000"))
	req.SetBasicAuth("deniz", gitPassword)
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	defer resp.Body.Close()

	line := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(resp.Body).ReadString('\n')
		line <- s
	}()
	select {
	case got := <-line:
		if got != "first\n" {
			t.Fatalf("first line = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first bytes did not arrive while git-svc was still sending: the gateway buffers")
	}
}

func TestReadyzChecksEveryUpstream(t *testing.T) {
	g := newGitFixture(t)
	if rec := g.do(t, http.MethodGet, "/readyz", nil); rec.Code != http.StatusOK {
		t.Fatalf("all up: status %d, want 200", rec.Code)
	}
	g.gitSvc.srv.Close()
	rec := g.do(t, http.MethodGet, "/readyz", nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "git-svc") {
		t.Fatalf("git-svc down: status %d body %s, want 503 naming git-svc", rec.Code, rec.Body)
	}
	if rec := g.do(t, http.MethodGet, "/healthz", nil); rec.Code != http.StatusOK {
		t.Fatalf("/healthz: status %d, want 200 while an upstream is down", rec.Code)
	}
}

// The username is the client's; a huge one must not become a huge Redis key.
func TestSignInLimiterBoundsUsernameKeys(t *testing.T) {
	_, userKey := NewSignInLimiter(nil).keys("203.0.113.7", strings.Repeat("a", 1<<16))
	if len(userKey) > len("rl:git-signin-fail:user:")+maxUsernameKey {
		t.Fatalf("user key is %d bytes", len(userKey))
	}
}

// An upstream that answers but is not healthy (git-svc's /readyz-style 503)
// counts as not ready, like one that does not answer.
func TestReadyzNeedsA200(t *testing.T) {
	sick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer sick.Close()
	u, _ := url.Parse(sick.URL)
	rec := httptest.NewRecorder()
	readyz(map[string]*url.URL{"git2": u}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
}
