package dashapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	gitv1 "github.com/git2-project/git2-codeplane/gen/go/git/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGetTree(t *testing.T) {
	h := newHarness(t)
	var got *gitv1.GetTreeRequest
	size := int64(612)
	h.commits.getTree = func(req *gitv1.GetTreeRequest) (*gitv1.GetTreeResponse, error) {
		got = req
		return &gitv1.GetTreeResponse{
			Ref: "main", CommitSha: sha1, Path: "docs",
			Entries: []*gitv1.TreeEntry{
				{Name: "img", Path: "docs/img", Type: gitv1.TreeEntryType_TREE_ENTRY_TYPE_DIRECTORY, Mode: "040000", Sha: sha2},
				{Name: "README.md", Path: "docs/README.md", Type: gitv1.TreeEntryType_TREE_ENTRY_TYPE_FILE, Mode: "100644", Sha: sha1, Size: &size},
				{Name: "link", Path: "docs/link", Type: gitv1.TreeEntryType_TREE_ENTRY_TYPE_SYMLINK, Mode: "120000", Sha: sha1, Size: &size},
				{Name: "vendor", Path: "docs/vendor", Type: gitv1.TreeEntryType_TREE_ENTRY_TYPE_SUBMODULE, Mode: "160000", Sha: sha2},
			},
			LastCommit: commit(sha1, "Round invoice totals"),
		}, nil
	}
	rec := h.get(repoPath("/tree?ref=main&path=docs"), h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	if got.GetRepositoryId() != h.repoID.String() || got.GetRef() != "main" || got.GetPath() != "docs" {
		t.Fatalf("request = %v", got)
	}
	assertJSON(t, decode(t, rec), map[string]any{
		"ref": "main", "commit_sha": sha1, "path": "docs", "empty": false, "truncated": false,
		"last_commit": map[string]any{"sha": sha1, "title": "Round invoice totals", "committed_at": "2026-11-12T16:40:00Z"},
		"entries": []any{
			map[string]any{"name": "img", "path": "docs/img", "type": "dir", "mode": "040000", "sha": sha2, "size": nil},
			map[string]any{"name": "README.md", "path": "docs/README.md", "type": "file", "mode": "100644", "sha": sha1, "size": float64(612)},
			map[string]any{"name": "link", "path": "docs/link", "type": "symlink", "mode": "120000", "sha": sha1, "size": float64(612)},
			map[string]any{"name": "vendor", "path": "docs/vendor", "type": "submodule", "mode": "160000", "sha": sha2, "size": nil},
		},
	})
}

func TestGetTreeEmptyRepository(t *testing.T) {
	h := newHarness(t)
	h.commits.getTree = func(req *gitv1.GetTreeRequest) (*gitv1.GetTreeResponse, error) {
		return &gitv1.GetTreeResponse{Ref: "main", Empty: true}, nil
	}
	rec := h.get(repoPath("/tree"), h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	assertJSON(t, decode(t, rec), map[string]any{
		"ref": "main", "commit_sha": "", "path": "", "empty": true, "truncated": false,
		"entries": []any{}, "last_commit": nil,
	})
}

func TestContentErrors(t *testing.T) {
	h := newHarness(t)
	h.commits.getTree = func(req *gitv1.GetTreeRequest) (*gitv1.GetTreeResponse, error) {
		switch req.GetRef() {
		case "gone":
			return nil, status.Error(codes.NotFound, "ref not found")
		case "HEAD^":
			return nil, status.Error(codes.InvalidArgument, "ref is malformed")
		}
		return nil, status.Error(codes.Internal, "broken")
	}
	wantError(t, h.get(repoPath("/tree?ref=gone"), h.adaToken), http.StatusNotFound, "ref not found")
	wantError(t, h.get(repoPath("/tree?ref=HEAD%5E"), h.adaToken), http.StatusBadRequest, "ref is malformed")
	wantError(t, h.get(repoPath("/tree?ref=x"), h.adaToken), http.StatusInternalServerError, "internal error")

	wantError(t, h.get(repoPath("/blob"), h.adaToken), http.StatusBadRequest, "path is required")
	wantError(t, h.get(repoPath("/raw"), h.adaToken), http.StatusBadRequest, "path is required")
	long := bytes.Repeat([]byte("a"), maxRefLen+1)
	wantStatus(t, h.get(repoPath("/tree?ref="+string(long)), h.adaToken), http.StatusBadRequest)
}

func TestGetBlob(t *testing.T) {
	cases := map[string]struct {
		resp         *gitv1.GetBlobResponse
		wantContent  any
		wantLanguage any
	}{
		"text": {
			resp:         &gitv1.GetBlobResponse{Name: "main.go", Path: "cmd/main.go", Size: 12, Content: []byte("package main")},
			wantContent:  "package main",
			wantLanguage: "go",
		},
		"invalid UTF-8": {
			resp:         &gitv1.GetBlobResponse{Name: "notes", Path: "notes", Size: 4, Content: []byte{'a', 0xff, 0xfe, 'b'}},
			wantContent:  "a�b",
			wantLanguage: nil,
		},
		"binary": {
			resp:         &gitv1.GetBlobResponse{Name: "logo.png", Path: "logo.png", Size: 9000, Binary: true},
			wantContent:  nil,
			wantLanguage: nil,
		},
		"lfs pointer": {
			resp:         &gitv1.GetBlobResponse{Name: "model.py", Path: "model.py", Size: 130, Lfs: true, Content: []byte("version https://git-lfs.github.com/spec/v1")},
			wantContent:  "version https://git-lfs.github.com/spec/v1",
			wantLanguage: nil,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.commits.getBlob = func(req *gitv1.GetBlobRequest) (*gitv1.GetBlobResponse, error) {
				c.resp.Ref, c.resp.CommitSha = req.GetRef(), sha1
				return c.resp, nil
			}
			rec := h.get(repoPath("/blob?ref=main&path="+c.resp.Path), h.adaToken)
			wantStatus(t, rec, http.StatusOK)
			assertJSON(t, decode(t, rec), map[string]any{
				"ref": "main", "commit_sha": sha1, "path": c.resp.Path, "name": c.resp.Name,
				"size": float64(c.resp.Size), "binary": c.resp.Binary, "lfs": c.resp.Lfs, "truncated": false,
				"content": c.wantContent, "language": c.wantLanguage,
			})
		})
	}
}

// rawFile streams data as GetRawBlob does: size and name first, then
// pieces of at most 64 KiB.
func rawFile(name string, data []byte) func(*gitv1.GetRawBlobRequest, grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error {
	return func(_ *gitv1.GetRawBlobRequest, s grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error {
		if err := s.Send(&gitv1.GetRawBlobResponse{Size: int64(len(data)), Name: name}); err != nil {
			return err
		}
		for len(data) > 0 {
			n := min(len(data), 64<<10)
			if err := s.Send(&gitv1.GetRawBlobResponse{Data: data[:n]}); err != nil {
				return err
			}
			data = data[n:]
		}
		return nil
	}
}

func TestGetRaw(t *testing.T) {
	h := newHarness(t)
	data := bytes.Repeat([]byte("<script>alert(1)</script>\n"), 10000) // several pieces
	var got *gitv1.GetRawBlobRequest
	send := rawFile("index.html", data)
	h.commits.getRawBlob = func(req *gitv1.GetRawBlobRequest, s grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error {
		got = req
		return send(req, s)
	}
	rec := h.get(repoPath("/raw?ref=v1&path=web/index.html"), h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	if got.GetRef() != "v1" || got.GetPath() != "web/index.html" || got.GetRepositoryId() != h.repoID.String() {
		t.Fatalf("request = %v", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), data) {
		t.Fatalf("body has %d bytes, want %d", rec.Body.Len(), len(data))
	}
	wantHeaders := map[string]string{
		"Content-Type":            "application/octet-stream",
		"Content-Disposition":     `attachment; filename=index.html`,
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
		"Cache-Control":           "private",
		"Content-Length":          "260000",
	}
	for k, v := range wantHeaders {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestGetRawSizeLimit(t *testing.T) {
	h := newHarness(t)
	h.commits.getRawBlob = rawFile("big.bin", make([]byte, maxRawBytes))
	rec := h.get(repoPath("/raw?path=big.bin"), h.adaToken)
	wantStatus(t, rec, http.StatusOK)
	if rec.Body.Len() != maxRawBytes {
		t.Fatalf("body has %d bytes, want %d", rec.Body.Len(), maxRawBytes)
	}

	// One byte over: refused from the size in the first message alone.
	h.commits.getRawBlob = func(_ *gitv1.GetRawBlobRequest, s grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error {
		return s.Send(&gitv1.GetRawBlobResponse{Size: maxRawBytes + 1, Name: "bigger.bin"})
	}
	rec = h.get(repoPath("/raw?path=bigger.bin"), h.adaToken)
	wantError(t, rec, http.StatusRequestEntityTooLarge, "file is larger than 10 MiB; use git clone to get it")
	if cd := rec.Header().Get("Content-Disposition"); cd != "" {
		t.Errorf("413 carries Content-Disposition %q", cd)
	}
}

func TestGetRawErrors(t *testing.T) {
	h := newHarness(t)
	h.commits.getRawBlob = func(*gitv1.GetRawBlobRequest, grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error {
		return status.Error(codes.NotFound, "file not found")
	}
	wantError(t, h.get(repoPath("/raw?path=nope"), h.adaToken), http.StatusNotFound, "file not found")

	h.commits.getRawBlob = func(*gitv1.GetRawBlobRequest, grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error {
		return status.Error(codes.InvalidArgument, "path is a directory")
	}
	wantError(t, h.get(repoPath("/raw?path=docs"), h.adaToken), http.StatusBadRequest, "path is a directory")
}

// A stream that fails after the 200 cuts the response: the client must see
// an error, never a short file that looks complete.
func TestGetRawAbortsMidStream(t *testing.T) {
	cases := map[string]func(*gitv1.GetRawBlobRequest, grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error{
		"stream error": func(_ *gitv1.GetRawBlobRequest, s grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error {
			_ = s.Send(&gitv1.GetRawBlobResponse{Size: 1000, Name: "f.bin", Data: make([]byte, 100)})
			return status.Error(codes.Internal, "read failed")
		},
		"short stream": func(_ *gitv1.GetRawBlobRequest, s grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error {
			return s.Send(&gitv1.GetRawBlobResponse{Size: 1000, Name: "f.bin", Data: make([]byte, 100)})
		},
		"long stream": func(_ *gitv1.GetRawBlobRequest, s grpc.ServerStreamingServer[gitv1.GetRawBlobResponse]) error {
			_ = s.Send(&gitv1.GetRawBlobResponse{Size: 10, Name: "f.bin", Data: make([]byte, 10)})
			return s.Send(&gitv1.GetRawBlobResponse{Data: make([]byte, 10)})
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.commits.getRawBlob = fn
			srv := httptest.NewServer(h.router)
			t.Cleanup(srv.Close)
			req, _ := http.NewRequest(http.MethodGet, srv.URL+repoPath("/raw?path=f.bin"), nil)
			req.Header.Set("Authorization", "Bearer "+h.adaToken)
			resp, err := srv.Client().Do(req)
			if err != nil {
				return // cut before the headers arrived: also an error for the client
			}
			defer resp.Body.Close()
			if _, err := io.ReadAll(resp.Body); err == nil {
				t.Fatalf("read the body without an error, status %d", resp.StatusCode)
			}
		})
	}
}

func TestAttachment(t *testing.T) {
	cases := map[string]string{
		"logo.png":    `attachment; filename=logo.png`,
		"my file.txt": `attachment; filename="my file.txt"`,
		`a"b.txt`:     `attachment; filename="a\"b.txt"`,
		"résumé.pdf":  `attachment; filename*=utf-8''r%C3%A9sum%C3%A9.pdf`,
		// Control characters are encoded, so they cannot start a new header.
		"evil\r\nX: y": `attachment; filename*=utf-8''evil%0D%0AX%3A%20y`,
	}
	for name, want := range cases {
		if got := attachment(name); got != want {
			t.Errorf("attachment(%q) = %q, want %q", name, got, want)
		}
	}
}
