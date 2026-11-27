package dashapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNoTokenIs401(t *testing.T) {
	h := newHarness(t)
	wantStatus(t, h.get(repoPath(""), ""), http.StatusUnauthorized)
	wantStatus(t, h.get(repoPath("/tree"), "not-a-token"), http.StatusUnauthorized)
}

func TestGetRepository(t *testing.T) {
	h := newHarness(t)
	rec := h.get(repoPath(""), h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	got := decode(t, rec)
	want := map[string]any{
		"id":             h.repoID.String(),
		"owner":          map[string]any{"id": h.ada.ID.String(), "name": "ada", "kind": "user"},
		"name":           "billing-service",
		"full_name":      "ada/billing-service",
		"description":    "Invoices and payments",
		"default_branch": "main",
		"clone_url":      "https://git2.example.com/ada/billing-service.git",
		"created_at":     "2026-11-01T10:00:00Z",
		"updated_at":     "2026-11-02T10:00:00Z",
	}
	assertJSON(t, got, want)

	// The trailing slash form reaches the same route.
	wantStatus(t, h.get(repoPath("/"), h.adaToken), http.StatusOK)
}

func TestOwnerNameIsCaseInsensitive(t *testing.T) {
	h := newHarness(t)
	wantStatus(t, h.get("/api/repos/Ada/billing-service", h.adaToken), http.StatusOK)
}

// A missing owner, a missing repository and someone else's repository give
// the same answer, so private names do not leak.
func TestRepoNotFoundLooksTheSame(t *testing.T) {
	h := newHarness(t)
	cases := map[string]struct{ path, token string }{
		"unknown owner":         {"/api/repos/nobody/billing-service/tree", h.adaToken},
		"unknown repository":    {"/api/repos/ada/nothing/tree", h.adaToken},
		"another user's repo":   {repoPath("/tree"), h.bobToken},
		"another user's, bare":  {repoPath(""), h.bobToken},
		"name git-svc rejects":  {"/api/repos/ada/..bad/tree", h.adaToken},
		"bob's empty namespace": {"/api/repos/bob/billing-service", h.adaToken},
	}
	h.repos.find = func(req *gitv1.FindRepositoryRequest) (*gitv1.FindRepositoryResponse, error) {
		if req.GetName() == "..bad" {
			return nil, status.Error(codes.InvalidArgument, "name must not start with a dot")
		}
		if req.GetOwnerId() == h.ada.ID.String() && req.GetName() == "billing-service" {
			return &gitv1.FindRepositoryResponse{Repository: h.repo}, nil
		}
		return nil, status.Error(codes.NotFound, "repository "+req.GetName()+" not found")
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			wantError(t, h.get(c.path, c.token), http.StatusNotFound, msgRepoNotFound)
		})
	}
}

// Faults answer 503 and never 401 or 404: a 401 signs the user out, a 404
// looks like a deleted repository.
func TestRepoResolutionFaultsAre503(t *testing.T) {
	t.Run("user store", func(t *testing.T) {
		h := newHarness(t)
		h.users.setFail(errors.New("connection refused"))
		wantStatus(t, h.get(repoPath("/tree"), h.adaToken), http.StatusServiceUnavailable)
	})
	for _, code := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded, codes.Unauthenticated, codes.PermissionDenied} {
		t.Run("FindRepository "+code.String(), func(t *testing.T) {
			h := newHarness(t)
			h.repos.find = func(*gitv1.FindRepositoryRequest) (*gitv1.FindRepositoryResponse, error) {
				return nil, status.Error(code, "down")
			}
			wantError(t, h.get(repoPath("/tree"), h.adaToken), http.StatusServiceUnavailable, "git service unavailable")
		})
	}
	t.Run("bad repository id from git-svc", func(t *testing.T) {
		h := newHarness(t)
		h.repos.find = func(*gitv1.FindRepositoryRequest) (*gitv1.FindRepositoryResponse, error) {
			return &gitv1.FindRepositoryResponse{Repository: &gitv1.Repository{Id: "x", OwnerId: h.ada.ID.String()}}, nil
		}
		wantStatus(t, h.get(repoPath(""), h.adaToken), http.StatusServiceUnavailable)
	})
	t.Run("access check", func(t *testing.T) {
		h := newHarness(t, withAccess(levelFunc(func(uuid.UUID, uuid.UUID, uuid.UUID) (Level, error) {
			return LevelMaintain, errors.New("database down")
		})))
		wantStatus(t, h.get(repoPath(""), h.adaToken), http.StatusServiceUnavailable)
	})
}

// The checker gets the caller, the repository and its owner as git-svc
// reports them, and its answer decides.
func TestAccessCheckerDecides(t *testing.T) {
	var gotUser, gotRepo, gotOwner uuid.UUID
	h := newHarness(t, withAccess(levelFunc(func(userID, repoID, ownerID uuid.UUID) (Level, error) {
		gotUser, gotRepo, gotOwner = userID, repoID, ownerID
		return LevelRead, nil
	})))
	// bob is not the owner, but this checker lets every caller read.
	wantStatus(t, h.get(repoPath(""), h.bobToken), http.StatusOK)
	if gotUser != h.bob.ID || gotRepo != h.repoID || gotOwner != h.ada.ID {
		t.Fatalf("Level(%v, %v, %v), want (%v, %v, %v)", gotUser, gotRepo, gotOwner, h.bob.ID, h.repoID, h.ada.ID)
	}
}

func TestOwnerOnly(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	cases := []struct {
		user uuid.UUID
		want Level
	}{
		{owner, LevelMaintain},
		{other, LevelNone},
		{uuid.Nil, LevelNone},
	}
	for _, c := range cases {
		got, err := ownerOnly{}.Level(t.Context(), c.user, uuid.New(), owner)
		if err != nil || got != c.want {
			t.Errorf("Level(%v) = %v, %v; want %v", c.user, got, err, c.want)
		}
	}
	if got, _ := (ownerOnly{}).Level(t.Context(), uuid.Nil, uuid.New(), uuid.Nil); got != LevelNone {
		t.Errorf("nil caller on a nil owner = %v, want none", got)
	}
}

func TestRequireLevel(t *testing.T) {
	cases := []struct {
		have, need Level
		want       int
	}{
		{LevelRead, LevelRead, http.StatusNoContent},
		{LevelRead, LevelWrite, http.StatusForbidden},
		{LevelWrite, LevelMaintain, http.StatusForbidden},
		{LevelMaintain, LevelMaintain, http.StatusNoContent},
	}
	for _, c := range cases {
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := req.Context()
				next.ServeHTTP(w, req.WithContext(withRepo(ctx, &repoInfo{Level: c.have})))
			})
		})
		r.With(requireLevel(c.need)).Get("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != c.want {
			t.Errorf("have %v, need %v: status %d, want %d", c.have, c.need, rec.Code, c.want)
		}
		if c.want == http.StatusForbidden {
			wantError(t, rec, http.StatusForbidden, c.need.String()+" access to the repository is required")
		}
	}
}

// assertJSON compares a decoded body with want, key by key.
func assertJSON(t *testing.T, got, want map[string]any) {
	t.Helper()
	for k, w := range want {
		if !jsonEqual(got[k], w) {
			t.Errorf("%s = %#v, want %#v", k, got[k], w)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected field %s = %#v", k, got[k])
		}
	}
}
