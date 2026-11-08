package gateway

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// assetDir is where Vite writes the hashed bundles. "assets" is a reserved
// owner name (git2-deploy docs/api/gateway.md), so no client route starts
// with it.
const assetDir = "assets/"

// spa serves a built single-page app from dir. A path that names a file is
// served as that file. A missing file under assets/ (a stale bundle) is a
// 404, so a script tag never receives HTML. Every other path is a client-side
// route and gets index.html, even with a dot in it: repository names and
// code-browser file paths (/alice/repo/blob/main/README.md) have one.
//
// prefix is stripped from the URL first ("/admin" for Admin, "" for Dash).
func spa(dir, prefix string) http.Handler {
	fsys := os.DirFS(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// path.Clean on a rooted path removes every "..", so the name stays
		// inside fsys; fs.ValidPath double-checks it.
		name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, prefix)), "/")
		if name != "" && fs.ValidPath(name) {
			if info, err := fs.Stat(fsys, name); err == nil && !info.IsDir() {
				http.ServeFileFS(w, r, fsys, name)
				return
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "could not read file", http.StatusInternalServerError)
				return
			}
			if strings.HasPrefix(name, assetDir) {
				http.NotFound(w, r)
				return
			}
		}
		// index.html must not be cached: it names the current asset hashes.
		w.Header().Set("Cache-Control", "no-cache")
		serveIndex(w, r, fsys)
	})
}

// serveIndex writes index.html without http.ServeFileFS's redirect of
// ".../index.html" URLs, which would loop for client-side routes.
func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	f, err := fsys.Open("index.html")
	if err != nil {
		http.Error(w, "web app not installed", http.StatusServiceUnavailable)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "web app not installed", http.StatusServiceUnavailable)
		return
	}
	rs, ok := f.(io.ReadSeeker) // true for os.DirFS files
	if !ok {
		http.Error(w, "web app not installed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", info.ModTime(), rs)
}
