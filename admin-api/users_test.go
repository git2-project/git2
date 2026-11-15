package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	authsvc "github.com/git2-project/git2/auth-svc"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// memStore is an in-memory UserStore with the uniqueness and last-admin
// rules of authsvc.PostgresStore. Users are kept in id order, and ids are
// UUIDv7, so the order is creation order, as in Postgres.
type memStore struct {
	mu    sync.Mutex
	users []authsvc.User
	// teams in id order, and their members by team and user id.
	teams   []authsvc.Team
	members map[uuid.UUID]map[uuid.UUID]authsvc.TeamMember
	// fail, when set, is returned by every call to simulate a database fault.
	fail error
	// teamFail, when set, is returned by the team calls only.
	teamFail error
	// lastAdmin, when set, is treated as the last active admin, as if
	// another admin had just been removed concurrently.
	lastAdmin uuid.UUID
}

func newMemStore() *memStore {
	return &memStore{members: map[uuid.UUID]map[uuid.UUID]authsvc.TeamMember{}}
}

func (s *memStore) index(id uuid.UUID) int {
	return slices.IndexFunc(s.users, func(u authsvc.User) bool { return u.ID == id })
}

func (s *memStore) isLastAdmin(u authsvc.User) bool {
	if u.ID == s.lastAdmin {
		return true
	}
	return u.Role == authsvc.RoleAdmin && u.IsActive && s.activeAdmins() == 1
}

func (s *memStore) activeAdmins() int {
	n := 0
	for _, u := range s.users {
		if u.Role == authsvc.RoleAdmin && u.IsActive {
			n++
		}
	}
	return n
}

func (s *memStore) Create(_ context.Context, u authsvc.User) (authsvc.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return authsvc.User{}, s.fail
	}
	for _, o := range s.users {
		if o.Email == u.Email {
			return authsvc.User{}, authsvc.ErrEmailExists
		}
		if o.Username == u.Username {
			return authsvc.User{}, authsvc.ErrUsernameExists
		}
	}
	u.ID = uuid.Must(uuid.NewV7())
	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	s.users = append(s.users, u)
	return u, nil
}

func (s *memStore) FindByID(_ context.Context, id uuid.UUID) (authsvc.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return authsvc.User{}, s.fail
	}
	if i := s.index(id); i >= 0 {
		return s.users[i], nil
	}
	return authsvc.User{}, authsvc.ErrNotFound
}

func (s *memStore) ListUsers(_ context.Context, f authsvc.UserFilter) ([]authsvc.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return nil, s.fail
	}
	var out []authsvc.User
	for _, u := range s.users {
		switch {
		case f.After != uuid.Nil && bytes.Compare(u.ID[:], f.After[:]) <= 0:
		case f.Prefix != "" && !strings.HasPrefix(u.Username, strings.ToLower(f.Prefix)) && !strings.HasPrefix(u.Email, strings.ToLower(f.Prefix)):
		case f.Role != "" && u.Role != f.Role:
		case f.Active != nil && u.IsActive != *f.Active:
		default:
			out = append(out, u)
		}
		if len(out) == f.Limit {
			break
		}
	}
	return out, nil
}

func (s *memStore) UpdateUser(_ context.Context, id uuid.UUID, upd authsvc.UserUpdate) (authsvc.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return authsvc.User{}, s.fail
	}
	i := s.index(id)
	if i < 0 {
		return authsvc.User{}, authsvc.ErrNotFound
	}
	u := s.users[i]
	if upd.Role != nil && *upd.Role != authsvc.RoleAdmin && s.isLastAdmin(u) {
		return authsvc.User{}, authsvc.ErrLastAdmin
	}
	if upd.Email != nil {
		for _, o := range s.users {
			if o.ID != id && o.Email == *upd.Email {
				return authsvc.User{}, authsvc.ErrEmailExists
			}
		}
		u.Email = *upd.Email
	}
	if upd.Role != nil {
		u.Role = *upd.Role
	}
	u.UpdatedAt = time.Now()
	s.users[i] = u
	return u, nil
}

func (s *memStore) UserTeams(_ context.Context, id uuid.UUID) ([]authsvc.UserTeam, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return nil, s.fail
	}
	var out []authsvc.UserTeam
	for _, t := range s.teams {
		if m, ok := s.members[t.ID][id]; ok {
			out = append(out, authsvc.UserTeam{TeamID: t.ID, TeamName: t.Name, Role: m.Role, AddedAt: m.AddedAt})
		}
	}
	slices.SortFunc(out, func(a, b authsvc.UserTeam) int { return strings.Compare(a.TeamName, b.TeamName) })
	return out, nil
}

func (s *memStore) Disable(_ context.Context, id uuid.UUID) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return time.Time{}, s.fail
	}
	i := s.index(id)
	if i < 0 {
		return time.Time{}, authsvc.ErrNotFound
	}
	if s.isLastAdmin(s.users[i]) {
		return time.Time{}, authsvc.ErrLastAdmin
	}
	now := time.Now()
	s.users[i].IsActive = false
	s.users[i].TokensValidAfter = &now
	return now, nil
}

func (s *memStore) Enable(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	i := s.index(id)
	if i < 0 {
		return authsvc.ErrNotFound
	}
	s.users[i].IsActive = true
	return nil
}

func (s *memStore) setFail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = err
}

type harness struct {
	t      *testing.T
	router http.Handler
	store  *memStore
	jwt    *authjwt.Manager
	mr     *miniredis.Miniredis
	// admin is a signed-in active admin; adminToken is their access token.
	admin      authsvc.User
	adminToken string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	h := &harness{
		t:     t,
		store: newMemStore(),
		jwt:   authjwt.NewManager("test-secret-that-is-at-least-32-chars", time.Minute, time.Hour),
		mr:    mr,
	}
	r := chi.NewRouter()
	r.Route("/api", NewHandler(Deps{Users: h.store, Teams: h.store, JWT: h.jwt, Redis: rdb}).Mount)
	// A route behind RequireAuth only, like every Dash route: it reads no
	// database, so it sees a disable only through the Redis marker.
	r.With(authsvc.RequireAuth(h.jwt, rdb)).Get("/api/dash-probe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h.router = r

	h.admin = h.addUser("root@example.com", "root", authsvc.RoleAdmin)
	h.adminToken = h.token(h.admin.ID)
	return h
}

func (h *harness) addUser(email, username string, role authsvc.Role) authsvc.User {
	h.t.Helper()
	u, err := h.store.Create(context.Background(), authsvc.User{Email: email, Username: username, IsActive: true, Role: role})
	if err != nil {
		h.t.Fatalf("add user %s: %v", email, err)
	}
	return u
}

func (h *harness) token(id uuid.UUID) string {
	h.t.Helper()
	tok, err := h.jwt.IssueAccess(id)
	if err != nil {
		h.t.Fatalf("issue token: %v", err)
	}
	return tok
}

func (h *harness) do(method, path, bearer string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	var buf bytes.Buffer
	if s, ok := body.(string); ok {
		buf.WriteString(s)
	} else if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return v
}

func errorOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decode[struct {
		Error string `json:"error"`
	}](t, rec).Error
}

func TestAdminRoutesNeedAnAdmin(t *testing.T) {
	h := newHarness(t)
	plain := h.addUser("ada@example.com", "ada", authsvc.RoleUser)
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/admin/users"},
		{http.MethodPost, "/api/admin/users"},
		{http.MethodGet, "/api/admin/users/" + plain.ID.String()},
		{http.MethodPatch, "/api/admin/users/" + plain.ID.String()},
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

func TestCreateUser(t *testing.T) {
	h := newHarness(t)

	rec := h.do(http.MethodPost, "/api/admin/users", h.adminToken,
		map[string]string{"email": "  Ada@Example.com ", "username": "Ada", "password": "s3cret-pass"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status %d body %s", rec.Code, rec.Body)
	}
	got := decode[AdminUser](t, rec)
	if got.Email != "ada@example.com" || got.Username != "ada" || got.Role != authsvc.RoleUser || !got.IsActive {
		t.Errorf("created %+v, want ada@example.com / ada / user / active", got)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("response mentions the password: %s", rec.Body)
	}
	stored, _ := h.store.FindByID(context.Background(), got.ID)
	if stored.PasswordHash == "" || stored.PasswordHash == "s3cret-pass" {
		t.Errorf("password stored as %q, want a hash", stored.PasswordHash)
	}

	rec = h.do(http.MethodPost, "/api/admin/users", h.adminToken,
		map[string]string{"email": "grace@example.com", "username": "grace", "password": "s3cret-pass", "role": "admin"})
	if rec.Code != http.StatusCreated || decode[AdminUser](t, rec).Role != authsvc.RoleAdmin {
		t.Errorf("create admin: status %d body %s", rec.Code, rec.Body)
	}

	tests := []struct {
		name string
		body any
		want int
	}{
		{"email taken", map[string]string{"email": "ADA@example.com", "username": "ada2", "password": "s3cret-pass"}, http.StatusConflict},
		{"username taken", map[string]string{"email": "ada2@example.com", "username": "ada", "password": "s3cret-pass"}, http.StatusConflict},
		{"bad email", map[string]string{"email": "nope", "username": "ada3", "password": "s3cret-pass"}, http.StatusBadRequest},
		{"bad username", map[string]string{"email": "ada3@example.com", "username": "a!", "password": "s3cret-pass"}, http.StatusBadRequest},
		{"short password", map[string]string{"email": "ada3@example.com", "username": "ada3", "password": "short"}, http.StatusBadRequest},
		{"unknown role", map[string]string{"email": "ada3@example.com", "username": "ada3", "password": "s3cret-pass", "role": "auditor"}, http.StatusBadRequest},
		{"unknown field", `{"email":"ada3@example.com","username":"ada3","password":"s3cret-pass","is_admin":true}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := h.do(http.MethodPost, "/api/admin/users", h.adminToken, tt.body); rec.Code != tt.want {
				t.Errorf("status %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestListUsersFiltersAndPages(t *testing.T) {
	h := newHarness(t)
	h.addUser("ada@example.com", "ada", authsvc.RoleUser)
	h.addUser("adam@example.com", "adam", authsvc.RoleUser)
	bob := h.addUser("bob@example.com", "bob", authsvc.RoleAdmin)
	h.addUser("carol@ada.dev", "carol", authsvc.RoleUser)
	if _, err := h.store.Disable(context.Background(), bob.ID); err != nil {
		t.Fatal(err)
	}

	names := func(path string) ([]string, string) {
		t.Helper()
		rec := h.do(http.MethodGet, path, h.adminToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d body %s", path, rec.Code, rec.Body)
		}
		page := decode[UserPage](t, rec)
		var out []string
		for _, u := range page.Users {
			out = append(out, u.Username)
		}
		return out, page.NextPageToken
	}

	if got, next := names("/api/admin/users"); !slices.Equal(got, []string{"root", "ada", "adam", "bob", "carol"}) || next != "" {
		t.Errorf("all users = %v (next %q), want creation order and no next page", got, next)
	}
	if got, _ := names("/api/admin/users?q=ADA"); !slices.Equal(got, []string{"ada", "adam"}) {
		t.Errorf("q=ADA = %v, want ada and adam (email prefix too, case-insensitive)", got)
	}
	if got, _ := names("/api/admin/users?role=admin"); !slices.Equal(got, []string{"root", "bob"}) {
		t.Errorf("role=admin = %v", got)
	}
	if got, _ := names("/api/admin/users?active=false"); !slices.Equal(got, []string{"bob"}) {
		t.Errorf("active=false = %v", got)
	}

	var all []string
	path := "/api/admin/users?page_size=2"
	for range 10 {
		got, next := names(path)
		all = append(all, got...)
		if next == "" {
			break
		}
		path = "/api/admin/users?page_size=2&page_token=" + next
	}
	if !slices.Equal(all, []string{"root", "ada", "adam", "bob", "carol"}) {
		t.Errorf("pages of 2 = %v", all)
	}

	for _, bad := range []string{"page_size=0", "page_size=101", "page_size=x", "page_token=!!", "page_token=AAAA", "role=auditor", "active=maybe"} {
		if rec := h.do(http.MethodGet, "/api/admin/users?"+bad, h.adminToken, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("?%s: status %d, want 400", bad, rec.Code)
		}
	}
}

func TestGetUser(t *testing.T) {
	h := newHarness(t)
	ada := h.addUser("ada@example.com", "ada", authsvc.RoleUser)
	ctx := context.Background()
	payments, _ := h.store.CreateTeam(ctx, "payments", "")
	if _, _, err := h.store.PutTeamMember(ctx, payments.ID, ada.ID, authsvc.RoleMaintainer); err != nil {
		t.Fatal(err)
	}
	team := payments.ID

	rec := h.do(http.MethodGet, "/api/admin/users/"+ada.ID.String(), h.adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	got := decode[AdminUserDetail](t, rec)
	if got.Username != "ada" || len(got.Teams) != 1 || got.Teams[0].Team.ID != team || got.Teams[0].Team.Name != "payments" || got.Teams[0].Role != authsvc.RoleMaintainer {
		t.Errorf("got %+v", got)
	}

	// No teams is an empty list, not null.
	rec = h.do(http.MethodGet, "/api/admin/users/"+h.admin.ID.String(), h.adminToken, nil)
	if !strings.Contains(rec.Body.String(), `"teams":[]`) {
		t.Errorf("user without teams: %s", rec.Body)
	}

	for _, id := range []string{uuid.NewString(), "not-a-uuid"} {
		if rec := h.do(http.MethodGet, "/api/admin/users/"+id, h.adminToken, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", id, rec.Code)
		}
	}
}

func TestUpdateUser(t *testing.T) {
	h := newHarness(t)
	ada := h.addUser("ada@example.com", "ada", authsvc.RoleUser)
	h.addUser("grace@example.com", "grace", authsvc.RoleUser)
	path := "/api/admin/users/" + ada.ID.String()

	rec := h.do(http.MethodPatch, path, h.adminToken, map[string]any{"email": " Ada@New.example ", "role": "admin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if got := decode[AdminUser](t, rec); got.Email != "ada@new.example" || got.Role != authsvc.RoleAdmin {
		t.Errorf("updated %+v", got)
	}

	tests := []struct {
		name string
		body any
		want int
	}{
		{"nothing", `{}`, http.StatusBadRequest},
		{"bad email", map[string]any{"email": "nope"}, http.StatusBadRequest},
		{"unknown role", map[string]any{"role": "maintainer"}, http.StatusBadRequest},
		{"username is not editable", map[string]any{"username": "ada2"}, http.StatusBadRequest},
		{"email taken", map[string]any{"email": "grace@example.com"}, http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := h.do(http.MethodPatch, path, h.adminToken, tt.body); rec.Code != tt.want {
				t.Errorf("status %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
	if rec := h.do(http.MethodPatch, "/api/admin/users/"+uuid.NewString(), h.adminToken, map[string]any{"role": "user"}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user: status %d, want 404", rec.Code)
	}
}

func TestDisableUserEndsAccessAtOnce(t *testing.T) {
	h := newHarness(t)
	ada := h.addUser("ada@example.com", "ada", authsvc.RoleAdmin)
	adaToken := h.token(ada.ID)
	path := "/api/admin/users/" + ada.ID.String()

	if rec := h.do(http.MethodGet, "/api/dash-probe", adaToken, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("before disable: status %d", rec.Code)
	}
	rec := h.do(http.MethodPatch, path, h.adminToken, map[string]any{"is_active": false})
	if rec.Code != http.StatusOK || decode[AdminUser](t, rec).IsActive {
		t.Fatalf("disable: status %d body %s", rec.Code, rec.Body)
	}
	// Her access token was issued before the disable, so it stops working
	// at once: on admin routes, which read the database, and on routes
	// behind RequireAuth only, which do not.
	if rec := h.do(http.MethodGet, "/api/admin/users", adaToken, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("admin route after disable: status %d, want 401", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/api/dash-probe", adaToken, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("RequireAuth-only route after disable: status %d, want 401", rec.Code)
	}

	rec = h.do(http.MethodPatch, path, h.adminToken, map[string]any{"is_active": true})
	if rec.Code != http.StatusOK || !decode[AdminUser](t, rec).IsActive {
		t.Errorf("enable: status %d body %s", rec.Code, rec.Body)
	}
}

func TestAdminsCannotLockThemselvesOut(t *testing.T) {
	h := newHarness(t)
	// A second admin, so the last-admin rule is not what refuses below.
	grace := h.addUser("grace@example.com", "grace", authsvc.RoleAdmin)
	self := "/api/admin/users/" + h.admin.ID.String()
	for _, body := range []map[string]any{{"role": "user"}, {"is_active": false}} {
		rec := h.do(http.MethodPatch, self, h.adminToken, body)
		if rec.Code != http.StatusConflict || errorOf(t, rec) == "" {
			t.Errorf("own account %v: status %d body %s, want 409 with a message", body, rec.Code, rec.Body)
		}
	}
	// Changing their own email is fine.
	if rec := h.do(http.MethodPatch, self, h.adminToken, map[string]any{"email": "root@new.example"}); rec.Code != http.StatusOK {
		t.Errorf("own email: status %d body %s", rec.Code, rec.Body)
	}

	// With a second admin, one may demote the other; the demoted one loses
	// the admin plane on their next request.
	if rec := h.do(http.MethodPatch, "/api/admin/users/"+grace.ID.String(), h.adminToken, map[string]any{"role": "user"}); rec.Code != http.StatusOK {
		t.Fatalf("demote grace: status %d body %s", rec.Code, rec.Body)
	}
	if rec := h.do(http.MethodGet, "/api/admin/users", h.token(grace.ID), nil); rec.Code != http.StatusForbidden {
		t.Errorf("demoted admin: status %d, want 403", rec.Code)
	}
}

// Through the API the last active admin can only be reached by two admins
// removing each other at the same moment; the store's row lock stops the
// second one (TestPostgresUpdateUser). Here: its error is a 409.
func TestLastAdminIsAConflict(t *testing.T) {
	h := newHarness(t)
	grace := h.addUser("grace@example.com", "grace", authsvc.RoleAdmin)
	h.store.lastAdmin = grace.ID
	for _, body := range []map[string]any{{"role": "user"}, {"is_active": false}} {
		rec := h.do(http.MethodPatch, "/api/admin/users/"+grace.ID.String(), h.adminToken, body)
		if rec.Code != http.StatusConflict {
			t.Errorf("%v: status %d, want 409 (body %s)", body, rec.Code, rec.Body)
		}
	}
}

func TestDatabaseFaultIsNotUnauthorized(t *testing.T) {
	h := newHarness(t)
	h.store.setFail(errors.New("connection refused"))
	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/users"},
		{http.MethodGet, "/api/admin/users/" + h.admin.ID.String()},
	} {
		if rec := h.do(rt.method, rt.path, h.adminToken, nil); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status %d, want 503", rt.method, rt.path, rec.Code)
		}
	}
}
