package authsvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/git2-project/git2/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// directoryDB connects to TEST_POSTGRES_DSN, or skips the test. Rows whose
// username starts with prefix are deleted afterwards.
func directoryDB(t *testing.T, prefix string) (*PostgresStore, *pgxpool.Pool) {
	t.Helper()
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
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM auth_svc.team_members WHERE user_id IN (SELECT id FROM auth_svc.users WHERE username LIKE $1)`, prefix+"%")
		_, _ = db.Exec(ctx, `DELETE FROM auth_svc.teams WHERE name LIKE $1`, prefix+"%")
		_, _ = db.Exec(ctx, `DELETE FROM auth_svc.users WHERE username LIKE $1`, prefix+"%")
	})
	return NewPostgresStore(db), db
}

func TestPostgresListUsers(t *testing.T) {
	p := "dl" + uuid.NewString()[:6] // no '-': the escaping case below adds '_'
	store, _ := directoryDB(t, p)
	ctx := context.Background()

	create := func(name string, role Role, active bool) User {
		t.Helper()
		u, err := store.Create(ctx, User{Email: name + "@example.com", Username: name, PasswordHash: "x", IsActive: true, Role: role})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if !active {
			if _, err := store.Disable(ctx, u.ID); err != nil {
				t.Fatalf("disable %s: %v", name, err)
			}
		}
		return u
	}
	create(p+"_a", RoleUser, true)
	create(p+"xa", RoleUser, true)
	create(p+"_b", RoleAdmin, true)
	create(p+"_c", RoleUser, false)

	names := func(f UserFilter) []string {
		t.Helper()
		if f.Limit == 0 {
			f.Limit = 100
		}
		users, err := store.ListUsers(ctx, f)
		if err != nil {
			t.Fatalf("list %+v: %v", f, err)
		}
		var out []string
		for _, u := range users {
			out = append(out, u.Username)
		}
		return out
	}

	if got := names(UserFilter{Prefix: p}); !slices.Equal(got, []string{p + "_a", p + "xa", p + "_b", p + "_c"}) {
		t.Errorf("prefix = %v, want creation order", got)
	}
	// '_' is a LIKE wildcard; the prefix must match it literally.
	if got := names(UserFilter{Prefix: p + "_"}); !slices.Equal(got, []string{p + "_a", p + "_b", p + "_c"}) {
		t.Errorf("prefix with _ = %v, want no %sxa", got, p)
	}
	if got := names(UserFilter{Prefix: p + "%"}); len(got) != 0 {
		t.Errorf("prefix with %% = %v, want none", got)
	}
	// Usernames have no '@', so only the email can match this one. The
	// prefix is compared lowercase.
	if got := names(UserFilter{Prefix: p + "_A@Example"}); !slices.Equal(got, []string{p + "_a"}) {
		t.Errorf("email prefix = %v", got)
	}
	if got := names(UserFilter{Prefix: p, Role: RoleAdmin}); !slices.Equal(got, []string{p + "_b"}) {
		t.Errorf("role=admin = %v", got)
	}
	inactive := false
	if got := names(UserFilter{Prefix: p, Active: &inactive}); !slices.Equal(got, []string{p + "_c"}) {
		t.Errorf("active=false = %v", got)
	}

	var paged []string
	f := UserFilter{Prefix: p, Limit: 3}
	for range 5 {
		users, err := store.ListUsers(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range users {
			paged = append(paged, u.Username)
		}
		if len(users) < f.Limit {
			break
		}
		f.After = users[len(users)-1].ID
	}
	if !slices.Equal(paged, []string{p + "_a", p + "xa", p + "_b", p + "_c"}) {
		t.Errorf("pages of 3 = %v", paged)
	}
}

func TestPostgresUpdateUser(t *testing.T) {
	p := "du" + uuid.NewString()[:6]
	store, _ := directoryDB(t, p)
	ctx := context.Background()

	a, err := store.Create(ctx, User{Email: p + "a@example.com", Username: p + "a", PasswordHash: "x", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	// b is an admin, so demoting a below is not refused as the last one
	// (TestPostgresUpdateUserLastAdmin covers that).
	if _, err := store.Create(ctx, User{Email: p + "b@example.com", Username: p + "b", PasswordHash: "x", IsActive: true, Role: RoleAdmin}); err != nil {
		t.Fatal(err)
	}

	email, role := p+"a@new.example", RoleAdmin
	got, err := store.UpdateUser(ctx, a.ID, UserUpdate{Email: &email, Role: &role})
	if err != nil || got.Email != email || got.Role != RoleAdmin || !got.UpdatedAt.After(a.UpdatedAt) {
		t.Fatalf("update = %+v, %v", got, err)
	}
	// Only the given fields change.
	role = RoleUser
	if got, err := store.UpdateUser(ctx, a.ID, UserUpdate{Role: &role}); err != nil || got.Email != email {
		t.Errorf("role only: %+v, %v", got, err)
	}

	taken := p + "b@example.com"
	if _, err := store.UpdateUser(ctx, a.ID, UserUpdate{Email: &taken}); !errors.Is(err, ErrEmailExists) {
		t.Errorf("email taken: err %v, want ErrEmailExists", err)
	}
	if _, err := store.UpdateUser(ctx, uuid.New(), UserUpdate{Role: &role}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user: err %v, want ErrNotFound", err)
	}
}

// Two admins demoting each other at the same moment: one must stay.
func TestPostgresUpdateUserLastAdmin(t *testing.T) {
	p := "dla" + uuid.NewString()[:6]
	store, db := directoryDB(t, p)
	ctx := context.Background()
	if exists, err := store.AdminExists(ctx); err != nil || exists {
		t.Skipf("database already has an active admin (err %v)", err)
	}

	for round := range 10 {
		var ids [2]uuid.UUID
		for i := range ids {
			name := fmt.Sprintf("%s-%d-%d", p, round, i)
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
				role := RoleUser
				_, errs[i] = store.UpdateUser(ctx, ids[i], UserUpdate{Role: &role})
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
			t.Fatalf("round %d: %d demoted, %d refused; want 1 and 1", round, ok, last)
		}
		if _, err := db.Exec(ctx, `DELETE FROM auth_svc.users WHERE username LIKE $1`, fmt.Sprintf("%s-%d-%%", p, round)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostgresUserTeams(t *testing.T) {
	p := "dt" + uuid.NewString()[:6]
	store, db := directoryDB(t, p)
	ctx := context.Background()

	u, err := store.Create(ctx, User{Email: p + "@example.com", Username: p, PasswordHash: "x", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if teams, err := store.UserTeams(ctx, u.ID); err != nil || len(teams) != 0 {
		t.Fatalf("no teams yet: %v, %v", teams, err)
	}
	for _, team := range []struct {
		name string
		role Role
	}{{p + "-web", RoleMember}, {p + "-api", RoleMaintainer}} {
		id := uuid.Must(uuid.NewV7())
		if _, err := db.Exec(ctx, `INSERT INTO auth_svc.teams (id, name) VALUES ($1, $2)`, id, team.name); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO auth_svc.team_members (team_id, user_id, role) VALUES ($1, $2, $3)`, id, u.ID, team.role); err != nil {
			t.Fatal(err)
		}
	}
	teams, err := store.UserTeams(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 2 || teams[0].TeamName != p+"-api" || teams[0].Role != RoleMaintainer || teams[1].TeamName != p+"-web" || teams[1].Role != RoleMember {
		t.Errorf("teams = %+v, want api (maintainer) then web (member)", teams)
	}
}
