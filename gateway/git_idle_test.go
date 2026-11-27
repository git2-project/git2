package gateway

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// testIdle is the idle deadline in these tests: long enough for a loaded CI
// machine to move bytes, short enough to keep the tests fast.
const testIdle = 300 * time.Millisecond

// newIdleGateway runs a real gateway (deadlines need a real connection)
// whose git-svc is svc, with the idle deadline set to testIdle.
func newIdleGateway(t *testing.T, svc http.Handler) *httptest.Server {
	t.Helper()
	gw := httptest.NewServer(idleGatewayHandler(t, svc))
	t.Cleanup(gw.Close)
	return gw
}

func idleGatewayHandler(t *testing.T, svc http.Handler) http.Handler {
	t.Helper()
	up := httptest.NewServer(svc)
	t.Cleanup(up.Close)
	target, _ := url.Parse(up.URL)

	g := newGitFixture(t)
	rdb := redis.NewClient(&redis.Options{Addr: g.mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	h, err := New(Deps{JWT: g.jwt, Redis: rdb, Core: target, WebDir: webDir(t),
		Git: &GitDeps{Creds: g.creds, Repos: g.repos, Limiter: NewSignInLimiter(rdb),
			GitSvc: target, IdleTimeout: testIdle}})
	must(t, err)
	return h
}

// rawRequest opens a TCP connection to the gateway and sends a request's
// head (and body prefix) without reading anything back.
func rawRequest(t *testing.T, gw *httptest.Server, head string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(gw.URL, "http://"))
	must(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = io.WriteString(conn, head)
	must(t, err)
	return conn
}

func basicHeader() string {
	return basic("deniz", gitPassword)["Authorization"]
}

// waitDone fails the test unless done is closed within limit.
func waitDone(t *testing.T, done <-chan struct{}, limit time.Duration, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("%s: still open after %s, want it cut after the %s idle deadline", what, limit, testIdle)
	}
}

// A client that asks for a clone and then stops reading must not hold the
// gateway and git-svc forever: once nothing moves for the idle deadline,
// the gateway gives up and git-svc's request ends.
func TestGitStalledReaderIsCut(t *testing.T) {
	upstreamDone := make(chan struct{})
	gw := newIdleGateway(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		w.WriteHeader(http.StatusOK)
		chunk := make([]byte, 32<<10)
		for {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			if r.Context().Err() != nil {
				return
			}
		}
	}))

	rawRequest(t, gw, "POST /deniz/api.git/git-upload-pack HTTP/1.1\r\nHost: x\r\n"+
		"Authorization: "+basicHeader()+"\r\nContent-Length: 4\r\n\r\n0000")
	// The client reads nothing; socket buffers fill and the gateway's write blocks.
	waitDone(t, upstreamDone, 20*testIdle, "git-svc stream behind a client that stopped reading")
}

// With TLS the gateway also speaks HTTP/2 (net/http's default), where the
// deadlines apply to the stream: a client that stops reading is cut too.
func TestGitStalledReaderIsCutOverHTTP2(t *testing.T) {
	upstreamDone := make(chan struct{})
	gw := httptest.NewUnstartedServer(idleGatewayHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		w.WriteHeader(http.StatusOK)
		chunk := make([]byte, 32<<10)
		for r.Context().Err() == nil {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})))
	gw.EnableHTTP2 = true
	gw.StartTLS()
	t.Cleanup(gw.Close)

	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/deniz/api.git/git-upload-pack", strings.NewReader("0000"))
	req.SetBasicAuth("deniz", gitPassword)
	resp, err := gw.Client().Do(req)
	must(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.ProtoMajor != 2 {
		t.Fatalf("got %s, want HTTP/2", resp.Proto)
	}
	// Read nothing: the stream's flow-control window fills and the gateway's write blocks.
	waitDone(t, upstreamDone, 20*testIdle, "git-svc stream behind an HTTP/2 client that stopped reading")
}

// A push whose client stops sending halfway is cut the same way.
func TestGitStalledSenderIsCut(t *testing.T) {
	upstreamDone := make(chan struct{})
	gw := newIdleGateway(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		_, _ = io.Copy(io.Discard, r.Body)
	}))

	rawRequest(t, gw, "POST /deniz/api.git/git-receive-pack HTTP/1.1\r\nHost: x\r\n"+
		"Authorization: "+basicHeader()+"\r\nContent-Length: 1048576\r\n\r\n"+strings.Repeat("x", 1024))
	waitDone(t, upstreamDone, 20*testIdle, "git-svc request behind a client that stopped sending")
}

// A slow clone that keeps moving finishes, though it takes several idle
// deadlines in total.
func TestGitSlowSteadyCloneCompletes(t *testing.T) {
	const parts = 10
	gw := newIdleGateway(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		for i := range parts {
			_, _ = fmt.Fprintf(w, "part %d\n", i)
			w.(http.Flusher).Flush()
			time.Sleep(testIdle / 3)
		}
	}))

	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/deniz/api.git/git-upload-pack", strings.NewReader("0000"))
	req.SetBasicAuth("deniz", gitPassword)
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("slow clone cut after %d bytes: %v", len(body), err)
	}
	if n := strings.Count(string(body), "\n"); n != parts {
		t.Fatalf("got %d parts, want %d", n, parts)
	}
}

// A Git request's deadlines must not outlive it: the next request on the same
// keep-alive connection, long after them, still works. net/http clears them
// after each request (net/http server.go, conn.serve); this pins it.
func TestGitIdleDeadlineDoesNotLeakIntoTheNextRequest(t *testing.T) {
	gw := newIdleGateway(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "refs\n")
	}))
	conn := rawRequest(t, gw, "GET /deniz/api.git/info/refs?service=git-upload-pack HTTP/1.1\r\nHost: x\r\n"+
		"Authorization: "+basicHeader()+"\r\n\r\n")
	br := bufio.NewReader(conn)
	first, err := http.ReadResponse(br, nil)
	must(t, err)
	_, _ = io.Copy(io.Discard, first.Body)
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first request: status %d", first.StatusCode)
	}

	time.Sleep(3 * testIdle) // well past the first request's deadlines

	_, err = io.WriteString(conn, "GET /healthz HTTP/1.1\r\nHost: x\r\n\r\n")
	must(t, err)
	must(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	second, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("second request on the same connection failed: %v", err)
	}
	body, err := io.ReadAll(second.Body)
	second.Body.Close()
	if err != nil || second.StatusCode != http.StatusOK || !strings.Contains(string(body), "ok") {
		t.Fatalf("second request: status %d body %q err %v", second.StatusCode, body, err)
	}
}
