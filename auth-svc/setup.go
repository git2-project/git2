package authsvc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/git2-project/git2/internal/httpx"
	"github.com/git2-project/git2/internal/password"
)

// First-admin setup (docs/design/identity-v0.md, "The first admin"). A fresh
// install has no admin. Instead of "the first account to register becomes
// admin", which lets whoever reaches the server first take it over, the
// first admin needs a one-time setup token that only the operator sees.

var (
	ErrSetupDone     = errors.New("setup already completed")
	ErrBadSetupToken = errors.New("invalid setup token")
)

// MinSetupTokenLen is the shortest token accepted from GIT2_SETUP_TOKEN.
const MinSetupTokenLen = 32

// SetupStore is what setup needs from persistence. PostgresStore implements
// it; only the token's SHA-256 is ever stored.
type SetupStore interface {
	AdminExists(ctx context.Context) (bool, error)
	// PutSetupToken stores the hash unless an admin exists, replacing any
	// earlier one. It reports whether it stored it.
	PutSetupToken(ctx context.Context, tokenSHA256 string) (bool, error)
	// CreateFirstAdmin checks the hash, creates u as an admin and deletes the
	// token, all in one transaction. ErrBadSetupToken, ErrSetupDone,
	// ErrEmailExists or ErrUsernameExists on refusal.
	CreateFirstAdmin(ctx context.Context, tokenSHA256 string, u User) (User, error)
}

func hashSetupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// PrepareSetup runs at startup. When an admin exists it does nothing and
// returns "". Otherwise it stores a setup token: fixed (from
// GIT2_SETUP_TOKEN, for scripted installs; returned as "" since the operator
// already knows it) or freshly generated and returned so the caller can
// print it once. Every start without an admin replaces the token, so an
// operator who lost the log only has to restart.
func PrepareSetup(ctx context.Context, s SetupStore, fixed string) (string, error) {
	token := fixed
	if token == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("generate setup token: %w", err)
		}
		token = base64.RawURLEncoding.EncodeToString(b)
	} else if len(token) < MinSetupTokenLen {
		return "", fmt.Errorf("GIT2_SETUP_TOKEN must be at least %d characters", MinSetupTokenLen)
	}

	stored, err := s.PutSetupToken(ctx, hashSetupToken(token))
	if err != nil {
		return "", fmt.Errorf("store setup token: %w", err)
	}
	if !stored || fixed != "" {
		return "", nil
	}
	return token, nil
}

type SetupStatusResponse struct {
	NeedsSetup bool `json:"needs_setup"`
}

// SetupAdminRequest is RegisterRequest plus the setup token.
type SetupAdminRequest struct {
	SetupToken string `json:"setup_token"`
	Email      string `json:"email"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

func (h *Handler) SetupStatus(w http.ResponseWriter, r *http.Request) {
	exists, err := h.deps.Setup.AdminExists(r.Context())
	if err != nil {
		slog.Error("setup status: admin exists", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not check setup, try again")
		return
	}
	httpx.JSON(w, http.StatusOK, SetupStatusResponse{NeedsSetup: !exists})
}

func (h *Handler) SetupAdmin(w http.ResponseWriter, r *http.Request) {
	var req SetupAdminRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	acct := RegisterRequest{Email: req.Email, Username: req.Username, Password: req.Password}
	acct.Normalize()
	if err := acct.Validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.SetupToken == "" {
		httpx.Error(w, http.StatusForbidden, "invalid setup token")
		return
	}

	hash, err := password.Hash(acct.Password)
	if err != nil {
		slog.Error("hash password", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not process password")
		return
	}

	created, err := h.deps.Setup.CreateFirstAdmin(r.Context(), hashSetupToken(req.SetupToken), User{
		Email:        acct.Email,
		Username:     acct.Username,
		PasswordHash: hash,
		IsActive:     true,
		Role:         RoleAdmin,
	})
	switch {
	case errors.Is(err, ErrBadSetupToken):
		// 403, not 401: clients read a 401 as "signed out".
		httpx.Error(w, http.StatusForbidden, "invalid setup token")
		return
	case errors.Is(err, ErrSetupDone):
		httpx.Error(w, http.StatusConflict, "setup already completed")
		return
	case errors.Is(err, ErrEmailExists):
		httpx.Error(w, http.StatusConflict, "email already registered")
		return
	case errors.Is(err, ErrUsernameExists):
		httpx.Error(w, http.StatusConflict, "username already taken")
		return
	case err != nil:
		slog.Error("setup: create first admin", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not create the admin, try again")
		return
	}
	slog.Info("first admin created; setup token burned", "user_id", created.ID, "username", created.Username)
	httpx.JSON(w, http.StatusCreated, toUserResponse(created))
}
