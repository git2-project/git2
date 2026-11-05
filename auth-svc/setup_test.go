package authsvc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const fixedSetupToken = "fixed-setup-token-for-tests-0123456789"

func setupBody(token, email, username string) SetupAdminRequest {
	return SetupAdminRequest{SetupToken: token, Email: email, Username: username, Password: "s3cret-pass"}
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
		tok, err := PrepareSetup(ctx, s, "")
		if err != nil || len(tok) < MinSetupTokenLen {
			t.Fatalf("PrepareSetup = %q, %v; want a token of at least %d chars", tok, err, MinSetupTokenLen)
		}
		if s.setupHash != hashSetupToken(tok) || strings.Contains(s.setupHash, tok) {
			t.Errorf("stored %q, want only the SHA-256 of the token", s.setupHash)
		}
		// A restart replaces it, so a lost log is fixed by restarting.
		tok2, _ := PrepareSetup(ctx, s, "")
		if tok2 == tok || s.setupHash != hashSetupToken(tok2) {
			t.Errorf("second start did not replace the token")
		}
	})

	t.Run("fixed token is stored but not returned for printing", func(t *testing.T) {
		s := newMemStore()
		tok, err := PrepareSetup(ctx, s, fixedSetupToken)
		if err != nil || tok != "" {
			t.Fatalf("PrepareSetup = %q, %v; want \"\", nil", tok, err)
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

	t.Run("nothing to do once an admin exists", func(t *testing.T) {
		s := newMemStore()
		_, _ = s.Create(ctx, User{Email: "a@example.com", Username: "adm", Role: RoleAdmin})
		tok, err := PrepareSetup(ctx, s, "")
		if err != nil || tok != "" || s.setupHash != "" {
			t.Errorf("PrepareSetup = %q, %v, stored %q; want nothing", tok, err, s.setupHash)
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
	// And a restart does not bring it back.
	if tok, _ := PrepareSetup(context.Background(), h.store, ""); tok != "" {
		t.Error("PrepareSetup issued a token although an admin exists")
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
