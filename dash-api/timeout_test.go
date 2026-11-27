package dashapi

import (
	"net/http"
	"testing"
	"time"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
)

// git-svc gives GetTree's last_commit 10 s of its own after listing the
// directory, and a long ListCommits with a path can take as long. dash-api's
// deadline must leave room for that, or a slow directory answers 503
// instead of its listing (review of #57).
func TestGitCallsLeaveGitSvcItsBudget(t *testing.T) {
	h := newHarness(t)
	h.commits.getTree = func(*gitv1.GetTreeRequest) (*gitv1.GetTreeResponse, error) {
		return &gitv1.GetTreeResponse{Ref: "main", CommitSha: sha1}, nil
	}
	h.commits.listCommits = func(*gitv1.ListCommitsRequest) (*gitv1.ListCommitsResponse, error) {
		return &gitv1.ListCommitsResponse{}, nil
	}
	for _, path := range []string{"/tree?ref=main&path=docs", "/commits?ref=main&path=docs"} {
		t.Run(path, func(t *testing.T) {
			start := time.Now()
			wantStatus(t, h.get(repoPath(path), h.adaToken), http.StatusOK)
			d := h.commits.lastDeadline()
			if d.IsZero() {
				t.Fatal("the call to git-svc has no deadline")
			}
			// More than git-svc's 10 s last_commit budget plus the listing,
			// and not past dash-api's own rpcTimeout.
			if left := d.Sub(start); left < 20*time.Second || left > rpcTimeout+time.Second {
				t.Fatalf("git-svc got %v, want between 20s and %v", left.Round(time.Millisecond), rpcTimeout)
			}
		})
	}
}
