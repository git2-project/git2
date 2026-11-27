"""The ``git2`` command.

With no subcommand it opens the terminal UI (UC-27 step 5). The Typer
subcommands (clone, status, commit, mr ...) are WBS-14.1 and land here.
"""

from typing import Annotated

import typer

from git2cli import __version__
from git2cli.tui.app import Git2App

app = typer.Typer(no_args_is_help=False, add_completion=False)


def _print_version(value: bool) -> None:
    if value:
        typer.echo(f"git2 {__version__}")
        raise typer.Exit()


@app.callback(invoke_without_command=True)
def root(
    ctx: typer.Context,
    version: Annotated[
        bool,
        typer.Option(
            "--version", callback=_print_version, is_eager=True, help="Print the version and exit."
        ),
    ] = False,
) -> None:
    """git2 on the command line. Run it with no arguments for the terminal UI."""
    if ctx.invoked_subcommand is None:
        Git2App().run()


def main() -> None:
    app()
