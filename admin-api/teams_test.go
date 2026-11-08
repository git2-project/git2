package adminapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/google/uuid"
)

// The memStore's TeamStore side, with the rules of the auth_svc tables:
// unique names, memberships deleted with their team, foreign keys to teams
// and users.

func (s *memStore) ListRoles(context.Context) ([]authsvc.RoleInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return nil, err
	}
	return []authsvc.RoleInfo{
		{Name: authsvc.RoleAdmin, Scope: "instance", Description: "Everything in the admin plane"},
		{Name: authsvc.RoleUser, Scope: "instance", Description: "The Dash plane"},
		{Name: authsvc.RoleMaintainer, Scope: "team", Description: "Rename the team, add and remove members"},
		{Name: authsvc.RoleMember, Scope: "team", Description: "Belongs to the team"},
	}, nil
}

// teamFailure is teamFail, or else fail: the team calls can fail on their
// own while the caller lookup of RequirePermission still works.
func (s *memStore) teamFailure() error {
	if s.teamFail != nil {
		return s.teamFail
	}
	return s.fail
}

func (s *memStore) teamIndex(id uuid.UUID) int {
	return slices.IndexFunc(s.teams, func(t authsvc.Team) bool { return t.ID == id })
}

// withCount fills in MemberCount, as the store's subquery does.
func (s *memStore) withCount(t authsvc.Team) authsvc.Team {
	t.MemberCount = len(s.members[t.ID])
	return t
}

func (s *memStore) ListTeams(_ context.Context, f authsvc.TeamFilter) ([]authsvc.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return nil, err
	}
	var out []authsvc.Team
	for _, t := range s.teams {
		if len(out) == f.Limit {
			break
		}
		if (f.After == uuid.Nil || strings.Compare(t.ID.String(), f.After.String()) > 0) && strings.HasPrefix(t.Name, f.Prefix) {
			out = append(out, s.withCount(t))
		}
	}
	return out, nil
}

func (s *memStore) CreateTeam(_ context.Context, name, description string) (authsvc.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return authsvc.Team{}, err
	}
	if slices.ContainsFunc(s.teams, func(t authsvc.Team) bool { return t.Name == name }) {
		return authsvc.Team{}, authsvc.ErrTeamExists
	}
	now := time.Now()
	t := authsvc.Team{ID: uuid.Must(uuid.NewV7()), Name: name, Description: description, CreatedAt: now, UpdatedAt: now}
	s.teams = append(s.teams, t)
	return t, nil
}

func (s *memStore) FindTeam(_ context.Context, id uuid.UUID) (authsvc.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return authsvc.Team{}, err
	}
	if i := s.teamIndex(id); i >= 0 {
		return s.withCount(s.teams[i]), nil
	}
	return authsvc.Team{}, authsvc.ErrTeamNotFound
}

func (s *memStore) UpdateTeam(_ context.Context, id uuid.UUID, u authsvc.TeamUpdate) (authsvc.Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return authsvc.Team{}, err
	}
	i := s.teamIndex(id)
	if i < 0 {
		return authsvc.Team{}, authsvc.ErrTeamNotFound
	}
	if u.Name != nil {
		if slices.ContainsFunc(s.teams, func(t authsvc.Team) bool { return t.ID != id && t.Name == *u.Name }) {
			return authsvc.Team{}, authsvc.ErrTeamExists
		}
		s.teams[i].Name = *u.Name
	}
	if u.Description != nil {
		s.teams[i].Description = *u.Description
	}
	s.teams[i].UpdatedAt = time.Now()
	return s.withCount(s.teams[i]), nil
}

func (s *memStore) DeleteTeam(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return err
	}
	i := s.teamIndex(id)
	if i < 0 {
		return authsvc.ErrTeamNotFound
	}
	s.teams = slices.Delete(s.teams, i, i+1)
	delete(s.members, id)
	return nil
}

func (s *memStore) ListTeamMembers(_ context.Context, teamID uuid.UUID, afterName string, limit int) ([]authsvc.TeamMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return nil, err
	}
	var out []authsvc.TeamMember
	for _, m := range s.members[teamID] {
		if m.Username > afterName {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b authsvc.TeamMember) int { return strings.Compare(a.Username, b.Username) })
	return out[:min(len(out), limit)], nil
}

func (s *memStore) PutTeamMember(_ context.Context, teamID, userID uuid.UUID, role authsvc.Role) (authsvc.TeamMember, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return authsvc.TeamMember{}, false, err
	}
	if s.teamIndex(teamID) < 0 {
		return authsvc.TeamMember{}, false, authsvc.ErrTeamNotFound
	}
	ui := s.index(userID)
	if ui < 0 {
		return authsvc.TeamMember{}, false, authsvc.ErrNotFound
	}
	if s.members[teamID] == nil {
		s.members[teamID] = map[uuid.UUID]authsvc.TeamMember{}
	}
	m, existed := s.members[teamID][userID]
	if !existed {
		m = authsvc.TeamMember{UserID: userID, Username: s.users[ui].Username, AddedAt: time.Now()}
	}
	m.Role = role
	s.members[teamID][userID] = m
	return m, !existed, nil
}

func (s *memStore) DeleteTeamMember(_ context.Context, teamID, userID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.teamFailure(); err != nil {
		return err
	}
	if _, ok := s.members[teamID][userID]; !ok {
		return authsvc.ErrMemberNotFound
	}
	delete(s.members[teamID], userID)
	return nil
}

func (h *harness) createTeam(name string) Team {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/api/admin/teams", h.adminToken, map[string]string{"name": name})
	if rec.Code != http.StatusCreated {
		h.t.Fatalf("create team %s: status %d body %s", name, rec.Code, rec.Body)
	}
	return decode[Team](h.t, rec)
}

func TestTeamRoutesNeedAnAdmin(t *testing.T) {
	h := newHarness(t)
	plain := h.addUser("ada@example.com", "ada", authsvc.RoleUser)
	team := h.createTeam("payments")
	teamPath := "/api/admin/teams/" + team.ID.String()
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/admin/roles"},
		{http.MethodGet, "/api/admin/teams"},
		{http.MethodPost, "/api/admin/teams"},
		{http.MethodGet, teamPath},
		{http.MethodPatch, teamPath},
		{http.MethodDelete, teamPath},
		{http.MethodGet, teamPath + "/members"},
		{http.MethodPut, teamPath + "/members/" + plain.ID.String()},
		{http.MethodDelete, teamPath + "/members/" + plain.ID.String()},
	}
	for _, rt := range routes {
		if rec := h.do(rt.method, rt.path, "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", rt.method, rt.path, rec.Code)
		}
		if rec := h.do(rt.method, rt.path, h.token(plain.ID), nil); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a user: status %d, want 403", rt.method, rt.path, rec.Code)
		}
	}
}

func TestListRoles(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/api/admin/roles", h.adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	roles := decode[struct {
		Roles []Role `json:"roles"`
	}](t, rec).Roles
	if len(roles) != 4 || roles[0].Name != authsvc.RoleAdmin || roles[0].Scope != "instance" || roles[2].Scope != "team" || roles[0].Description == "" {
		t.Errorf("roles = %+v", roles)
	}
}

func TestCreateTeam(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodPost, "/api/admin/teams", h.adminToken, map[string]string{"name": "payments", "description": "Pays the bills"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if got := decode[Team](t, rec); got.Name != "payments" || got.Description != "Pays the bills" || got.MemberCount != 0 || got.ID == uuid.Nil {
		t.Errorf("created %+v", got)
	}

	tests := []struct {
		name string
		body any
		want int
	}{
		{"name taken", map[string]string{"name": "payments"}, http.StatusConflict},
		{"no name", map[string]string{"description": "x"}, http.StatusBadRequest},
		{"uppercase", map[string]string{"name": "Payments"}, http.StatusBadRequest},
		{"too short", map[string]string{"name": "ab"}, http.StatusBadRequest},
		{"bad character", map[string]string{"name": "pay ments"}, http.StatusBadRequest},
		// 501 characters, not bytes: "ü" is two bytes.
		{"description too long", map[string]string{"name": "web", "description": strings.Repeat("ü", 501)}, http.StatusBadRequest},
		{"unknown field", `{"name":"web","members":[]}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := h.do(http.MethodPost, "/api/admin/teams", h.adminToken, tt.body); rec.Code != tt.want {
				t.Errorf("status %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
	// 500 characters is still fine.
	if rec := h.do(http.MethodPost, "/api/admin/teams", h.adminToken, map[string]string{"name": "web", "description": strings.Repeat("ü", 500)}); rec.Code != http.StatusCreated {
		t.Errorf("500-character description: status %d body %s", rec.Code, rec.Body)
	}
}

func TestListTeams(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"payments", "pay-ops", "web", "api-team"} {
		h.createTeam(name)
	}
	names := func(path string) ([]string, string) {
		t.Helper()
		rec := h.do(http.MethodGet, path, h.adminToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d body %s", path, rec.Code, rec.Body)
		}
		page := decode[TeamPage](t, rec)
		var out []string
		for _, tm := range page.Teams {
			out = append(out, tm.Name)
		}
		return out, page.NextPageToken
	}
	if got, next := names("/api/admin/teams"); !slices.Equal(got, []string{"payments", "pay-ops", "web", "api-team"}) || next != "" {
		t.Errorf("all = %v (next %q), want creation order", got, next)
	}
	if got, _ := names("/api/admin/teams?q=pay"); !slices.Equal(got, []string{"payments", "pay-ops"}) {
		t.Errorf("q=pay = %v", got)
	}
	var all []string
	path := "/api/admin/teams?page_size=3"
	for range 5 {
		got, next := names(path)
		all = append(all, got...)
		if next == "" {
			break
		}
		path = "/api/admin/teams?page_size=3&page_token=" + next
	}
	if !slices.Equal(all, []string{"payments", "pay-ops", "web", "api-team"}) {
		t.Errorf("pages of 3 = %v", all)
	}
	for _, bad := range []string{"page_size=0", "page_token=!!", "q=" + strings.Repeat("a", 40)} {
		if rec := h.do(http.MethodGet, "/api/admin/teams?"+bad, h.adminToken, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("?%s: status %d, want 400", bad, rec.Code)
		}
	}
}

func TestGetUpdateDeleteTeam(t *testing.T) {
	h := newHarness(t)
	team := h.createTeam("payments")
	h.createTeam("web")
	path := "/api/admin/teams/" + team.ID.String()

	if rec := h.do(http.MethodGet, path, h.adminToken, nil); rec.Code != http.StatusOK || decode[Team](t, rec).Name != "payments" {
		t.Errorf("get: status %d body %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/admin/teams/" + uuid.NewString(), "/api/admin/teams/nope"} {
		if rec := h.do(http.MethodGet, p, h.adminToken, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", p, rec.Code)
		}
	}

	rec := h.do(http.MethodPatch, path, h.adminToken, map[string]string{"name": "billing"})
	if got := decode[Team](t, rec); rec.Code != http.StatusOK || got.Name != "billing" {
		t.Errorf("rename: status %d body %s", rec.Code, rec.Body)
	}
	rec = h.do(http.MethodPatch, path, h.adminToken, map[string]string{"description": "Invoices"})
	if got := decode[Team](t, rec); rec.Code != http.StatusOK || got.Name != "billing" || got.Description != "Invoices" {
		t.Errorf("description only: status %d body %s", rec.Code, rec.Body)
	}
	for _, tt := range []struct {
		body any
		want int
	}{
		{`{}`, http.StatusBadRequest},
		{map[string]string{"name": "Billing"}, http.StatusBadRequest},
		{map[string]string{"name": "web"}, http.StatusConflict},
	} {
		if rec := h.do(http.MethodPatch, path, h.adminToken, tt.body); rec.Code != tt.want {
			t.Errorf("PATCH %v: status %d, want %d", tt.body, rec.Code, tt.want)
		}
	}

	// Deleting a team takes its memberships with it.
	ada := h.addUser("ada@example.com", "ada", authsvc.RoleUser)
	if rec := h.do(http.MethodPut, path+"/members/"+ada.ID.String(), h.adminToken, map[string]string{"role": "member"}); rec.Code != http.StatusCreated {
		t.Fatalf("add member: status %d body %s", rec.Code, rec.Body)
	}
	if rec := h.do(http.MethodDelete, path, h.adminToken, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d body %s", rec.Code, rec.Body)
	}
	if rec := h.do(http.MethodGet, path, h.adminToken, nil); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: status %d, want 404", rec.Code)
	}
	if got := decode[AdminUserDetail](t, h.do(http.MethodGet, "/api/admin/users/"+ada.ID.String(), h.adminToken, nil)); len(got.Teams) != 0 {
		t.Errorf("ada's teams after delete = %+v, want none", got.Teams)
	}
	if rec := h.do(http.MethodDelete, path, h.adminToken, nil); rec.Code != http.StatusNotFound {
		t.Errorf("delete again: status %d, want 404", rec.Code)
	}
}

func TestTeamMembers(t *testing.T) {
	h := newHarness(t)
	team := h.createTeam("payments")
	members := "/api/admin/teams/" + team.ID.String() + "/members"
	grace := h.addUser("grace@example.com", "grace", authsvc.RoleUser)
	ada := h.addUser("ada@example.com", "ada", authsvc.RoleUser)
	bob := h.addUser("bob@example.com", "bob", authsvc.RoleUser)

	// Added: 201. The same request again, or a role change: 200.
	rec := h.do(http.MethodPut, members+"/"+ada.ID.String(), h.adminToken, map[string]string{"role": "member"})
	if got := decode[TeamMember](t, rec); rec.Code != http.StatusCreated || got.User.ID != ada.ID || got.User.Username != "ada" || got.Role != authsvc.RoleMember {
		t.Fatalf("add: status %d body %s", rec.Code, rec.Body)
	}
	if rec := h.do(http.MethodPut, members+"/"+ada.ID.String(), h.adminToken, map[string]string{"role": "member"}); rec.Code != http.StatusOK {
		t.Errorf("same again: status %d, want 200", rec.Code)
	}
	rec = h.do(http.MethodPut, members+"/"+ada.ID.String(), h.adminToken, map[string]string{"role": "maintainer"})
	if rec.Code != http.StatusOK || decode[TeamMember](t, rec).Role != authsvc.RoleMaintainer {
		t.Errorf("role change: status %d body %s", rec.Code, rec.Body)
	}
	for _, u := range []authsvc.User{grace, bob} {
		if rec := h.do(http.MethodPut, members+"/"+u.ID.String(), h.adminToken, map[string]string{"role": "member"}); rec.Code != http.StatusCreated {
			t.Fatalf("add %s: status %d", u.Username, rec.Code)
		}
	}
	if got := decode[Team](t, h.do(http.MethodGet, "/api/admin/teams/"+team.ID.String(), h.adminToken, nil)); got.MemberCount != 3 {
		t.Errorf("member_count = %d, want 3", got.MemberCount)
	}

	// Listed by username, also across pages.
	var names []string
	path := members + "?page_size=2"
	for range 5 {
		rec := h.do(http.MethodGet, path, h.adminToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list: status %d body %s", rec.Code, rec.Body)
		}
		page := decode[TeamMemberPage](t, rec)
		for _, m := range page.Members {
			names = append(names, m.User.Username)
		}
		if page.NextPageToken == "" {
			break
		}
		path = members + "?page_size=2&page_token=" + page.NextPageToken
	}
	if !slices.Equal(names, []string{"ada", "bob", "grace"}) {
		t.Errorf("members = %v, want ada, bob, grace", names)
	}

	tests := []struct {
		name, method, path string
		body               any
		want               int
	}{
		{"instance role", http.MethodPut, members + "/" + bob.ID.String(), map[string]string{"role": "admin"}, http.StatusBadRequest},
		{"no role", http.MethodPut, members + "/" + bob.ID.String(), `{}`, http.StatusBadRequest},
		{"unknown user", http.MethodPut, members + "/" + uuid.NewString(), map[string]string{"role": "member"}, http.StatusNotFound},
		{"unknown team", http.MethodPut, "/api/admin/teams/" + uuid.NewString() + "/members/" + bob.ID.String(), map[string]string{"role": "member"}, http.StatusNotFound},
		{"list unknown team", http.MethodGet, "/api/admin/teams/" + uuid.NewString() + "/members", nil, http.StatusNotFound},
		{"bad page token", http.MethodGet, members + "?page_token=!!", nil, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := h.do(tt.method, tt.path, h.adminToken, tt.body); rec.Code != tt.want {
				t.Errorf("status %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}

	if rec := h.do(http.MethodDelete, members+"/"+bob.ID.String(), h.adminToken, nil); rec.Code != http.StatusNoContent {
		t.Errorf("remove: status %d body %s", rec.Code, rec.Body)
	}
	if rec := h.do(http.MethodDelete, members+"/"+bob.ID.String(), h.adminToken, nil); rec.Code != http.StatusNotFound {
		t.Errorf("remove again: status %d, want 404", rec.Code)
	}
}

func TestTeamDatabaseFaultIsNotUnauthorized(t *testing.T) {
	h := newHarness(t)
	team := h.createTeam("payments")
	// RequirePermission reads the same store, so make only the team calls
	// fail: the caller lookup must still work.
	h.store.mu.Lock()
	h.store.teamFail = errors.New("connection refused")
	h.store.mu.Unlock()
	for _, p := range []string{"/api/admin/roles", "/api/admin/teams", "/api/admin/teams/" + team.ID.String(), "/api/admin/teams/" + team.ID.String() + "/members"} {
		if rec := h.do(http.MethodGet, p, h.adminToken, nil); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s: status %d, want 503", p, rec.Code)
		}
	}
}
