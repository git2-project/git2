"""Signing in to auth-svc and keeping the session alive
(contracts/auth-svc/openapi.yaml; docs/design/tui-v0.md, section 6).

``SessionAuth`` is an ``httpx.Auth``: give it to any client, including the
generated API client, and every request carries the access token, refreshed
when needed. No call can skip the refresh.

Refresh tokens are single use: presenting one twice ends the whole session
(FR-10). So a refresh is single-flight: it runs under a lock on
``credentials.lock``, then reads the file again, and if another request,
thread or process already rotated the tokens, it uses them as they are. A
file lock alone covers all three: each lock object holds its own handle, so
two of them exclude each other inside one process too (tested).
"""

import time
from collections.abc import AsyncGenerator, Callable, Generator
from urllib.parse import urlsplit

import httpx
from filelock import AsyncFileLock, FileLock

from git2cli.auth.store import Credentials, CredentialStore

# Refresh when the access token has less than this left (design, section 6).
REFRESH_MARGIN = 60.0

Clock = Callable[[], float]


class SignedOut(Exception):
    """Nobody is signed in, or the server ended the session. Sign in again."""


class BadCredentials(Exception):
    """Wrong email or password (login answered 401)."""


class AccountDisabled(Exception):
    """The account is disabled (login answered 403)."""


class AuthUnavailable(Exception):
    """auth-svc could not answer (429, 5xx). The tokens are kept (NFR-22)."""

    def __init__(self, status_code: int) -> None:
        super().__init__(f"git2 is not available (HTTP {status_code}), try again")
        self.status_code = status_code


def instance_base(url: str) -> str:
    """``https://host[:port]`` from what the user typed, or ValueError."""
    parts = urlsplit(url.strip())
    if parts.scheme not in ("http", "https") or not parts.netloc:
        raise ValueError(f"instance URL must look like https://git2.example.com, got {url!r}")
    return f"{parts.scheme}://{parts.netloc}"


def _tokens(response: httpx.Response, now: float) -> tuple[str, str, float]:
    """The pair auth-svc sends in headers, never in the body."""
    h = response.headers
    try:
        return h["X-Access-Token"], h["X-Refresh-Token"], now + int(h["X-Access-Expires-In"])
    except (KeyError, ValueError) as e:
        raise AuthUnavailable(response.status_code) from e


class SessionAuth(httpx.Auth):
    """Adds ``Authorization: Bearer`` from the store; refreshes before the
    token expires or once after a 401, then retries the request once.

    Raises SignedOut when there is no session or auth-svc refuses the
    refresh (the stored tokens are then dropped), and AuthUnavailable when
    auth-svc cannot answer (the tokens are kept).
    """

    def __init__(self, store: CredentialStore, *, clock: Clock = time.time) -> None:
        self._store = store
        self._clock = clock

    def sync_auth_flow(self, request: httpx.Request) -> Generator[httpx.Request, httpx.Response]:
        creds = self._current()
        refresh = self._expiring(creds)
        for _ in range(2):  # at most one refresh and one retry
            if refresh:
                self._store.ensure_directory()
                with FileLock(self._store.lock_path, mode=0o600):
                    creds = yield from self._refresh(creds)
            response = yield self._authorize(request, creds)
            if response.status_code != 401 or refresh:
                return
            refresh = True

    async def async_auth_flow(
        self, request: httpx.Request
    ) -> AsyncGenerator[httpx.Request, httpx.Response]:
        creds = self._current()
        refresh = self._expiring(creds)
        for _ in range(2):  # at most one refresh and one retry
            if refresh:
                self._store.ensure_directory()
                async with AsyncFileLock(self._store.lock_path, mode=0o600):
                    # An async generator cannot `yield from`; drive _refresh by hand.
                    steps, response = self._refresh(creds), None
                    while True:
                        try:
                            step = steps.send(response)
                        except StopIteration as done:
                            creds = done.value
                            break
                        response = yield step
            response = yield self._authorize(request, creds)
            if response.status_code != 401 or refresh:
                return
            refresh = True

    def _current(self) -> Credentials:
        creds = self._store.load()
        if creds is None:
            raise SignedOut("not signed in")
        return creds

    def _expiring(self, creds: Credentials) -> bool:
        return creds.expires_at - self._clock() < REFRESH_MARGIN

    @staticmethod
    def _authorize(request: httpx.Request, creds: Credentials) -> httpx.Request:
        request.headers["Authorization"] = f"Bearer {creds.access_token}"
        return request

    def _refresh(self, seen: Credentials) -> Generator[httpx.Request, httpx.Response, Credentials]:
        """Runs under both locks. Yields the refresh request, if one is still
        needed, and returns the credentials to use."""
        latest = self._store.load()
        if latest is None:
            raise SignedOut("signed out in another git2 window")
        if latest.refresh_token != seen.refresh_token and not self._expiring(latest):
            return latest  # someone else refreshed while we waited for the lock
        response = yield httpx.Request(
            "POST",
            f"{latest.instance_url}/api/auth/refresh",
            json={"refresh_token": latest.refresh_token},
        )
        if response.status_code == 401:
            self._store.clear()
            raise SignedOut("the session ended; sign in again")
        if response.status_code != 200:
            raise AuthUnavailable(response.status_code)
        updated = latest.with_tokens(*_tokens(response, self._clock()))
        self._store.save(updated)
        return updated


async def login(
    client: httpx.AsyncClient,
    store: CredentialStore,
    instance_url: str,
    email: str,
    password: str,
    *,
    clock: Clock = time.time,
) -> Credentials:
    """Signs in with email and password and stores the session. The
    password goes to auth-svc only and is never stored."""
    base = instance_base(instance_url)
    response = await client.post(
        f"{base}/api/auth/login", json={"email": email, "password": password}
    )
    if response.status_code == 401:
        raise BadCredentials("wrong email or password")
    if response.status_code == 403:
        raise AccountDisabled("this account is disabled")
    if response.status_code != 200:
        raise AuthUnavailable(response.status_code)
    access, refresh, expires_at = _tokens(response, clock())

    # Git over HTTPS signs in by username, which login does not return.
    me = await client.get(f"{base}/api/auth/me", headers={"Authorization": f"Bearer {access}"})
    if me.status_code != 200:
        raise AuthUnavailable(me.status_code)
    creds = Credentials(base, me.json()["username"], access, refresh, expires_at)
    store.save(creds)
    return creds


async def logout(client: httpx.AsyncClient, store: CredentialStore) -> None:
    """Ends the session on the server and forgets it here. The local tokens
    are dropped even when auth-svc cannot be reached."""
    creds = store.load()
    if creds is None:
        return
    try:
        await client.post(
            f"{creds.instance_url}/api/auth/logout", json={"refresh_token": creds.refresh_token}
        )
    except httpx.TransportError:
        pass  # the refresh token expires on its own; nothing to retry
    finally:
        store.clear()
