// Package authsvc is git2's sign-in service: accounts, passwords, and the
// access/refresh token pair every other service trusts.
package authsvc

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/git2-project/git2/internal/httpx"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/git2-project/git2/internal/password"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Deps struct {
	Users UserStore
	JWT   *authjwt.Manager
	Redis *redis.Client
}

type Handler struct {
	deps Deps
	// dummyHash is compared against when the email is unknown, so a login for
	// a missing account costs the same bcrypt time as a wrong password and the
	// response time does not reveal which emails are registered.
	dummyHash string
}

func NewHandler(deps Deps) (*Handler, error) {
	dummy, err := password.Hash(uuid.NewString())
	if err != nil {
		return nil, err
	}
	return &Handler{deps: deps, dummyHash: dummy}, nil
}

// Register creates an active account. There is no email verification yet:
// git2 is self-hosted and the admin plane will own who may sign up.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Normalize()
	if err := req.Validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	hash, err := password.Hash(req.Password)
	if err != nil {
		slog.Error("hash password", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not process password")
		return
	}

	created, err := h.deps.Users.Create(r.Context(), User{
		Email:        req.Email,
		Username:     req.Username,
		PasswordHash: hash,
		IsActive:     true,
	})
	switch {
	case errors.Is(err, ErrEmailExists):
		httpx.Error(w, http.StatusConflict, "email already registered")
		return
	case errors.Is(err, ErrUsernameExists):
		httpx.Error(w, http.StatusConflict, "username already taken")
		return
	case err != nil:
		slog.Error("create user", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not create account")
		return
	}
	httpx.JSON(w, http.StatusCreated, toUserResponse(created))
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Normalize()
	if req.Email == "" || req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "email and password are required")
		return
	}

	u, err := h.deps.Users.FindByEmail(r.Context(), req.Email)
	if errors.Is(err, ErrNotFound) {
		password.Verify(h.dummyHash, req.Password)
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		slog.Error("login: find user", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not sign in, try again")
		return
	}
	if !password.Verify(u.PasswordHash, req.Password) {
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !u.IsActive {
		httpx.Error(w, http.StatusForbidden, "account is disabled")
		return
	}

	access, err := h.deps.JWT.IssueAccess(u.ID)
	if err != nil {
		slog.Error("issue access token", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	refresh, err := h.startSession(r.Context(), u.ID)
	if err != nil {
		slog.Error("start session", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	h.writeTokenHeaders(w, access, refresh)
	httpx.JSON(w, http.StatusOK, LoginResponse{Message: "login successful", User: toUserResponse(u)})
}

// Refresh swaps a valid refresh token for a new access + refresh pair within
// the same session. Presenting an already-rotated refresh token means it was
// copied, so the whole session is killed.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil || req.RefreshToken == "" {
		httpx.Error(w, http.StatusBadRequest, "refresh_token is required")
		return
	}
	claims, err := h.deps.JWT.Parse(req.RefreshToken, authjwt.PurposeRefresh)
	if err != nil || claims.SessionID == "" {
		httpx.Error(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	sid := claims.SessionID
	u, err := h.deps.Users.FindByID(r.Context(), claims.UserID)
	switch {
	case errors.Is(err, ErrNotFound) || (err == nil && !u.IsActive):
		httpx.Error(w, http.StatusUnauthorized, "account is not available")
		return
	case err != nil:
		// A database fault must not read as "signed out": clients drop their
		// tokens on a 401.
		slog.Error("refresh: find user", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not refresh, try again")
		return
	}

	access, err := h.deps.JWT.IssueAccess(u.ID)
	if err != nil {
		slog.Error("issue access token", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	refresh, jti, err := h.deps.JWT.IssueRefresh(u.ID, sid)
	if err != nil {
		slog.Error("issue refresh token", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	// The new JTI becomes the only valid one before the token is handed out,
	// so a Redis failure can't leave the client holding a token the session
	// won't recognise.
	result, err := rotateSession(r.Context(), h.deps.Redis, sid, claims.ID, jti, h.deps.JWT.RefreshExpiry())
	if err != nil {
		// Fail closed but retryable: the access token stays valid for its TTL,
		// so a blip only delays rotation instead of signing the user out.
		slog.Warn("refresh: rotate session", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not refresh, try again")
		return
	}
	if result != rotated {
		if result == tokenReused {
			slog.Warn("refresh token reuse detected, session killed", "session", sid, "user_id", claims.UserID)
		}
		// One message for both cases, so the response does not tell a token
		// holder whether the token was rotated away or the session ended.
		httpx.Error(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	h.writeTokenHeaders(w, access, refresh)
	httpx.JSON(w, http.StatusOK, messageResponse{Message: "token refreshed"})
}

// Logout revokes whatever the caller presents: the bearer access token and,
// when given, the refresh token's whole session. It always answers 200 so a
// client can force a sign-out even with tokens that are already invalid.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req LogoutRequest
	_ = httpx.DecodeJSON(w, r, &req)

	if raw := bearerToken(r); raw != "" {
		if claims, err := h.deps.JWT.Parse(raw, authjwt.PurposeAccess); err == nil {
			if err := revokeAccess(r.Context(), h.deps.Redis, claims.ID, time.Until(claims.ExpiresAt.Time)); err != nil {
				slog.Warn("logout: revoke access token", "err", err)
			}
		}
	}
	if req.RefreshToken != "" {
		if claims, err := h.deps.JWT.Parse(req.RefreshToken, authjwt.PurposeRefresh); err == nil && claims.SessionID != "" {
			if err := deleteSession(r.Context(), h.deps.Redis, claims.SessionID); err != nil {
				slog.Warn("logout: delete session", "err", err)
			}
		}
	}
	httpx.JSON(w, http.StatusOK, messageResponse{Message: "logged out"})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	u, err := h.deps.Users.FindByID(r.Context(), UserID(r.Context()))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	httpx.JSON(w, http.StatusOK, toUserResponse(u))
}

// startSession mints a new session id, issues a refresh token bound to it and
// records it in Redis. A failure to record is returned so login refuses to
// hand out a refresh token that could never be rotated.
func (h *Handler) startSession(ctx context.Context, userID uuid.UUID) (string, error) {
	sid := uuid.NewString()
	refresh, jti, err := h.deps.JWT.IssueRefresh(userID, sid)
	if err != nil {
		return "", err
	}
	if err := storeSession(ctx, h.deps.Redis, sid, jti, h.deps.JWT.RefreshExpiry()); err != nil {
		return "", err
	}
	return refresh, nil
}

// writeTokenHeaders puts the tokens in response headers, never in the body:
//
//	X-Access-Token       Bearer JWT for the Authorization header
//	X-Refresh-Token      JWT for /api/auth/refresh and /api/auth/logout
//	X-Access-Expires-In  access token TTL in seconds
func (h *Handler) writeTokenHeaders(w http.ResponseWriter, access, refresh string) {
	w.Header().Set("X-Access-Token", access)
	w.Header().Set("X-Refresh-Token", refresh)
	w.Header().Set("X-Access-Expires-In", strconv.FormatInt(int64(h.deps.JWT.AccessExpiry().Seconds()), 10))
	w.Header().Set("Cache-Control", "no-store")
}
