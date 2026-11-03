package authsvc

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/git2-project/git2/internal/httpx"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type ctxKey struct{ name string }

var claimsKey = &ctxKey{"claims"}

// RequireAuth demands a valid, unrevoked access token in the Authorization
// header and stores its claims in the request context. Other services mount
// it in front of their own authenticated routes.
func RequireAuth(jwtMgr *authjwt.Manager, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				httpx.Error(w, http.StatusUnauthorized, "missing or malformed Authorization header")
				return
			}
			claims, err := jwtMgr.Parse(raw, authjwt.PurposeAccess)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "invalid or expired access token")
				return
			}
			if isRevoked(r.Context(), rdb, claims.ID) {
				httpx.Error(w, http.StatusUnauthorized, "token has been revoked")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
		})
	}
}

// UserFinder is what RequirePermission needs to look the caller up.
// PostgresStore implements it.
type UserFinder interface {
	FindByID(ctx context.Context, id uuid.UUID) (User, error)
}

// RequirePermission lets the request through only when the caller's instance
// role grants p. It must run after RequireAuth. The role is read from the
// database on every request, never from the token, so a demotion takes
// effect at once instead of when the access token expires.
//
//	401  no authenticated caller, or the account is gone or disabled
//	403  signed in, but the role does not grant p
//	503  the database could not be reached (never 401: clients drop tokens on 401)
func RequirePermission(users UserFinder, p Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := UserID(r.Context())
			if id == uuid.Nil {
				httpx.Error(w, http.StatusUnauthorized, "authentication required")
				return
			}
			u, err := users.FindByID(r.Context(), id)
			switch {
			case errors.Is(err, ErrNotFound) || (err == nil && !u.IsActive):
				httpx.Error(w, http.StatusUnauthorized, "account is not available")
				return
			case err != nil:
				slog.Error("require permission: find user", "err", err)
				httpx.Error(w, http.StatusServiceUnavailable, "could not check permissions, try again")
				return
			}
			if !Can(u.Role, p) {
				httpx.Error(w, http.StatusForbidden, "your role does not allow this")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserID returns the authenticated user's id, or uuid.Nil outside RequireAuth.
func UserID(ctx context.Context) uuid.UUID {
	if c, ok := ctx.Value(claimsKey).(*authjwt.Claims); ok {
		return c.UserID
	}
	return uuid.Nil
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}
