package dashapi

import (
	"strings"
	"time"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// JSON shapes of contracts/dash-api/openapi.yaml. Fields the contract marks
// as nullable are pointers without omitempty, so they come out as null.

type ownerJSON struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Kind string    `json:"kind"` // "user" until organizations (WBS-3.1)
}

type repositoryJSON struct {
	ID            string    `json:"id"`
	Owner         ownerJSON `json:"owner"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Description   string    `json:"description"`
	DefaultBranch string    `json:"default_branch"`
	CloneURL      string    `json:"clone_url"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type userRefJSON struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
}

type commitRefJSON struct {
	SHA         string    `json:"sha"`
	Title       string    `json:"title"`
	CommittedAt time.Time `json:"committed_at"`
}

type branchJSON struct {
	Name      string         `json:"name"`
	Commit    *commitRefJSON `json:"commit"`
	Default   bool           `json:"default"`
	Protected bool           `json:"protected"`
}

type tagJSON struct {
	Name    string         `json:"name"`
	Commit  *commitRefJSON `json:"commit"`
	Message *string        `json:"message"`
}

type signatureJSON struct {
	Name  string       `json:"name"`
	Email string       `json:"email"`
	Date  time.Time    `json:"date"`
	User  *userRefJSON `json:"user"`
}

type commitJSON struct {
	SHA        string        `json:"sha"`
	ShortSHA   string        `json:"short_sha"`
	Title      string        `json:"title"`
	Message    string        `json:"message"`
	Author     signatureJSON `json:"author"`
	Committer  signatureJSON `json:"committer"`
	ParentSHAs []string      `json:"parent_shas"`
}

type diffStatsJSON struct {
	Additions    int32 `json:"additions"`
	Deletions    int32 `json:"deletions"`
	FilesChanged int32 `json:"files_changed"`
}

type fileDiffJSON struct {
	Status    string  `json:"status"`
	OldPath   *string `json:"old_path"`
	NewPath   *string `json:"new_path"`
	Additions int32   `json:"additions"`
	Deletions int32   `json:"deletions"`
	Binary    bool    `json:"binary"`
	Truncated bool    `json:"truncated"`
	Patch     *string `json:"patch"`
}

type commitDetailJSON struct {
	commitJSON
	Stats     diffStatsJSON  `json:"stats"`
	Files     []fileDiffJSON `json:"files"`
	Truncated bool           `json:"truncated"`
}

type treeEntryJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Mode string `json:"mode"`
	SHA  string `json:"sha"`
	Size *int64 `json:"size"`
}

type treeJSON struct {
	Ref        string          `json:"ref"`
	CommitSHA  string          `json:"commit_sha"`
	Path       string          `json:"path"`
	Empty      bool            `json:"empty"`
	Entries    []treeEntryJSON `json:"entries"`
	Truncated  bool            `json:"truncated"`
	LastCommit *commitRefJSON  `json:"last_commit"`
}

type blobJSON struct {
	Ref       string  `json:"ref"`
	CommitSHA string  `json:"commit_sha"`
	Path      string  `json:"path"`
	Name      string  `json:"name"`
	Size      int64   `json:"size"`
	Binary    bool    `json:"binary"`
	LFS       bool    `json:"lfs"`
	Truncated bool    `json:"truncated"`
	Content   *string `json:"content"`
	Language  *string `json:"language"`
}

// --- conversions from git.v1 ---

func toRepository(repo *gitv1.Repository, owner ownerJSON, externalURL string) repositoryJSON {
	return repositoryJSON{
		ID:            repo.GetId(),
		Owner:         owner,
		Name:          repo.GetName(),
		FullName:      owner.Name + "/" + repo.GetName(),
		Description:   repo.GetDescription(),
		DefaultBranch: repo.GetDefaultBranch(),
		CloneURL:      externalURL + "/" + owner.Name + "/" + repo.GetName() + ".git",
		CreatedAt:     toTime(repo.GetCreatedAt()),
		UpdatedAt:     toTime(repo.GetUpdatedAt()),
	}
}

// toCommitRef is the short form of a commit; nil stays nil (a tag that
// leads to a tree, a directory without a known last commit).
func toCommitRef(c *gitv1.Commit) *commitRefJSON {
	if c == nil {
		return nil
	}
	return &commitRefJSON{SHA: c.GetSha(), Title: c.GetTitle(), CommittedAt: toTime(c.GetCommitter().GetTime())}
}

func toBranch(b *gitv1.Branch) branchJSON {
	// protected stays false until branch protection (WBS-7.1).
	return branchJSON{Name: b.GetName(), Commit: toCommitRef(b.GetCommit()), Default: b.GetIsDefault()}
}

func toTag(t *gitv1.Tag) tagJSON {
	return tagJSON{Name: t.GetName(), Commit: toCommitRef(t.GetCommit()), Message: t.Message}
}

// toCommit converts a commit; author and committer get their user from
// users, which the caller resolves first.
func toCommit(c *gitv1.Commit, users map[string]*userRefJSON) commitJSON {
	sha := c.GetSha()
	parents := c.GetParentShas()
	if parents == nil {
		parents = []string{}
	}
	return commitJSON{
		SHA:        sha,
		ShortSHA:   sha[:min(7, len(sha))],
		Title:      c.GetTitle(),
		Message:    c.GetMessage(),
		Author:     toSignature(c.GetAuthor(), users),
		Committer:  toSignature(c.GetCommitter(), users),
		ParentSHAs: parents,
	}
}

func toSignature(s *gitv1.Signature, users map[string]*userRefJSON) signatureJSON {
	return signatureJSON{
		Name:  s.GetName(),
		Email: s.GetEmail(),
		Date:  toTime(s.GetTime()),
		User:  users[emailKey(s.GetEmail())],
	}
}

var fileStatuses = map[gitv1.FileStatus]string{
	gitv1.FileStatus_FILE_STATUS_ADDED:    "added",
	gitv1.FileStatus_FILE_STATUS_MODIFIED: "modified",
	gitv1.FileStatus_FILE_STATUS_DELETED:  "deleted",
	gitv1.FileStatus_FILE_STATUS_RENAMED:  "renamed",
	gitv1.FileStatus_FILE_STATUS_COPIED:   "copied",
}

func toFileDiff(f *gitv1.FileDiff) fileDiffJSON {
	out := fileDiffJSON{
		Status:    fileStatuses[f.GetStatus()],
		OldPath:   nonEmpty(f.GetOldPath()),
		NewPath:   nonEmpty(f.GetNewPath()),
		Additions: f.GetAdditions(),
		Deletions: f.GetDeletions(),
		Binary:    f.GetBinary(),
		Truncated: f.GetTruncated(),
	}
	if out.Status == "" {
		// Never returned by git-svc; a newer status must still be a valid enum.
		out.Status = "modified"
	}
	// No patch for binary or cut files; an empty string when only the mode
	// or the name changed.
	if !out.Binary && !out.Truncated {
		p := validUTF8(f.GetPatch())
		out.Patch = &p
	}
	return out
}

var entryTypes = map[gitv1.TreeEntryType]string{
	gitv1.TreeEntryType_TREE_ENTRY_TYPE_FILE:      "file",
	gitv1.TreeEntryType_TREE_ENTRY_TYPE_DIRECTORY: "dir",
	gitv1.TreeEntryType_TREE_ENTRY_TYPE_SYMLINK:   "symlink",
	gitv1.TreeEntryType_TREE_ENTRY_TYPE_SUBMODULE: "submodule",
}

func toTreeEntry(e *gitv1.TreeEntry) treeEntryJSON {
	typ, ok := entryTypes[e.GetType()]
	if !ok {
		typ = "file"
	}
	return treeEntryJSON{Name: e.GetName(), Path: e.GetPath(), Type: typ, Mode: e.GetMode(), SHA: e.GetSha(), Size: e.Size}
}

func toTree(t *gitv1.GetTreeResponse) treeJSON {
	entries := make([]treeEntryJSON, 0, len(t.GetEntries()))
	for _, e := range t.GetEntries() {
		entries = append(entries, toTreeEntry(e))
	}
	return treeJSON{
		Ref:        t.GetRef(),
		CommitSHA:  t.GetCommitSha(),
		Path:       t.GetPath(),
		Empty:      t.GetEmpty(),
		Entries:    entries,
		Truncated:  t.GetTruncated(),
		LastCommit: toCommitRef(t.GetLastCommit()),
	}
}

func toBlob(b *gitv1.GetBlobResponse) blobJSON {
	out := blobJSON{
		Ref:       b.GetRef(),
		CommitSHA: b.GetCommitSha(),
		Path:      b.GetPath(),
		Name:      b.GetName(),
		Size:      b.GetSize(),
		Binary:    b.GetBinary(),
		LFS:       b.GetLfs(),
		Truncated: b.GetTruncated(),
	}
	if !out.Binary {
		c := validUTF8(b.GetContent())
		out.Content = &c
		// An LFS pointer is pointer text, not the file's language.
		if !out.LFS {
			out.Language = languageFor(out.Name)
		}
	}
	return out
}

// --- helpers ---

func toTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime().UTC()
}

// validUTF8 turns bytes into text, with U+FFFD in place of invalid bytes
// (contract, "File contents").
func validUTF8(b []byte) string { return strings.ToValidUTF8(string(b), "�") }

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// emailKey is how commit emails are matched to accounts: auth-svc stores
// emails trimmed and lowercase.
func emailKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
