package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"time"
)

// GitIdleTimeout cuts a Git request once no byte moved in either direction
// for this long: the contract's "5 min idle" (git2-deploy docs/api/gateway.md,
// Timeouts), the same as git-svc's stalled read or write (git2-deploy#27).
// Git routes have no overall timeout, since a clone may stream for minutes,
// and http.Server.IdleTimeout only covers the wait between requests, so
// without it a client that stops reading or sending holds its connection and
// the git-svc stream open forever.
//
// A busy but healthy git-svc is never silent this long: upload-pack and
// receive-pack send a keepalive every 5 s while they work without output
// (git-config uploadpack.keepAlive, receive.keepAlive).
const GitIdleTimeout = 5 * time.Minute

// withIdleDeadline runs next with a read and a write deadline of idle on the
// connection, both pushed forward by every successful read of the request
// body and every write of the response.
//
// Both move together: while a clone streams, the client sends nothing, and
// net/http's background read of the connection would hit a read deadline
// that only reads pushed forward, cancelling the request. net/http clears
// both after the request, so a keep-alive connection's next request starts
// without them.
func withIdleDeadline(idle time.Duration, w http.ResponseWriter, r *http.Request, next http.Handler) {
	rc := http.NewResponseController(w)
	extend := func() {
		t := time.Now().Add(idle)
		_ = rc.SetReadDeadline(t)
		_ = rc.SetWriteDeadline(t)
	}
	if err := rc.SetWriteDeadline(time.Now().Add(idle)); err != nil {
		// Only a ResponseWriter that hides the connection fails here; the
		// gateway's middleware does not wrap it. Serve without a deadline
		// rather than refuse Git, and say so.
		slog.Warn("gateway: no idle deadline on a Git request", "request_id", requestID(r.Context()), "err", err)
		next.ServeHTTP(w, r)
		return
	}
	extend()

	out := r.Clone(r.Context())
	out.Body = &idleBody{ReadCloser: r.Body, extend: extend}
	next.ServeHTTP(&idleWriter{ResponseWriter: w, extend: extend}, out)
}

// idleBody pushes the deadlines forward on every read that got bytes.
type idleBody struct {
	io.ReadCloser
	extend func()
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.extend()
	}
	return n, err
}

// idleWriter pushes the deadlines forward after every write that sent
// bytes. Unwrap lets http.ResponseController (the proxy's flushes) reach the
// connection underneath.
type idleWriter struct {
	http.ResponseWriter
	extend func()
}

func (w *idleWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if n > 0 {
		w.extend()
	}
	return n, err
}

func (w *idleWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
