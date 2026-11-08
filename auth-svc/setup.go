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
	"time"

	"github.com/git2-project/git2/internal/httpx"
	"github.com/git2-project/git2/internal/password"
)

// First-admin setup (docs/design/identity-v0.md, "The first admin"). A fresh
// install has no admin. Instead of "the first account to register becomes
// admin", which lets whoever reaches the server first take it over, the
// first admin needs a one-time setup token that only the operator sees.

var (
	ErrSetupDone         = errors.New("setup already completed")
	ErrBadSetupToken     = errors.New("invalid setup token")
	ErrSetupTokenExpired = errors.New("setup token expired")
)

// MinSetupTokenLen is the shortest token accepted from GIT2_SETUP_TOKEN.
const MinSetupTokenLen = 32

// SetupTokenTTL is how long a generated setup token works. Until then a
// restart, or a second git2 process, keeps it instead of replacing it, so
// the token in the first log stays valid. After it, the next start issues a
// new one: that is how an operator who lost the log gets back in.
const SetupTokenTTL = time.Hour

// PutSetupResult is what PutSetupToken did.
type PutSetupResult struct {
	Stored      bool      // this hash is now the setup token
	AdminExists bool      // nothing stored: setup is done
	ExpiresAt   time.Time // of the token now in place (stored or kept)
}

// SetupStore is what setup needs from persistence. PostgresStore implements
// it; only the token's SHA-256 is ever stored.
type SetupStore interface {
	// AdminExists reports whether an active admin exists.
	AdminExists(ctx context.Context) (bool, error)
	// PutSetupToken stores the hash unless an active admin exists. An
	// earlier token that has not expired is kept, unless replace is set.
	PutSetupToken(ctx context.Context, tokenSHA256 string, replace bool) (PutSetupResult, error)
	// CreateFirstAdmin checks the hash, creates u as an admin and deletes the
	// token, all in one transaction. ErrBadSetupToken, ErrSetupTokenExpired,
	// ErrSetupDone, ErrEmailExists or ErrUsernameExists on refusal.
	CreateFirstAdmin(ctx context.Context, tokenSHA256 string, u User) (User, error)
}

func hashSetupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SetupState is what PrepareSetup found or did, for the startup log.
type SetupState struct {
	// Token is set only when a new token was generated: print it once.
	Token string
	// Kept: a token from an earlier start (or another git2 process) is
	// still valid; the operator uses the one printed then.
	Kept      bool
	Done      bool // an active admin exists; nothing to do
	ExpiresAt time.Time
}

// PrepareSetup runs at startup. When an active admin exists it does nothing.
// Otherwise it makes sure a setup token is in place:
//
//   - fixed (GIT2_SETUP_TOKEN, for scripted installs) is always stored, and
//     not returned, since the operator already knows it;
//   - otherwise an unexpired token from an earlier start is kept, or a new
//     one is generated and returned so the caller can print it once.
func PrepareSetup(ctx context.Context, s SetupStore, fixed string) (SetupState, error) {
	token := fixed
	if token == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return SetupState{}, fmt.Errorf("generate setup token: %w", err)
		}
		token = base64.RawURLEncoding.EncodeToString(b)
	} else if len(token) < MinSetupTokenLen {
		return SetupState{}, fmt.Errorf("GIT2_SETUP_TOKEN must be at least %d characters", MinSetupTokenLen)
	}

	res, err := s.PutSetupToken(ctx, hashSetupToken(token), fixed != "")
	if err != nil {
		return SetupState{}, fmt.Errorf("store setup token: %w", err)
	}
	switch {
	case res.AdminExists:
		return SetupState{Done: true}, nil
	case !res.Stored:
		return SetupState{Kept: true, ExpiresAt: res.ExpiresAt}, nil
	case fixed != "":
		return SetupState{ExpiresAt: res.ExpiresAt}, nil
	}
	return SetupState{Token: token, ExpiresAt: res.ExpiresAt}, nil
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
	// Once setup is done, every request gets the 409 the wizard relies on,
	// whatever its body. CreateFirstAdmin checks again inside its
	// transaction; this is only for the answer.
	done, err := h.deps.Setup.AdminExists(r.Context())
	if err != nil {
		slog.Error("setup: admin exists", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not check setup, try again")
		return
	}
	if done {
		httpx.Error(w, http.StatusConflict, "setup already completed")
		return
	}

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
	})
	switch {
	case errors.Is(err, ErrBadSetupToken):
		// 403, not 401: clients read a 401 as "signed out".
		httpx.Error(w, http.StatusForbidden, "invalid setup token")
		return
	case errors.Is(err, ErrSetupTokenExpired):
		httpx.Error(w, http.StatusForbidden, "setup token expired; restart git2 to get a new one")
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
