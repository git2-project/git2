package authsvc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const fixedSetupToken = "fixed-setup-token-for-tests-0123456789"

func setupBody(token, email, username string) SetupAdminRequest {
	return SetupAdminRequest{SetupToken: token, Email: email, Username: username, Password: "s3cret-pass"}
}

// resetRateLimits clears the per-IP counters, as if a minute had passed.
func (h *harness) resetRateLimits() {
	h.t.Helper()
	for _, k := range h.mr.Keys() {
		if strings.HasPrefix(k, "rl:") {
			h.mr.Del(k)
		}
	}
}

func (h *harness) needsSetup() bool {
	h.t.Helper()
	rec := h.do(http.MethodGet, "/api/setup/status", "", nil)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("setup status: %d %s", rec.Code, rec.Body)
	}
	var got SetupStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		h.t.Fatalf("decode status: %v", err)
	}
	return got.NeedsSetup
}

func TestPrepareSetup(t *testing.T) {
	ctx := context.Background()

	t.Run("generates a token and stores only its hash", func(t *testing.T) {
		s := newMemStore()
		st, err := PrepareSetup(ctx, s, "")
		if err != nil || len(st.Token) < MinSetupTokenLen {
			t.Fatalf("PrepareSetup = %+v, %v; want a token of at least %d chars", st, err, MinSetupTokenLen)
		}
		if s.setupHash != hashSetupToken(st.Token) || strings.Contains(s.setupHash, st.Token) {
			t.Errorf("stored %q, want only the SHA-256 of the token", s.setupHash)
		}
		if d := time.Until(st.ExpiresAt); d < SetupTokenTTL-time.Minute || d > SetupTokenTTL {
			t.Errorf("ExpiresAt in %v, want about %v", d, SetupTokenTTL)
		}
	})

	t.Run("a second start keeps an unexpired token", func(t *testing.T) {
		s := newMemStore()
		first, _ := PrepareSetup(ctx, s, "")
		second, err := PrepareSetup(ctx, s, "")
		if err != nil || !second.Kept || second.Token != "" || s.setupHash != hashSetupToken(first.Token) {
			t.Errorf("second start = %+v, %v; want the first token kept and nothing printed", second, err)
		}
	})

	t.Run("an expired token is replaced", func(t *testing.T) {
		s := newMemStore()
		first, _ := PrepareSetup(ctx, s, "")
		s.setupCreated = s.setupCreated.Add(-SetupTokenTTL - time.Second)
		second, err := PrepareSetup(ctx, s, "")
		if err != nil || second.Token == "" || second.Token == first.Token || s.setupHash != hashSetupToken(second.Token) {
			t.Errorf("start after expiry = %+v, %v; want a new token", second, err)
		}
	})

	t.Run("fixed token is stored, replaces any other, and is not printed", func(t *testing.T) {
		s := newMemStore()
		_, _ = PrepareSetup(ctx, s, "")
		st, err := PrepareSetup(ctx, s, fixedSetupToken)
		if err != nil || st.Token != "" || st.Kept || st.Done {
			t.Fatalf("PrepareSetup = %+v, %v; want stored, nothing to print", st, err)
		}
		if s.setupHash != hashSetupToken(fixedSetupToken) {
			t.Errorf("fixed token not stored")
		}
	})

	t.Run("short fixed token is refused", func(t *testing.T) {
		if _, err := PrepareSetup(ctx, newMemStore(), "short"); err == nil {
			t.Error("want an error for a short GIT2_SETUP_TOKEN")
		}
	})

	t.Run("nothing to do once an active admin exists", func(t *testing.T) {
		s := newMemStore()
		_, _ = s.Create(ctx, User{Email: "a@example.com", Username: "adm", Role: RoleAdmin, IsActive: true})
		st, err := PrepareSetup(ctx, s, "")
		if err != nil || !st.Done || st.Token != "" || s.setupHash != "" {
			t.Errorf("PrepareSetup = %+v, %v, stored %q; want Done and nothing stored", st, err, s.setupHash)
		}
	})
}

func TestSetupAdmin(t *testing.T) {
	h := newHarness(t)
	if _, err := PrepareSetup(context.Background(), h.store, fixedSetupToken); err != nil {
		t.Fatal(err)
	}
	// An ordinary account from before setup must not count as an admin.
	h.register("early@example.com", "early")
	if !h.needsSetup() {
		t.Fatal("needs_setup = false before the first admin")
	}

	refusals := []struct {
		name string
		body any
		want int
	}{
		{"wrong token", setupBody("wrong-token-wrong-token-wrong-token", "root@example.com", "root"), http.StatusForbidden},
		{"empty token", setupBody("", "root@example.com", "root"), http.StatusForbidden},
		{"bad email", setupBody(fixedSetupToken, "nope", "root"), http.StatusBadRequest},
		{"bad username", setupBody(fixedSetupToken, "root@example.com", "-r"), http.StatusBadRequest},
		{"short password", SetupAdminRequest{SetupToken: fixedSetupToken, Email: "root@example.com", Username: "root", Password: "short"}, http.StatusBadRequest},
		{"unknown field", `{"setup_token":"x","email":"root@example.com","username":"root","password":"s3cret-pass","role":"admin"}`, http.StatusBadRequest},
		{"email taken", setupBody(fixedSetupToken, "early@example.com", "root"), http.StatusConflict},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.do(http.MethodPost, "/api/setup/admin", "", tt.body)
			if rec.Code != tt.want {
				t.Errorf("status %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
	if !h.needsSetup() {
		t.Fatal("a refused request burned the token")
	}

	rec := h.do(http.MethodPost, "/api/setup/admin", "", setupBody(fixedSetupToken, " Root@Example.com ", "Root"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: status %d body %s", rec.Code, rec.Body)
	}
	var created UserResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Role != RoleAdmin || created.Email != "root@example.com" || created.Username != "root" {
		t.Errorf("created %+v, want admin root / root@example.com", created)
	}
	if h.needsSetup() {
		t.Error("needs_setup = true after the first admin")
	}

	// The new admin signs in normally and passes admin routes.
	access, _ := h.login("root@example.com")
	if code := callAdmin(t, h.adminRouter(), http.MethodPost, access); code != http.StatusNoContent {
		t.Errorf("admin write: status %d, want 204", code)
	}

	// The token is burned.
	rec = h.do(http.MethodPost, "/api/setup/admin", "", setupBody(fixedSetupToken, "two@example.com", "two"))
	if rec.Code != http.StatusConflict {
		t.Errorf("second setup: status %d, want 409 (body %s)", rec.Code, rec.Body)
	}
	// Once done, the wizard gets 409 whatever it sends. Start a fresh
	// rate-limit window: this test sends more than 10 setup requests.
	h.resetRateLimits()
	for name, body := range map[string]any{
		"empty token":  setupBody("", "two@example.com", "two"),
		"invalid body": `{"nope":true}`,
	} {
		if rec := h.do(http.MethodPost, "/api/setup/admin", "", body); rec.Code != http.StatusConflict {
			t.Errorf("after setup, %s: status %d, want 409 (body %s)", name, rec.Code, rec.Body)
		}
	}
	// And a restart does not bring it back.
	if st, _ := PrepareSetup(context.Background(), h.store, ""); !st.Done || st.Token != "" {
		t.Errorf("PrepareSetup = %+v although an admin exists", st)
	}
}

func TestSetupAdminConcurrent(t *testing.T) {
	h := newHarness(t)
	if _, err := PrepareSetup(context.Background(), h.store, fixedSetupToken); err != nil {
		t.Fatal(err)
	}
	const n = 8
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "admin" + string(rune('a'+i))
			codes[i] = h.do(http.MethodPost, "/api/setup/admin", "", setupBody(fixedSetupToken, name+"@example.com", name)).Code
		}()
	}
	wg.Wait()
	created := 0
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	if created != 1 {
		t.Errorf("%d admins created, want exactly 1 (codes %v)", created, codes)
	}
}

func TestSetupTokenExpired(t *testing.T) {
	h := newHarness(t)
	if _, err := PrepareSetup(context.Background(), h.store, fixedSetupToken); err != nil {
		t.Fatal(err)
	}
	h.store.setupCreated = h.store.setupCreated.Add(-SetupTokenTTL - time.Second)
	rec := h.do(http.MethodPost, "/api/setup/admin", "", setupBody(fixedSetupToken, "root@example.com", "root"))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("expired token: status %d body %s, want 403 mentioning expiry", rec.Code, rec.Body)
	}
	// A wrong token is told nothing about expiry.
	rec = h.do(http.MethodPost, "/api/setup/admin", "", setupBody("wrong-token-wrong-token-wrong-token", "root@example.com", "root"))
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("wrong token: status %d body %s, want a plain 403", rec.Code, rec.Body)
	}
}

func TestSetupDatabaseFault(t *testing.T) {
	h := newHarness(t)
	h.store.setFail(errors.New("db down"))
	if rec := h.do(http.MethodGet, "/api/setup/status", "", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: %d, want 503", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/api/setup/admin", "", setupBody(fixedSetupToken, "root@example.com", "root")); rec.Code != http.StatusInternalServerError {
		t.Errorf("admin: %d, want 500 (never 401 or 403)", rec.Code)
	}
}
