package dashapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/httpx"
	"github.com/go-chi/chi/v5"
)

// shaPattern is the contract's rule for {sha}: a full SHA or a prefix of at
// least 7.
var shaPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

type commitPage struct {
	Commits       []commitJSON `json:"commits"`
	NextPageToken string       `json:"next_page_token,omitempty"`
}

// listCommits answers GET …/commits (CommitService.ListCommits).
func (h *Handler) listCommits(w http.ResponseWriter, r *http.Request) {
	size, ok := pageSize(w, r)
	if !ok {
		return
	}
	ref, path, ok := refAndPath(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := rpcContext(r)
	defer cancel()
	resp, err := h.deps.Commits.ListCommits(ctx, &gitv1.ListCommitsRequest{
		RepositoryId: repoFrom(r.Context()).ID.String(),
		Ref:          ref,
		Path:         path,
		PageSize:     size,
		PageToken:    r.URL.Query().Get("page_token"),
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	users, err := h.commitUsers(r.Context(), resp.GetCommits()...)
	if err != nil {
		writeUnavailable(w, r, "find commit authors", err)
		return
	}
	page := commitPage{Commits: make([]commitJSON, 0, len(resp.GetCommits())), NextPageToken: resp.GetNextPageToken()}
	for _, c := range resp.GetCommits() {
		page.Commits = append(page.Commits, toCommit(c, users))
	}
	httpx.JSON(w, http.StatusOK, page)
}

// getCommit answers GET …/commits/{sha} (CommitService.GetCommit): the
// commit with its changes against its first parent.
func (h *Handler) getCommit(w http.ResponseWriter, r *http.Request) {
	sha := chi.URLParam(r, "sha")
	if !shaPattern.MatchString(sha) {
		httpx.Error(w, http.StatusBadRequest, "sha must be 7 to 40 lowercase hex characters")
		return
	}
	ctx, cancel := rpcContext(r)
	defer cancel()
	resp, err := h.deps.Commits.GetCommit(ctx, &gitv1.GetCommitRequest{
		RepositoryId: repoFrom(r.Context()).ID.String(), Sha: sha,
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	users, err := h.commitUsers(r.Context(), resp.GetCommit())
	if err != nil {
		writeUnavailable(w, r, "find commit authors", err)
		return
	}
	out := commitDetailJSON{
		commitJSON: toCommit(resp.GetCommit(), users),
		Stats: diffStatsJSON{
			Additions:    resp.GetStats().GetAdditions(),
			Deletions:    resp.GetStats().GetDeletions(),
			FilesChanged: resp.GetStats().GetFilesChanged(),
		},
		Files:     make([]fileDiffJSON, 0, len(resp.GetFiles())),
		Truncated: resp.GetTruncated(),
	}
	for _, f := range resp.GetFiles() {
		out.Files = append(out.Files, toFileDiff(f))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// commitUsers finds the git2 account behind each author and committer email
// (Signature.user), one lookup per distinct email: a page of 50 commits by
// one person is one query, not 100. Unknown emails map to nil. A database
// fault is an error, which callers answer with 503.
//
// Anyone can write any email into a commit, so the match says whose account
// the email belongs to, not who made the commit.
func (h *Handler) commitUsers(ctx context.Context, commits ...*gitv1.Commit) (map[string]*userRefJSON, error) {
	users := map[string]*userRefJSON{}
	for _, c := range commits {
		for _, s := range []*gitv1.Signature{c.GetAuthor(), c.GetCommitter()} {
			key := emailKey(s.GetEmail())
			if key == "" {
				continue
			}
			if _, seen := users[key]; seen {
				continue
			}
			u, err := h.deps.Users.FindByEmail(ctx, key)
			switch {
			case errors.Is(err, authsvc.ErrNotFound):
				users[key] = nil
			case err != nil:
				return nil, err
			default:
				users[key] = &userRefJSON{ID: u.ID, Username: u.Username}
			}
		}
	}
	return users, nil
}
