package dashapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ownedBy(owner uuid.UUID, name string) *gitv1.Repository {
	ts := timestamppb.New(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	return &gitv1.Repository{
		Id: uuid.Must(uuid.NewV7()).String(), OwnerId: owner.String(), Name: name,
		DefaultBranch: "main", CreatedAt: ts, UpdatedAt: ts,
	}
}

func names(t *testing.T, body map[string]any) []string {
	t.Helper()
	var out []string
	for _, r := range body["repositories"].([]any) {
		out = append(out, r.(map[string]any)["full_name"].(string))
	}
	return out
}

func TestListRepositories(t *testing.T) {
	h := newHarness(t)
	var got *gitv1.ListRepositoriesRequest
	h.repos.list = func(req *gitv1.ListRepositoriesRequest) (*gitv1.ListRepositoriesResponse, error) {
		got = req
		return &gitv1.ListRepositoriesResponse{
			Repositories:  []*gitv1.Repository{h.repo, ownedBy(h.ada.ID, "web-shop")},
			NextPageToken: "next",
		}, nil
	}

	rec := h.get("/api/repos", h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	if got.GetOwnerId() != h.ada.ID.String() || got.GetPageSize() != 50 || got.GetPageToken() != "" {
		t.Fatalf("request = %v", got)
	}
	body := decode(t, rec)
	if n := names(t, body); strings.Join(n, ",") != "ada/billing-service,ada/web-shop" {
		t.Fatalf("repositories = %v", n)
	}
	if body["next_page_token"] != "next" {
		t.Fatalf("next_page_token = %v", body["next_page_token"])
	}
	assertJSON(t, body["repositories"].([]any)[0].(map[string]any), map[string]any{
		"id":             h.repoID.String(),
		"owner":          map[string]any{"id": h.ada.ID.String(), "name": "ada", "kind": "user"},
		"name":           "billing-service",
		"full_name":      "ada/billing-service",
		"description":    "Invoices and payments",
		"default_branch": "main",
		"clone_url":      "https://git2.example.com/ada/billing-service.git",
		"created_at":     "2026-11-01T10:00:00Z",
		"updated_at":     "2026-11-02T10:00:00Z",
	})

	// Paging passes through; the owner's name is case-insensitive.
	wantStatus(t, h.get("/api/repos?owner=Ada&page_size=2&page_token=next", h.adaToken), http.StatusOK)
	if got.GetOwnerId() != h.ada.ID.String() || got.GetPageSize() != 2 || got.GetPageToken() != "next" {
		t.Fatalf("request = %v", got)
	}
}

func TestListRepositoriesEmptyHasNoToken(t *testing.T) {
	h := newHarness(t)
	h.repos.list = func(*gitv1.ListRepositoriesRequest) (*gitv1.ListRepositoriesResponse, error) {
		return &gitv1.ListRepositoriesResponse{}, nil
	}
	rec := h.get("/api/repos", h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	if strings.TrimSpace(rec.Body.String()) != `{"repositories":[]}` {
		t.Fatalf("body = %s", rec.Body)
	}
}

// Another owner's repositories the caller cannot read and an unknown owner
// both answer an empty list (FR-97).
func TestListRepositoriesOfAnotherOwner(t *testing.T) {
	h := newHarness(t)
	bobs := ownedBy(h.bob.ID, "notes")
	calls := 0
	h.repos.list = func(req *gitv1.ListRepositoriesRequest) (*gitv1.ListRepositoriesResponse, error) {
		calls++
		if req.GetOwnerId() != h.bob.ID.String() {
			t.Errorf("owner_id = %s, want bob", req.GetOwnerId())
		}
		return &gitv1.ListRepositoriesResponse{Repositories: []*gitv1.Repository{bobs}}, nil
	}

	for _, path := range []string{"/api/repos?owner=bob", "/api/repos?owner=nobody"} {
		rec := h.get(path, h.adaToken)
		wantStatus(t, rec, http.StatusOK)
		if n := names(t, decode(t, rec)); len(n) != 0 {
			t.Errorf("%s: repositories = %v, want none", path, n)
		}
	}
	if calls != 1 {
		t.Fatalf("ListRepositories called %d times, want 1 (not for an unknown owner)", calls)
	}

	// With read access, bob's repository is listed under bob.
	h = newHarness(t, withAccess(levelFunc(func(_, repoID, _ uuid.UUID) (Level, error) {
		if repoID.String() == bobs.GetId() {
			return LevelRead, nil
		}
		return LevelNone, nil
	})))
	other := ownedBy(h.bob.ID, "secret")
	bobs.OwnerId = h.bob.ID.String()
	h.repos.list = func(*gitv1.ListRepositoriesRequest) (*gitv1.ListRepositoriesResponse, error) {
		return &gitv1.ListRepositoriesResponse{Repositories: []*gitv1.Repository{bobs, other}}, nil
	}
	rec := h.get("/api/repos?owner=bob", h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	if n := names(t, decode(t, rec)); strings.Join(n, ",") != "bob/notes" {
		t.Fatalf("repositories = %v, want bob/notes", n)
	}
}

func TestListRepositoriesBadInput(t *testing.T) {
	h := newHarness(t)
	h.repos.list = func(*gitv1.ListRepositoriesRequest) (*gitv1.ListRepositoriesResponse, error) {
		return nil, status.Error(codes.InvalidArgument, "page_token is invalid")
	}
	wantError(t, h.get("/api/repos?page_size=0", h.adaToken), http.StatusBadRequest, "page_size must be between 1 and 100")
	wantError(t, h.get("/api/repos?owner="+strings.Repeat("a", 40), h.adaToken), http.StatusBadRequest, "owner must be at most 39 bytes")
	wantError(t, h.get("/api/repos?page_token=bad", h.adaToken), http.StatusBadRequest, "page_token is invalid")
}

func TestListRepositoriesFaults(t *testing.T) {
	t.Run("user store", func(t *testing.T) {
		h := newHarness(t)
		h.users.setFail(errors.New("database down"))
		wantStatus(t, h.get("/api/repos", h.adaToken), http.StatusServiceUnavailable)
	})
	t.Run("git-svc", func(t *testing.T) {
		h := newHarness(t)
		h.repos.list = func(*gitv1.ListRepositoriesRequest) (*gitv1.ListRepositoriesResponse, error) {
			return nil, status.Error(codes.Unavailable, "down")
		}
		wantError(t, h.get("/api/repos", h.adaToken), http.StatusServiceUnavailable, "git service unavailable")
	})
	t.Run("access check", func(t *testing.T) {
		h := newHarness(t, withAccess(levelFunc(func(uuid.UUID, uuid.UUID, uuid.UUID) (Level, error) {
			return LevelNone, errors.New("database down")
		})))
		h.repos.list = func(*gitv1.ListRepositoriesRequest) (*gitv1.ListRepositoriesResponse, error) {
			return &gitv1.ListRepositoriesResponse{Repositories: []*gitv1.Repository{h.repo}}, nil
		}
		wantStatus(t, h.get("/api/repos", h.adaToken), http.StatusServiceUnavailable)
	})
	t.Run("bad id from git-svc", func(t *testing.T) {
		h := newHarness(t)
		h.repos.list = func(*gitv1.ListRepositoriesRequest) (*gitv1.ListRepositoriesResponse, error) {
			return &gitv1.ListRepositoriesResponse{Repositories: []*gitv1.Repository{{Id: "x", OwnerId: h.ada.ID.String()}}}, nil
		}
		wantStatus(t, h.get("/api/repos", h.adaToken), http.StatusServiceUnavailable)
	})
}

// A valid token for an account that is gone or disabled is a 401, as in
// authsvc.RequirePermission.
func TestRepositoriesCallerGone(t *testing.T) {
	h := newHarness(t)
	gone := h.token(h.addUser("carol", "carol@example.com"))
	h.users.users = h.users.users[:2]
	wantError(t, h.get("/api/repos", gone), http.StatusUnauthorized, "account is not available")

	h.users.users[0].IsActive = false
	wantError(t, h.do(http.MethodPost, "/api/repos", h.adaToken, `{"name":"x"}`), http.StatusUnauthorized, "account is not available")
}

func TestCreateRepository(t *testing.T) {
	h := newHarness(t)
	var got *gitv1.CreateRepositoryRequest
	h.repos.create = func(req *gitv1.CreateRepositoryRequest) (*gitv1.CreateRepositoryResponse, error) {
		got = req
		repo := ownedBy(h.ada.ID, req.GetName())
		repo.Description = req.GetDescription()
		if req.DefaultBranch != nil {
			repo.DefaultBranch = req.GetDefaultBranch()
		}
		return &gitv1.CreateRepositoryResponse{Repository: repo}, nil
	}

	rec := h.do(http.MethodPost, "/api/repos", h.adaToken, `{"name":"web-shop"}`)
	wantStatus(t, rec, http.StatusCreated)
	// Unset fields stay unset, so git-svc applies its defaults.
	if got.GetOwnerId() != h.ada.ID.String() || got.GetName() != "web-shop" || got.DefaultBranch != nil || got.Description != nil {
		t.Fatalf("request = %v", got)
	}
	body := decode(t, rec)
	assertJSON(t, body, map[string]any{
		"id":             body["id"],
		"owner":          map[string]any{"id": h.ada.ID.String(), "name": "ada", "kind": "user"},
		"name":           "web-shop",
		"full_name":      "ada/web-shop",
		"description":    "",
		"default_branch": "main",
		"clone_url":      "https://git2.example.com/ada/web-shop.git",
		"created_at":     "2026-11-20T09:00:00Z",
		"updated_at":     "2026-11-20T09:00:00Z",
	})

	rec = h.do(http.MethodPost, "/api/repos", h.adaToken,
		`{"owner":"Ada","name":"docs","description":"Handbook","default_branch":"trunk"}`)
	wantStatus(t, rec, http.StatusCreated)
	if got.GetDescription() != "Handbook" || got.GetDefaultBranch() != "trunk" {
		t.Fatalf("request = %v", got)
	}
	if b := decode(t, rec); b["default_branch"] != "trunk" || b["description"] != "Handbook" {
		t.Fatalf("body = %v", b)
	}
}

func TestCreateRepositoryErrors(t *testing.T) {
	h := newHarness(t)
	h.repos.create = func(req *gitv1.CreateRepositoryRequest) (*gitv1.CreateRepositoryResponse, error) {
		switch req.GetName() {
		case "billing-service":
			return nil, status.Error(codes.AlreadyExists, "repository billing-service already exists")
		case "app.git":
			return nil, status.Error(codes.InvalidArgument, `name must not end with ".git"`)
		}
		return nil, status.Error(codes.Unavailable, "down")
	}
	post := func(body string) *httptest.ResponseRecorder {
		return h.do(http.MethodPost, "/api/repos", h.adaToken, body)
	}

	wantError(t, post(`{"name":"billing-service"}`), http.StatusConflict, "repository billing-service already exists")
	wantError(t, post(`{"name":"app.git"}`), http.StatusBadRequest, `name must not end with ".git"`)
	wantError(t, post(`{"name":"x"}`), http.StatusServiceUnavailable, "git service unavailable")
	for _, body := range []string{`{"name":"x","visibility":"private"}`, `{"name":`, `{"name":"x"}{}`, ``} {
		wantError(t, post(body), http.StatusBadRequest, "invalid request body")
	}
	// Not under someone else's name; git-svc is not asked.
	h.repos.create = nil
	wantError(t, post(`{"owner":"bob","name":"x"}`), http.StatusForbidden, "repositories can only be created under your own name")
	wantStatus(t, h.do(http.MethodPost, "/api/repos", "", `{"name":"x"}`), http.StatusUnauthorized)
}

func TestDeleteRepository(t *testing.T) {
	h := newHarness(t)
	var got *gitv1.DeleteRepositoryRequest
	h.repos.del = func(req *gitv1.DeleteRepositoryRequest) (*gitv1.DeleteRepositoryResponse, error) {
		got = req
		return &gitv1.DeleteRepositoryResponse{}, nil
	}
	rec := h.do(http.MethodDelete, repoPath(""), h.adaToken, "")
	wantStatus(t, rec, http.StatusNoContent)
	if got.GetId() != h.repoID.String() {
		t.Fatalf("request = %v", got)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body)
	}
}

func TestDeleteRepositoryDenied(t *testing.T) {
	var h *harness
	h = newHarness(t, withAccess(levelFunc(func(userID, _, _ uuid.UUID) (Level, error) {
		if userID == h.bob.ID {
			return LevelWrite, nil
		}
		return LevelNone, nil
	})))
	h.repos.del = func(*gitv1.DeleteRepositoryRequest) (*gitv1.DeleteRepositoryResponse, error) {
		t.Fatal("DeleteRepository called")
		return nil, nil
	}
	// bob may write but not maintain; ada, here, may not even read.
	wantError(t, h.do(http.MethodDelete, repoPath(""), h.bobToken, ""), http.StatusForbidden, "maintain access to the repository is required")
	wantError(t, h.do(http.MethodDelete, repoPath(""), h.adaToken, ""), http.StatusNotFound, msgRepoNotFound)
}

func TestDeleteRepositoryGitErrors(t *testing.T) {
	h := newHarness(t)
	h.repos.del = func(*gitv1.DeleteRepositoryRequest) (*gitv1.DeleteRepositoryResponse, error) {
		return nil, status.Error(codes.NotFound, "repository 0192 not found")
	}
	// Deleted by another request in between: the same 404 as everywhere.
	wantError(t, h.do(http.MethodDelete, repoPath(""), h.adaToken, ""), http.StatusNotFound, msgRepoNotFound)

	h.repos.del = func(*gitv1.DeleteRepositoryRequest) (*gitv1.DeleteRepositoryResponse, error) {
		return nil, status.Error(codes.Unavailable, "down")
	}
	wantError(t, h.do(http.MethodDelete, repoPath(""), h.adaToken, ""), http.StatusServiceUnavailable, "git service unavailable")
}
