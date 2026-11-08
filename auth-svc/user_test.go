package authsvc

import (
	"net/http"
	"strings"
	"testing"
)

func TestValidateUsernameReserved(t *testing.T) {
	// The gateway contract's list (B-05) plus the Dash routes.
	reserved := []string{
		"api", "admin", "healthz", "readyz", "assets", "static",
		"login", "logout", "settings", "explore", "new",
		"sign-in", "register",
	}
	if len(reserved) != len(reservedNames) {
		t.Errorf("reservedNames has %d names, this test %d: keep them in step", len(reservedNames), len(reserved))
	}
	for _, name := range reserved {
		err := validateUsername(name)
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("validateUsername(%q) = %v, want a reserved-name error", name, err)
		}
	}
	// Only exact names are reserved.
	for _, name := range []string{"admins", "api-docs", "newton", "my-settings", "static_site"} {
		if err := validateUsername(name); err != nil {
			t.Errorf("validateUsername(%q) = %v, want ok", name, err)
		}
	}
}

// Register refuses a reserved name, also when it only becomes one after
// trimming and lowercasing. Its own harness: TestRegister is near the
// per-IP limit of the register route.
func TestRegisterRefusesReservedNames(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"admin", " Sign-In "} {
		rec := h.do(http.MethodPost, "/api/auth/register", "", RegisterRequest{Email: "bob@example.com", Username: name, Password: "s3cret-pass"})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "reserved") {
			t.Errorf("register %q: status %d body %s, want 400 reserved", name, rec.Code, rec.Body)
		}
	}
}
