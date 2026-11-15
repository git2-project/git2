package authsvc

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestPostgresOIDCProviders(t *testing.T) {
	p := "op" + uuid.NewString()[:6]
	store, db := directoryDB(t, p)
	ctx := context.Background()
	t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM auth_svc.oidc_providers WHERE slug LIKE $1`, p+"%") })

	newProvider := func(slug string, enabled bool) OIDCProvider {
		t.Helper()
		created, err := store.CreateOIDCProvider(ctx, OIDCProvider{
			ID: uuid.Must(uuid.NewV7()), Slug: slug, DisplayName: "SSO " + slug,
			Issuer: "https://idp.example.test/" + slug, ClientID: "git2", SealedSecret: []byte{1, 2, 3}, Enabled: enabled,
		})
		if err != nil {
			t.Fatalf("create %s: %v", slug, err)
		}
		return created
	}
	a := newProvider(p+"-a", true)
	b := newProvider(p+"-b", false)
	if a.CreatedAt.IsZero() || string(a.SealedSecret) != "\x01\x02\x03" || !a.Enabled {
		t.Errorf("created %+v", a)
	}
	if _, err := store.CreateOIDCProvider(ctx, OIDCProvider{ID: uuid.Must(uuid.NewV7()), Slug: p + "-a", DisplayName: "x", Issuer: "x", ClientID: "x", SealedSecret: []byte{1}}); !errors.Is(err, ErrProviderExists) {
		t.Errorf("same slug: err %v, want ErrProviderExists", err)
	}

	mine := func(enabledOnly bool) []string {
		t.Helper()
		ps, err := store.ListOIDCProviders(ctx, enabledOnly)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, pr := range ps {
			if pr.ID == a.ID || pr.ID == b.ID {
				out = append(out, pr.Slug)
			}
		}
		return out
	}
	if got := mine(false); len(got) != 2 || got[0] != a.Slug {
		t.Errorf("all = %v, want a then b", got)
	}
	if got := mine(true); len(got) != 1 || got[0] != a.Slug {
		t.Errorf("enabled = %v, want only a", got)
	}

	if got, err := store.FindOIDCProviderBySlug(ctx, b.Slug); err != nil || got.ID != b.ID {
		t.Errorf("by slug = %+v, %v", got, err)
	}
	if _, err := store.FindOIDCProvider(ctx, uuid.New()); !errors.Is(err, ErrProviderNotFound) {
		t.Errorf("unknown id: err %v", err)
	}

	name, on := "Okta", true
	got, err := store.UpdateOIDCProvider(ctx, b.ID, OIDCProviderUpdate{DisplayName: &name, Enabled: &on})
	if err != nil || got.DisplayName != "Okta" || !got.Enabled || got.Issuer != b.Issuer || string(got.SealedSecret) != "\x01\x02\x03" {
		t.Errorf("update = %+v, %v; want only name and enabled changed", got, err)
	}
	if got, err := store.UpdateOIDCProvider(ctx, b.ID, OIDCProviderUpdate{SealedSecret: []byte{9}}); err != nil || string(got.SealedSecret) != "\x09" {
		t.Errorf("new secret = %v, %v", got.SealedSecret, err)
	}
	if _, err := store.UpdateOIDCProvider(ctx, uuid.New(), OIDCProviderUpdate{Enabled: &on}); !errors.Is(err, ErrProviderNotFound) {
		t.Errorf("update unknown: err %v", err)
	}

	// Deleting a provider deletes its identity links, not the user.
	u, err := store.Create(ctx, User{Email: p + "@example.com", Username: p, PasswordHash: "x", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO auth_svc.user_identities (provider_id, subject, user_id) VALUES ($1, 'sub-1', $2)`, a.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteOIDCProvider(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	var links int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM auth_svc.user_identities WHERE user_id = $1`, u.ID).Scan(&links); err != nil || links != 0 {
		t.Errorf("identity links after delete = %d, %v", links, err)
	}
	if _, err := store.FindByID(ctx, u.ID); err != nil {
		t.Errorf("user after provider delete: %v", err)
	}
	if err := store.DeleteOIDCProvider(ctx, a.ID); !errors.Is(err, ErrProviderNotFound) {
		t.Errorf("delete again: err %v", err)
	}
}
