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
