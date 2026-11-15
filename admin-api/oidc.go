package adminapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/httpx"
	"github.com/google/uuid"
)

// OIDCProviderStore is what the provider routes need from auth-svc.
// authsvc.PostgresStore implements it; handler tests use an in-memory fake.
type OIDCProviderStore interface {
	ListOIDCProviders(ctx context.Context, enabledOnly bool) ([]authsvc.OIDCProvider, error)
	FindOIDCProvider(ctx context.Context, id uuid.UUID) (authsvc.OIDCProvider, error)
	CreateOIDCProvider(ctx context.Context, p authsvc.OIDCProvider) (authsvc.OIDCProvider, error)
	UpdateOIDCProvider(ctx context.Context, id uuid.UUID, u authsvc.OIDCProviderUpdate) (authsvc.OIDCProvider, error)
	DeleteOIDCProvider(ctx context.Context, id uuid.UUID) error
}

// OIDCProvider is the contract's OidcProvider. The client secret is never in
// it, only whether one is set.
type OIDCProvider struct {
	ID              uuid.UUID `json:"id"`
	Slug            string    `json:"slug"`
	DisplayName     string    `json:"display_name"`
	Issuer          string    `json:"issuer"`
	ClientID        string    `json:"client_id"`
	HasClientSecret bool      `json:"has_client_secret"`
	Enabled         bool      `json:"enabled"`
	RedirectURI     string    `json:"redirect_uri"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (h *Handler) toOIDCProvider(p authsvc.OIDCProvider) OIDCProvider {
	return OIDCProvider{
		ID: p.ID, Slug: p.Slug, DisplayName: p.DisplayName, Issuer: p.Issuer, ClientID: p.ClientID,
		HasClientSecret: len(p.SealedSecret) > 0, Enabled: p.Enabled,
		RedirectURI: h.deps.OIDC.RedirectURI(p.Slug), CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

type CreateOIDCProviderRequest struct {
	Slug         string `json:"slug"`
	DisplayName  string `json:"display_name"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Enabled      *bool  `json:"enabled"`
}

type UpdateOIDCProviderRequest struct {
	DisplayName  *string `json:"display_name"`
	Issuer       *string `json:"issuer"`
	ClientID     *string `json:"client_id"`
	ClientSecret *string `json:"client_secret"`
	Enabled      *bool   `json:"enabled"`
}

func (h *Handler) ListOIDCProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := h.deps.OIDCProviders.ListOIDCProviders(r.Context(), false)
	if err != nil {
		slog.Error("admin: list oidc providers", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not list providers, try again")
		return
	}
	out := make([]OIDCProvider, 0, len(ps))
	for _, p := range ps {
		out = append(out, h.toOIDCProvider(p))
	}
	httpx.JSON(w, http.StatusOK, map[string][]OIDCProvider{"providers": out})
}

// CreateOIDCProvider checks the input, then the issuer's discovery document
// (FR-21), and only then seals the secret and saves.
func (h *Handler) CreateOIDCProvider(w http.ResponseWriter, r *http.Request) {
	var req CreateOIDCProviderRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := errors.Join(
		authsvc.CheckProviderSlug(req.Slug),
		authsvc.CheckProviderFields(&req.DisplayName, &req.Issuer, &req.ClientID, &req.ClientSecret),
	); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.oidcOn(w) || !h.issuerOK(w, r, req.Issuer) {
		return
	}

	id, err := uuid.NewV7()
	if err != nil {
		slog.Error("admin: provider id", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not create the provider")
		return
	}
	sealed, err := h.deps.OIDC.SealClientSecret(id, req.ClientSecret)
	if err != nil {
		slog.Error("admin: seal client secret", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "could not store the client secret")
		return
	}
	enabled := req.Enabled == nil || *req.Enabled
	p, err := h.deps.OIDCProviders.CreateOIDCProvider(r.Context(), authsvc.OIDCProvider{
		ID: id, Slug: req.Slug, DisplayName: req.DisplayName, Issuer: req.Issuer,
		ClientID: req.ClientID, SealedSecret: sealed, Enabled: enabled,
	})
	if !providerStoreOK(w, err, "create oidc provider") {
		return
	}
	httpx.JSON(w, http.StatusCreated, h.toOIDCProvider(p))
}

func (h *Handler) GetOIDCProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "provider_id", "provider not found")
	if !ok {
		return
	}
	p, err := h.deps.OIDCProviders.FindOIDCProvider(r.Context(), id)
	if !providerStoreOK(w, err, "find oidc provider") {
		return
	}
	httpx.JSON(w, http.StatusOK, h.toOIDCProvider(p))
}

func (h *Handler) UpdateOIDCProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "provider_id", "provider not found")
	if !ok {
		return
	}
	var req UpdateOIDCProviderRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DisplayName == nil && req.Issuer == nil && req.ClientID == nil && req.ClientSecret == nil && req.Enabled == nil {
		httpx.Error(w, http.StatusBadRequest, "nothing to update")
		return
	}
	if err := authsvc.CheckProviderFields(req.DisplayName, req.Issuer, req.ClientID, req.ClientSecret); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.deps.OIDCProviders.FindOIDCProvider(r.Context(), id); !providerStoreOK(w, err, "find oidc provider") {
		return
	}

	upd := authsvc.OIDCProviderUpdate{DisplayName: req.DisplayName, Issuer: req.Issuer, ClientID: req.ClientID, Enabled: req.Enabled}
	if req.Issuer != nil || req.ClientSecret != nil {
		if !h.oidcOn(w) {
			return
		}
	}
	if req.Issuer != nil && !h.issuerOK(w, r, *req.Issuer) {
		return
	}
	if req.ClientSecret != nil {
		sealed, err := h.deps.OIDC.SealClientSecret(id, *req.ClientSecret)
		if err != nil {
			slog.Error("admin: seal client secret", "err", err)
			httpx.Error(w, http.StatusInternalServerError, "could not store the client secret")
			return
		}
		upd.SealedSecret = sealed
	}
	p, err := h.deps.OIDCProviders.UpdateOIDCProvider(r.Context(), id, upd)
	if !providerStoreOK(w, err, "update oidc provider") {
		return
	}
	httpx.JSON(w, http.StatusOK, h.toOIDCProvider(p))
}

func (h *Handler) DeleteOIDCProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "provider_id", "provider not found")
	if !ok {
		return
	}
	if !providerStoreOK(w, h.deps.OIDCProviders.DeleteOIDCProvider(r.Context(), id), "delete oidc provider") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// oidcOn answers 503 when GIT2_SECRET_KEY or GIT2_EXTERNAL_URL is missing:
// a provider cannot be saved or used without them.
func (h *Handler) oidcOn(w http.ResponseWriter) bool {
	if !h.deps.OIDC.On() {
		httpx.Error(w, http.StatusServiceUnavailable, authsvc.ErrOIDCOff.Error())
		return false
	}
	return true
}

// issuerOK runs the discovery check and answers 400 with the reason when it
// fails.
func (h *Handler) issuerOK(w http.ResponseWriter, r *http.Request, issuer string) bool {
	if err := h.deps.OIDC.CheckIssuer(r.Context(), issuer); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func providerStoreOK(w http.ResponseWriter, err error, op string) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, authsvc.ErrProviderNotFound):
		httpx.Error(w, http.StatusNotFound, "provider not found")
	case errors.Is(err, authsvc.ErrProviderExists):
		httpx.Error(w, http.StatusConflict, "slug already taken")
	default:
		slog.Error("admin: "+op, "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "the database could not be reached, try again")
	}
	return false
}
