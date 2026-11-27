import os
import stat
from pathlib import Path

import pytest

from git2cli.auth.store import (
    Credentials,
    CredentialStore,
    InsecureCredentialsFile,
    InvalidCredentialsFile,
    default_directory,
)

CREDS = Credentials("https://git2.example.com", "ada", "access-1", "refresh-1", 1000.0)
posix_only = pytest.mark.skipif(os.name != "posix", reason="file modes are POSIX")


def mode(path: Path) -> int:
    return stat.S_IMODE(path.stat().st_mode)


def test_round_trip(tmp_path):
    store = CredentialStore(tmp_path / "git2")
    assert store.load() is None
    store.save(CREDS)
    assert store.load() == CREDS
    store.clear()
    assert store.load() is None


@posix_only
def test_file_is_0600_in_a_0700_directory(tmp_path):
    store = CredentialStore(tmp_path / "git2")
    store.save(CREDS)
    assert mode(store.directory) == 0o700
    assert mode(store.path) == 0o600
    # Saving again replaces the file and keeps the mode.
    store.save(CREDS.with_tokens("access-2", "refresh-2", 2000.0))
    assert mode(store.path) == 0o600
    assert [p.name for p in store.directory.iterdir()] == ["credentials.json"]


@posix_only
@pytest.mark.parametrize("bits", [0o640, 0o604, 0o644])
def test_a_file_others_can_read_is_refused(tmp_path, bits):
    store = CredentialStore(tmp_path / "git2")
    store.save(CREDS)
    store.path.chmod(bits)
    with pytest.raises(InsecureCredentialsFile, match="chmod 600"):
        store.load()


def test_mode_check_can_be_off_for_windows(tmp_path):
    store = CredentialStore(tmp_path / "git2", check_mode=False)
    store.save(CREDS)
    store.path.chmod(0o644)
    assert store.load() == CREDS


def test_default_directory_follows_xdg():
    assert default_directory({"XDG_CONFIG_HOME": "/cfg"}) == Path("/cfg/git2")
    assert default_directory({}, home=Path("/home/ada")) == Path("/home/ada/.config/git2")


@pytest.mark.parametrize("content", ["", "{", "{}", '{"instance_url": "x"}', "[1]"])
def test_a_file_git2_did_not_write_asks_to_sign_in_again(tmp_path, content):
    store = CredentialStore(tmp_path / "git2")
    store.save(CREDS)
    store.path.write_text(content)
    with pytest.raises(InvalidCredentialsFile, match="sign in again"):
        store.load()


@posix_only
def test_a_symlink_in_place_of_the_file_is_refused(tmp_path):
    store = CredentialStore(tmp_path / "git2")
    store.ensure_directory()
    elsewhere = tmp_path / "elsewhere.json"
    CredentialStore(tmp_path / "other").save(CREDS)
    elsewhere.write_text((tmp_path / "other" / "credentials.json").read_text())
    elsewhere.chmod(0o600)
    store.path.symlink_to(elsewhere)
    with pytest.raises(InvalidCredentialsFile):
        store.load()


@posix_only
def test_an_existing_loose_directory_is_tightened(tmp_path):
    directory = tmp_path / "git2"
    directory.mkdir(mode=0o755)
    directory.chmod(0o755)
    CredentialStore(directory).save(CREDS)
    assert mode(directory) == 0o700


def test_repr_never_shows_the_tokens():
    text = repr(CREDS)
    assert "access-1" not in text and "refresh-1" not in text
    assert "ada" in text
