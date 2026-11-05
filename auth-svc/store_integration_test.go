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
	if created.Role != RoleUser {
		t.Errorf("Create role = %q, want default %q", created.Role, RoleUser)
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

	if err := store.SetActive(ctx, created.ID, false); err != nil {
		t.Fatalf("SetActive(false): %v", err)
	}
	if got, err := store.FindByID(ctx, created.ID); err != nil || got.IsActive || !got.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("after SetActive(false): active=%v updated_at=%v (was %v), err=%v", got.IsActive, got.UpdatedAt, created.UpdatedAt, err)
	}
	if err := store.SetActive(ctx, created.ID, true); err != nil {
		t.Fatalf("SetActive(true): %v", err)
	}
	if err := store.SetActive(ctx, uuid.New(), false); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetActive(missing) err = %v, want ErrNotFound", err)
	}
}

// TestRolesSeedMatchesCode keeps the role constants in roles.go and the seed
// in 004_auth_roles_teams.sql in step: a role in one but not the other would
// either fail a foreign key or grant nothing.
func TestRolesSeedMatchesCode(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	db, err := database.NewPostgresPool(ctx, dsn, 2)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	rows, err := db.Query(ctx, `SELECT name, scope FROM auth_svc.roles`)
	if err != nil {
		t.Fatalf("query roles: %v", err)
	}
	got := map[Role]string{}
	for rows.Next() {
		var name, scope string
		if err := rows.Scan(&name, &scope); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[Role(name)] = scope
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	want := map[Role]string{
		RoleAdmin: "instance", RoleAuditor: "instance", RoleUser: "instance",
		RoleMaintainer: "team", RoleMember: "team",
	}
	if len(got) != len(want) {
		t.Errorf("auth_svc.roles has %d roles, code has %d: %v", len(got), len(want), got)
	}
	for r, scope := range want {
		if got[r] != scope {
			t.Errorf("role %q: scope in db = %q, want %q", r, got[r], scope)
		}
	}
}
