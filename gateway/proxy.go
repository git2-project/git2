package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/git2-project/git2/internal/httpx"
)

// newProxy forwards to target. It uses Rewrite, not Director: Go removes the
// client's X-Forwarded-* and hop-by-hop headers before Rewrite runs, so a
// client cannot list our headers in Connection to have them dropped.
func newProxy(target *url.URL) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			stripClientHeaders(pr.Out.Header)
			pr.SetXForwarded()
			pr.Out.Header.Set(headerRealIP, clientIP(pr.In))
			pr.Out.Header.Set(headerRequestID, requestID(pr.In.Context()))
		},
		ModifyResponse: func(resp *http.Response) error {
			// withNoSniff already set it on the client response.
			resp.Header.Del(headerNoSniff)
			return nil
		},
		ErrorHandler: proxyError,
	}
}

// proxyError maps a failed upstream call to the contract's statuses:
// 504 when the per-request timeout ran out, 503 when the upstream cannot be
// reached, 502 for anything else it got wrong.
func proxyError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(r.Context().Err(), context.Canceled) {
		return // the client went away; nobody reads an answer
	}
	status, msg := http.StatusBadGateway, "upstream answered with a broken response"
	var opErr *net.OpError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		status, msg = http.StatusGatewayTimeout, "upstream did not answer in time"
	case errors.As(err, &opErr) && opErr.Op == "dial":
		status, msg = http.StatusServiceUnavailable, "service unavailable, try again"
	}
	slog.Warn("gateway: upstream error", "path", r.URL.Path, "status", status,
		"request_id", requestID(r.Context()), "err", err)
	httpx.Error(w, status, msg)
}

// withTimeout bounds one proxied request.
func withTimeout(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
