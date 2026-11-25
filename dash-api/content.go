package dashapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	"github.com/git2-project/git2/internal/httpx"
)

// maxRawBytes is the largest file /raw serves (contract README, open
// question 6): bigger files take git clone, so a download never runs into
// the request timeout.
const maxRawBytes = 10 << 20

// getTree answers GET …/tree (CommitService.GetTree).
func (h *Handler) getTree(w http.ResponseWriter, r *http.Request) {
	ref, path, ok := refAndPath(w, r, false)
	if !ok {
		return
	}
	ctx, cancel := rpcContext(r)
	defer cancel()
	resp, err := h.deps.Commits.GetTree(ctx, &gitv1.GetTreeRequest{
		RepositoryId: repoFrom(r.Context()).ID.String(), Ref: ref, Path: path,
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toTree(resp))
}

// getBlob answers GET …/blob (CommitService.GetBlob) and adds the language.
func (h *Handler) getBlob(w http.ResponseWriter, r *http.Request) {
	ref, path, ok := refAndPath(w, r, true)
	if !ok {
		return
	}
	ctx, cancel := rpcContext(r)
	defer cancel()
	resp, err := h.deps.Commits.GetBlob(ctx, &gitv1.GetBlobRequest{
		RepositoryId: repoFrom(r.Context()).ID.String(), Ref: ref, Path: path,
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toBlob(resp))
}

// getRaw answers GET …/raw (CommitService.GetRawBlob, a stream) with the
// file's bytes, always as a download that cannot run as a page on the git2
// origin. The size comes in the first message, so a file over maxRawBytes
// is refused with 413 before anything is written.
//
// Once the 200 is sent, a failure can no longer change the status: the
// response is aborted instead, and the client sees a cut body, never a
// complete-looking file.
func (h *Handler) getRaw(w http.ResponseWriter, r *http.Request) {
	ref, path, ok := refAndPath(w, r, true)
	if !ok {
		return
	}
	// No rpcTimeout: the request's own deadline bounds the stream.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stream, err := h.deps.Commits.GetRawBlob(ctx, &gitv1.GetRawBlobRequest{
		RepositoryId: repoFrom(r.Context()).ID.String(), Ref: ref, Path: path,
	})
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	first, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		logFault(r, "GetRawBlob", errors.New("stream ended without a first message"))
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err != nil {
		writeGitError(w, r, err)
		return
	}
	size := first.GetSize()
	if size > maxRawBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "file is larger than 10 MiB; use git clone to get it")
		return
	}
	if size < 0 {
		logFault(r, "GetRawBlob", fmt.Errorf("negative size %d", size))
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	hdr := w.Header()
	hdr.Set("Content-Type", "application/octet-stream")
	hdr.Set("Content-Disposition", attachment(first.GetName()))
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	hdr.Set("Cache-Control", "private")
	hdr.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)

	written := int64(0)
	for msg := first; ; {
		data := msg.GetData()
		if written+int64(len(data)) > size {
			abort(r, fmt.Errorf("git-svc sent more than the announced %d bytes", size))
		}
		if _, err := w.Write(data); err != nil {
			return // the client went away
		}
		written += int64(len(data))

		msg, err = stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			abort(r, fmt.Errorf("GetRawBlob stream: %w", err))
		}
	}
	if written != size {
		abort(r, fmt.Errorf("git-svc sent %d of the announced %d bytes", written, size))
	}
}

// attachment is the Content-Disposition for a download named name. mime
// quotes the name, or encodes it (RFC 2231) when it is not plain ASCII.
func attachment(name string) string {
	if v := mime.FormatMediaType("attachment", map[string]string{"filename": name}); v != "" {
		return v
	}
	return "attachment"
}

// abort cuts the response after its status was sent. net/http closes the
// connection without finishing the body; chi's Recoverer passes the panic
// on without logging it.
func abort(r *http.Request, err error) {
	logFault(r, "raw download aborted", err)
	panic(http.ErrAbortHandler)
}
