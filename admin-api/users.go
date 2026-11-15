// Package adminapi is git2's admin plane API (contracts/admin-api). Every
// route needs the admin instance role; the accounts themselves live in
// auth-svc, which owns the auth_svc schema.
package adminapi

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/httpx"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// UserStore is what the user routes need from auth-svc. authsvc.PostgresStore
// implements it; handler tests use an in-memory fake.
type UserStore interface {
	authsvc.UserFinder
	authsvc.UserStatusStore
	Create(ctx context.Context, u authsvc.User) (authsvc.User, error)
	ListUsers(ctx context.Context, f authsvc.UserFilter) ([]authsvc.User, error)
	UpdateUser(ctx context.Context, id uuid.UUID, u authsvc.UserUpdate) (authsvc.User, error)
	UserTeams(ctx context.Context, userID uuid.UUID) ([]authsvc.UserTeam, error)
}

type Deps struct {
	Users UserStore
	JWT   *authjwt.Manager
	Redis *redis.Client
}

type Handler struct {
	deps Deps
}

func NewHandler(deps Deps) *Handler { return &Handler{deps: deps} }

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// Mount registers every /admin/* route on r.
func (h *Handler) Mount(r chi.Router) {
	read := authsvc.RequirePermission(h.deps.Users, authsvc.PermAdminRead)
	write := authsvc.RequirePermission(h.deps.Users, authsvc.PermAdminWrite)

	r.Route("/admin", func(r chi.Router) {
		r.Use(authsvc.RequireAuth(h.deps.JWT, h.deps.Redis))
		r.With(read).Get("/users", h.ListUsers)
		r.With(write).Post("/users", h.CreateUser)
		r.With(read).Get("/users/{user_id}", h.GetUser)
		r.With(write).Patch("/users/{user_id}", h.UpdateUser)
	})
}

// AdminUser is the contract's AdminUser.
type AdminUser struct {
	ID        uuid.UUID    `json:"id"`
	Email     string       `json:"email"`
	Username  string       `json:"username"`
	Role      authsvc.Role `json:"role"`
	IsActive  bool         `json:"is_active"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

func toAdminUser(u authsvc.User) AdminUser {
	return AdminUser{
		ID: u.ID, Email: u.Email, Username: u.Username, Role: u.Role,
		IsActive: u.IsActive, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

type TeamRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type UserTeam struct {
	Team TeamRef      `json:"team"`
	Role authsvc.Role `json:"role"`
}

// AdminUserDetail is the contract's AdminUserDetail: the user and their teams.
type AdminUserDetail struct {
	AdminUser
	Teams []UserTeam `json:"teams"`
}

type UserPage struct {
	Users         []AdminUser `json:"users"`
	NextPageToken string      `json:"next_page_token,omitempty"`
}

type CreateUserRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UpdateUserRequest has pointers so an absent field stays unchanged.
type UpdateUserRequest struct {
	Email    *string `json:"email"`
	Role     *string `json:"role"`
	IsActive *bool   `json:"is_active"`
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := authsvc.UserFilter{Prefix: q.Get("q"), Limit: defaultPageSize}
	if len(f.Prefix) > 254 {
		httpx.Error(w, http.StatusBadRequest, "q is too long")
		return
	}
	if v := q.Get("role"); v != "" {
		role, err := authsvc.ParseInstanceRole(v)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		f.Role = role
	}
	if v := q.Get("active"); v != "" {
		active, err := strconv.ParseBool(v)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "active must be true or false")
			return
		}
		f.Active = &active
	}
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageSize {
			httpx.Error(w, http.StatusBadRequest, "page_size must be between 1 and 100")
			return
		}
		f.Limit = n
	}
	if v := q.Get("page_token"); v != "" {
		after, err := decodePageToken(v)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid page_token")
			return
		}
		f.After = after
	}

	// One extra row tells whether another page exists.
	pageSize := f.Limit
	f.Limit++
	users, err := h.deps.Users.ListUsers(r.Context(), f)
	if err != nil {
		slog.Error("admin: list users", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not list users, try again")
		return
	}
	page := UserPage{Users: make([]AdminUser, 0, min(len(users), pageSize))}
	for i, u := range users {
		if i == pageSize {
			page.NextPageToken = encodePageToken(users[i-1].ID)
			break
		}
		page.Users = append(page.Users, toAdminUser(u))
	}
	httpx.JSON(w, http.StatusOK, page)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var role authsvc.Role
	if req.Role != "" {
		var err error
		if role, err = authsvc.ParseInstanceRole(req.Role); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	u, err := authsvc.NewUser(req.Email, req.Username, req.Password, role)
	var inputErr *authsvc.InputError
	switch {
	case errors.As(err, &inputErr):
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		slog.Error("admin: new user", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not process password")
		return
	}

	created, err := h.deps.Users.Create(r.Context(), u)
	switch {
	case errors.Is(err, authsvc.ErrEmailExists):
		httpx.Error(w, http.StatusConflict, "email already registered")
		return
	case errors.Is(err, authsvc.ErrUsernameExists):
		httpx.Error(w, http.StatusConflict, "username already taken")
		return
	case err != nil:
		slog.Error("admin: create user", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not create user, try again")
		return
	}
	httpx.JSON(w, http.StatusCreated, toAdminUser(created))
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDParam(w, r)
	if !ok {
		return
	}
	u, ok := h.findUser(w, r, id)
	if !ok {
		return
	}
	teams, err := h.deps.Users.UserTeams(r.Context(), id)
	if err != nil {
		slog.Error("admin: user teams", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not load user, try again")
		return
	}
	detail := AdminUserDetail{AdminUser: toAdminUser(u), Teams: make([]UserTeam, 0, len(teams))}
	for _, t := range teams {
		detail.Teams = append(detail.Teams, UserTeam{Team: TeamRef{ID: t.TeamID, Name: t.TeamName}, Role: t.Role})
	}
	httpx.JSON(w, http.StatusOK, detail)
}

// UpdateUser changes the email or role, then enables or disables the user.
// The steps are not one transaction: if disabling fails after the email or
// role changed, the answer is an error and repeating the request is safe.
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDParam(w, r)
	if !ok {
		return
	}
	var req UpdateUserRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == nil && req.Role == nil && req.IsActive == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}

	var upd authsvc.UserUpdate
	if req.Email != nil {
		email, err := authsvc.CheckEmail(*req.Email)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		upd.Email = &email
	}
	if req.Role != nil {
		role, err := authsvc.ParseInstanceRole(*req.Role)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		upd.Role = &role
	}

	// An admin locking themselves out is almost always a mistake, and with
	// one admin left it would leave nobody to undo it.
	if id == authsvc.UserID(r.Context()) &&
		((upd.Role != nil && *upd.Role != authsvc.RoleAdmin) || (req.IsActive != nil && !*req.IsActive)) {
		httpx.Error(w, http.StatusConflict, "you cannot demote or disable your own account")
		return
	}

	if upd.Email != nil || upd.Role != nil {
		_, err := h.deps.Users.UpdateUser(r.Context(), id, upd)
		if !h.storeOK(w, err, "update user") {
			return
		}
	}

	if req.IsActive != nil {
		var err error
		if *req.IsActive {
			err = authsvc.EnableUser(r.Context(), h.deps.Users, id)
		} else {
			err = authsvc.DisableUser(r.Context(), h.deps.Users, h.deps.JWT, h.deps.Redis, id)
		}
		if errors.Is(err, authsvc.ErrSessionsNotEnded) {
			// The account is off in the database; only ending the sessions
			// in Redis failed. Repeating the request finishes it.
			slog.Error("admin: disable user", "err", err)
			httpx.Error(w, http.StatusServiceUnavailable, "user disabled, but their sessions could not be ended; try again")
			return
		}
		if !h.storeOK(w, err, "enable or disable user") {
			return
		}
	}

	u, ok := h.findUser(w, r, id)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, toAdminUser(u))
}

// storeOK answers the store errors every write can return and reports
// whether err was nil.
func (h *Handler) storeOK(w http.ResponseWriter, err error, op string) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, authsvc.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "user not found")
	case errors.Is(err, authsvc.ErrLastAdmin):
		httpx.Error(w, http.StatusConflict, "the last active admin cannot be demoted or disabled")
	case errors.Is(err, authsvc.ErrEmailExists):
		httpx.Error(w, http.StatusConflict, "email already registered")
	default:
		slog.Error("admin: "+op, "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not save the change, try again")
	}
	return false
}

func (h *Handler) findUser(w http.ResponseWriter, r *http.Request, id uuid.UUID) (authsvc.User, bool) {
	u, err := h.deps.Users.FindByID(r.Context(), id)
	switch {
	case errors.Is(err, authsvc.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "user not found")
		return authsvc.User{}, false
	case err != nil:
		slog.Error("admin: find user", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not load user, try again")
		return authsvc.User{}, false
	}
	return u, true
}

func userIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		// Not a uuid, so no such user.
		httpx.Error(w, http.StatusNotFound, "user not found")
		return uuid.Nil, false
	}
	return id, true
}

// A page token is the id of the last user on the previous page. It is opaque
// to clients, so the encoding can change.
func encodePageToken(id uuid.UUID) string { return base64.RawURLEncoding.EncodeToString(id[:]) }

func decodePageToken(s string) (uuid.UUID, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.FromBytes(b)
}
