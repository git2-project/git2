package authsvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

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

	at, err := store.Disable(ctx, created.ID)
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	got, err := store.FindByID(ctx, created.ID)
	if err != nil || got.IsActive || got.TokensValidAfter == nil || !got.TokensValidAfter.Equal(at) || !got.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("after Disable: active=%v tokens_valid_after=%v (returned %v) updated_at=%v, err=%v", got.IsActive, got.TokensValidAfter, at, got.UpdatedAt, err)
	}
	if err := store.Enable(ctx, created.ID); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if got, _ := store.FindByID(ctx, created.ID); !got.IsActive || got.TokensValidAfter == nil {
		t.Errorf("after Enable: active=%v tokens_valid_after=%v; want active and kept", got.IsActive, got.TokensValidAfter)
	}
	if _, err := store.Disable(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Disable(missing) err = %v, want ErrNotFound", err)
	}
	if err := store.Enable(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Enable(missing) err = %v, want ErrNotFound", err)
	}
}

// TestPostgresLastAdmin disables two admins at the same time, many times
// over: the row locks must let exactly one through, so one active admin is
// always left. Skips on a database that already has an active admin.
func TestPostgresLastAdmin(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	db, err := database.NewPostgresPool(ctx, dsn, 10)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	store := NewPostgresStore(db)
	if exists, err := store.AdminExists(ctx); err != nil || exists {
		t.Skipf("database already has an active admin (err %v)", err)
	}
	suffix := uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM auth_svc.users WHERE username LIKE $1`, "la-"+suffix+"-%")
	})

	for round := range 10 {
		var ids [2]uuid.UUID
		for i := range ids {
			name := fmt.Sprintf("la-%s-%d-%d", suffix, round, i)
			u, err := store.Create(ctx, User{Email: name + "@example.com", Username: name, PasswordHash: "x", IsActive: true, Role: RoleAdmin})
			if err != nil {
				t.Fatalf("create admin: %v", err)
			}
			ids[i] = u.ID
		}
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = store.Disable(ctx, ids[i])
			}()
		}
		wg.Wait()
		ok, last := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrLastAdmin):
				last++
			default:
				t.Fatalf("round %d: unexpected error %v", round, err)
			}
		}
		if ok != 1 || last != 1 {
			t.Fatalf("round %d: %d disabled, %d refused; want 1 and 1", round, ok, last)
		}
		if exists, _ := store.AdminExists(ctx); !exists {
			t.Fatalf("round %d: no active admin left", round)
		}
		if _, err := db.Exec(ctx, `DELETE FROM auth_svc.users WHERE username LIKE $1`, fmt.Sprintf("la-%s-%d-%%", suffix, round)); err != nil {
			t.Fatal(err)
		}
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

// TestPostgresSetup runs the first-admin flow against the real tables,
// including many concurrent requests with the right token: the transaction
// must let exactly one through. It skips on a database that already has an
// admin, and removes what it created.
func TestPostgresSetup(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	db, err := database.NewPostgresPool(ctx, dsn, 10)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	store := NewPostgresStore(db)

	if exists, err := store.AdminExists(ctx); err != nil {
		t.Fatalf("AdminExists: %v", err)
	} else if exists {
		t.Skip("database already has an admin")
	}
	suffix := uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM auth_svc.users WHERE username LIKE $1`, "su-"+suffix+"-%")
		_, _ = db.Exec(ctx, `DELETE FROM auth_svc.setup_token`)
	})

	token := "integration-setup-token-" + suffix + "-0123456789"
	if st, err := PrepareSetup(ctx, store, token); err != nil || st.Token != "" || st.Done || st.Kept {
		t.Fatalf("PrepareSetup = %+v, %v", st, err)
	}
	// A generated token does not replace the fixed one while it is valid.
	if st, err := PrepareSetup(ctx, store, ""); err != nil || !st.Kept || st.Token != "" {
		t.Fatalf("second PrepareSetup = %+v, %v; want Kept", st, err)
	}
	var stored string
	if err := db.QueryRow(ctx, `SELECT token_sha256 FROM auth_svc.setup_token`).Scan(&stored); err != nil || stored != hashSetupToken(token) {
		t.Fatalf("stored hash = %q, %v; want SHA-256 of the token", stored, err)
	}

	user := func(i int) User {
		name := fmt.Sprintf("su-%s-%d", suffix, i)
		return User{Email: name + "@example.com", Username: name, PasswordHash: "x", IsActive: true}
	}
	if _, err := store.CreateFirstAdmin(ctx, hashSetupToken("wrong"), user(0)); !errors.Is(err, ErrBadSetupToken) {
		t.Errorf("wrong token err = %v, want ErrBadSetupToken", err)
	}

	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = store.CreateFirstAdmin(ctx, hashSetupToken(token), user(i+1))
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrSetupDone):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("%d admins created, want exactly 1 (errors %v)", ok, errs)
	}

	var admins, tokens int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM auth_svc.users WHERE role = 'admin' AND username LIKE $1`, "su-"+suffix+"-%").Scan(&admins)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM auth_svc.setup_token`).Scan(&tokens)
	if admins != 1 || tokens != 0 {
		t.Errorf("admins = %d, setup tokens = %d; want 1 and 0", admins, tokens)
	}
	if res, err := store.PutSetupToken(ctx, hashSetupToken("again"), true); err != nil || res.Stored || !res.AdminExists {
		t.Errorf("PutSetupToken after setup = %+v, %v; want AdminExists", res, err)
	}

	// The goroutines above rarely overlap in practice, so prove the lock
	// directly: while another transaction holds the token row, a second
	// CreateFirstAdmin must wait, and once that transaction has made an admin
	// and burned the token, it must give up with ErrSetupDone.
	if _, err := db.Exec(ctx, `DELETE FROM auth_svc.users WHERE username LIKE $1`, "su-"+suffix+"-%"); err != nil {
		t.Fatalf("reset admins: %v", err)
	}
	if _, err := PrepareSetup(ctx, store, token); err != nil {
		t.Fatalf("PrepareSetup again: %v", err)
	}
	holder, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `SELECT 1 FROM auth_svc.setup_token WHERE singleton FOR UPDATE`); err != nil {
		t.Fatalf("lock token row: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := store.CreateFirstAdmin(ctx, hashSetupToken(token), user(100))
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("CreateFirstAdmin did not wait for the locked token row (err %v)", err)
	case <-time.After(300 * time.Millisecond):
	}

	if _, err := insertUser(ctx, holder, User{Email: "su-" + suffix + "-99@example.com", Username: "su-" + suffix + "-99", PasswordHash: "x", IsActive: true, Role: RoleAdmin}); err != nil {
		t.Fatalf("holder: insert admin: %v", err)
	}
	if _, err := holder.Exec(ctx, `DELETE FROM auth_svc.setup_token`); err != nil {
		t.Fatalf("holder: burn token: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("holder: commit: %v", err)
	}
	if err := <-done; !errors.Is(err, ErrSetupDone) {
		t.Errorf("waiting CreateFirstAdmin err = %v, want ErrSetupDone", err)
	}
}
