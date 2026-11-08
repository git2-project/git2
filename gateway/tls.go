package gateway

import (
	"crypto/tls"
	"net/http"
	"net/url"
)

// hstsValue tells browsers to use HTTPS only, for a year (OWASP HTTP Strict
// Transport Security cheat sheet). No includeSubDomains: a self-hosted
// git2 often shares its parent domain with services it does not control.
const hstsValue = "max-age=31536000"

// TLSConfig is the server side of the gateway's TLS: TLS 1.2 and newer with
// Go's default cipher suites, which match Mozilla's "intermediate" profile.
func TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12}
}

// withHSTS sets Strict-Transport-Security on every response. Only used when
// the gateway serves TLS itself: the header is ignored over plain HTTP.
func withHSTS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", hstsValue)
		next.ServeHTTP(w, r)
	})
}

// RedirectToHTTPS answers every plain-HTTP request with a redirect to the
// same path and query on external, git2's HTTPS address (NFR-04: the UIs
// and the API are not served over plain HTTP). 308, not 301: it keeps the
// method and body, so a git push sent to http:// is not replayed as a GET.
// The target host is external's, never the request's Host header, so the
// redirect cannot be pointed at another site.
func RedirectToHTTPS(external *url.URL) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := *external
		target.Path = r.URL.Path
		target.RawPath = ""
		target.RawQuery = r.URL.RawQuery
		http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
	})
}
