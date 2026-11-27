package dashapi

import (
	"net/http"

	"github.com/git2-project/git2/internal/httpx"
)

// getRepository answers GET /repos/{owner}/{repo} from what resolveRepo
// already found; it makes no call of its own.
func (h *Handler) getRepository(w http.ResponseWriter, r *http.Request) {
	info := repoFrom(r.Context())
	owner := ownerJSON{ID: info.Owner.ID, Name: info.Owner.Username, Kind: "user"}
	httpx.JSON(w, http.StatusOK, toRepository(info.Repo, owner, h.deps.ExternalURL))
}
