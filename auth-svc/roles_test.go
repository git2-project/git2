package authsvc

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestCan(t *testing.T) {
	tests := []struct {
		role Role
		perm Permission
		want bool
	}{
		{RoleAdmin, PermAdminRead, true},
		{RoleAdmin, PermAdminWrite, true},
		{RoleAuditor, PermAdminRead, true},
		{RoleAuditor, PermAdminWrite, false},
		{RoleUser, PermAdminRead, false},
		{RoleUser, PermAdminWrite, false},
		// Team roles grant nothing at instance level, and an unknown role
		// (say, one added by a newer migration) grants nothing at all.
		{RoleMaintainer, PermAdminRead, false},
		{Role("root"), PermAdminRead, false},
		{Role(""), PermAdminRead, false},
		// Instance roles do not carry team permissions.
		{RoleAdmin, PermTeamManage, false},
	}
	for _, tt := range tests {
		if got := Can(tt.role, tt.perm); got != tt.want {
			t.Errorf("Can(%q, %q) = %v, want %v", tt.role, tt.perm, got, tt.want)
		}
	}
}

func TestCanInTeam(t *testing.T) {
	tests := []struct {
		role Role
		perm Permission
		want bool
	}{
		{RoleMaintainer, PermTeamManage, true},
		{RoleMember, PermTeamManage, false},
		{RoleAdmin, PermTeamManage, false},
		{RoleMaintainer, PermAdminRead, false},
	}
	for _, tt := range tests {
		if got := CanInTeam(tt.role, tt.perm); got != tt.want {
			t.Errorf("CanInTeam(%q, %q) = %v, want %v", tt.role, tt.perm, got, tt.want)
		}
	}
}

// adminRouter mounts two routes behind RequireAuth + RequirePermission, the
// way admin-api will: a read and a write.
func (h *harness) adminRouter() http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r := chi.NewRouter()
	r.Use(RequireAuth(h.jwt, h.redis))
	r.With(RequirePermission(h.store, PermAdminRead)).Get("/admin/thing", ok)
	r.With(RequirePermission(h.store, PermAdminWrite)).Post("/admin/thing", ok)
	return r
}

func callAdmin(t *testing.T, router http.Handler, method, bearer string) int {
	t.Helper()
	req := httptest.NewRequest(method, "/admin/thing", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

func TestRequirePermission(t *testing.T) {
	h := newHarness(t)
	admin := h.adminRouter()
	h.register("ada@example.com", "ada")
	access, _ := h.login("ada@example.com")

	tests := []struct {
		name      string
		role      Role
		wantRead  int
		wantWrite int
	}{
		{"user", RoleUser, http.StatusForbidden, http.StatusForbidden},
		{"auditor", RoleAuditor, http.StatusNoContent, http.StatusForbidden},
		{"admin", RoleAdmin, http.StatusNoContent, http.StatusNoContent},
		// The same access token, demoted again: the role is read from the
		// store on every request, so the change applies at once.
		{"demoted back to user", RoleUser, http.StatusForbidden, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h.store.setRole("ada@example.com", tt.role)
			if got := callAdmin(t, admin, http.MethodGet, access); got != tt.wantRead {
				t.Errorf("read: status %d, want %d", got, tt.wantRead)
			}
			if got := callAdmin(t, admin, http.MethodPost, access); got != tt.wantWrite {
				t.Errorf("write: status %d, want %d", got, tt.wantWrite)
			}
		})
	}

	t.Run("no token", func(t *testing.T) {
		if got := callAdmin(t, admin, http.MethodGet, ""); got != http.StatusUnauthorized {
			t.Errorf("status %d, want 401", got)
		}
	})

	t.Run("disabled account", func(t *testing.T) {
		h.store.setRole("ada@example.com", RoleAdmin)
		h.store.setActive("ada@example.com", false)
		t.Cleanup(func() { h.store.setActive("ada@example.com", true) })
		if got := callAdmin(t, admin, http.MethodGet, access); got != http.StatusUnauthorized {
			t.Errorf("status %d, want 401", got)
		}
	})

	t.Run("database fault is not unauthorized", func(t *testing.T) {
		h.store.setRole("ada@example.com", RoleAdmin)
		h.store.setFail(errors.New("connection refused"))
		t.Cleanup(func() { h.store.setFail(nil) })
		if got := callAdmin(t, admin, http.MethodGet, access); got != http.StatusServiceUnavailable {
			t.Errorf("status %d, want 503", got)
		}
	})
}

func TestRequirePermissionWithoutRequireAuth(t *testing.T) {
	h := newHarness(t)
	// Mounted by mistake without RequireAuth: there is no caller, so it must
	// refuse rather than look up uuid.Nil.
	r := chi.NewRouter()
	r.With(RequirePermission(h.store, PermAdminRead)).Get("/admin/thing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	if got := callAdmin(t, r, http.MethodGet, ""); got != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", got)
	}
}
