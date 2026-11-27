# git2 CLI and terminal UI

The `git2` command (WBS-14.1) and its full-screen terminal UI (WBS-14.2). Design: [`docs/design/tui-v0.md`](../docs/design/tui-v0.md).

Python 3.11+, managed with [uv](https://docs.astral.sh/uv/).

| Task | Command (in `cli/`) |
|---|---|
| Install for development | `uv sync` |
| Run | `uv run git2` (no arguments opens the terminal UI) |
| Test | `uv run pytest` · one test: `uv run pytest tests/tui/test_app.py::test_ctrl_q_quits` |
| Lint and format check | `uv run ruff check .` · `uv run ruff format --check .` |
| Install like a user | `pipx install ./cli` (from the repo root) |

All tests live in `tests/`, mirroring `src/git2cli/`.
