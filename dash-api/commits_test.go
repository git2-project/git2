package dashapi

import (
	"errors"
	"net/http"
	"testing"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListCommits(t *testing.T) {
	h := newHarness(t)
	var got *gitv1.ListCommitsRequest
	merge := commit(sha1, "Merge feature")
	merge.Message = "Merge feature\n\nLong body."
	merge.ParentShas = []string{sha2, "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"}
	// Upper case and spaces still match ada's stored email; the committer
	// has no account.
	merge.Author = sig("Ada", " ADA@example.com ")
	merge.Committer = sig("CI", "ci@example.org")
	root := commit(sha2, "Initial commit")
	h.commits.listCommits = func(req *gitv1.ListCommitsRequest) (*gitv1.ListCommitsResponse, error) {
		got = req
		return &gitv1.ListCommitsResponse{Commits: []*gitv1.Commit{merge, root}, NextPageToken: "more"}, nil
	}
	rec := h.get(repoPath("/commits?ref=main&path=docs&page_size=2&page_token=t"), h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	if got.GetRef() != "main" || got.GetPath() != "docs" || got.GetPageSize() != 2 || got.GetPageToken() != "t" || got.GetRepositoryId() != h.repoID.String() {
		t.Fatalf("request = %v", got)
	}
	ada := map[string]any{"id": h.ada.ID.String(), "username": "ada"}
	assertJSON(t, decode(t, rec), map[string]any{
		"commits": []any{
			map[string]any{
				"sha": sha1, "short_sha": "4bdc553", "title": "Merge feature", "message": "Merge feature\n\nLong body.",
				"author":      map[string]any{"name": "Ada", "email": " ADA@example.com ", "date": "2026-11-12T16:40:00Z", "user": ada},
				"committer":   map[string]any{"name": "CI", "email": "ci@example.org", "date": "2026-11-12T16:40:00Z", "user": nil},
				"parent_shas": []any{sha2, "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"},
			},
			map[string]any{
				"sha": sha2, "short_sha": "9e1f2a3", "title": "Initial commit", "message": "Initial commit",
				"author":      map[string]any{"name": "Ada Lovelace", "email": "ada@example.com", "date": "2026-11-12T16:40:00Z", "user": ada},
				"committer":   map[string]any{"name": "Ada Lovelace", "email": "ada@example.com", "date": "2026-11-12T16:40:00Z", "user": ada},
				"parent_shas": []any{},
			},
		},
		"next_page_token": "more",
	})
	// Two distinct emails, two lookups, however many commits.
	if h.users.emailLookups != 2 {
		t.Fatalf("FindByEmail called %d times, want 2", h.users.emailLookups)
	}
}

func TestListCommitsUserLookupFault(t *testing.T) {
	h := newHarness(t)
	h.commits.listCommits = func(*gitv1.ListCommitsRequest) (*gitv1.ListCommitsResponse, error) {
		// The fault starts after the repository was resolved.
		h.users.setFail(errors.New("database down"))
		return &gitv1.ListCommitsResponse{Commits: []*gitv1.Commit{commit(sha1, "x")}}, nil
	}
	wantStatus(t, h.get(repoPath("/commits"), h.adaToken), http.StatusServiceUnavailable)
}

func TestGetCommit(t *testing.T) {
	h := newHarness(t)
	var got *gitv1.GetCommitRequest
	h.commits.getCommit = func(req *gitv1.GetCommitRequest) (*gitv1.GetCommitResponse, error) {
		got = req
		c := commit(sha1, "Round invoice totals")
		c.ParentShas = []string{sha2}
		return &gitv1.GetCommitResponse{
			Commit: c,
			Stats:  &gitv1.DiffStats{FilesChanged: 4, Additions: 3, Deletions: 2},
			Files: []*gitv1.FileDiff{
				{Status: gitv1.FileStatus_FILE_STATUS_MODIFIED, OldPath: "invoice.go", NewPath: "invoice.go", Additions: 3, Deletions: 2, Patch: []byte("@@ -1 +1 @@\n-a\n+b\xff\n")},
				{Status: gitv1.FileStatus_FILE_STATUS_RENAMED, OldPath: "a.txt", NewPath: "a.md"},
				{Status: gitv1.FileStatus_FILE_STATUS_ADDED, NewPath: "logo.png", Binary: true},
				{Status: gitv1.FileStatus_FILE_STATUS_DELETED, OldPath: "big.sql", Truncated: true},
			},
			Truncated: true,
		}, nil
	}
	rec := h.get(repoPath("/commits/4bdc553"), h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	if got.GetSha() != "4bdc553" || got.GetRepositoryId() != h.repoID.String() {
		t.Fatalf("request = %v", got)
	}
	body := decode(t, rec)
	assertJSON(t, body, map[string]any{
		"sha": sha1, "short_sha": "4bdc553", "title": "Round invoice totals", "message": "Round invoice totals",
		"author":      body["author"],
		"committer":   body["committer"],
		"parent_shas": []any{sha2},
		"stats":       map[string]any{"files_changed": float64(4), "additions": float64(3), "deletions": float64(2)},
		"truncated":   true,
		"files": []any{
			map[string]any{"status": "modified", "old_path": "invoice.go", "new_path": "invoice.go", "additions": float64(3), "deletions": float64(2), "binary": false, "truncated": false, "patch": "@@ -1 +1 @@\n-a\n+b�\n"},
			map[string]any{"status": "renamed", "old_path": "a.txt", "new_path": "a.md", "additions": float64(0), "deletions": float64(0), "binary": false, "truncated": false, "patch": ""},
			map[string]any{"status": "added", "old_path": nil, "new_path": "logo.png", "additions": float64(0), "deletions": float64(0), "binary": true, "truncated": false, "patch": nil},
			map[string]any{"status": "deleted", "old_path": "big.sql", "new_path": nil, "additions": float64(0), "deletions": float64(0), "binary": false, "truncated": true, "patch": nil},
		},
	})
	author := body["author"].(map[string]any)
	if author["user"] == nil {
		t.Fatal("author.user is null, want ada")
	}
}

func TestGetCommitErrors(t *testing.T) {
	h := newHarness(t)
	for _, sha := range []string{"abc", "4BDC553", "4bdc553g", "4bdc5531f0e2a7d9c6b8e3f1a2d4c6b8e0f1a3c5a"} {
		wantError(t, h.get(repoPath("/commits/"+sha), h.adaToken), http.StatusBadRequest, "sha must be 7 to 40 lowercase hex characters")
	}
	h.commits.getCommit = func(req *gitv1.GetCommitRequest) (*gitv1.GetCommitResponse, error) {
		if req.GetSha() == "aaaaaaa" {
			return nil, status.Error(codes.InvalidArgument, "sha prefix is ambiguous")
		}
		return nil, status.Error(codes.NotFound, "commit not found")
	}
	wantError(t, h.get(repoPath("/commits/aaaaaaa"), h.adaToken), http.StatusBadRequest, "sha prefix is ambiguous")
	wantError(t, h.get(repoPath("/commits/bbbbbbb"), h.adaToken), http.StatusNotFound, "commit not found")
}
