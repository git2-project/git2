package authsvc

import (
	"time"

	"github.com/git2-project/git2/internal/ratelimit"
	"github.com/go-chi/chi/v5"
)

// Per-client-IP budgets for the unauthenticated surface. Loose on purpose:
// they stop a single-source hammer, the credential check is the real boundary.
// /refresh is the loosest: clients call it on every cold start.
const (
	registerLimit = 10
	loginLimit    = 20
	logoutLimit   = 30
	refreshLimit  = 60
	setupLimit    = 10
	publicWindow  = time.Minute
)

// Mount registers every /auth/* and /setup/* route on r.
func (h *Handler) Mount(r chi.Router) {
	if h.deps.Setup != nil {
		r.Route("/setup", func(r chi.Router) {
			r.Get("/status", h.SetupStatus)
			r.With(ratelimit.ByIP(h.deps.Redis, "setup-admin", setupLimit, publicWindow)).Post("/admin", h.SetupAdmin)
		})
	}

	r.Route("/auth", func(r chi.Router) {
		rdb := h.deps.Redis
		r.With(ratelimit.ByIP(rdb, "auth-register", registerLimit, publicWindow)).Post("/register", h.Register)
		r.With(ratelimit.ByIP(rdb, "auth-login", loginLimit, publicWindow)).Post("/login", h.Login)
		r.With(ratelimit.ByIP(rdb, "auth-refresh", refreshLimit, publicWindow)).Post("/refresh", h.Refresh)
		// Logout works with or without a valid token, so it stays public.
		r.With(ratelimit.ByIP(rdb, "auth-logout", logoutLimit, publicWindow)).Post("/logout", h.Logout)

		r.With(RequireAuth(h.deps.JWT, rdb)).Get("/me", h.Me)
	})
}
