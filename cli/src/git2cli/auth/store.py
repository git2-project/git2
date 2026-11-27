"""The one place git2's tokens live on this machine (docs/design/tui-v0.md,
section 6): ``credentials.json`` in the git2 config directory, shared by the
CLI, the terminal UI and later git2-mcp. The account password is never
stored."""

import json
import os
import tempfile
from collections.abc import Mapping
from dataclasses import asdict, dataclass, replace
from pathlib import Path
from typing import Literal

CREDENTIALS_FILE = "credentials.json"
# A separate lock file: os.replace swaps credentials.json for a new inode, so
# a lock taken on it would stay on the old file (review of #50).
LOCK_FILE = "credentials.lock"


class InsecureCredentialsFile(Exception):
    """The credentials file can be read by other users of this machine."""

    def __init__(self, path: Path) -> None:
        super().__init__(
            f"{path} can be read by other users; run `chmod 600 {path}` or sign in again"
        )
        self.path = path


@dataclass(frozen=True)
class Credentials:
    instance_url: str  # scheme://host[:port], no trailing slash
    username: str  # for Git over HTTPS, which signs in by username
    access_token: str
    refresh_token: str
    expires_at: float  # when the access token expires, Unix seconds
    # "pat" arrives with personal access tokens (WBS-3.2) without a format change.
    kind: Literal["session", "pat"] = "session"

    def with_tokens(
        self, access_token: str, refresh_token: str, expires_at: float
    ) -> "Credentials":
        return replace(
            self, access_token=access_token, refresh_token=refresh_token, expires_at=expires_at
        )


def default_directory(env: Mapping[str, str] = os.environ, home: Path | None = None) -> Path:
    """``$XDG_CONFIG_HOME/git2``, else ``~/.config/git2``."""
    base = env.get("XDG_CONFIG_HOME") or str((home or Path.home()) / ".config")
    return Path(base) / "git2"


class CredentialStore:
    """Reads and writes ``credentials.json`` in ``directory``.

    ``check_mode`` refuses a file other users can read. It is on for POSIX
    only: on Windows ``st_mode`` shows group and other bits for every file,
    so the check would refuse them all.
    """

    def __init__(self, directory: Path, *, check_mode: bool = os.name == "posix") -> None:
        self.directory = directory
        self.path = directory / CREDENTIALS_FILE
        self.lock_path = directory / LOCK_FILE
        self._check_mode = check_mode

    def load(self) -> Credentials | None:
        """The stored credentials, or None when nobody is signed in."""
        try:
            fd = os.open(self.path, os.O_RDONLY)
        except FileNotFoundError:
            return None
        with os.fdopen(fd, encoding="utf-8") as f:
            if self._check_mode and os.fstat(fd).st_mode & 0o077:
                raise InsecureCredentialsFile(self.path)
            return Credentials(**json.load(f))

    def save(self, credentials: Credentials) -> None:
        """Replaces the file atomically: readers see the old or the new one."""
        self.ensure_directory()
        # mkstemp creates the file with O_EXCL and mode 0600.
        fd, tmp = tempfile.mkstemp(dir=self.directory, prefix=".credentials-", suffix=".tmp")
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as f:
                json.dump(asdict(credentials), f)
                f.flush()
                os.fsync(f.fileno())
            os.replace(tmp, self.path)
        except BaseException:
            Path(tmp).unlink(missing_ok=True)
            raise

    def clear(self) -> None:
        self.path.unlink(missing_ok=True)

    def ensure_directory(self) -> None:
        self.directory.mkdir(mode=0o700, parents=True, exist_ok=True)
