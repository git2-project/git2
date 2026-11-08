package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/google/uuid"
)

// Git over HTTPS (docs/api/gateway.md, "Git over HTTPS"): the gateway signs
// the user in with HTTP Basic auth, turns /<owner>/<repo>.git into the
// repository ID and streams the request to git-svc's Smart HTTP port. git-svc
// does not authenticate (git2-codeplane ADR 0012), so this is the only way in.

// headerUserID is the signed-in user, set only by the gateway and only on Git
// routes. stripClientHeaders removes any value a client sent.
const headerUserID = "X-Git2-User-Id"

// gitChallenge is sent with every 401 so the Git client asks for credentials.
const gitChallenge = `Basic realm="git2"`

// gitServices are the Smart HTTP paths after /<repo>.git/ (gitprotocol-http).
// Anything else under a .git path is a 404: dumb HTTP and file reads are
// not served.
var gitServices = map[string]bool{
	"info/refs":        true,
	"git-upload-pack":  true,
	"git-receive-pack": true,
}

// errRepoNotFound means the owner has no repository with that name.
var errRepoNotFound = errors.New("repository not found")

// Credentials checks a Git client's username and password.
// *authsvc.Credentials is the real one.
type Credentials interface {
	Check(ctx context.Context, username, password string) (authsvc.User, error)
}

// Repos finds a repository's ID from its owner and name. gitSvcRepos asks
// git-svc; it returns errRepoNotFound for an unknown repository.
type Repos interface {
	Find(ctx context.Context, ownerID uuid.UUID, name string) (uuid.UUID, error)
}

// GitDeps is what the Git route needs.
type GitDeps struct {
	Creds   Credentials
	Repos   Repos
	Limiter *SignInLimiter
	GitSvc  *url.URL // git-svc's Smart HTTP port
}

func (g *GitDeps) validate() error {
	if g.Creds == nil || g.Repos == nil || g.Limiter == nil || g.GitSvc == nil {
		return errors.New("gateway: Git needs Creds, Repos, Limiter and GitSvc")
	}
	return nil
}

// gitPath is a request path split as /<owner>/<repo>.git/<service>.
type gitPath struct {
	owner, repo, service string
}

// parseGitPath reports whether p is a Git route: its second segment ends in
// ".git" after a non-empty name. ok is false for every other path, which
// then goes to the API or the web apps. service may be outside gitServices;
// the caller refuses it.
func parseGitPath(p string) (gp gitPath, ok bool) {
	parts := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 3)
	if len(parts) != 3 || parts[0] == "" {
		return gitPath{}, false
	}
	repo, isGit := strings.CutSuffix(parts[1], ".git")
	if !isGit || repo == "" {
		return gitPath{}, false
	}
	return gitPath{owner: parts[0], repo: repo, service: parts[2]}, true
}

// withGitRoute sends Git paths to git and everything else on. It runs after
// withCleanPath, so the path it splits is the one git-svc will see.
func withGitRoute(git http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := parseGitPath(r.URL.Path); ok {
				git.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type gitUserKey struct{}

// gitHandler signs the caller in, checks the v0 access rule and forwards to
// git-svc by repository ID.
type gitHandler struct {
	deps  GitDeps
	proxy *httputil.ReverseProxy
}

func newGitHandler(d GitDeps) *gitHandler {
	return &gitHandler{deps: d, proxy: newGitProxy(d.GitSvc)}
}

func (h *gitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	gp, _ := parseGitPath(r.URL.Path)
	if !gitServices[gp.service] {
		gitError(w, http.StatusNotFound, "not found")
		return
	}
	user, ok := h.signIn(w, r)
	if !ok {
		return
	}
	// v0 access rule: only the owner uses a repository. Another owner looks
	// the same as a missing repository, so names do not leak.
	if !strings.EqualFold(gp.owner, user.Username) {
		gitError(w, http.StatusNotFound, "repository not found")
		return
	}
	id, err := h.deps.Repos.Find(r.Context(), user.ID, gp.repo)
	switch {
	case errors.Is(err, errRepoNotFound):
		gitError(w, http.StatusNotFound, "repository not found")
		return
	case err != nil:
		slog.Error("gateway: find repository", "request_id", requestID(r.Context()), "err", err)
		gitError(w, http.StatusServiceUnavailable, "service unavailable, try again")
		return
	}

	out := r.Clone(context.WithValue(r.Context(), gitUserKey{}, user.ID))
	out.URL.Path = "/" + id.String() + ".git/" + gp.service
	out.URL.RawPath = ""
	h.proxy.ServeHTTP(w, out)
}

// signIn checks HTTP Basic credentials. It writes the answer itself and
// returns false when the request must stop: 401 for missing or wrong
// credentials, 429 when too many sign-ins failed, 503 when the user store
// cannot answer (never 401, so the client keeps its stored credentials).
func (h *gitHandler) signIn(w http.ResponseWriter, r *http.Request) (authsvc.User, bool) {
	username, pass, ok := r.BasicAuth()
	if !ok || username == "" {
		challenge(w)
		return authsvc.User{}, false
	}
	ip := clientIP(r)
	if retry, blocked := h.deps.Limiter.Blocked(r.Context(), ip, username); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
		gitError(w, http.StatusTooManyRequests, "too many failed sign-ins, try again later")
		return authsvc.User{}, false
	}
	user, err := h.deps.Creds.Check(r.Context(), username, pass)
	switch {
	case errors.Is(err, authsvc.ErrBadCredentials):
		h.deps.Limiter.Failed(r.Context(), ip, username)
		challenge(w)
		return authsvc.User{}, false
	case err != nil:
		slog.Error("gateway: git sign-in", "request_id", requestID(r.Context()), "err", err)
		gitError(w, http.StatusServiceUnavailable, "service unavailable, try again")
		return authsvc.User{}, false
	}
	return user, true
}

func challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", gitChallenge)
	gitError(w, http.StatusUnauthorized, "authentication required")
}

// gitError answers in plain text: Git clients print the body to the user.
func gitError(w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

// newGitProxy is newProxy for git-svc, with three differences: the user ID
// is added, the client's Authorization (its password) is not passed on, and
// every write is flushed at once so a clone streams instead of buffering.
// There is no per-request timeout: git-svc bounds each operation (2 h) and
// each stalled read or write (5 min).
func newGitProxy(target *url.URL) *httputil.ReverseProxy {
	p := newProxy(target)
	base := p.Rewrite
	p.Rewrite = func(pr *httputil.ProxyRequest) {
		base(pr)
		pr.Out.Header.Del("Authorization")
		if id, ok := pr.In.Context().Value(gitUserKey{}).(uuid.UUID); ok {
			pr.Out.Header.Set(headerUserID, id.String())
		}
	}
	p.FlushInterval = -1
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(r.Context().Err(), context.Canceled) {
			return
		}
		slog.Warn("gateway: git-svc error", "path", r.URL.Path,
			"request_id", requestID(r.Context()), "err", err)
		gitError(w, http.StatusServiceUnavailable, "service unavailable, try again")
	}
	return p
}
