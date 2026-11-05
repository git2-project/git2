package authsvc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// loginUser signs in and returns the user id with the token pair.
func (h *harness) loginUser(email string) (uuid.UUID, string, string) {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/api/auth/login", "", LoginRequest{Email: email, Password: "s3cret-pass"})
	if rec.Code != http.StatusOK {
		h.t.Fatalf("login %s: status %d body %s", email, rec.Code, rec.Body)
	}
	var body LoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		h.t.Fatalf("decode login: %v", err)
	}
	return body.User.ID, rec.Header().Get("X-Access-Token"), rec.Header().Get("X-Refresh-Token")
}

func (h *harness) refreshStatus(refresh string) int {
	h.t.Helper()
	return h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{RefreshToken: refresh}).Code
}

func TestDisableUserEndsEverythingAtOnce(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.register("ada@example.com", "ada")
	h.register("bob@example.com", "bob")

	// Two devices for Ada, one for Bob, who must not be affected.
	id, access1, refresh1 := h.loginUser("ada@example.com")
	_, access2, refresh2 := h.loginUser("ada@example.com")
	_, bobAccess, bobRefresh := h.loginUser("bob@example.com")

	if err := DisableUser(ctx, h.store, h.jwt, h.redis, id); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}

	for name, tok := range map[string]string{"device 1": access1, "device 2": access2} {
		if rec := h.do(http.MethodGet, "/api/auth/me", tok, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s access token: status %d, want 401 (body %s)", name, rec.Code, rec.Body)
		}
	}
	if code := callAdmin(t, h.adminRouter(), http.MethodGet, access1); code != http.StatusUnauthorized {
		t.Errorf("admin route: status %d, want 401", code)
	}
	for name, tok := range map[string]string{"device 1": refresh1, "device 2": refresh2} {
		if code := h.refreshStatus(tok); code != http.StatusUnauthorized {
			t.Errorf("%s refresh: status %d, want 401", name, code)
		}
	}
	if h.mr.Exists(userSessionsKey(id)) {
		t.Errorf("%s still exists", userSessionsKey(id))
	}
	if rec := h.do(http.MethodPost, "/api/auth/login", "", LoginRequest{Email: "ada@example.com", Password: "s3cret-pass"}); rec.Code != http.StatusForbidden {
		t.Errorf("login while disabled: status %d, want 403", rec.Code)
	}

	// Bob is untouched.
	if rec := h.do(http.MethodGet, "/api/auth/me", bobAccess, nil); rec.Code != http.StatusOK {
		t.Errorf("other user's access token: status %d, want 200", rec.Code)
	}
	if code := h.refreshStatus(bobRefresh); code != http.StatusOK {
		t.Errorf("other user's refresh: status %d, want 200", code)
	}

	// Disabling twice is fine.
	if err := DisableUser(ctx, h.store, h.jwt, h.redis, id); err != nil {
		t.Errorf("second DisableUser: %v", err)
	}
}

func TestEnableUserAfterDisable(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.register("ada@example.com", "ada")
	id, oldAccess, _ := h.loginUser("ada@example.com")

	if err := DisableUser(ctx, h.store, h.jwt, h.redis, id); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}
	if err := EnableUser(ctx, h.store, id); err != nil {
		t.Fatalf("EnableUser: %v", err)
	}

	// A token from before the disable stays dead, even right after enabling.
	if rec := h.do(http.MethodGet, "/api/auth/me", oldAccess, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("pre-disable token: status %d, want 401", rec.Code)
	}
	// A fresh sign-in works at once: tokens carry milliseconds, so it is
	// newer than the marker even within the same second.
	_, newAccess, newRefresh := h.loginUser("ada@example.com")
	if rec := h.do(http.MethodGet, "/api/auth/me", newAccess, nil); rec.Code != http.StatusOK {
		t.Errorf("new token: status %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if code := h.refreshStatus(newRefresh); code != http.StatusOK {
		t.Errorf("new refresh: status %d, want 200", code)
	}
}

func TestDisableUserErrors(t *testing.T) {
	t.Run("unknown user", func(t *testing.T) {
		h := newHarness(t)
		if err := DisableUser(context.Background(), h.store, h.jwt, h.redis, uuid.New()); !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("database fault touches nothing", func(t *testing.T) {
		h := newHarness(t)
		h.register("ada@example.com", "ada")
		id, access, _ := h.loginUser("ada@example.com")
		h.store.setFail(errors.New("db down"))
		if err := DisableUser(context.Background(), h.store, h.jwt, h.redis, id); err == nil || errors.Is(err, ErrSessionsNotEnded) {
			t.Errorf("err = %v, want the database error", err)
		}
		h.store.setFail(nil)
		if rec := h.do(http.MethodGet, "/api/auth/me", access, nil); rec.Code != http.StatusOK {
			t.Errorf("access token after failed disable: status %d, want 200", rec.Code)
		}
	})

	t.Run("redis fault after the database", func(t *testing.T) {
		h := newHarness(t)
		h.register("ada@example.com", "ada")
		id, _, refresh := h.loginUser("ada@example.com")
		h.mr.SetError("redis down")
		err := DisableUser(context.Background(), h.store, h.jwt, h.redis, id)
		if !errors.Is(err, ErrSessionsNotEnded) {
			t.Fatalf("err = %v, want ErrSessionsNotEnded", err)
		}
		h.mr.SetError("")
		// The database half held: refresh reads is_active and refuses.
		if code := h.refreshStatus(refresh); code != http.StatusUnauthorized {
			t.Errorf("refresh after partial disable: status %d, want 401", code)
		}
	})
}

// The access check fails open: a Redis outage must not turn every
// authenticated request into an error, and never into a 401.
func TestRequireAuthFailsOpenOnRedisFault(t *testing.T) {
	h := newHarness(t)
	h.register("ada@example.com", "ada")
	_, access, _ := h.loginUser("ada@example.com")

	h.mr.SetError("redis down")
	t.Cleanup(func() { h.mr.SetError("") })
	if rec := h.do(http.MethodGet, "/api/auth/me", access, nil); rec.Code != http.StatusOK {
		t.Errorf("status %d, want 200 (body %s)", rec.Code, rec.Body)
	}
}

func TestLogoutForgetsTheSession(t *testing.T) {
	h := newHarness(t)
	h.register("ada@example.com", "ada")
	id, access, refresh := h.loginUser("ada@example.com")
	if n, _ := h.mr.SMembers(userSessionsKey(id)); len(n) != 1 {
		t.Fatalf("sessions after login = %v, want 1", n)
	}
	if rec := h.do(http.MethodPost, "/api/auth/logout", access, LogoutRequest{RefreshToken: refresh}); rec.Code != http.StatusOK {
		t.Fatalf("logout: status %d", rec.Code)
	}
	if n, _ := h.mr.SMembers(userSessionsKey(id)); len(n) != 0 {
		t.Errorf("sessions after logout = %v, want none", n)
	}
}

func TestRefreshKeepsTheSessionListed(t *testing.T) {
	h := newHarness(t)
	h.register("ada@example.com", "ada")
	id, _, refresh := h.loginUser("ada@example.com")
	rec := h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{RefreshToken: refresh})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: status %d", rec.Code)
	}
	// The set's TTL follows the session's, so it must still be there and
	// still expire.
	if n, _ := h.mr.SMembers(userSessionsKey(id)); len(n) != 1 {
		t.Errorf("sessions after refresh = %v, want 1", n)
	}
	if ttl := h.mr.TTL(userSessionsKey(id)); ttl <= 0 {
		t.Errorf("%s TTL = %v, want > 0", userSessionsKey(id), ttl)
	}

	// Replaying the old token kills the session and drops it from the set.
	if code := h.refreshStatus(refresh); code != http.StatusUnauthorized {
		t.Fatalf("replay: status %d, want 401", code)
	}
	if n, _ := h.mr.SMembers(userSessionsKey(id)); len(n) != 0 {
		t.Errorf("sessions after replay = %v, want none", n)
	}
}
