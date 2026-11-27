from textual.widgets import Footer, Header

from git2cli.tui.app import Git2App


async def test_app_starts_with_title_header_and_footer():
    app = Git2App()
    async with app.run_test() as pilot:
        await pilot.pause()
        assert app.title == "git2"
        assert app.query_one(Header)
        assert app.query_one(Footer)


async def test_ctrl_q_quits():
    app = Git2App()
    async with app.run_test() as pilot:
        await pilot.press("ctrl+q")
        await pilot.pause()
        assert not app.is_running
