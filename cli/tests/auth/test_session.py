import asyncio
import multiprocessing
import threading
from pathlib import Path

import httpx
import pytest

from git2cli.auth.session import (
    REFRESH_MARGIN,
    AuthUnavailable,
    BadCredentials,
    SessionAuth,
    SignedOut,
    instance_base,
    login,
    logout,
)
from git2cli.auth.store import Credentials, CredentialStore
from tests.auth.fake_auth_svc import EMAIL, PASSWORD, USERNAME, FakeAuthSvc, serve

URL = "https://git2.example.com"
NOW = 10_000.0


def clock() -> float:
    return NOW


@pytest.fixture
def store(tmp_path) -> CredentialStore:
    return CredentialStore(tmp_path / "git2")


def signed_in(store: CredentialStore, *, access="access-1", expires_in=900.0, url=URL):
    store.save(Credentials(url, USERNAME, access, "refresh-1", NOW + expires_in))


def client(svc: FakeAuthSvc, store, before=None) -> httpx.AsyncClient:
    return httpx.AsyncClient(
        transport=svc.transport(before), auth=SessionAuth(store, clock=clock), base_url=URL
    )


async def test_adds_the_bearer_token(store):
    svc = FakeAuthSvc()
    signed_in(store)
    async with client(svc, store) as c:
        r = await c.get("/api/thing")
    assert r.json()["token"] == "access-1"
    assert svc.refresh_calls == 0


async def test_a_401_refreshes_once_and_retries_once(store):
    svc = FakeAuthSvc()
    signed_in(store, access="revoked")  # not expired, but the server refuses it
    async with client(svc, store) as c:
        r = await c.get("/api/thing")
    assert r.status_code == 200
    assert r.json()["token"] == "access-2"
    assert svc.refresh_calls == 1
    assert svc.log == ["GET /api/thing", "POST /api/auth/refresh", "GET /api/thing"]
    assert store.load().refresh_token == "refresh-2"


async def test_a_second_401_is_returned_not_retried_again(store):
    svc = FakeAuthSvc()
    signed_in(store, access="revoked")
    svc.issue = lambda: {  # the server hands out a token it then refuses
        "X-Access-Token": "useless",
        "X-Refresh-Token": "refresh-2",
        "X-Access-Expires-In": "900",
    }
    async with client(svc, store) as c:
        r = await c.get("/api/thing")
    assert r.status_code == 401
    assert svc.refresh_calls == 1


async def test_a_401_right_after_an_early_refresh_is_returned(store):
    svc = FakeAuthSvc()
    signed_in(store, expires_in=REFRESH_MARGIN - 1)
    svc.issue = lambda: {
        "X-Access-Token": "useless",
        "X-Refresh-Token": "refresh-2",
        "X-Access-Expires-In": "900",
    }
    async with client(svc, store) as c:
        r = await c.get("/api/thing")
    assert r.status_code == 401
    assert svc.refresh_calls == 1


async def test_a_token_about_to_expire_is_refreshed_first(store):
    svc = FakeAuthSvc()
    signed_in(store, expires_in=REFRESH_MARGIN - 1)
    async with client(svc, store) as c:
        r = await c.get("/api/thing")
    assert r.json()["token"] == "access-2"
    assert svc.log == ["POST /api/auth/refresh", "GET /api/thing"]


async def test_a_refused_refresh_signs_out_and_drops_the_tokens(store):
    svc = FakeAuthSvc()
    svc.refresh = {"someone-else"}
    signed_in(store, access="revoked")
    async with client(svc, store) as c:
        with pytest.raises(SignedOut):
            await c.get("/api/thing")
    assert store.load() is None


async def test_auth_svc_down_keeps_the_tokens(store):
    svc = FakeAuthSvc(refresh_status=503)
    signed_in(store, access="revoked")
    async with client(svc, store) as c:
        with pytest.raises(AuthUnavailable):
            await c.get("/api/thing")
    assert store.load().refresh_token == "refresh-1"


async def test_not_signed_in(store):
    async with client(FakeAuthSvc(), store) as c:
        with pytest.raises(SignedOut):
            await c.get("/api/thing")


async def test_two_concurrent_401s_refresh_once(store):
    # Both requests reach the server with the old token before either
    # refreshes; without single-flight the second refresh replays a used
    # token and the server ends the session (FR-10).
    svc = FakeAuthSvc()
    signed_in(store, access="revoked")
    gate = asyncio.Barrier(2)

    async def hold_stale(request: httpx.Request) -> None:
        if request.url.path == "/api/thing" and "revoked" in request.headers["Authorization"]:
            await gate.wait()
        if request.url.path == "/api/auth/refresh":
            await asyncio.sleep(0.05)  # a real refresh takes a round trip; others run meanwhile

    async with client(svc, store, before=hold_stale) as c:
        first, second = await asyncio.wait_for(
            asyncio.gather(c.get("/api/thing"), c.get("/api/thing")), timeout=5
        )
    assert (first.status_code, second.status_code) == (200, 200)
    assert svc.refresh_calls == 1
    assert not svc.session_ended


def test_sync_client_uses_the_same_flow(store):
    svc = FakeAuthSvc()
    signed_in(store, access="revoked")
    with httpx.Client(transport=svc.transport(), auth=SessionAuth(store, clock=clock)) as c:
        r = c.get(f"{URL}/api/thing")
    assert r.json()["token"] == "access-2"
    assert svc.refresh_calls == 1


def test_two_threads_refresh_once(tmp_path):
    svc = FakeAuthSvc()
    server = serve(svc, gate=threading.Barrier(2, timeout=10))
    base = f"http://127.0.0.1:{server.server_address[1]}"
    store = CredentialStore(tmp_path / "git2")
    store.save(Credentials(base, USERNAME, "revoked", "refresh-1", 4_000_000_000.0))
    auth = SessionAuth(store)  # shared, as one client used from two threads
    codes: list[int] = []

    def request() -> None:
        with httpx.Client(auth=auth, timeout=10) as c:
            codes.append(c.get(f"{base}/api/thing").status_code)

    threads = [threading.Thread(target=request) for _ in range(2)]
    try:
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=30)
    finally:
        server.shutdown()
    assert codes == [200, 200]
    assert svc.refresh_calls == 1
    assert not svc.session_ended


def _process_request(store_dir: str, base_url: str, results) -> None:
    store = CredentialStore(Path(store_dir))
    with httpx.Client(auth=SessionAuth(store), timeout=10) as c:
        results.put(c.get(f"{base_url}/api/thing").status_code)


def test_two_processes_refresh_once(tmp_path):
    # A git2 command and the TUI can run at the same time; each has its own
    # in-process lock, so only the file lock keeps them from both refreshing.
    svc = FakeAuthSvc()
    server = serve(svc, gate=threading.Barrier(2, timeout=10))
    base = f"http://127.0.0.1:{server.server_address[1]}"
    store = CredentialStore(tmp_path / "git2")
    store.save(Credentials(base, USERNAME, "revoked", "refresh-1", 4_000_000_000.0))

    ctx = multiprocessing.get_context("spawn")
    results = ctx.Queue()
    procs = [
        ctx.Process(target=_process_request, args=(str(store.directory), base, results))
        for _ in range(2)
    ]
    try:
        for p in procs:
            p.start()
        for p in procs:
            p.join(timeout=30)
        assert sorted(results.get(timeout=5) for _ in procs) == [200, 200]
    finally:
        server.shutdown()
    assert svc.refresh_calls == 1
    assert not svc.session_ended


async def test_login_stores_the_session_but_never_the_password(store):
    svc = FakeAuthSvc()
    async with httpx.AsyncClient(transport=svc.transport()) as c:
        creds = await login(c, store, URL + "/", EMAIL, PASSWORD, clock=clock)
    assert creds == store.load()
    assert (creds.instance_url, creds.username) == (URL, USERNAME)
    assert creds.expires_at == NOW + 900
    assert PASSWORD not in store.path.read_text()


async def test_login_with_a_wrong_password(store):
    async with httpx.AsyncClient(transport=FakeAuthSvc().transport()) as c:
        with pytest.raises(BadCredentials):
            await login(c, store, URL, EMAIL, "wrong")
    assert store.load() is None


@pytest.mark.parametrize("bad", ["git2.example.com", "ftp://git2.example.com", "https://"])
async def test_login_refuses_a_url_that_is_not_http(store, bad):
    async with httpx.AsyncClient(transport=FakeAuthSvc().transport()) as c:
        with pytest.raises(ValueError):
            await login(c, store, bad, EMAIL, PASSWORD)


async def test_logout_ends_the_session_and_forgets_it(store):
    svc = FakeAuthSvc()
    signed_in(store)
    async with httpx.AsyncClient(transport=svc.transport()) as c:
        await logout(c, store)
    assert store.load() is None
    assert "refresh-1" not in svc.refresh


async def test_the_token_never_goes_to_another_host(store):
    seen = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(200)

    signed_in(store, expires_in=1)  # expiring: a refresh would also be a leak
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(handler), auth=SessionAuth(store, clock=clock)
    ) as c:
        for url in [
            "https://evil.example/api/thing",
            "http://git2.example.com/api/thing",  # same host, other scheme
            "https://git2.example.com:8443/api/thing",  # same host, other port
        ]:
            await c.get(url)
    assert [r.url.host for r in seen] == ["evil.example", "git2.example.com", "git2.example.com"]
    assert all("Authorization" not in r.headers for r in seen)


class StreamingTransport(httpx.AsyncBaseTransport):
    """Reads request bodies chunk by chunk, as a network transport does.
    httpx.MockTransport reads and caches the whole body first, which would
    make any body replayable and the test below pass for the wrong reason."""

    def __init__(self, svc: FakeAuthSvc) -> None:
        self.svc = svc
        self.bodies: list[bytes] = []

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        body = b"".join([chunk async for chunk in request.stream])
        if request.url.path == "/api/thing":
            self.bodies.append(body)
        status, hdrs, payload = self.svc.respond(
            request.method, request.url.path, request.headers, body
        )
        return httpx.Response(status, headers=hdrs, json=payload)


async def test_a_streamed_body_is_sent_again_on_the_retry(store):
    svc = FakeAuthSvc()
    signed_in(store, access="revoked")
    transport = StreamingTransport(svc)

    async def chunks():
        yield b"hello "
        yield b"world"

    async with httpx.AsyncClient(
        transport=transport, auth=SessionAuth(store, clock=clock), base_url=URL
    ) as c:
        r = await c.post("/api/thing", content=chunks())
    assert r.status_code == 200
    assert transport.bodies == [b"hello world", b"hello world"]


async def test_a_refresh_400_signs_out(store):
    svc = FakeAuthSvc(refresh_status=400)
    signed_in(store, access="revoked")
    async with client(svc, store) as c:
        with pytest.raises(SignedOut):
            await c.get("/api/thing")
    assert store.load() is None


async def test_logout_waits_for_a_refresh_in_another_window(store):
    svc = FakeAuthSvc()
    signed_in(store)
    held = store.lock()
    held.acquire()
    try:
        async with httpx.AsyncClient(transport=svc.transport()) as c:
            task = asyncio.create_task(logout(c, store))
            await asyncio.sleep(0.3)
            assert not task.done()
            assert "POST /api/auth/logout" not in svc.log
            held.release()
            await asyncio.wait_for(task, timeout=5)
    finally:
        if held.is_locked:
            held.release()
    assert store.load() is None


async def test_login_waits_for_a_refresh_in_another_window(store):
    held = store.lock()
    held.acquire()
    try:
        async with httpx.AsyncClient(transport=FakeAuthSvc().transport()) as c:
            task = asyncio.create_task(login(c, store, URL, EMAIL, PASSWORD, clock=clock))
            await asyncio.sleep(0.3)
            assert not task.done() and store.load() is None
            held.release()
            await asyncio.wait_for(task, timeout=5)
    finally:
        if held.is_locked:
            held.release()
    assert store.load().username == USERNAME


def test_instance_url_drops_user_info_and_does_not_echo_it():
    assert (
        instance_base("https://ada:pw@git2.example.com:8443/x") == "https://git2.example.com:8443"
    )
    with pytest.raises(ValueError) as err:
        instance_base("ftp://ada:pw@git2.example.com")
    assert "pw" not in str(err.value)


async def test_login_with_a_malformed_me_answer(store):
    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/api/auth/login":
            hdrs = {"X-Access-Token": "a", "X-Refresh-Token": "r", "X-Access-Expires-In": "900"}
            return httpx.Response(200, headers=hdrs, json={})
        return httpx.Response(200, json={"id": "u1"})  # no username

    async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as c:
        with pytest.raises(AuthUnavailable):
            await login(c, store, URL, EMAIL, PASSWORD)
    assert store.load() is None


class SyncStreamingTransport(httpx.BaseTransport):
    def __init__(self, svc: FakeAuthSvc) -> None:
        self.svc = svc
        self.bodies: list[bytes] = []

    def handle_request(self, request: httpx.Request) -> httpx.Response:
        body = b"".join(request.stream)
        if request.url.path == "/api/thing":
            self.bodies.append(body)
        status, hdrs, payload = self.svc.respond(
            request.method, request.url.path, request.headers, body
        )
        return httpx.Response(status, headers=hdrs, json=payload)


def test_a_streamed_body_is_sent_again_on_the_retry_sync(store):
    svc = FakeAuthSvc()
    signed_in(store, access="revoked")
    transport = SyncStreamingTransport(svc)
    with httpx.Client(transport=transport, auth=SessionAuth(store, clock=clock)) as c:
        r = c.post(f"{URL}/api/thing", content=(part for part in [b"hello ", b"world"]))
    assert r.status_code == 200
    assert transport.bodies == [b"hello world", b"hello world"]


async def test_login_and_logout_work_on_a_client_that_carries_the_session(store):
    # The TUI keeps one client with SessionAuth. Login must not need a session,
    # and logout (holding the lock) must not wait on the session's own refresh.
    svc = FakeAuthSvc()
    async with client(svc, store) as c:
        await asyncio.wait_for(login(c, store, URL, EMAIL, PASSWORD, clock=clock), timeout=5)
        signed_in(store, access="access-2", expires_in=1)  # expiring: would refresh under the lock
        await asyncio.wait_for(logout(c, store), timeout=5)
    assert store.load() is None
    assert svc.refresh_calls == 0
