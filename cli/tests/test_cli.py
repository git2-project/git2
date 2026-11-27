from typer.testing import CliRunner

from git2cli import __version__, cli

runner = CliRunner()


def test_version_prints_and_does_not_open_the_tui(monkeypatch):
    opened = []
    monkeypatch.setattr(cli.Git2App, "run", lambda self: opened.append(self))
    result = runner.invoke(cli.app, ["--version"])
    assert result.exit_code == 0
    assert result.stdout.strip() == f"git2 {__version__}"
    assert opened == []


def test_no_arguments_opens_the_tui(monkeypatch):
    opened = []
    monkeypatch.setattr(cli.Git2App, "run", lambda self: opened.append(self))
    result = runner.invoke(cli.app, [])
    assert result.exit_code == 0
    assert len(opened) == 1
