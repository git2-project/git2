package authsvc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/git2-project/git2/internal/secretbox"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ---- a fake OIDC provider ------------------------------------------------

// fakeIDP is an OIDC provider in the test: discovery, signing keys and a
// token endpoint that checks the client secret, the redirect URI and the
// PKCE verifier, and answers with an RS256-signed ID token.
type fakeIDP struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]idpGrant

	// Faults a test can switch on.
	otherKey     *rsa.PrivateKey // sign ID tokens with a key not in the JWKS
	wrongNonce   bool
	wrongAud     bool
	noIDToken    bool
	tokenRefuses bool
}

type idpGrant struct {
	nonce, challenge, redirectURI string
	claims                        map[string]any
}

const idpClientID, idpSecret = "git2", "s3cret-client"

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{t: t, key: key, codes: map[string]idpGrant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                idp.srv.URL,
			"authorization_endpoint":                idp.srv.URL + "/authorize",
			"token_endpoint":                        idp.srv.URL + "/token",
			"jwks_uri":                              idp.srv.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", idp.token)
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// authorize plays the user signing in at the provider: it reads the
// authorization request git2 sent the browser to, and returns the code the
// provider would send back.
func (idp *fakeIDP) authorize(authURL string, claims map[string]any) (code, state string) {
	idp.t.Helper()
	u, err := url.Parse(authURL)
	if err != nil || !strings.HasPrefix(authURL, idp.srv.URL+"/authorize") {
		idp.t.Fatalf("start redirected to %q, not to the provider", authURL)
	}
	q := u.Query()
	if q.Get("client_id") != idpClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
		!strings.Contains(q.Get("scope"), "openid") || q.Get("nonce") == "" || q.Get("state") == "" {
		idp.t.Fatalf("bad authorization request: %s", u.RawQuery)
	}
	code = randomToken()
	idp.mu.Lock()
	idp.codes[code] = idpGrant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri"), claims: claims}
	idp.mu.Unlock()
	return code, q.Get("state")
}

func (idp *fakeIDP) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	idp.mu.Lock()
	g, found := idp.codes[r.PostForm.Get("code")]
	delete(idp.codes, r.PostForm.Get("code"))
	refuse := idp.tokenRefuses
	idp.mu.Unlock()

	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	switch {
	case refuse, !found:
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	case id != idpClientID || secret != idpSecret:
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "invalid_client"})
		return
	case base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge:
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant", "error_description": "PKCE verifier does not match"})
		return
	case r.PostForm.Get("redirect_uri") != g.redirectURI:
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant", "error_description": "redirect_uri"})
		return
	}

	claims := jwt.MapClaims{"iss": idp.srv.URL, "aud": idpClientID, "nonce": g.nonce,
		"iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix()}
	for k, v := range g.claims {
		claims[k] = v
	}
	if idp.wrongNonce {
		claims["nonce"] = "not-the-nonce"
	}
	if idp.wrongAud {
		claims["aud"] = "someone-else"
	}
	key := idp.key
	if idp.otherKey != nil {
		key = idp.otherKey
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	signed, err := tok.SignedString(key)
	if err != nil {
		idp.t.Errorf("sign id token: %v", err)
	}
	resp := map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 300, "id_token": signed}
	if idp.noIDToken {
		delete(resp, "id_token")
	}
	writeJSON(w, resp)
}

// ---- a store with providers and identities --------------------------------

type identityKey struct {
	provider uuid.UUID
	subject  string
}

// oidcMemStore adds providers and identities to memStore, with the rules of
// the auth_svc tables.
type oidcMemStore struct {
	*memStore
	providers  []OIDCProvider
	identities map[identityKey]uuid.UUID
}

func (s *oidcMemStore) ListOIDCProviders(_ context.Context, enabledOnly bool) ([]OIDCProvider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []OIDCProvider
	for _, p := range s.providers {
		if p.Enabled || !enabledOnly {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *oidcMemStore) FindOIDCProviderBySlug(_ context.Context, slug string) (OIDCProvider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.providers {
		if p.Slug == slug {
			return p, nil
		}
	}
	return OIDCProvider{}, ErrProviderNotFound
}

func (s *oidcMemStore) FindUserByIdentity(_ context.Context, providerID uuid.UUID, subject string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.identities[identityKey{providerID, subject}]; ok {
		return s.users[id], nil
	}
	return User{}, ErrNotFound
}

func (s *oidcMemStore) linkLocked(providerID uuid.UUID, subject string, userID uuid.UUID) error {
	for k, u := range s.identities {
		if k == (identityKey{providerID, subject}) || (k.provider == providerID && u == userID) {
			return ErrIdentityTaken
		}
	}
	s.identities[identityKey{providerID, subject}] = userID
	return nil
}

func (s *oidcMemStore) LinkIdentity(_ context.Context, providerID uuid.UUID, subject string, userID uuid.UUID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.linkLocked(providerID, subject, userID)
}

func (s *oidcMemStore) CreateUserWithIdentity(_ context.Context, u User, providerID uuid.UUID, subject, _ string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.identities[identityKey{providerID, subject}]; ok {
		return User{}, ErrIdentityTaken
	}
	created, err := s.createLocked(u)
	if err != nil {
		return User{}, err
	}
	s.identities[identityKey{providerID, subject}] = created.ID
	return created, nil
}

func (s *oidcMemStore) TouchIdentity(context.Context, uuid.UUID, string, string) error { return nil }

// ---- harness ---------------------------------------------------------------

type oidcHarness struct {
	*harness
	store    *oidcMemStore
	idp      *fakeIDP
	provider OIDCProvider
}

func newOIDCHarness(t *testing.T) *oidcHarness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	key := make([]byte, secretbox.KeySize)
	_, _ = rand.Read(key)
	box, err := secretbox.New(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	ext, _ := url.Parse("https://git.example.test")
	settings := OIDCSettings{Box: box, ExternalURL: ext}

	idp := newFakeIDP(t)
	store := &oidcMemStore{memStore: newMemStore(), identities: map[identityKey]uuid.UUID{}}
	p := OIDCProvider{ID: uuid.New(), Slug: "company", DisplayName: "Company SSO", Issuer: idp.srv.URL, ClientID: idpClientID, Enabled: true}
	if p.SealedSecret, err = settings.SealClientSecret(p.ID, idpSecret); err != nil {
		t.Fatal(err)
	}
	store.providers = []OIDCProvider{p}

	jwtMgr := authjwt.NewManager("test-secret-that-is-at-least-32-chars", time.Minute, time.Hour)
	h, err := NewHandler(Deps{Users: store, Providers: store, OIDC: settings, JWT: jwtMgr, Redis: rdb})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Route("/api", h.Mount)
	return &oidcHarness{
		harness:  &harness{t: t, router: r, store: store.memStore, jwt: jwtMgr, redis: rdb, mr: mr},
		store:    store,
		idp:      idp,
		provider: p,
	}
}

// callback runs start and the provider's sign-in, and returns the answer to
// the provider's redirect back to git2.
func (h *oidcHarness) callback(returnTo string, claims map[string]any) *httptest.ResponseRecorder {
	h.t.Helper()
	path := "/api/auth/oidc/company/start"
	if returnTo != "" {
		path += "?return_to=" + url.QueryEscape(returnTo)
	}
	start := h.do(http.MethodGet, path, "", nil)
	if start.Code != http.StatusFound {
		h.t.Fatalf("start: status %d body %s", start.Code, start.Body)
	}
	code, state := h.idp.authorize(start.Header().Get("Location"), claims)
	return h.do(http.MethodGet, "/api/auth/oidc/company/callback?"+url.Values{"code": {code}, "state": {state}}.Encode(), "", nil)
}

// signIn runs the whole flow and returns the exchange answer.
func (h *oidcHarness) signIn(claims map[string]any) *httptest.ResponseRecorder {
	h.t.Helper()
	cb := h.callback("", claims)
	code := handoffCode(h.t, cb, "/sign-in/callback")
	return h.do(http.MethodPost, "/api/auth/oidc/exchange", "", map[string]string{"code": code})
}

// handoffCode checks that rec redirects to page with a code and returns it.
func handoffCode(t *testing.T, rec *httptest.ResponseRecorder, page string) string {
	t.Helper()
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || loc == nil || loc.Path != page || loc.Query().Get("code") == "" {
		t.Fatalf("callback: status %d, Location %q; want 303 to %s?code=…", rec.Code, rec.Header().Get("Location"), page)
	}
	return loc.Query().Get("code")
}

// ssoError checks that rec sends the browser to page with ?sso_error=reason.
func ssoError(t *testing.T, rec *httptest.ResponseRecorder, page, reason string) {
	t.Helper()
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || loc == nil || loc.Path != page || loc.Query().Get("sso_error") != reason {
		t.Errorf("status %d, Location %q; want 303 to %s?sso_error=%s", rec.Code, rec.Header().Get("Location"), page, reason)
	}
}

func claimsFor(sub, email string, verified bool, username string) map[string]any {
	return map[string]any{"sub": sub, "email": email, "email_verified": verified, "preferred_username": username}
}

// ---- tests -------------------------------------------------------------------

func TestOIDCProvidersList(t *testing.T) {
	h := newOIDCHarness(t)
	h.store.providers = append(h.store.providers,
		OIDCProvider{ID: uuid.New(), Slug: "off", DisplayName: "Disabled", Enabled: false},
		OIDCProvider{ID: uuid.New(), Slug: "azure", DisplayName: "Azure AD", Enabled: true})
	rec := h.do(http.MethodGet, "/api/auth/oidc/providers", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct{ Providers []SignInProvider }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Providers) != 2 || got.Providers[0].Slug != "azure" || got.Providers[1].Slug != "company" {
		t.Errorf("providers = %+v, want the enabled ones by display name", got.Providers)
	}
	if strings.Contains(rec.Body.String(), "issuer") || strings.Contains(rec.Body.String(), "client") {
		t.Errorf("sign-in list leaks provider settings: %s", rec.Body)
	}
}

// FR-22: the first sign-in creates the account; the next ones reuse it.
func TestOIDCFirstSignInCreatesTheAccount(t *testing.T) {
	h := newOIDCHarness(t)
	rec := h.signIn(claimsFor("sub-ada", "Ada@Example.com", true, "Ada.Lovelace"))
	if rec.Code != http.StatusOK || rec.Header().Get("X-Access-Token") == "" || rec.Header().Get("X-Refresh-Token") == "" {
		t.Fatalf("exchange: status %d headers %v body %s", rec.Code, rec.Header(), rec.Body)
	}
	if strings.Contains(rec.Body.String(), rec.Header().Get("X-Access-Token")) {
		t.Error("token in the response body")
	}
	var body LoginResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.User.Email != "ada@example.com" || body.User.Username != "ada-lovelace" || body.User.Role != RoleUser {
		t.Errorf("created %+v", body.User)
	}

	// Signed in with the new tokens.
	if me := h.do(http.MethodGet, "/api/auth/me", rec.Header().Get("X-Access-Token"), nil); me.Code != http.StatusOK {
		t.Errorf("/me with the new token: status %d", me.Code)
	}
	// The account has no password: password sign-in is refused.
	if rec := h.do(http.MethodPost, "/api/auth/login", "", LoginRequest{Email: "ada@example.com", Password: "anything-at-all"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("password sign-in of an SSO-only account: status %d, want 401", rec.Code)
	}

	// Second sign-in: same account, none created.
	again := h.signIn(claimsFor("sub-ada", "ada@example.com", true, "someone-else"))
	var body2 LoginResponse
	_ = json.Unmarshal(again.Body.Bytes(), &body2)
	if again.Code != http.StatusOK || body2.User.ID != body.User.ID || len(h.store.users) != 1 {
		t.Errorf("second sign-in: status %d user %v, %d users", again.Code, body2.User.ID, len(h.store.users))
	}
}

func TestOIDCLinksAVerifiedEmail(t *testing.T) {
	h := newOIDCHarness(t)
	h.register("grace@example.com", "grace")
	rec := h.signIn(claimsFor("sub-grace", "grace@example.com", true, ""))
	var body LoginResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusOK || body.User.Username != "grace" || len(h.store.users) != 1 {
		t.Fatalf("status %d user %+v, %d users; want the existing account", rec.Code, body.User, len(h.store.users))
	}
	if _, ok := h.store.identities[identityKey{h.provider.ID, "sub-grace"}]; !ok {
		t.Error("identity not linked")
	}
	// email_verified as the string "true", as some providers send it.
	h.register("hopper@example.com", "hopper")
	claims := claimsFor("sub-hopper", "hopper@example.com", true, "")
	claims["email_verified"] = "true"
	if rec := h.signIn(claims); rec.Code != http.StatusOK {
		t.Errorf(`email_verified "true": status %d`, rec.Code)
	}
}

// An unverified email that matches an account is refused: linking on it
// would hand the account to whoever set that address at the provider.
func TestOIDCRefusesAnUnverifiedEmailThatIsTaken(t *testing.T) {
	h := newOIDCHarness(t)
	h.register("grace@example.com", "grace")
	ssoError(t, h.callback("", claimsFor("sub-x", "grace@example.com", false, "")), "/sign-in", ssoEmailInUse)
	if len(h.store.identities) != 0 {
		t.Error("identity linked on an unverified email")
	}
}

func TestOIDCRefusesADisabledAccount(t *testing.T) {
	h := newOIDCHarness(t)
	h.signIn(claimsFor("sub-ada", "ada@example.com", true, "ada"))
	h.store.setActive("ada@example.com", false)
	ssoError(t, h.callback("", claimsFor("sub-ada", "ada@example.com", true, "ada")), "/sign-in", ssoAccountDisabled)
}

func TestOIDCPicksAFreeUsername(t *testing.T) {
	h := newOIDCHarness(t)
	h.register("ada@other.example", "ada")
	rec := h.signIn(claimsFor("sub-ada", "ada@example.com", true, "ada"))
	var body LoginResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.User.Username != "ada-2" {
		t.Errorf("username = %q, want ada-2", body.User.Username)
	}
	// No preferred_username: the part of the email before "@".
	rec = h.signIn(claimsFor("sub-bob", "bob.smith@example.com", true, ""))
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.User.Username != "bob-smith" {
		t.Errorf("username = %q, want bob-smith", body.User.Username)
	}
}

// Everything the callback checks: a failed check never signs anyone in.
func TestOIDCCallbackChecks(t *testing.T) {
	ada := claimsFor("sub-ada", "ada@example.com", true, "ada")
	tests := []struct {
		name   string
		fault  func(*fakeIDP)
		reason string
	}{
		{"id token signed with another key", func(i *fakeIDP) { i.otherKey, _ = rsa.GenerateKey(rand.Reader, 2048) }, ssoFailed},
		{"wrong nonce", func(i *fakeIDP) { i.wrongNonce = true }, ssoFailed},
		{"wrong audience", func(i *fakeIDP) { i.wrongAud = true }, ssoFailed},
		{"no id token", func(i *fakeIDP) { i.noIDToken = true }, ssoFailed},
		{"provider refuses the code", func(i *fakeIDP) { i.tokenRefuses = true }, ssoFailed},
		{"provider down at callback", func(i *fakeIDP) { i.srv.Close() }, ssoProviderUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newOIDCHarness(t)
			start := h.do(http.MethodGet, "/api/auth/oidc/company/start", "", nil)
			code, state := h.idp.authorize(start.Header().Get("Location"), ada)
			tt.fault(h.idp)
			rec := h.do(http.MethodGet, "/api/auth/oidc/company/callback?"+url.Values{"code": {code}, "state": {state}}.Encode(), "", nil)
			ssoError(t, rec, "/sign-in", tt.reason)
			if len(h.store.users) != 0 {
				t.Error("an account was created")
			}
		})
	}
}

func TestOIDCStateWorksOnce(t *testing.T) {
	h := newOIDCHarness(t)
	start := h.do(http.MethodGet, "/api/auth/oidc/company/start", "", nil)
	ada := claimsFor("sub-ada", "ada@example.com", true, "ada")
	// Two valid codes for the same state: the provider would accept both,
	// so only git2's own state check can refuse the second.
	code1, state := h.idp.authorize(start.Header().Get("Location"), ada)
	code2, _ := h.idp.authorize(start.Header().Get("Location"), ada)
	handoffCode(t, h.do(http.MethodGet, "/api/auth/oidc/company/callback?"+url.Values{"code": {code1}, "state": {state}}.Encode(), "", nil), "/sign-in/callback")
	ssoError(t, h.do(http.MethodGet, "/api/auth/oidc/company/callback?"+url.Values{"code": {code2}, "state": {state}}.Encode(), "", nil), "/sign-in", ssoFailed)

	ssoError(t, h.do(http.MethodGet, "/api/auth/oidc/company/callback?code=x&state=forged", "", nil), "/sign-in", ssoFailed)
	ssoError(t, h.do(http.MethodGet, "/api/auth/oidc/company/callback?code=x", "", nil), "/sign-in", ssoFailed)

	// A state expires after 10 minutes.
	start = h.do(http.MethodGet, "/api/auth/oidc/company/start", "", nil)
	code3, state3 := h.idp.authorize(start.Header().Get("Location"), ada)
	h.mr.FastForward(oidcStateTTL + time.Second)
	ssoError(t, h.do(http.MethodGet, "/api/auth/oidc/company/callback?"+url.Values{"code": {code3}, "state": {state3}}.Encode(), "", nil), "/sign-in", ssoFailed)
}

func TestOIDCUserCancelled(t *testing.T) {
	h := newOIDCHarness(t)
	start := h.do(http.MethodGet, "/api/auth/oidc/company/start?return_to=%2Fadmin%2Fusers", "", nil)
	_, state := h.idp.authorize(start.Header().Get("Location"), nil)
	ssoError(t, h.do(http.MethodGet, "/api/auth/oidc/company/callback?error=access_denied&state="+state, "", nil), "/admin/sign-in", ssoCancelled)
}

func TestOIDCReturnTo(t *testing.T) {
	h := newOIDCHarness(t)
	// A path under /admin/ comes back through Admin's callback page.
	cb := h.callback("/admin/users?page=2", claimsFor("sub-ada", "ada@example.com", true, "ada"))
	handoffCode(t, cb, "/admin/sign-in/callback")
	loc, _ := url.Parse(cb.Header().Get("Location"))
	if loc.Query().Get("return_to") != "/admin/users?page=2" {
		t.Errorf("return_to = %q", loc.Query().Get("return_to"))
	}
	if loc.Host != "" {
		t.Errorf("callback redirect leaves the instance: %q", cb.Header().Get("Location"))
	}

	for _, bad := range []string{"https://evil.example", "//evil.example", `/\evil.example`, "relative", "/x\ny"} {
		if rec := h.do(http.MethodGet, "/api/auth/oidc/company/start?return_to="+url.QueryEscape(bad), "", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("return_to %q: status %d, want 400", bad, rec.Code)
		}
	}
}

func TestOIDCStartErrors(t *testing.T) {
	h := newOIDCHarness(t)
	if rec := h.do(http.MethodGet, "/api/auth/oidc/nope/start", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown provider: status %d, want 404", rec.Code)
	}
	h.store.providers[0].Enabled = false
	if rec := h.do(http.MethodGet, "/api/auth/oidc/company/start", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("disabled provider: status %d, want 404", rec.Code)
	}
	h.store.providers[0].Enabled = true
	h.idp.srv.Close()
	ssoError(t, h.do(http.MethodGet, "/api/auth/oidc/company/start", "", nil), "/sign-in", ssoProviderUnavailable)
}

func TestOIDCExchangeCodeWorksOnce(t *testing.T) {
	h := newOIDCHarness(t)
	code := handoffCode(t, h.callback("", claimsFor("sub-ada", "ada@example.com", true, "ada")), "/sign-in/callback")
	if rec := h.do(http.MethodPost, "/api/auth/oidc/exchange", "", map[string]string{"code": code}); rec.Code != http.StatusOK {
		t.Fatalf("first exchange: status %d", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/api/auth/oidc/exchange", "", map[string]string{"code": code}); rec.Code != http.StatusUnauthorized {
		t.Errorf("second exchange: status %d, want 401", rec.Code)
	}

	code = handoffCode(t, h.callback("", claimsFor("sub-ada", "ada@example.com", true, "ada")), "/sign-in/callback")
	h.mr.FastForward(oidcHandoffTTL + time.Second)
	if rec := h.do(http.MethodPost, "/api/auth/oidc/exchange", "", map[string]string{"code": code}); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired code: status %d, want 401", rec.Code)
	}

	// Disabled between the callback and the exchange.
	code = handoffCode(t, h.callback("", claimsFor("sub-ada", "ada@example.com", true, "ada")), "/sign-in/callback")
	h.store.setActive("ada@example.com", false)
	if rec := h.do(http.MethodPost, "/api/auth/oidc/exchange", "", map[string]string{"code": code}); rec.Code != http.StatusUnauthorized {
		t.Errorf("disabled meanwhile: status %d, want 401", rec.Code)
	}

	if rec := h.do(http.MethodPost, "/api/auth/oidc/exchange", "", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("no code: status %d, want 400", rec.Code)
	}
}

func TestOIDCOff(t *testing.T) {
	h := newOIDCHarness(t)
	_ = h // the provider exists, but without a key or URL OIDC is off:
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	hd, _ := NewHandler(Deps{Users: h.store, Providers: h.store, OIDC: OIDCSettings{}, JWT: h.jwt, Redis: rdb})
	r := chi.NewRouter()
	r.Route("/api", hd.Mount)
	for path, want := range map[string]int{"/api/auth/oidc/providers": http.StatusOK, "/api/auth/oidc/company/start": http.StatusNotFound} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want || (want == http.StatusOK && !strings.Contains(rec.Body.String(), `"providers":[]`)) {
			t.Errorf("%s: status %d body %s", path, rec.Code, rec.Body)
		}
	}
}

func TestUsernameCandidates(t *testing.T) {
	got := usernameCandidates("Ada.Lovelace", "ada@example.com")
	if len(got) < 4 || got[0] != "ada-lovelace" || got[1] != "ada" || got[2] != "ada-lovelace-2" || got[3] != "ada-2" {
		t.Errorf("first candidates = %v", got[:min(len(got), 4)])
	}
	// Nothing usable from the provider: "user", then "user-2".
	if got := usernameCandidates("", "@example.com"); len(got) < 2 || got[0] != "user" || got[1] != "user-2" {
		t.Errorf("fallback candidates = %v", got[:min(len(got), 2)])
	}
	for _, c := range got {
		if validateUsername(c) != nil {
			t.Errorf("invalid candidate %q", c)
		}
	}
	long := usernameCandidates(strings.Repeat("a", 60), "x@example.com")
	for _, c := range long {
		if len(c) > 39 {
			t.Errorf("candidate %q is longer than 39", c)
		}
	}
	for in, want := range map[string]string{"--Ada__": "ada__", "é.x.y": "x-y", "ab": "", "  Bob Smith ": "bob-smith"} {
		if got := cleanUsername(in); got != want {
			t.Errorf("cleanUsername(%q) = %q, want %q", in, got, want)
		}
	}
}
