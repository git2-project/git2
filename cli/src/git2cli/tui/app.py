"""The terminal UI's App. Screens (sign-in, changes, branches, merge
requests) are added by the next TUI items (git2#44 to git2#49)."""

from textual.app import App, ComposeResult
from textual.widgets import Footer, Header


class Git2App(App[None]):
    """git2's full-screen terminal UI. ``ctrl+q`` quits (Textual's default)."""

    TITLE = "git2"

    def compose(self) -> ComposeResult:
        yield Header()
        yield Footer()
