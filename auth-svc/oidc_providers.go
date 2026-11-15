package authsvc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/git2-project/git2/internal/secretbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OIDC providers are configured by admins in admin-api and used for sign-in
// by auth-svc (docs/design/oidc-v0.md). This file holds their storage and
// the checks admin-api runs before saving one.

var (
	ErrProviderNotFound = errors.New("oidc provider not found")
	ErrProviderExists   = errors.New("oidc provider slug already taken")
)

type OIDCProvider struct {
	ID          uuid.UUID
	Slug        string
	DisplayName string
	Issuer      string
	ClientID    string
	// SealedSecret is the client secret sealed with SealClientSecret.
	SealedSecret []byte
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// OIDCSettings is what OIDC needs from the instance configuration. Both are
// required; without either, OIDC is off.
type OIDCSettings struct {
	Box         *secretbox.Box // GIT2_SECRET_KEY
	ExternalURL *url.URL       // GIT2_EXTERNAL_URL
	// HTTPClient talks to providers; nil means one with a 10 s timeout.
	HTTPClient *http.Client
}

// ErrOIDCOff means GIT2_SECRET_KEY or GIT2_EXTERNAL_URL is not set.
var ErrOIDCOff = errors.New("OIDC needs GIT2_SECRET_KEY and GIT2_EXTERNAL_URL")

func (s OIDCSettings) On() bool { return s.Box != nil && s.ExternalURL != nil }

func (s OIDCSettings) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// RedirectURI is what the admin registers at the provider. "" when OIDC is
// off.
func (s OIDCSettings) RedirectURI(slug string) string {
	if s.ExternalURL == nil {
		return ""
	}
	return s.ExternalURL.JoinPath("/api/auth/oidc", slug, "callback").String()
}

func secretContext(id uuid.UUID) string {
	return "auth_svc.oidc_providers.client_secret:" + id.String()
}

// SealClientSecret encrypts a provider's client secret, bound to its id.
func (s OIDCSettings) SealClientSecret(id uuid.UUID, secret string) ([]byte, error) {
	if !s.On() {
		return nil, ErrOIDCOff
	}
	return s.Box.Seal([]byte(secret), secretContext(id))
}

// ClientSecret decrypts a provider's client secret.
func (s OIDCSettings) ClientSecret(p OIDCProvider) (string, error) {
	if !s.On() {
		return "", ErrOIDCOff
	}
	plain, err := s.Box.Open(p.SealedSecret, secretContext(p.ID))
	if err != nil {
		return "", fmt.Errorf("provider %s: %w", p.Slug, err)
	}
	return string(plain), nil
}

// CheckIssuer fetches the issuer's discovery document and checks that its
// issuer is exactly this one and that it has the endpoints sign-in needs
// (FR-21). A problem with what the admin entered is an *InputError with the
// reason; nothing else is returned.
func (s OIDCSettings) CheckIssuer(ctx context.Context, issuer string) error {
	u, err := url.Parse(issuer)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return &InputError{msg: "issuer must be an http(s) URL without query or fragment"}
	}
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, s.httpClient()), 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return &InputError{msg: "issuer check failed: " + err.Error()}
	}
	if ep := p.Endpoint(); ep.AuthURL == "" || ep.TokenURL == "" {
		return &InputError{msg: "issuer check failed: the discovery document has no authorization or token endpoint"}
	}
	return nil
}

// CheckProviderSlug validates a provider slug: the team-name rule
// (oidc_providers_slug_rule).
func CheckProviderSlug(s string) error {
	if !usernamePattern.MatchString(s) {
		return &InputError{msg: "slug must be 3-39 characters of a-z, 0-9, '-' or '_', starting with a letter or digit"}
	}
	return nil
}

// CheckProviderFields validates the fields an admin sends. Pointers that are
// nil are not checked (a PATCH leaves them unchanged).
func CheckProviderFields(displayName, issuer, clientID, clientSecret *string) error {
	var errs []error
	if displayName != nil && (strings.TrimSpace(*displayName) == "" || utf8.RuneCountInString(*displayName) > 100) {
		errs = append(errs, &InputError{msg: "display_name must be 1-100 characters"})
	}
	if issuer != nil && (*issuer == "" || len(*issuer) > 2048) {
		errs = append(errs, &InputError{msg: "issuer must be 1-2048 characters"})
	}
	if clientID != nil && (*clientID == "" || len(*clientID) > 255) {
		errs = append(errs, &InputError{msg: "client_id must be 1-255 characters"})
	}
	if clientSecret != nil && (*clientSecret == "" || len(*clientSecret) > 1024) {
		errs = append(errs, &InputError{msg: "client_secret must be 1-1024 characters"})
	}
	if len(errs) == 0 {
		return nil
	}
	return &InputError{msg: errors.Join(errs...).Error()}
}

const providerColumns = `id, slug, display_name, issuer, client_id, client_secret_sealed, enabled, created_at, updated_at`

func scanProvider(row pgx.Row) (OIDCProvider, error) {
	var p OIDCProvider
	err := row.Scan(&p.ID, &p.Slug, &p.DisplayName, &p.Issuer, &p.ClientID, &p.SealedSecret, &p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// ListOIDCProviders returns every provider, oldest first. enabledOnly keeps
// the enabled ones, for the sign-in page.
func (s *PostgresStore) ListOIDCProviders(ctx context.Context, enabledOnly bool) ([]OIDCProvider, error) {
	rows, err := s.db.Query(ctx, `SELECT `+providerColumns+` FROM auth_svc.oidc_providers WHERE enabled OR NOT $1 ORDER BY id`, enabledOnly)
	if err != nil {
		return nil, fmt.Errorf("list oidc providers: %w", err)
	}
	ps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (OIDCProvider, error) { return scanProvider(row) })
	if err != nil {
		return nil, fmt.Errorf("list oidc providers: %w", err)
	}
	return ps, nil
}

// FindOIDCProvider returns the provider by id. ErrProviderNotFound.
func (s *PostgresStore) FindOIDCProvider(ctx context.Context, id uuid.UUID) (OIDCProvider, error) {
	return s.findProvider(ctx, `SELECT `+providerColumns+` FROM auth_svc.oidc_providers WHERE id = $1`, id)
}

// FindOIDCProviderBySlug returns the provider by slug, enabled or not.
// ErrProviderNotFound.
func (s *PostgresStore) FindOIDCProviderBySlug(ctx context.Context, slug string) (OIDCProvider, error) {
	return s.findProvider(ctx, `SELECT `+providerColumns+` FROM auth_svc.oidc_providers WHERE slug = $1`, slug)
}

func (s *PostgresStore) findProvider(ctx context.Context, q string, arg any) (OIDCProvider, error) {
	p, err := scanProvider(s.db.QueryRow(ctx, q, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return OIDCProvider{}, ErrProviderNotFound
	}
	if err != nil {
		return OIDCProvider{}, fmt.Errorf("find oidc provider: %w", err)
	}
	return p, nil
}

// CreateOIDCProvider stores p. The caller sets p.ID (the sealed secret is
// bound to it). ErrProviderExists.
func (s *PostgresStore) CreateOIDCProvider(ctx context.Context, p OIDCProvider) (OIDCProvider, error) {
	created, err := scanProvider(s.db.QueryRow(ctx, `
		INSERT INTO auth_svc.oidc_providers (id, slug, display_name, issuer, client_id, client_secret_sealed, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+providerColumns, p.ID, p.Slug, p.DisplayName, p.Issuer, p.ClientID, p.SealedSecret, p.Enabled))
	if isUniqueViolation(err, "oidc_providers_slug_key") {
		return OIDCProvider{}, ErrProviderExists
	}
	if err != nil {
		return OIDCProvider{}, fmt.Errorf("insert oidc provider: %w", err)
	}
	return created, nil
}

// OIDCProviderUpdate holds the fields an admin may change; nil leaves one as
// it is. The slug never changes: it is part of the registered redirect URI.
type OIDCProviderUpdate struct {
	DisplayName  *string
	Issuer       *string
	ClientID     *string
	SealedSecret []byte // nil: keep the secret
	Enabled      *bool
}

// UpdateOIDCProvider applies u and returns the provider. ErrProviderNotFound.
func (s *PostgresStore) UpdateOIDCProvider(ctx context.Context, id uuid.UUID, u OIDCProviderUpdate) (OIDCProvider, error) {
	p, err := scanProvider(s.db.QueryRow(ctx, `
		UPDATE auth_svc.oidc_providers SET
			display_name = COALESCE($2, display_name),
			issuer = COALESCE($3, issuer),
			client_id = COALESCE($4, client_id),
			client_secret_sealed = COALESCE($5, client_secret_sealed),
			enabled = COALESCE($6, enabled),
			updated_at = now()
		WHERE id = $1
		RETURNING `+providerColumns, id, u.DisplayName, u.Issuer, u.ClientID, u.SealedSecret, u.Enabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return OIDCProvider{}, ErrProviderNotFound
	}
	if err != nil {
		return OIDCProvider{}, fmt.Errorf("update oidc provider: %w", err)
	}
	return p, nil
}

// DeleteOIDCProvider deletes the provider and its identity links (ON DELETE
// CASCADE); the users stay. ErrProviderNotFound.
func (s *PostgresStore) DeleteOIDCProvider(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM auth_svc.oidc_providers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete oidc provider: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProviderNotFound
	}
	return nil
}
