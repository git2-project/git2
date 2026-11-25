package dashapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/httpx"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// msgRepoNotFound is the one answer for a missing repository and for one the
// caller cannot read, so private names do not leak (FR-97).
const msgRepoNotFound = "repository not found"

// repoInfo is the repository a request is about, resolved by resolveRepo.
type repoInfo struct {
	ID    uuid.UUID
	Owner authsvc.User
	Repo  *gitv1.Repository
	// Level is the caller's level on the repository; at least LevelRead.
	Level Level
}

type repoKey struct{}

// repoFrom returns the repository resolveRepo stored. Only routes under
// /repos/{owner}/{repo} call it.
func repoFrom(ctx context.Context) *repoInfo {
	return ctx.Value(repoKey{}).(*repoInfo)
}

func withRepo(ctx context.Context, info *repoInfo) context.Context {
	return context.WithValue(ctx, repoKey{}, info)
}

// resolveRepo turns {owner}/{repo} into a git-svc repository and the
// caller's level on it, before any route runs:
//
//   - unknown owner, unknown repository or a level below read: 404, all
//     with the same body;
//   - a fault in auth-svc, git-svc or the access check: 503, never 401 or
//     404, so a fault neither signs the user out nor looks like a deletion.
func (h *Handler) resolveRepo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Usernames are stored lowercase, so /Ada/x is /ada/x, as on Git routes.
		owner, err := h.deps.Users.FindByUsername(ctx, strings.ToLower(chi.URLParam(r, "owner")))
		switch {
		case errors.Is(err, authsvc.ErrNotFound):
			httpx.Error(w, http.StatusNotFound, msgRepoNotFound)
			return
		case err != nil:
			writeUnavailable(w, r, "find owner", err)
			return
		}

		repo, ok := h.findRepository(w, r, owner.ID, chi.URLParam(r, "repo"))
		if !ok {
			return
		}
		id, ownerID, err := repoIDs(repo)
		if err != nil {
			writeUnavailable(w, r, "find repository", err)
			return
		}

		level, err := h.access.Level(ctx, authsvc.UserID(ctx), id, ownerID)
		if err != nil {
			writeUnavailable(w, r, "check repository access", err)
			return
		}
		if level < LevelRead {
			httpx.Error(w, http.StatusNotFound, msgRepoNotFound)
			return
		}

		info := &repoInfo{ID: id, Owner: owner, Repo: repo, Level: level}
		next.ServeHTTP(w, r.WithContext(withRepo(ctx, info)))
	})
}

// findRepository asks git-svc for the owner's repository by name. It answers
// itself and returns false when the request must stop. INVALID_ARGUMENT is
// a name git-svc could never store, so it is a 404 like NOT_FOUND.
func (h *Handler) findRepository(w http.ResponseWriter, r *http.Request, ownerID uuid.UUID, name string) (*gitv1.Repository, bool) {
	ctx, cancel := rpcContext(r)
	defer cancel()
	resp, err := h.deps.Repos.FindRepository(ctx, &gitv1.FindRepositoryRequest{OwnerId: ownerID.String(), Name: name})
	switch status.Code(err) {
	case codes.OK:
		return resp.GetRepository(), true
	case codes.NotFound, codes.InvalidArgument:
		httpx.Error(w, http.StatusNotFound, msgRepoNotFound)
	default:
		writeGitError(w, r, err)
	}
	return nil, false
}

func repoIDs(repo *gitv1.Repository) (id, ownerID uuid.UUID, err error) {
	if id, err = uuid.Parse(repo.GetId()); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("git-svc returned a bad repository id: %w", err)
	}
	if ownerID, err = uuid.Parse(repo.GetOwnerId()); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("git-svc returned a bad owner id: %w", err)
	}
	return id, ownerID, nil
}

// requireLevel lets the request through only when the caller's level on the
// repository is at least need; below that it answers 403 (FR-16). It runs
// after resolveRepo, which already answered 404 below read.
func requireLevel(need Level) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if repoFrom(r.Context()).Level < need {
				httpx.Error(w, http.StatusForbidden, need.String()+" access to the repository is required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
