// Package dashapi is git2's Dash plane API (contracts/dash-api):
// repositories and code browsing. Repository content lives in git-svc
// (git.v1, contracts/git), users and teams in auth-svc. Neither git-svc nor
// the gateway checks repository access, so dash-api checks it on every call
// (docs/design/repo-access-v0.md).
package dashapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/httpx"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// UserDirectory looks users up in auth-svc. *authsvc.PostgresStore is the
// real one; dash-api never reads auth_svc with SQL of its own.
type UserDirectory interface {
	// FindByUsername takes a lowercase username.
	FindByUsername(ctx context.Context, username string) (authsvc.User, error)
	FindByID(ctx context.Context, id uuid.UUID) (authsvc.User, error)
	// FindByEmail takes a lowercase email.
	FindByEmail(ctx context.Context, email string) (authsvc.User, error)
}

// TeamReader lists a user's teams in auth-svc, for team grants.
type TeamReader interface {
	UserTeams(ctx context.Context, userID uuid.UUID) ([]authsvc.UserTeam, error)
}

type Deps struct {
	Users UserDirectory
	Teams TeamReader

	// git-svc's clients (git.v1). BundleService is left out on purpose:
	// only backup-svc may call it.
	Repos   gitv1.RepositoryServiceClient
	Refs    gitv1.RefServiceClient
	Commits gitv1.CommitServiceClient

	// Access decides a user's level on a repository. Nil means the v0 rule,
	// where only the owner may use a repository.
	Access AccessChecker

	JWT   *authjwt.Manager
	Redis *redis.Client

	// ExternalURL is scheme://host[:port] without a trailing slash, for
	// clone URLs (config.Config.ExternalURL).
	ExternalURL string
}

type Handler struct {
	deps   Deps
	access AccessChecker
}

func NewHandler(deps Deps) (*Handler, error) {
	if deps.Users == nil || deps.Teams == nil || deps.Repos == nil || deps.Refs == nil ||
		deps.Commits == nil || deps.JWT == nil || deps.Redis == nil || deps.ExternalURL == "" {
		return nil, errors.New("dashapi: Users, Teams, Repos, Refs, Commits, JWT, Redis and ExternalURL are required")
	}
	deps.ExternalURL = strings.TrimSuffix(deps.ExternalURL, "/")
	access := deps.Access
	if access == nil {
		access = ownerOnly{}
	}
	return &Handler{deps: deps, access: access}, nil
}

// Mount registers the Dash routes on r, which is mounted at /api.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(authsvc.RequireAuth(h.deps.JWT, h.deps.Redis))

		// Every route under a repository resolves it and checks the caller's
		// level first (resolveRepo); each route then names the level it needs.
		r.Route("/repos/{owner}/{repo}", func(r chi.Router) {
			r.Use(h.resolveRepo)
			read := requireLevel(LevelRead)

			r.With(read).Get("/", h.getRepository)

			r.With(read).Get("/branches", h.listBranches)
			r.With(read).Get("/tags", h.listTags)
			r.With(read).Get("/tree", h.getTree)
			r.With(read).Get("/blob", h.getBlob)
			r.With(read).Get("/raw", h.getRaw)
			r.With(read).Get("/commits", h.listCommits)
			r.With(read).Get("/commits/{sha}", h.getCommit)
		})
	})
}

const (
	defaultPageSize = 50
	maxPageSize     = 100

	// Query limits of the contract. git-svc checks the content of refs and
	// paths; these only stop oversized input before it gets there.
	maxRefLen   = 255
	maxQueryLen = 255
	maxPathLen  = 4096
)

// pageSize reads page_size, or answers 400 and returns false.
func pageSize(w http.ResponseWriter, r *http.Request) (int32, bool) {
	v := r.URL.Query().Get("page_size")
	if v == "" {
		return defaultPageSize, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > maxPageSize {
		httpx.Error(w, http.StatusBadRequest, "page_size must be between 1 and 100")
		return 0, false
	}
	return int32(n), true
}

// queryParam reads a query parameter of at most max bytes, or answers 400
// and returns false.
func queryParam(w http.ResponseWriter, r *http.Request, name string, max int) (string, bool) {
	v := r.URL.Query().Get(name)
	if len(v) > max {
		httpx.Error(w, http.StatusBadRequest, name+" must be at most "+strconv.Itoa(max)+" bytes")
		return "", false
	}
	return v, true
}

// refAndPath reads the ref and path query parameters shared by the content
// routes. A required path must not be empty.
func refAndPath(w http.ResponseWriter, r *http.Request, pathRequired bool) (ref, path string, ok bool) {
	if ref, ok = queryParam(w, r, "ref", maxRefLen); !ok {
		return "", "", false
	}
	if path, ok = queryParam(w, r, "path", maxPathLen); !ok {
		return "", "", false
	}
	if pathRequired && path == "" {
		httpx.Error(w, http.StatusBadRequest, "path is required")
		return "", "", false
	}
	return ref, path, true
}
