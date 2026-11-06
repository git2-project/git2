// Package gateway is git2's one entry point: it routes /api to git2 core
// after checking the access token, and serves the Dash and Admin web apps.
// The contract is docs/api/gateway.md in git2-deploy; why it is its own
// process is git2-deploy ADR 0004.
package gateway

import (
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/httpx"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
)

// APITimeout is the contract's per-request limit on /api.
const APITimeout = 30 * time.Second

// Deps is what the gateway needs. APITimeout is a field so tests can shorten it.
type Deps struct {
	JWT        *authjwt.Manager
	Redis      *redis.Client
	Core       *url.URL
	WebDir     string
	APITimeout time.Duration
}

// New returns the gateway's handler.
func New(d Deps) (http.Handler, error) {
	if d.JWT == nil || d.Redis == nil || d.Core == nil || d.WebDir == "" {
		return nil, errors.New("gateway: JWT, Redis, Core and WebDir are required")
	}
	if d.APITimeout <= 0 {
		d.APITimeout = APITimeout
	}
	api := withTimeout(d.APITimeout, newProxy(d.Core))

	r := chi.NewRouter()
	r.Use(middleware.Recoverer, withRequestID, withNoSniff, withCleanPath)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Public API: sign-in and first-admin setup check their own credentials.
	r.Handle("/api/auth/*", api)
	r.Handle("/api/setup/*", api)
	// Every other API route needs a valid access token. auth-svc's own check,
	// so the gateway and the services can never disagree on a token.
	r.With(authsvc.RequireAuth(d.JWT, d.Redis)).Handle("/api/*", api)
	r.Handle("/api", http.HandlerFunc(apiNotFound))

	admin := spa(filepath.Join(d.WebDir, "admin"), "/admin")
	r.Handle("/admin", admin)
	r.Handle("/admin/*", admin)
	r.Handle("/*", spa(filepath.Join(d.WebDir, "dash"), ""))
	return r, nil
}

func apiNotFound(w http.ResponseWriter, _ *http.Request) {
	httpx.Error(w, http.StatusNotFound, "not found")
}
