package authsvc

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/git2-project/git2/internal/database"
	"github.com/google/uuid"
)

// TestPostgresStore runs against a real database with the migrations applied.
// It is skipped unless TEST_POSTGRES_DSN is set, and it never points at the
// shared Supabase project by default: rows it creates are deleted afterwards.
func TestPostgresStore(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	db, err := database.NewPostgresPool(ctx, dsn, 2)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Cleanups run last-in first-out, so the row delete below runs before this.
	t.Cleanup(db.Close)
	store := NewPostgresStore(db)

	suffix := uuid.NewString()[:8]
	email, username := "it-"+suffix+"@example.com", "it-"+suffix
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM auth_svc.users WHERE email = $1`, email)
	})

	created, err := store.Create(ctx, User{Email: email, Username: username, PasswordHash: "x", IsActive: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == uuid.Nil || created.CreatedAt.IsZero() {
		t.Errorf("Create did not return db-populated fields: %+v", created)
	}

	if _, err := store.Create(ctx, User{Email: email, Username: "other-" + suffix, PasswordHash: "x"}); !errors.Is(err, ErrEmailExists) {
		t.Errorf("duplicate email err = %v, want ErrEmailExists", err)
	}
	if _, err := store.Create(ctx, User{Email: "o-" + email, Username: username, PasswordHash: "x"}); !errors.Is(err, ErrUsernameExists) {
		t.Errorf("duplicate username err = %v, want ErrUsernameExists", err)
	}

	if got, err := store.FindByEmail(ctx, email); err != nil || got.ID != created.ID {
		t.Errorf("FindByEmail = %v, %v", got.ID, err)
	}
	if got, err := store.FindByID(ctx, created.ID); err != nil || got.Email != email {
		t.Errorf("FindByID = %v, %v", got.Email, err)
	}
	if _, err := store.FindByID(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindByID(missing) err = %v, want ErrNotFound", err)
	}
}
