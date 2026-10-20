package authsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// memStore is an in-memory UserStore with the same uniqueness rules as the
// auth_svc.users constraints.
type memStore struct {
	mu    sync.Mutex
	users map[uuid.UUID]User
	// fail, when set, is returned by every lookup to simulate a database fault.
	fail error
	// lookupDelay slows FindByID down, widening any race around it.
	lookupDelay time.Duration
}

func newMemStore() *memStore { return &memStore{users: map[uuid.UUID]User{}} }

func (s *memStore) Create(_ context.Context, u User) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.users {
		if existing.Email == u.Email {
			return User{}, ErrEmailExists
		}
		if existing.Username == u.Username {
			return User{}, ErrUsernameExists
		}
	}
	u.ID = uuid.New()
	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	s.users[u.ID] = u
	return u, nil
}

func (s *memStore) FindByEmail(_ context.Context, email string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return User{}, s.fail
	}
	for _, u := range s.users {
		if u.Email == email {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (s *memStore) FindByID(_ context.Context, id uuid.UUID) (User, error) {
	time.Sleep(s.lookupDelay)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return User{}, s.fail
	}
	if u, ok := s.users[id]; ok {
		return u, nil
	}
	return User{}, ErrNotFound
}

func (s *memStore) setFail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = err
}

func (s *memStore) setActive(email string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, u := range s.users {
		if u.Email == email {
			u.IsActive = active
			s.users[id] = u
		}
	}
}

type harness struct {
	t      *testing.T
	router http.Handler
	store  *memStore
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	store := newMemStore()
	h, err := NewHandler(Deps{
		Users: store,
		JWT:   authjwt.NewManager("test-secret-that-is-at-least-32-chars", time.Minute, time.Hour),
		Redis: rdb,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	r := chi.NewRouter()
	r.Route("/api", h.Mount)
	return &harness{t: t, router: r, store: store}
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

func (h *harness) register(email, username string) {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/api/auth/register", "", RegisterRequest{Email: email, Username: username, Password: "s3cret-pass"})
	if rec.Code != http.StatusCreated {
		h.t.Fatalf("register %s: status %d body %s", email, rec.Code, rec.Body)
	}
}

// login returns the access and refresh tokens from the response headers.
func (h *harness) login(email string) (string, string) {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/api/auth/login", "", LoginRequest{Email: email, Password: "s3cret-pass"})
	if rec.Code != http.StatusOK {
		h.t.Fatalf("login %s: status %d body %s", email, rec.Code, rec.Body)
	}
	return rec.Header().Get("X-Access-Token"), rec.Header().Get("X-Refresh-Token")
}

func TestRegister(t *testing.T) {
	h := newHarness(t)

	rec := h.do(http.MethodPost, "/api/auth/register", "", RegisterRequest{
		Email: "  Ada@Example.COM ", Username: "Ada", Password: "s3cret-pass",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var got UserResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Email != "ada@example.com" || got.Username != "ada" {
		t.Errorf("stored %q / %q, want normalized ada@example.com / ada", got.Email, got.Username)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("response leaks a password field: %s", rec.Body)
	}

	tests := []struct {
		name string
		body any
		want int
	}{
		{"duplicate email", RegisterRequest{Email: "ada@example.com", Username: "other", Password: "s3cret-pass"}, http.StatusConflict},
		{"duplicate username", RegisterRequest{Email: "new@example.com", Username: "ADA", Password: "s3cret-pass"}, http.StatusConflict},
		{"bad email", RegisterRequest{Email: "nope", Username: "bob", Password: "s3cret-pass"}, http.StatusBadRequest},
		{"bad username", RegisterRequest{Email: "bob@example.com", Username: "-b", Password: "s3cret-pass"}, http.StatusBadRequest},
		{"short password", RegisterRequest{Email: "bob@example.com", Username: "bob", Password: "short"}, http.StatusBadRequest},
		{"password over bcrypt limit", RegisterRequest{Email: "bob@example.com", Username: "bob", Password: strings.Repeat("a", 73)}, http.StatusBadRequest},
		{"unknown field", `{"email":"bob@example.com","username":"bob","password":"s3cret-pass","admin":true}`, http.StatusBadRequest},
		{"not json", `{`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := h.do(http.MethodPost, "/api/auth/register", "", tt.body); rec.Code != tt.want {
				t.Errorf("status %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	h := newHarness(t)
	h.register("ada@example.com", "ada")

	access, refresh := h.login("ADA@example.com")
	if access == "" || refresh == "" {
		t.Fatalf("missing token headers: access=%q refresh=%q", access, refresh)
	}

	tests := []struct {
		name string
		body LoginRequest
		want int
	}{
		{"wrong password", LoginRequest{Email: "ada@example.com", Password: "wrong-pass"}, http.StatusUnauthorized},
		{"unknown email", LoginRequest{Email: "who@example.com", Password: "s3cret-pass"}, http.StatusUnauthorized},
		{"empty fields", LoginRequest{}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.do(http.MethodPost, "/api/auth/login", "", tt.body)
			if rec.Code != tt.want {
				t.Errorf("status %d, want %d", rec.Code, tt.want)
			}
			if rec.Header().Get("X-Access-Token") != "" {
				t.Error("failed login still returned a token")
			}
		})
	}

	t.Run("disabled account", func(t *testing.T) {
		h.store.setActive("ada@example.com", false)
		if rec := h.do(http.MethodPost, "/api/auth/login", "", LoginRequest{Email: "ada@example.com", Password: "s3cret-pass"}); rec.Code != http.StatusForbidden {
			t.Errorf("status %d, want 403", rec.Code)
		}
	})
}

func TestMe(t *testing.T) {
	h := newHarness(t)
	h.register("ada@example.com", "ada")
	access, refresh := h.login("ada@example.com")

	rec := h.do(http.MethodGet, "/api/auth/me", access, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"username":"ada"`) {
		t.Fatalf("me: status %d body %s", rec.Code, rec.Body)
	}
	if rec := h.do(http.MethodGet, "/api/auth/me", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/api/auth/me", refresh, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh token as bearer: status %d, want 401", rec.Code)
	}
}

func TestRefreshRotatesAndDetectsReuse(t *testing.T) {
	h := newHarness(t)
	h.register("ada@example.com", "ada")
	_, first := h.login("ada@example.com")

	rec := h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{RefreshToken: first})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: status %d body %s", rec.Code, rec.Body)
	}
	second := rec.Header().Get("X-Refresh-Token")
	if second == "" || second == first {
		t.Fatalf("refresh did not rotate the token")
	}
	if access := rec.Header().Get("X-Access-Token"); h.do(http.MethodGet, "/api/auth/me", access, nil).Code != http.StatusOK {
		t.Error("rotated access token rejected by /me")
	}

	// Replaying the first token means it leaked: the whole session dies, so
	// even the legitimate second token stops working.
	if rec := h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{RefreshToken: first}); rec.Code != http.StatusUnauthorized {
		t.Errorf("replayed token: status %d, want 401", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{RefreshToken: second}); rec.Code != http.StatusUnauthorized {
		t.Errorf("token from killed session: status %d, want 401", rec.Code)
	}

	if rec := h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{}); rec.Code != http.StatusBadRequest {
		t.Errorf("missing token: status %d, want 400", rec.Code)
	}
}

func TestLogoutRevokesBothTokens(t *testing.T) {
	h := newHarness(t)
	h.register("ada@example.com", "ada")
	access, refresh := h.login("ada@example.com")

	if rec := h.do(http.MethodPost, "/api/auth/logout", access, LogoutRequest{RefreshToken: refresh}); rec.Code != http.StatusOK {
		t.Fatalf("logout: status %d", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/api/auth/me", access, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("access after logout: status %d, want 401", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{RefreshToken: refresh}); rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout: status %d, want 401", rec.Code)
	}
	// Logging out with junk still succeeds, so clients can always force it.
	if rec := h.do(http.MethodPost, "/api/auth/logout", "junk", LogoutRequest{RefreshToken: "junk"}); rec.Code != http.StatusOK {
		t.Errorf("logout with junk tokens: status %d, want 200", rec.Code)
	}
}

// Two refreshes racing with the same token must not both win: exactly one
// rotates, and the other is treated as reuse.
func TestRefreshConcurrentSameToken(t *testing.T) {
	h := newHarness(t)
	h.register("ada@example.com", "ada")
	_, refresh := h.login("ada@example.com")
	// Set before any goroutine starts, so reading it unlocked is race-free.
	h.store.lookupDelay = 50 * time.Millisecond

	const n = 8
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{RefreshToken: refresh}).Code
		}()
	}
	wg.Wait()
	close(codes)

	ok := 0
	for c := range codes {
		if c == http.StatusOK {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("%d of %d concurrent refreshes succeeded, want exactly 1", ok, n)
	}
}

func TestRefreshRejects(t *testing.T) {
	h := newHarness(t)
	h.register("ada@example.com", "ada")
	access, refresh := h.login("ada@example.com")

	if rec := h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{RefreshToken: access}); rec.Code != http.StatusUnauthorized {
		t.Errorf("access token as refresh: status %d, want 401", rec.Code)
	}

	h.store.setFail(errors.New("db down"))
	if rec := h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{RefreshToken: refresh}); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("db fault: status %d, want 503", rec.Code)
	}
	h.store.setFail(nil)

	h.store.setActive("ada@example.com", false)
	if rec := h.do(http.MethodPost, "/api/auth/refresh", "", RefreshRequest{RefreshToken: refresh}); rec.Code != http.StatusUnauthorized {
		t.Errorf("disabled account: status %d, want 401", rec.Code)
	}
}

func TestLoginDatabaseFaultIsNotUnauthorized(t *testing.T) {
	h := newHarness(t)
	h.store.setFail(errors.New("db down"))
	if rec := h.do(http.MethodPost, "/api/auth/login", "", LoginRequest{Email: "ada@example.com", Password: "s3cret-pass"}); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", rec.Code)
	}
}

func TestRequestBodyLimits(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name string
		body string
	}{
		{"oversized body", `{"email":"` + strings.Repeat("a", 70<<10) + `"}`},
		{"trailing json", `{"email":"ada@example.com","password":"x"}{"email":"b"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := h.do(http.MethodPost, "/api/auth/login", "", tt.body); rec.Code != http.StatusBadRequest {
				t.Errorf("status %d, want 400", rec.Code)
			}
		})
	}
}
