package adminapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/secretbox"
	"github.com/google/uuid"
)

// The memStore's OIDCProviderStore side, with the rules of
// auth_svc.oidc_providers: unique slug, the slug never changes.

func (s *memStore) ListOIDCProviders(_ context.Context, enabledOnly bool) ([]authsvc.OIDCProvider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return nil, err
	}
	var out []authsvc.OIDCProvider
	for _, p := range s.providers {
		if p.Enabled || !enabledOnly {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *memStore) providerIndex(id uuid.UUID) int {
	return slices.IndexFunc(s.providers, func(p authsvc.OIDCProvider) bool { return p.ID == id })
}

func (s *memStore) FindOIDCProvider(_ context.Context, id uuid.UUID) (authsvc.OIDCProvider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return authsvc.OIDCProvider{}, err
	}
	if i := s.providerIndex(id); i >= 0 {
		return s.providers[i], nil
	}
	return authsvc.OIDCProvider{}, authsvc.ErrProviderNotFound
}

func (s *memStore) CreateOIDCProvider(_ context.Context, p authsvc.OIDCProvider) (authsvc.OIDCProvider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return authsvc.OIDCProvider{}, err
	}
	if slices.ContainsFunc(s.providers, func(o authsvc.OIDCProvider) bool { return o.Slug == p.Slug }) {
		return authsvc.OIDCProvider{}, authsvc.ErrProviderExists
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	s.providers = append(s.providers, p)
	return p, nil
}

func (s *memStore) UpdateOIDCProvider(_ context.Context, id uuid.UUID, u authsvc.OIDCProviderUpdate) (authsvc.OIDCProvider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return authsvc.OIDCProvider{}, err
	}
	i := s.providerIndex(id)
	if i < 0 {
		return authsvc.OIDCProvider{}, authsvc.ErrProviderNotFound
	}
	p := &s.providers[i]
	if u.DisplayName != nil {
		p.DisplayName = *u.DisplayName
	}
	if u.Issuer != nil {
		p.Issuer = *u.Issuer
	}
	if u.ClientID != nil {
		p.ClientID = *u.ClientID
	}
	if u.SealedSecret != nil {
		p.SealedSecret = u.SealedSecret
	}
	if u.Enabled != nil {
		p.Enabled = *u.Enabled
	}
	p.UpdatedAt = time.Now()
	return *p, nil
}

func (s *memStore) DeleteOIDCProvider(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return err
	}
	i := s.providerIndex(id)
	if i < 0 {
		return authsvc.ErrProviderNotFound
	}
	s.providers = slices.Delete(s.providers, i, i+1)
	return nil
}

// fakeIssuer serves an OIDC discovery document. claimedIssuer is the issuer
// the document names ("" for the server's own URL); withEndpoints false
// leaves out the authorization and token endpoints.
func fakeIssuer(t *testing.T, claimedIssuer string, withEndpoints bool) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		issuer := claimedIssuer
		if issuer == "" {
			issuer = srv.URL
		}
		doc := map[string]any{"issuer": issuer, "jwks_uri": srv.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}}
		if withEndpoints {
			doc["authorization_endpoint"] = srv.URL + "/auth"
			doc["token_endpoint"] = srv.URL + "/token"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func oidcSettings(t *testing.T) authsvc.OIDCSettings {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	ext, _ := url.Parse("https://git.example.test")
	return authsvc.OIDCSettings{Box: box, ExternalURL: ext}
}

func providerBody(slug, issuer string) map[string]any {
	return map[string]any{"slug": slug, "display_name": "Company SSO", "issuer": issuer, "client_id": "git2", "client_secret": "s3cret-client"}
}

func TestOIDCProviderRoutesNeedAnAdmin(t *testing.T) {
	h := newHarness(t)
	plain := h.addUser("ada@example.com", "ada", authsvc.RoleUser)
	p := "/api/admin/oidc-providers/" + uuid.NewString()
	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/oidc-providers"},
		{http.MethodPost, "/api/admin/oidc-providers"},
		{http.MethodGet, p},
		{http.MethodPatch, p},
		{http.MethodDelete, p},
	} {
		if rec := h.do(rt.method, rt.path, "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", rt.method, rt.path, rec.Code)
		}
		if rec := h.do(rt.method, rt.path, h.token(plain.ID), nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a user: status %d, want 403", rt.method, rt.path, rec.Code)
		}
	}
}

func TestCreateOIDCProvider(t *testing.T) {
	h := newHarness(t)
	issuer := fakeIssuer(t, "", true)

	rec := h.do(http.MethodPost, "/api/admin/oidc-providers", h.adminToken, providerBody("company", issuer.URL))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status %d body %s", rec.Code, rec.Body)
	}
	got := decode[OIDCProvider](t, rec)
	if got.Slug != "company" || got.Issuer != issuer.URL || got.ClientID != "git2" || !got.HasClientSecret || !got.Enabled {
		t.Errorf("created %+v", got)
	}
	if want := "https://git.example.test/api/auth/oidc/company/callback"; got.RedirectURI != want {
		t.Errorf("redirect_uri = %q, want %q", got.RedirectURI, want)
	}
	if strings.Contains(rec.Body.String(), "s3cret-client") {
		t.Errorf("response contains the client secret: %s", rec.Body)
	}

	// Stored sealed, and it opens to the secret with the instance key.
	stored, _ := h.store.FindOIDCProvider(context.Background(), got.ID)
	if strings.Contains(string(stored.SealedSecret), "s3cret-client") {
		t.Error("client secret stored in plain text")
	}
	if secret, err := h.oidc.ClientSecret(stored); err != nil || secret != "s3cret-client" {
		t.Errorf("ClientSecret = %q, %v", secret, err)
	}

	if rec := h.do(http.MethodPost, "/api/admin/oidc-providers", h.adminToken, providerBody("company", issuer.URL)); rec.Code != http.StatusConflict {
		t.Errorf("same slug: status %d, want 409", rec.Code)
	}
	disabled := providerBody("second", issuer.URL)
	disabled["enabled"] = false
	if rec := h.do(http.MethodPost, "/api/admin/oidc-providers", h.adminToken, disabled); rec.Code != http.StatusCreated || decode[OIDCProvider](t, rec).Enabled {
		t.Errorf("created disabled: status %d body %s", rec.Code, rec.Body)
	}
}

// FR-21: the issuer's discovery document is checked before saving, and a
// failed check saves nothing and says why.
func TestCreateOIDCProviderChecksTheIssuer(t *testing.T) {
	h := newHarness(t)
	good := fakeIssuer(t, "", true)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	tests := []struct {
		name, issuer, want string
	}{
		{"another issuer in the document", fakeIssuer(t, "https://elsewhere.example", true).URL, "issuer"},
		{"no endpoints", fakeIssuer(t, "", false).URL, "endpoint"},
		{"unreachable", closed.URL, "issuer check failed"},
		{"not a discovery document", good.URL + "/nope", "issuer check failed"},
		{"not a URL", "company sso", "issuer must be"},
		{"query", good.URL + "?x=1", "issuer must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.do(http.MethodPost, "/api/admin/oidc-providers", h.adminToken, providerBody("company", tt.issuer))
			if rec.Code != http.StatusBadRequest || !strings.Contains(errorOf(t, rec), tt.want) {
				t.Errorf("status %d body %s, want 400 mentioning %q", rec.Code, rec.Body, tt.want)
			}
		})
	}
	if ps, _ := h.store.ListOIDCProviders(context.Background(), false); len(ps) != 0 {
		t.Errorf("failed checks saved %d providers", len(ps))
	}
}

func TestCreateOIDCProviderValidates(t *testing.T) {
	h := newHarness(t)
	issuer := fakeIssuer(t, "", true).URL
	with := func(k string, v any) map[string]any {
		b := providerBody("company", issuer)
		if v == nil {
			delete(b, k)
		} else {
			b[k] = v
		}
		return b
	}
	for name, body := range map[string]any{
		"bad slug":         with("slug", "Company SSO"),
		"no display name":  with("display_name", " "),
		"no client id":     with("client_id", nil),
		"no client secret": with("client_secret", nil),
		"unknown field":    `{"slug":"company","display_name":"x","issuer":"` + issuer + `","client_id":"a","client_secret":"b","scopes":["x"]}`,
	} {
		if rec := h.do(http.MethodPost, "/api/admin/oidc-providers", h.adminToken, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (body %s)", name, rec.Code, rec.Body)
		}
	}
}

func TestOIDCOffWithoutKeyOrURL(t *testing.T) {
	h := newHarness(t)
	issuer := fakeIssuer(t, "", true).URL
	for name, settings := range map[string]authsvc.OIDCSettings{
		"no key": {ExternalURL: h.oidc.ExternalURL},
		"no url": {Box: h.oidc.Box},
	} {
		h.setOIDC(settings)
		rec := h.do(http.MethodPost, "/api/admin/oidc-providers", h.adminToken, providerBody("company", issuer))
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(errorOf(t, rec), "GIT2_SECRET_KEY") {
			t.Errorf("%s: status %d body %s, want 503 naming the settings", name, rec.Code, rec.Body)
		}
		if rec := h.do(http.MethodGet, "/api/admin/oidc-providers", h.adminToken, nil); rec.Code != http.StatusOK {
			t.Errorf("%s: list status %d, want 200", name, rec.Code)
		}
	}
}

func TestGetUpdateDeleteOIDCProvider(t *testing.T) {
	h := newHarness(t)
	issuer := fakeIssuer(t, "", true)
	created := decode[OIDCProvider](t, h.do(http.MethodPost, "/api/admin/oidc-providers", h.adminToken, providerBody("company", issuer.URL)))
	path := "/api/admin/oidc-providers/" + created.ID.String()
	sealedBefore := h.store.providers[0].SealedSecret

	if rec := h.do(http.MethodGet, path, h.adminToken, nil); rec.Code != http.StatusOK || decode[OIDCProvider](t, rec).Slug != "company" {
		t.Errorf("get: status %d body %s", rec.Code, rec.Body)
	}
	if got := decode[struct {
		Providers []OIDCProvider `json:"providers"`
	}](t, h.do(http.MethodGet, "/api/admin/oidc-providers", h.adminToken, nil)).Providers; len(got) != 1 || got[0].ID != created.ID {
		t.Errorf("list = %+v", got)
	}

	// Disable and rename: no issuer check, the secret stays.
	rec := h.do(http.MethodPatch, path, h.adminToken, map[string]any{"display_name": "Okta", "enabled": false})
	if got := decode[OIDCProvider](t, rec); rec.Code != http.StatusOK || got.DisplayName != "Okta" || got.Enabled || !got.HasClientSecret {
		t.Errorf("rename and disable: status %d body %s", rec.Code, rec.Body)
	}
	if string(h.store.providers[0].SealedSecret) != string(sealedBefore) {
		t.Error("the secret changed without a new one")
	}

	// A new secret is sealed again and opens to the new value.
	if rec := h.do(http.MethodPatch, path, h.adminToken, map[string]any{"client_secret": "rotated"}); rec.Code != http.StatusOK {
		t.Fatalf("new secret: status %d body %s", rec.Code, rec.Body)
	}
	if secret, _ := h.oidc.ClientSecret(h.store.providers[0]); secret != "rotated" {
		t.Errorf("secret after rotation = %q", secret)
	}

	// A new issuer is checked; a failed check changes nothing.
	if rec := h.do(http.MethodPatch, path, h.adminToken, map[string]any{"issuer": fakeIssuer(t, "https://elsewhere.example", true).URL}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad new issuer: status %d, want 400", rec.Code)
	}
	if h.store.providers[0].Issuer != issuer.URL {
		t.Errorf("issuer changed to %q after a failed check", h.store.providers[0].Issuer)
	}

	for _, tt := range []struct {
		body any
		want int
	}{
		{`{}`, http.StatusBadRequest},
		{map[string]any{"slug": "other"}, http.StatusBadRequest}, // the slug never changes
		{map[string]any{"client_id": ""}, http.StatusBadRequest},
	} {
		if rec := h.do(http.MethodPatch, path, h.adminToken, tt.body); rec.Code != tt.want {
			t.Errorf("PATCH %v: status %d, want %d", tt.body, rec.Code, tt.want)
		}
	}

	unknown := "/api/admin/oidc-providers/" + uuid.NewString()
	for _, m := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		if rec := h.do(m, unknown, h.adminToken, map[string]any{"enabled": true}); rec.Code != http.StatusNotFound {
			t.Errorf("%s unknown: status %d, want 404", m, rec.Code)
		}
	}
	if rec := h.do(http.MethodDelete, path, h.adminToken, nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete: status %d", rec.Code)
	}
	if rec := h.do(http.MethodGet, path, h.adminToken, nil); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: status %d, want 404", rec.Code)
	}
}

func TestOIDCProviderDatabaseFault(t *testing.T) {
	h := newHarness(t)
	h.store.mu.Lock()
	h.store.teamFail = errors.New("connection refused")
	h.store.mu.Unlock()
	if rec := h.do(http.MethodGet, "/api/admin/oidc-providers", h.adminToken, nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("list: status %d, want 503", rec.Code)
	}
}
