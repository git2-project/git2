package authsvc

import (
	"context"
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
