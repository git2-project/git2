package gateway

import (
	"context"
	"net"
	"net/http"
	"path"
	"strings"

	"github.com/google/uuid"
)

const (
	headerRequestID = "X-Request-Id"
	headerRealIP    = "X-Real-IP"
	headerNoSniff   = "X-Content-Type-Options"
)

// maxRequestIDLen bounds a client-supplied request ID so it cannot bloat
// every log line it lands in.
const maxRequestIDLen = 128

type requestIDKey struct{}

// withRequestID keeps the client's X-Request-Id when it is short and made of
// safe characters, otherwise makes a new one. It returns the ID to the
// client and stores it for the proxy (docs/api/gateway.md, "What upstreams
// receive").
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerRequestID)
		if !validRequestID(id) {
			id = uuid.NewString()
		}
		w.Header().Set(headerRequestID, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// validRequestID accepts 1..maxRequestIDLen characters from [A-Za-z0-9._-].
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, c := range id {
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// withNoSniff adds X-Content-Type-Options: nosniff to every response. The
// proxy drops an upstream copy of the header so it is never sent twice.
func withNoSniff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerNoSniff, "nosniff")
		next.ServeHTTP(w, r)
	})
}

// withCleanPath refuses a path that path.Clean would change ("..", ".",
// "//"). Routing matches the raw path, so "/api/auth/../repos" would match
// the public /api/auth/* route and skip the token check if an upstream
// cleaned it later. An escaped path (%2F and the like) is refused for the
// same reason: the gateway and the upstream could split it differently.
func withCleanPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		clean := path.Clean(p)
		if strings.HasSuffix(p, "/") && clean != "/" {
			clean += "/"
		}
		if clean != p || r.URL.RawPath != "" {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// stripClientHeaders deletes the headers only the gateway may set. A client
// that sends them would otherwise claim another user or spoof its IP.
func stripClientHeaders(h http.Header) {
	for name := range h {
		if strings.HasPrefix(name, "X-Git2-") || strings.HasPrefix(name, "X-Forwarded-") || name == "X-Real-Ip" {
			h.Del(name)
		}
	}
}

// clientIP is the address the TCP connection came from. The gateway is the
// edge, so it never reads an IP from a request header.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
