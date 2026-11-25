package dashapi

import (
	"net/http"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	"github.com/git2-project/git2/internal/httpx"
)

type branchPage struct {
	Branches      []branchJSON `json:"branches"`
	NextPageToken string       `json:"next_page_token,omitempty"`
}

type tagPage struct {
	Tags          []tagJSON `json:"tags"`
	NextPageToken string    `json:"next_page_token,omitempty"`
}

// listBranches answers GET …/branches (RefService.ListBranches): the
// default branch first, then by name. Page tokens are git-svc's, passed
// through unchanged.
func (h *Handler) listBranches(w http.ResponseWriter, r *http.Request) {
	size, ok := pageSize(w, r)
	if !ok {
		return
	}
	q, ok := queryParam(w, r, "q", maxQueryLen)
	if !ok {
		return
	}
	ctx, cancel := rpcContext(r)
	defer cancel()
	resp, err := h.deps.Refs.ListBranches(ctx, &gitv1.ListBranchesRequest{
		RepositoryId: repoFrom(r.Context()).ID.String(),
		Query:        q,
		PageSize:     size,
		PageToken:    r.URL.Query().Get("page_token"),
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	page := branchPage{Branches: make([]branchJSON, 0, len(resp.GetBranches())), NextPageToken: resp.GetNextPageToken()}
	for _, b := range resp.GetBranches() {
		page.Branches = append(page.Branches, toBranch(b))
	}
	httpx.JSON(w, http.StatusOK, page)
}

// listTags answers GET …/tags (RefService.ListTags), newest first.
func (h *Handler) listTags(w http.ResponseWriter, r *http.Request) {
	size, ok := pageSize(w, r)
	if !ok {
		return
	}
	ctx, cancel := rpcContext(r)
	defer cancel()
	resp, err := h.deps.Refs.ListTags(ctx, &gitv1.ListTagsRequest{
		RepositoryId: repoFrom(r.Context()).ID.String(),
		PageSize:     size,
		PageToken:    r.URL.Query().Get("page_token"),
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	page := tagPage{Tags: make([]tagJSON, 0, len(resp.GetTags())), NextPageToken: resp.GetNextPageToken()}
	for _, t := range resp.GetTags() {
		page.Tags = append(page.Tags, toTag(t))
	}
	httpx.JSON(w, http.StatusOK, page)
}
