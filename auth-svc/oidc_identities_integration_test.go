package authsvc

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestPostgresIdentities(t *testing.T) {
	p := "id" + uuid.NewString()[:6]
	store, db := directoryDB(t, p)
	ctx := context.Background()
	t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM auth_svc.oidc_providers WHERE slug LIKE $1`, p+"%") })

	prov, err := store.CreateOIDCProvider(ctx, OIDCProvider{
		ID: uuid.Must(uuid.NewV7()), Slug: p + "-idp", DisplayName: "IdP", Issuer: "https://idp.example.test",
		ClientID: "git2", SealedSecret: []byte{1}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.FindUserByIdentity(ctx, prov.ID, "sub-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown identity: err %v, want ErrNotFound", err)
	}

	// A first sign-in: the account and its identity, together. No password.
	u, err := store.CreateUserWithIdentity(ctx, User{Email: p + "a@example.com", Username: p + "a", IsActive: true}, prov.ID, "sub-1", p+"a@example.com")
	if err != nil || u.PasswordHash != "" || u.Role != RoleUser {
		t.Fatalf("create = %+v, %v", u, err)
	}
	if got, err := store.FindUserByIdentity(ctx, prov.ID, "sub-1"); err != nil || got.ID != u.ID {
		t.Errorf("find by identity = %+v, %v", got, err)
	}
	if err := store.TouchIdentity(ctx, prov.ID, "sub-1", "new@example.com"); err != nil {
		t.Errorf("touch: %v", err)
	}

	// The same subject again (a concurrent first sign-in): nothing is
	// created, not even the account.
	if _, err := store.CreateUserWithIdentity(ctx, User{Email: p + "b@example.com", Username: p + "b", IsActive: true}, prov.ID, "sub-1", ""); !errors.Is(err, ErrIdentityTaken) {
		t.Errorf("same subject: err %v, want ErrIdentityTaken", err)
	}
	if _, err := store.FindByEmail(ctx, p+"b@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("account left behind by a failed create: err %v", err)
	}
	// A taken username or email rolls back too.
	if _, err := store.CreateUserWithIdentity(ctx, User{Email: p + "c@example.com", Username: p + "a", IsActive: true}, prov.ID, "sub-2", ""); !errors.Is(err, ErrUsernameExists) {
		t.Errorf("taken username: err %v", err)
	}

	// Linking an existing account; one identity per user and provider.
	v, err := store.Create(ctx, User{Email: p + "v@example.com", Username: p + "v", PasswordHash: "x", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LinkIdentity(ctx, prov.ID, "sub-v", v.ID, v.Email); err != nil {
		t.Fatalf("link: %v", err)
	}
	if err := store.LinkIdentity(ctx, prov.ID, "sub-v2", v.ID, v.Email); !errors.Is(err, ErrIdentityTaken) {
		t.Errorf("second identity for the same user: err %v", err)
	}
	if err := store.LinkIdentity(ctx, prov.ID, "sub-v", u.ID, ""); !errors.Is(err, ErrIdentityTaken) {
		t.Errorf("linked subject to another user: err %v", err)
	}
}
