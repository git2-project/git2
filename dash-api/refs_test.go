package dashapi

import (
	"net/http"
	"testing"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListBranches(t *testing.T) {
	h := newHarness(t)
	var got *gitv1.ListBranchesRequest
	h.refs.listBranches = func(req *gitv1.ListBranchesRequest) (*gitv1.ListBranchesResponse, error) {
		got = req
		return &gitv1.ListBranchesResponse{
			Branches: []*gitv1.Branch{
				{Name: "main", Commit: commit(sha1, "Round invoice totals"), IsDefault: true},
				{Name: "feature/retries", Commit: commit(sha2, "Retry payments")},
			},
			NextPageToken: "next",
		}, nil
	}
	rec := h.get(repoPath("/branches?q=ret&page_size=2&page_token=tok"), h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	if got.GetRepositoryId() != h.repoID.String() || got.GetQuery() != "ret" || got.GetPageSize() != 2 || got.GetPageToken() != "tok" {
		t.Fatalf("request = %v", got)
	}
	assertJSON(t, decode(t, rec), map[string]any{
		"branches": []any{
			map[string]any{
				"name":      "main",
				"commit":    map[string]any{"sha": sha1, "title": "Round invoice totals", "committed_at": "2026-11-12T16:40:00Z"},
				"default":   true,
				"protected": false,
			},
			map[string]any{
				"name":      "feature/retries",
				"commit":    map[string]any{"sha": sha2, "title": "Retry payments", "committed_at": "2026-11-12T16:40:00Z"},
				"default":   false,
				"protected": false,
			},
		},
		"next_page_token": "next",
	})
}

func TestListBranchesDefaultsAndLimits(t *testing.T) {
	h := newHarness(t)
	var got *gitv1.ListBranchesRequest
	h.refs.listBranches = func(req *gitv1.ListBranchesRequest) (*gitv1.ListBranchesResponse, error) {
		got = req
		return &gitv1.ListBranchesResponse{}, nil
	}
	rec := h.get(repoPath("/branches"), h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	if got.GetPageSize() != defaultPageSize {
		t.Fatalf("page_size = %d, want %d", got.GetPageSize(), defaultPageSize)
	}
	// An empty list is [], and no next_page_token.
	assertJSON(t, decode(t, rec), map[string]any{"branches": []any{}})

	for _, q := range []string{"page_size=0", "page_size=101", "page_size=x"} {
		wantError(t, h.get(repoPath("/branches?"+q), h.adaToken), http.StatusBadRequest, "page_size must be between 1 and 100")
	}
	long := make([]byte, maxQueryLen+1)
	for i := range long {
		long[i] = 'a'
	}
	wantStatus(t, h.get(repoPath("/branches?q="+string(long)), h.adaToken), http.StatusBadRequest)
}

func TestListTags(t *testing.T) {
	h := newHarness(t)
	msg := "Billing 1.2"
	h.refs.listTags = func(req *gitv1.ListTagsRequest) (*gitv1.ListTagsResponse, error) {
		return &gitv1.ListTagsResponse{Tags: []*gitv1.Tag{
			{Name: "v1.2.0", Commit: commit(sha1, "Round"), Message: &msg},
			{Name: "v1.1.0", Commit: commit(sha2, "PDFs")},
			{Name: "tree-tag"},
		}}, nil
	}
	rec := h.get(repoPath("/tags"), h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	assertJSON(t, decode(t, rec), map[string]any{"tags": []any{
		map[string]any{"name": "v1.2.0", "commit": map[string]any{"sha": sha1, "title": "Round", "committed_at": "2026-11-12T16:40:00Z"}, "message": "Billing 1.2"},
		map[string]any{"name": "v1.1.0", "commit": map[string]any{"sha": sha2, "title": "PDFs", "committed_at": "2026-11-12T16:40:00Z"}, "message": nil},
		map[string]any{"name": "tree-tag", "commit": nil, "message": nil},
	}})
}

func TestRefsErrors(t *testing.T) {
	h := newHarness(t)
	h.refs.listTags = func(*gitv1.ListTagsRequest) (*gitv1.ListTagsResponse, error) {
		return nil, status.Error(codes.InvalidArgument, "page_token is invalid")
	}
	wantError(t, h.get(repoPath("/tags?page_token=bad"), h.adaToken), http.StatusBadRequest, "page_token is invalid")
	h.refs.listTags = func(*gitv1.ListTagsRequest) (*gitv1.ListTagsResponse, error) {
		return nil, status.Error(codes.Unavailable, "down")
	}
	wantStatus(t, h.get(repoPath("/tags"), h.adaToken), http.StatusServiceUnavailable)
}
