package authsvc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/git2-project/git2/internal/httpx"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
)

// OIDC sign-in (FR-22, UC-06, docs/design/oidc-v0.md):
//
//	start     → state, nonce and PKCE verifier in Redis, redirect to the provider
//	callback  → check state, code and ID token; find, link or create the account;
//	            redirect to the web app with a one-time handoff code
//	exchange  → the web app trades the code for tokens in the usual headers
//
// Tokens never appear in a URL, and every failure comes back to the sign-in
// page as ?sso_error=<reason>.

// OIDCSignInStore is what sign-in needs from persistence. PostgresStore
// implements it.
type OIDCSignInStore interface {
	ListOIDCProviders(ctx context.Context, enabledOnly bool) ([]OIDCProvider, error)
	FindOIDCProviderBySlug(ctx context.Context, slug string) (OIDCProvider, error)
	FindUserByIdentity(ctx context.Context, providerID uuid.UUID, subject string) (User, error)
	LinkIdentity(ctx context.Context, providerID uuid.UUID, subject string, userID uuid.UUID, email string) error
	CreateUserWithIdentity(ctx context.Context, u User, providerID uuid.UUID, subject, email string) (User, error)
	TouchIdentity(ctx context.Context, providerID uuid.UUID, subject, email string) error
}

const (
	oidcStatePrefix   = "oidc-state:"
	oidcHandoffPrefix = "oidc-handoff:"
	oidcStateTTL      = 10 * time.Minute
	oidcHandoffTTL    = 60 * time.Second
)

// The reasons a sign-in comes back with, in ?sso_error=.
const (
	ssoCancelled           = "cancelled"
	ssoProviderUnavailable = "provider_unavailable"
	ssoAccountDisabled     = "account_disabled"
	ssoEmailInUse          = "email_in_use"
	ssoFailed              = "failed"
)

// oidcState is what start keeps for callback, under oidc-state:<state>.
type oidcState struct {
	Slug     string `json:"slug"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	ReturnTo string `json:"return_to"`
}

type SignInProvider struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}

// OIDCProviders lists the enabled providers for the sign-in buttons. With
// OIDC off the list is empty.
func (h *Handler) OIDCProviders(w http.ResponseWriter, r *http.Request) {
	out := []SignInProvider{}
	if h.oidcOn() {
		ps, err := h.deps.Providers.ListOIDCProviders(r.Context(), true)
		if err != nil {
			slog.Error("oidc: list providers", "err", err)
			httpx.Error(w, http.StatusServiceUnavailable, "could not list sign-in providers, try again")
			return
		}
		for _, p := range ps {
			out = append(out, SignInProvider{Slug: p.Slug, DisplayName: p.DisplayName})
		}
		slices.SortFunc(out, func(a, b SignInProvider) int { return strings.Compare(a.DisplayName, b.DisplayName) })
	}
	httpx.JSON(w, http.StatusOK, map[string][]SignInProvider{"providers": out})
}

// OIDCStart sends the browser to the provider.
func (h *Handler) OIDCStart(w http.ResponseWriter, r *http.Request) {
	returnTo, ok := checkReturnTo(r.URL.Query().Get("return_to"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "return_to must be a path on this instance")
		return
	}
	p, ok := h.enabledProvider(r.Context(), chi.URLParam(r, "slug"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "no enabled sign-in provider with this name")
		return
	}
	_, cfg, err := h.providerClient(r.Context(), p)
	if err != nil {
		slog.Warn("oidc: provider unavailable at start", "provider", p.Slug, "err", err)
		ssoRedirect(w, r, returnTo, ssoProviderUnavailable)
		return
	}

	st := oidcState{Slug: p.Slug, Nonce: randomToken(), Verifier: oauth2.GenerateVerifier(), ReturnTo: returnTo}
	state := randomToken()
	data, _ := json.Marshal(st)
	if err := h.deps.Redis.Set(r.Context(), oidcStatePrefix+state, data, oidcStateTTL).Err(); err != nil {
		slog.Error("oidc: store state", "err", err)
		ssoRedirect(w, r, returnTo, ssoFailed)
		return
	}
	http.Redirect(w, r, cfg.AuthCodeURL(state, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier)), http.StatusFound)
}

// OIDCCallback is where the provider sends the browser back.
func (h *Handler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	if state == "" {
		ssoRedirect(w, r, "/", ssoFailed)
		return
	}
	// The state works once: GETDEL, so a replayed callback finds nothing.
	raw, err := h.deps.Redis.GetDel(r.Context(), oidcStatePrefix+state).Bytes()
	var st oidcState
	if err == nil {
		err = json.Unmarshal(raw, &st)
	}
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Error("oidc: read state", "err", err)
		}
		ssoRedirect(w, r, "/", ssoFailed)
		return
	}
	if st.Slug != chi.URLParam(r, "slug") {
		ssoRedirect(w, r, st.ReturnTo, ssoFailed)
		return
	}
	if e := q.Get("error"); e != "" {
		reason := ssoFailed
		if e == "access_denied" {
			reason = ssoCancelled
		}
		ssoRedirect(w, r, st.ReturnTo, reason)
		return
	}
	code := q.Get("code")
	p, ok := h.enabledProvider(r.Context(), st.Slug)
	if code == "" || !ok {
		ssoRedirect(w, r, st.ReturnTo, ssoFailed)
		return
	}

	claims, reason := h.verifyCallback(r.Context(), p, code, st)
	if reason != "" {
		ssoRedirect(w, r, st.ReturnTo, reason)
		return
	}
	u, reason := h.resolveOIDCUser(r.Context(), p, claims)
	if reason != "" {
		ssoRedirect(w, r, st.ReturnTo, reason)
		return
	}
	// Refused after the provider accepted the user (UC-06 E3).
	if !u.IsActive {
		ssoRedirect(w, r, st.ReturnTo, ssoAccountDisabled)
		return
	}

	handoff := randomToken()
	if err := h.deps.Redis.Set(r.Context(), oidcHandoffPrefix+handoff, u.ID.String(), oidcHandoffTTL).Err(); err != nil {
		slog.Error("oidc: store handoff code", "err", err)
		ssoRedirect(w, r, st.ReturnTo, ssoFailed)
		return
	}
	v := url.Values{"code": {handoff}, "return_to": {st.ReturnTo}}
	http.Redirect(w, r, signInPage(st.ReturnTo)+"/callback?"+v.Encode(), http.StatusSeeOther)
}

type exchangeRequest struct {
	Code string `json:"code"`
}

// OIDCExchange trades a one-time handoff code for a token pair.
func (h *Handler) OIDCExchange(w http.ResponseWriter, r *http.Request) {
	var req exchangeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil || req.Code == "" || len(req.Code) > 128 {
		httpx.Error(w, http.StatusBadRequest, "code is required")
		return
	}
	id, err := h.deps.Redis.GetDel(r.Context(), oidcHandoffPrefix+req.Code).Result()
	if errors.Is(err, redis.Nil) {
		httpx.Error(w, http.StatusUnauthorized, "invalid or expired sign-in code")
		return
	}
	if err != nil {
		slog.Error("oidc: read handoff code", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not sign in, try again")
		return
	}
	uid, err := uuid.Parse(id)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid or expired sign-in code")
		return
	}
	u, err := h.deps.Users.FindByID(r.Context(), uid)
	switch {
	case errors.Is(err, ErrNotFound) || (err == nil && !u.IsActive):
		httpx.Error(w, http.StatusUnauthorized, "account is not available")
		return
	case err != nil:
		slog.Error("oidc: find user", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "could not sign in, try again")
		return
	}
	h.issueSession(w, r, u)
}

func (h *Handler) oidcOn() bool { return h.deps.Providers != nil && h.deps.OIDC.On() }

// enabledProvider finds an enabled provider by slug. A database fault reads
// as "no provider" here: callers answer it like an unknown provider, and it
// is logged.
func (h *Handler) enabledProvider(ctx context.Context, slug string) (OIDCProvider, bool) {
	if !h.oidcOn() {
		return OIDCProvider{}, false
	}
	p, err := h.deps.Providers.FindOIDCProviderBySlug(ctx, slug)
	if err != nil {
		if !errors.Is(err, ErrProviderNotFound) {
			slog.Error("oidc: find provider", "provider", slug, "err", err)
		}
		return OIDCProvider{}, false
	}
	return p, p.Enabled
}

// providerClient fetches the provider's discovery document and builds the
// OAuth2 client for it.
func (h *Handler) providerClient(ctx context.Context, p OIDCProvider) (*oidc.Provider, *oauth2.Config, error) {
	secret, err := h.deps.OIDC.ClientSecret(p)
	if err != nil {
		return nil, nil, err
	}
	prov, err := oidc.NewProvider(h.providerContext(ctx), p.Issuer)
	if err != nil {
		return nil, nil, err
	}
	return prov, &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: secret,
		Endpoint:     prov.Endpoint(),
		RedirectURL:  h.deps.OIDC.RedirectURI(p.Slug),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}, nil
}

// providerContext makes go-oidc and oauth2 use the configured HTTP client.
func (h *Handler) providerContext(ctx context.Context) context.Context {
	c := h.deps.OIDC.httpClient()
	return context.WithValue(oidc.ClientContext(ctx, c), oauth2.HTTPClient, c)
}

// oidcClaims are the ID token claims sign-in reads besides sub.
type oidcClaims struct {
	Subject           string
	Email             string
	EmailVerified     bool
	PreferredUsername string
}

// verifyCallback trades the code for an ID token and checks it: signature,
// issuer, audience and expiry (go-oidc), and the nonce from start. It
// returns the claims, or the sso_error reason.
func (h *Handler) verifyCallback(ctx context.Context, p OIDCProvider, code string, st oidcState) (oidcClaims, string) {
	prov, cfg, err := h.providerClient(ctx, p)
	if err != nil {
		slog.Warn("oidc: provider unavailable at callback", "provider", p.Slug, "err", err)
		return oidcClaims{}, ssoProviderUnavailable
	}
	pctx := h.providerContext(ctx)
	tok, err := cfg.Exchange(pctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		// The provider answered and refused, or it could not be reached.
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			slog.Warn("oidc: code exchange refused", "provider", p.Slug, "err", err)
			return oidcClaims{}, ssoFailed
		}
		slog.Warn("oidc: token endpoint unavailable", "provider", p.Slug, "err", err)
		return oidcClaims{}, ssoProviderUnavailable
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		slog.Warn("oidc: token response has no id_token", "provider", p.Slug)
		return oidcClaims{}, ssoFailed
	}
	idt, err := prov.Verifier(&oidc.Config{ClientID: p.ClientID}).Verify(pctx, rawID)
	if err != nil {
		slog.Warn("oidc: id token refused", "provider", p.Slug, "err", err)
		return oidcClaims{}, ssoFailed
	}
	if idt.Nonce != st.Nonce {
		slog.Warn("oidc: id token nonce does not match", "provider", p.Slug)
		return oidcClaims{}, ssoFailed
	}
	var c struct {
		Email             string          `json:"email"`
		EmailVerified     json.RawMessage `json:"email_verified"`
		PreferredUsername string          `json:"preferred_username"`
	}
	if err := idt.Claims(&c); err != nil || idt.Subject == "" {
		slog.Warn("oidc: id token claims", "provider", p.Slug, "err", err)
		return oidcClaims{}, ssoFailed
	}
	// Some providers send email_verified as the string "true".
	verified, _ := strconv.ParseBool(strings.Trim(string(c.EmailVerified), `"`))
	return oidcClaims{Subject: idt.Subject, Email: c.Email, EmailVerified: verified, PreferredUsername: c.PreferredUsername}, ""
}

// resolveOIDCUser finds the account an identity belongs to
// (docs/design/oidc-v0.md, "Which account the identity belongs to"):
//
//  1. a known identity: its account;
//  2. an account with the same email, if the provider says it is verified:
//     link the identity to it;
//  3. no account with that email: create one and link it;
//  4. an account with that email but not verified: refused (email_in_use),
//     or anyone who can set that address at the provider could take it over.
//
// It returns the account, or the sso_error reason.
func (h *Handler) resolveOIDCUser(ctx context.Context, p OIDCProvider, c oidcClaims) (User, string) {
	store := h.deps.Providers
	email := normalizeEmail(c.Email)

	u, err := store.FindUserByIdentity(ctx, p.ID, c.Subject)
	if err == nil {
		if err := store.TouchIdentity(ctx, p.ID, c.Subject, email); err != nil {
			slog.Warn("oidc: touch identity", "err", err)
		}
		return u, ""
	}
	if !errors.Is(err, ErrNotFound) {
		slog.Error("oidc: find identity", "err", err)
		return User{}, ssoFailed
	}

	if err := validateEmail(email); err != nil {
		slog.Warn("oidc: provider gave no usable email", "provider", p.Slug, "err", err)
		return User{}, ssoFailed
	}
	existing, err := h.deps.Users.FindByEmail(ctx, email)
	switch {
	case err == nil:
		if !c.EmailVerified {
			return User{}, ssoEmailInUse
		}
		if err := store.LinkIdentity(ctx, p.ID, c.Subject, existing.ID, email); err != nil {
			if errors.Is(err, ErrIdentityTaken) {
				// The account already has another identity at this provider.
				return User{}, ssoEmailInUse
			}
			slog.Error("oidc: link identity", "err", err)
			return User{}, ssoFailed
		}
		return existing, ""
	case !errors.Is(err, ErrNotFound):
		slog.Error("oidc: find user by email", "err", err)
		return User{}, ssoFailed
	}

	for _, name := range usernameCandidates(c.PreferredUsername, email) {
		// No password: the account signs in through its providers only.
		created, err := store.CreateUserWithIdentity(ctx, User{
			Email: email, Username: name, IsActive: true, Role: RoleUser,
		}, p.ID, c.Subject, email)
		switch {
		case err == nil:
			return created, ""
		case errors.Is(err, ErrUsernameExists):
			continue
		case errors.Is(err, ErrIdentityTaken):
			// A concurrent first sign-in with the same identity won.
			if u, err := store.FindUserByIdentity(ctx, p.ID, c.Subject); err == nil {
				return u, ""
			}
			return User{}, ssoFailed
		case errors.Is(err, ErrEmailExists):
			// Another account took the email meanwhile.
			return User{}, ssoEmailInUse
		default:
			slog.Error("oidc: create user", "err", err)
			return User{}, ssoFailed
		}
	}
	slog.Warn("oidc: no free username", "provider", p.Slug, "email", email)
	return User{}, ssoFailed
}

// usernameCandidates lists usernames to try for a new account, in order:
// the provider's preferred_username, then the part of the email before "@",
// each cleaned to the username rule, then each with -2, -3, ... appended.
// "user" is the base only when neither gives a usable name.
func usernameCandidates(preferred, email string) []string {
	var bases []string
	for _, s := range []string{preferred, email[:max(0, strings.IndexByte(email, '@'))]} {
		if b := cleanUsername(s); b != "" && validateUsername(b) == nil && !slices.Contains(bases, b) {
			bases = append(bases, b)
		}
	}
	if len(bases) == 0 {
		bases = []string{"user"}
	}
	out := slices.Clone(bases)
	for n := 2; n <= 20; n++ {
		for _, b := range bases {
			suffix := "-" + strconv.Itoa(n)
			if name := strings.TrimRight(b[:min(len(b), 39-len(suffix))], "-_") + suffix; validateUsername(name) == nil {
				out = append(out, name)
			}
		}
	}
	return out
}

// cleanUsername lowercases s and turns it into the username alphabet: other
// characters become "-", runs of "-" collapse, and the name starts with a
// letter or digit. "" when fewer than 3 characters are left.
func cleanUsername(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			if !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	name := strings.TrimLeft(b.String(), "-_")
	name = strings.TrimRight(name[:min(len(name), 39)], "-")
	if len(name) < 3 {
		return ""
	}
	return name
}

// checkReturnTo accepts a path on this instance: one leading "/", not "//"
// or "/\" (which browsers read as another host), no control characters.
// "" means "/".
func checkReturnTo(s string) (string, bool) {
	if s == "" {
		return "/", true
	}
	if len(s) > 2048 || !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, `/\`) {
		return "", false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return s, true
}

// signInPage is the sign-in page of the plane returnTo belongs to.
func signInPage(returnTo string) string {
	if returnTo == "/admin" || strings.HasPrefix(returnTo, "/admin/") {
		return "/admin/sign-in"
	}
	return "/sign-in"
}

// ssoRedirect sends the browser back to the sign-in page with the reason.
func ssoRedirect(w http.ResponseWriter, r *http.Request, returnTo, reason string) {
	http.Redirect(w, r, signInPage(returnTo)+"?"+url.Values{"sso_error": {reason}}.Encode(), http.StatusSeeOther)
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never fails on supported platforms
	return base64.RawURLEncoding.EncodeToString(b)
}
