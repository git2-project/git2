"""A fake auth-svc with the contract's token rules (contracts/auth-svc):
tokens in response headers, and single-use refresh tokens where presenting
one twice ends the session (FR-10). Used in-process through
httpx.MockTransport, or over a local socket for the two-process test."""

import json
import threading
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import httpx

EMAIL, PASSWORD, USERNAME = "ada@example.com", "s3cret-password", "ada"
LIFETIME = 900


@dataclass
class FakeAuthSvc:
    access: set[str] = field(default_factory=lambda: {"access-1"})
    refresh: set[str] = field(default_factory=lambda: {"refresh-1"})
    session_ended: bool = False
    refresh_calls: int = 0
    refresh_status: int | None = None  # force an answer, e.g. 503
    log: list[str] = field(default_factory=list)
    _n: int = 1
    _mu: threading.Lock = field(default_factory=threading.Lock)

    def issue(self) -> dict[str, str]:
        self._n += 1
        a, r = f"access-{self._n}", f"refresh-{self._n}"
        self.access, self.refresh = {a}, {r}
        return {"X-Access-Token": a, "X-Refresh-Token": r, "X-Access-Expires-In": str(LIFETIME)}

    def respond(self, method: str, path: str, headers, body: bytes):
        """(status, headers, json body) for one request."""
        with self._mu:
            self.log.append(f"{method} {path}")
            bearer = headers.get("Authorization", "").removeprefix("Bearer ")
            if path == "/api/auth/login":
                req = json.loads(body)
                if (req["email"], req["password"]) != (EMAIL, PASSWORD):
                    return 401, {}, {"error": "invalid credentials"}
                self.session_ended = False
                return 200, self.issue(), {"id": "u1", "username": USERNAME}
            if path == "/api/auth/refresh":
                self.refresh_calls += 1
                if self.refresh_status:
                    return self.refresh_status, {}, {"error": "unavailable"}
                token = json.loads(body)["refresh_token"]
                if self.session_ended or token not in self.refresh:
                    self.session_ended = True  # a replayed refresh token ends the session
                    self.access, self.refresh = set(), set()
                    return 401, {}, {"error": "invalid refresh token"}
                return 200, self.issue(), {}
            if path == "/api/auth/logout":
                self.refresh.discard(json.loads(body).get("refresh_token", ""))
                return 200, {}, {"message": "logged out"}
            if bearer in self.access:
                if path == "/api/auth/me":
                    return 200, {}, {"id": "u1", "username": USERNAME, "email": EMAIL}
                return 200, {}, {"ok": True, "token": bearer}
            return 401, {}, {"error": "unauthorized"}

    def transport(self, before=None) -> httpx.MockTransport:
        """In-process transport. ``before(request)`` may be a coroutine, so a
        test can hold requests at a barrier."""

        async def handle(request: httpx.Request) -> httpx.Response:
            if before is not None:
                await before(request)
            status, hdrs, payload = self.respond(
                request.method, request.url.path, request.headers, request.content
            )
            return httpx.Response(status, headers=hdrs, json=payload)

        def handle_sync(request: httpx.Request) -> httpx.Response:
            status, hdrs, payload = self.respond(
                request.method, request.url.path, request.headers, request.content
            )
            return httpx.Response(status, headers=hdrs, json=payload)

        return httpx.MockTransport(handle if before is not None else handle_sync)


def serve(svc: FakeAuthSvc, gate: threading.Barrier | None = None) -> ThreadingHTTPServer:
    """svc on 127.0.0.1 for tests across processes. With ``gate``, requests
    that will get a 401 wait there, so every client sees its 401 before any
    of them refreshes."""

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            self._answer()

        def do_POST(self):
            self._answer()

        def _answer(self):
            body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
            bearer = self.headers.get("Authorization", "").removeprefix("Bearer ")
            if gate is not None and self.path == "/api/thing" and bearer not in svc.access:
                gate.wait()
            status, hdrs, payload = svc.respond(self.command, self.path, self.headers, body)
            data = json.dumps(payload).encode()
            self.send_response(status)
            for k, v in hdrs.items():
                self.send_header(k, v)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, *args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server
