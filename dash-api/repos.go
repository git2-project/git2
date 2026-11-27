package dashapi

import (
	"errors"
	"net/http"
	"strings"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/httpx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxOwnerLen is the longest OwnerName of the contract.
const maxOwnerLen = 39

func userOwner(u authsvc.User) ownerJSON {
	return ownerJSON{ID: u.ID, Name: u.Username, Kind: "user"}
}

// caller looks the signed-in user up, or answers and returns false. As in
// authsvc.RequirePermission, an account that is gone or disabled is a 401
// and a fault is a 503, never a 401.
func (h *Handler) caller(w http.ResponseWriter, r *http.Request) (authsvc.User, bool) {
	u, err := h.deps.Users.FindByID(r.Context(), authsvc.UserID(r.Context()))
	switch {
	case errors.Is(err, authsvc.ErrNotFound), err == nil && !u.IsActive:
		httpx.Error(w, http.StatusUnauthorized, "account is not available")
		return authsvc.User{}, false
	case err != nil:
		writeUnavailable(w, r, "find caller", err)
		return authsvc.User{}, false
	}
	return u, true
}

type repositoryPageJSON struct {
	Repositories  []repositoryJSON `json:"repositories"`
	NextPageToken string           `json:"next_page_token,omitempty"`
}

// listRepositories answers GET /repos: one owner's repositories that the
// caller can read, sorted by name (FR-97). The owner defaults to the caller.
//
// An unknown owner and an owner whose repositories the caller cannot read
// both get an empty list, so the list does not tell them apart. Repositories
// the caller cannot read are dropped from git-svc's page, so a page can be
// shorter than page_size and still have a next page.
func (h *Handler) listRepositories(w http.ResponseWriter, r *http.Request) {
	size, ok := pageSize(w, r)
	if !ok {
		return
	}
	name, ok := queryParam(w, r, "owner", maxOwnerLen)
	if !ok {
		return
	}
	me, ok := h.caller(w, r)
	if !ok {
		return
	}
	owner := me
	if name = strings.ToLower(name); name != "" && name != me.Username {
		var err error
		owner, err = h.deps.Users.FindByUsername(r.Context(), name)
		switch {
		case errors.Is(err, authsvc.ErrNotFound):
			httpx.JSON(w, http.StatusOK, repositoryPageJSON{Repositories: []repositoryJSON{}})
			return
		case err != nil:
			writeUnavailable(w, r, "find owner", err)
			return
		}
	}

	ctx, cancel := rpcContext(r)
	defer cancel()
	resp, err := h.deps.Repos.ListRepositories(ctx, &gitv1.ListRepositoriesRequest{
		OwnerId:   owner.ID.String(),
		PageSize:  size,
		PageToken: r.URL.Query().Get("page_token"),
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}

	page := repositoryPageJSON{Repositories: []repositoryJSON{}, NextPageToken: resp.GetNextPageToken()}
	for _, repo := range resp.GetRepositories() {
		id, ownerID, err := repoIDs(repo)
		if err != nil {
			writeUnavailable(w, r, "list repositories", err)
			return
		}
		level, err := h.access.Level(r.Context(), me.ID, id, ownerID)
		if err != nil {
			writeUnavailable(w, r, "check repository access", err)
			return
		}
		if level >= LevelRead {
			page.Repositories = append(page.Repositories, toRepository(repo, userOwner(owner), h.deps.ExternalURL))
		}
	}
	httpx.JSON(w, http.StatusOK, page)
}

type createRepositoryRequest struct {
	Owner         *string `json:"owner"`
	Name          string  `json:"name"`
	Description   *string `json:"description"`
	DefaultBranch *string `json:"default_branch"`
}

// createRepository answers POST /repos. The caller creates under their own
// name; organizations come later. git-svc owns the name rules (FR-25): its
// INVALID_ARGUMENT is a 400 and ALREADY_EXISTS a 409, with its message.
func (h *Handler) createRepository(w http.ResponseWriter, r *http.Request) {
	var req createRepositoryRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	me, ok := h.caller(w, r)
	if !ok {
		return
	}
	if req.Owner != nil && strings.ToLower(*req.Owner) != me.Username {
		httpx.Error(w, http.StatusForbidden, "repositories can only be created under your own name")
		return
	}

	ctx, cancel := rpcContext(r)
	defer cancel()
	resp, err := h.deps.Repos.CreateRepository(ctx, &gitv1.CreateRepositoryRequest{
		OwnerId:       me.ID.String(),
		Name:          req.Name,
		DefaultBranch: req.DefaultBranch,
		Description:   req.Description,
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toRepository(resp.GetRepository(), userOwner(me), h.deps.ExternalURL))
}

// deleteRepository answers DELETE /repos/{owner}/{repo}; the route needs
// maintain (FR-26). Dash asks for the name to be typed back before it calls
// this. git-svc frees the name at once.
func (h *Handler) deleteRepository(w http.ResponseWriter, r *http.Request) {
	info := repoFrom(r.Context())
	ctx, cancel := rpcContext(r)
	defer cancel()
	_, err := h.deps.Repos.DeleteRepository(ctx, &gitv1.DeleteRepositoryRequest{Id: info.ID.String()})
	switch status.Code(err) {
	case codes.OK:
		w.WriteHeader(http.StatusNoContent)
	case codes.NotFound:
		// Deleted by another request since resolveRepo found it.
		httpx.Error(w, http.StatusNotFound, msgRepoNotFound)
	default:
		writeGitError(w, r, err)
	}
}
