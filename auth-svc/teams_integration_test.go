package authsvc

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// These tests share the database with others: they only look at rows whose
// names start with their own prefix, and make no assumption about the rest.

func TestPostgresListRoles(t *testing.T) {
	store, _ := directoryDB(t, "unused-")
	roles, err := store.ListRoles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range roles {
		got = append(got, r.Scope+"/"+string(r.Name))
		if r.Description == "" {
			t.Errorf("role %s has no description", r.Name)
		}
	}
	if want := []string{"instance/admin", "instance/user", "team/maintainer", "team/member"}; !slices.Equal(got, want) {
		t.Errorf("roles = %v, want %v", got, want)
	}
}

func TestPostgresTeams(t *testing.T) {
	p := "tm" + uuid.NewString()[:6]
	store, _ := directoryDB(t, p)
	ctx := context.Background()

	create := func(name string) Team {
		t.Helper()
		tm, err := store.CreateTeam(ctx, name, "about "+name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return tm
	}
	a := create(p + "_a")
	create(p + "xa")
	create(p + "_b")
	if a.Name != p+"_a" || a.Description != "about "+p+"_a" || a.MemberCount != 0 || a.CreatedAt.IsZero() {
		t.Errorf("created %+v", a)
	}
	if _, err := store.CreateTeam(ctx, p+"_a", ""); !errors.Is(err, ErrTeamExists) {
		t.Errorf("duplicate name: err %v, want ErrTeamExists", err)
	}

	names := func(f TeamFilter) []string {
		t.Helper()
		if f.Limit == 0 {
			f.Limit = 100
		}
		teams, err := store.ListTeams(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, tm := range teams {
			out = append(out, tm.Name)
		}
		return out
	}
	if got := names(TeamFilter{Prefix: p}); !slices.Equal(got, []string{p + "_a", p + "xa", p + "_b"}) {
		t.Errorf("prefix = %v, want creation order", got)
	}
	if got := names(TeamFilter{Prefix: p + "_"}); !slices.Equal(got, []string{p + "_a", p + "_b"}) {
		t.Errorf("prefix with _ = %v, want _ matched literally", got)
	}
	if got := names(TeamFilter{Prefix: p, After: a.ID, Limit: 1}); !slices.Equal(got, []string{p + "xa"}) {
		t.Errorf("page after a = %v", got)
	}

	if got, err := store.FindTeam(ctx, a.ID); err != nil || got.Name != a.Name {
		t.Errorf("find = %+v, %v", got, err)
	}
	if _, err := store.FindTeam(ctx, uuid.New()); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("find unknown: err %v", err)
	}

	newName, desc := p+"_renamed", "new description"
	if got, err := store.UpdateTeam(ctx, a.ID, TeamUpdate{Name: &newName}); err != nil || got.Name != newName || got.Description != a.Description || !got.UpdatedAt.After(a.UpdatedAt) {
		t.Errorf("rename = %+v, %v", got, err)
	}
	if got, err := store.UpdateTeam(ctx, a.ID, TeamUpdate{Description: &desc}); err != nil || got.Name != newName || got.Description != desc {
		t.Errorf("description = %+v, %v", got, err)
	}
	taken := p + "xa"
	if _, err := store.UpdateTeam(ctx, a.ID, TeamUpdate{Name: &taken}); !errors.Is(err, ErrTeamExists) {
		t.Errorf("rename to a taken name: err %v", err)
	}
	if _, err := store.UpdateTeam(ctx, uuid.New(), TeamUpdate{Description: &desc}); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("update unknown: err %v", err)
	}

	if err := store.DeleteTeam(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteTeam(ctx, a.ID); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("delete again: err %v", err)
	}
}

func TestPostgresTeamMembers(t *testing.T) {
	p := "tmm" + uuid.NewString()[:6]
	store, _ := directoryDB(t, p)
	ctx := context.Background()

	team, err := store.CreateTeam(ctx, p+"-team", "")
	if err != nil {
		t.Fatal(err)
	}
	users := map[string]User{}
	for _, n := range []string{"grace", "ada", "bob"} {
		u, err := store.Create(ctx, User{Email: p + n + "@example.com", Username: p + n, PasswordHash: "x", IsActive: true})
		if err != nil {
			t.Fatal(err)
		}
		users[n] = u
	}

	m, added, err := store.PutTeamMember(ctx, team.ID, users["ada"].ID, RoleMember)
	if err != nil || !added || m.Username != p+"ada" || m.Role != RoleMember || m.AddedAt.IsZero() {
		t.Fatalf("add = %+v, added %v, %v", m, added, err)
	}
	if _, added, err := store.PutTeamMember(ctx, team.ID, users["ada"].ID, RoleMember); err != nil || added {
		t.Errorf("same again: added %v, %v; want not added", added, err)
	}
	m2, added, err := store.PutTeamMember(ctx, team.ID, users["ada"].ID, RoleMaintainer)
	if err != nil || added || m2.Role != RoleMaintainer || !m2.AddedAt.Equal(m.AddedAt) {
		t.Errorf("role change = %+v, added %v, %v; want maintainer, same added_at", m2, added, err)
	}
	for _, n := range []string{"grace", "bob"} {
		if _, _, err := store.PutTeamMember(ctx, team.ID, users[n].ID, RoleMember); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := store.PutTeamMember(ctx, uuid.New(), users["bob"].ID, RoleMember); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("unknown team: err %v", err)
	}
	if _, _, err := store.PutTeamMember(ctx, team.ID, uuid.New(), RoleMember); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user: err %v", err)
	}

	if got, _ := store.FindTeam(ctx, team.ID); got.MemberCount != 3 {
		t.Errorf("member_count = %d, want 3", got.MemberCount)
	}

	var names []string
	after := ""
	for range 5 {
		page, err := store.ListTeamMembers(ctx, team.ID, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range page {
			names = append(names, m.Username)
		}
		if len(page) < 2 {
			break
		}
		after = page[len(page)-1].Username
	}
	if want := []string{p + "ada", p + "bob", p + "grace"}; !slices.Equal(names, want) {
		t.Errorf("members = %v, want %v", names, want)
	}

	if teams, err := store.UserTeams(ctx, users["ada"].ID); err != nil || len(teams) != 1 || teams[0].TeamID != team.ID || teams[0].Role != RoleMaintainer {
		t.Errorf("ada's teams = %+v, %v", teams, err)
	}

	if err := store.DeleteTeamMember(ctx, team.ID, users["bob"].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteTeamMember(ctx, team.ID, users["bob"].ID); !errors.Is(err, ErrMemberNotFound) {
		t.Errorf("remove again: err %v", err)
	}

	// Deleting the team deletes its memberships.
	if err := store.DeleteTeam(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	if teams, err := store.UserTeams(ctx, users["ada"].ID); err != nil || len(teams) != 0 {
		t.Errorf("ada's teams after delete = %+v, %v", teams, err)
	}
}
