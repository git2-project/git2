// Package adminapi is git2's admin plane API (contracts/admin-api). Every
// route needs the admin instance role; the accounts and teams themselves
// live in auth-svc, which owns the auth_svc schema.
package adminapi

import (
	"encoding/base64"
	"net/http"
	"strconv"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/httpx"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Deps struct {
	Users UserStore
	Teams TeamStore
	JWT   *authjwt.Manager
	Redis *redis.Client
}

type Handler struct {
	deps Deps
}

func NewHandler(deps Deps) *Handler { return &Handler{deps: deps} }

// Mount registers every /admin/* route on r.
func (h *Handler) Mount(r chi.Router) {
	read := authsvc.RequirePermission(h.deps.Users, authsvc.PermAdminRead)
	write := authsvc.RequirePermission(h.deps.Users, authsvc.PermAdminWrite)

	r.Route("/admin", func(r chi.Router) {
		r.Use(authsvc.RequireAuth(h.deps.JWT, h.deps.Redis))

		r.With(read).Get("/roles", h.ListRoles)

		r.With(read).Get("/users", h.ListUsers)
		r.With(write).Post("/users", h.CreateUser)
		r.With(read).Get("/users/{user_id}", h.GetUser)
		r.With(write).Patch("/users/{user_id}", h.UpdateUser)

		r.With(read).Get("/teams", h.ListTeams)
		r.With(write).Post("/teams", h.CreateTeam)
		r.With(read).Get("/teams/{team_id}", h.GetTeam)
		r.With(write).Patch("/teams/{team_id}", h.UpdateTeam)
		r.With(write).Delete("/teams/{team_id}", h.DeleteTeam)
		r.With(read).Get("/teams/{team_id}/members", h.ListTeamMembers)
		r.With(write).Put("/teams/{team_id}/members/{user_id}", h.PutTeamMember)
		r.With(write).Delete("/teams/{team_id}/members/{user_id}", h.DeleteTeamMember)
	})
}

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// pageSize reads page_size, or answers 400 and returns false.
func pageSize(w http.ResponseWriter, r *http.Request) (int, bool) {
	v := r.URL.Query().Get("page_size")
	if v == "" {
		return defaultPageSize, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > maxPageSize {
		httpx.Error(w, http.StatusBadRequest, "page_size must be between 1 and 100")
		return 0, false
	}
	return n, true
}

// Page tokens are opaque to clients, so their encoding can change. Users
// and teams are paged by id, team members by username.

func encodePageToken(id uuid.UUID) string { return base64.RawURLEncoding.EncodeToString(id[:]) }

func decodePageToken(s string) (uuid.UUID, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.FromBytes(b)
}

func encodeNameToken(name string) string { return base64.RawURLEncoding.EncodeToString([]byte(name)) }

func decodeNameToken(s string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return string(b), err
}

// pathID parses a uuid path parameter. A malformed one cannot name anything,
// so it answers 404 with notFound.
func pathID(w http.ResponseWriter, r *http.Request, param, notFound string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, notFound)
		return uuid.Nil, false
	}
	return id, true
}
