import pytest

from sideboarder.app import SideboarderApp
from sideboarder.models import CardEntry
from sideboarder.screens.main_screen import MainScreen
from sideboarder.widgets.deck_pane import DeckPane
from sideboarder.widgets.plan_editor import PlanEditor

pytestmark = pytest.mark.asyncio

SAMPLE = "4 Lightning Bolt\n4 Goblin Guide\n\n3 Smash to Smithereens\n2 Rest in Peace\n"


async def test_app_boots_and_shows_panes():
    app = SideboarderApp()
    async with app.run_test() as pilot:
        assert isinstance(app.screen, MainScreen)
        app.screen.query_one(PlanEditor)
        app.screen.query_one(DeckPane)
        await pilot.pause()


async def test_import_and_archetype_flow():
    app = SideboarderApp()
    async with app.run_test() as pilot:
        # Simulate an import by setting the deck directly through the parser path.
        from sideboarder.decklist import parse_decklist

        app.document.deck = parse_decklist(SAMPLE, "Burn").deck
        app.mark_dirty()
        app.main_screen.refresh_deck()
        await pilot.pause()
        assert app.document.deck.mainboard_count() == 8
        assert app.dirty is True

        # Add an archetype and a base OUT/IN entry.
        from sideboarder.models import Archetype

        app.document.archetypes.append(Archetype(name="Azorius Control"))
        app.main_screen.refresh_archetypes(select_index=0)
        await pilot.pause()
        editor = app.screen.query_one(PlanEditor)
        editor._arch.base.out.append(CardEntry("Lightning Bolt", 2))
        editor._arch.base.in_.append(CardEntry("Smash to Smithereens", 2))
        editor.refresh_summary()
        await pilot.pause()

        # Round-trip through the document serializer.
        from sideboarder.models import SideboardDocument

        restored = SideboardDocument.from_dict(app.document.to_dict())
        assert restored == app.document


async def test_modal_screens_mount():
    from sideboarder.app import HelpScreen
    from sideboarder.models import Archetype, Plan
    from sideboarder.screens.dialogs import CardEntryScreen
    from sideboarder.screens.import_screen import ImportScreen
    from sideboarder.screens.report_screen import ReportScreen
    from sideboarder.screens.settings_screen import SettingsScreen

    app = SideboarderApp()
    async with app.run_test(size=(120, 40)) as pilot:
        archs = [Archetype(name="X", base=Plan(out=[CardEntry("A", 1)], in_=[CardEntry("B", 1)]))]
        for screen in (
            ReportScreen(archs),
            SettingsScreen(),
            ImportScreen("Deck"),
            HelpScreen(),
            CardEntryScreen("Pick", ["Lightning Bolt", "Goblin Guide"]),
            CardEntryScreen("Pick", lambda t: app.carddb.autocomplete(t)),
        ):
            app.push_screen(screen)
            await pilot.pause()
            app.pop_screen()
            await pilot.pause()


async def test_results_view_toggle_and_entry():
    from textual.widgets import DataTable, Label

    from sideboarder.models import MatchResult
    from sideboarder.screens.results_screen import ResultEntryScreen, ResultsScreen

    app = SideboarderApp()
    async with app.run_test(size=(120, 40)) as pilot:
        app.document.results.append(
            MatchResult(
                date="2026-08-01",
                event="FNM",
                archetype="Burn",
                games_won=2,
                games_lost=1,
                play_draw="play",
            )
        )
        await pilot.press("t")
        await pilot.pause()
        assert isinstance(app.screen, ResultsScreen)
        screen = app.screen
        table = screen.query_one("#results-table", DataTable)
        assert table.row_count == 1
        # Cells are Rich Text tinted by outcome (2-1 is a win).
        win_row = table.get_row_at(0)
        assert [str(cell) for cell in win_row[3:5]] == ["Play", "2-1"]
        assert str(win_row[4].style) == screen._result_style("W")

        # New rows show up and stats aggregate.
        app.document.results.append(
            MatchResult(date="2026-08-02", event="FNM", archetype="Burn", games_won=0, games_lost=2)
        )
        screen.refresh_results()
        await pilot.pause()
        assert table.row_count == 2
        assert str(table.get_row_at(1)[4].style) == screen._result_style("L")
        assert "1-1-0" in str(screen.query_one("#overall-line", Label).render())

        # The entry form mounts (new and edit modes), as does the revision viewer.
        from sideboarder.screens.results_screen import RevisionViewScreen

        for modal in (
            ResultEntryScreen(["Burn"]),
            ResultEntryScreen(["Burn"], existing=app.document.results[0]),
            RevisionViewScreen("Deck revision 1", app.document.deck),
        ):
            app.push_screen(modal)
            await pilot.pause()
            app.pop_screen()
            await pilot.pause()

        # A deck edit after logging a result freezes the old list, but the
        # revision only advances once the revised deck is used (result / plan).
        app.document.results[0].deck_revision = app.document.deck_revision
        app.document.note_deck_change()
        assert app.document.deck_revision == 1
        assert app.document.deck_modified is True
        assert app.document.revisions[0].revision == 1
        assert app.document.commit_deck_revision() == 2
        screen.refresh_results()
        await pilot.pause()

        # Toggle back to the sideboarding view.
        await pilot.press("t")
        await pilot.pause()
        assert isinstance(app.screen, MainScreen)


async def test_report_mode_toggle():
    from sideboarder.models import Archetype, Plan
    from sideboarder.report import MODE_DRAW
    from sideboarder.screens.report_screen import ReportScreen

    archs = [
        Archetype(
            name="X",
            base=Plan(out=[CardEntry("A", 1)]),
            draw_override=Plan(in_=[CardEntry("Extra", 1)]),
        )
    ]
    app = SideboarderApp()
    async with app.run_test(size=(120, 40)) as pilot:
        screen = ReportScreen(archs)
        app.push_screen(screen)
        await pilot.pause()
        screen._mode = MODE_DRAW
        screen._refresh()
        await pilot.pause()
        from textual.widgets import DataTable

        table = screen.query_one("#freq-table", DataTable)
        assert table.row_count >= 2  # A (out) and Extra (in, from draw override)


async def test_import_screen_terminal_paste():
    """Bracketed paste from the terminal (the SSH-friendly path) lands in the TextArea."""
    from textual.events import Paste

    from sideboarder.screens.import_screen import DecklistArea, ImportScreen

    app = SideboarderApp()
    async with app.run_test(size=(120, 40)) as pilot:
        app.push_screen(ImportScreen("Deck"))
        await pilot.pause()
        area = app.screen.query_one("#decklist-text", DecklistArea)
        assert area.has_focus
        area.post_message(Paste("4 Lightning Bolt\n\n2 Pyroblast"))
        await pilot.pause()
        assert "Lightning Bolt" in area.text
        assert "Pyroblast" in area.text


async def test_import_screen_ctrl_v_reads_system_clipboard(monkeypatch):
    """Ctrl+V falls back to the system clipboard when the app clipboard is empty."""
    import sideboarder.screens.import_screen as import_screen

    monkeypatch.setattr(import_screen, "read_clipboard", lambda: "3 Counterspell")
    app = SideboarderApp()
    async with app.run_test(size=(120, 40)) as pilot:
        app.push_screen(import_screen.ImportScreen("Deck"))
        await pilot.pause()
        await pilot.press("ctrl+v")
        await pilot.pause()
        area = app.screen.query_one("#decklist-text", import_screen.DecklistArea)
        assert "Counterspell" in area.text


async def test_import_screen_ctrl_v_warns_when_clipboard_unreadable(monkeypatch):
    """Over SSH the system clipboard is unreadable; Ctrl+V should warn, not crash."""
    import sideboarder.screens.import_screen as import_screen

    monkeypatch.setattr(import_screen, "read_clipboard", lambda: None)
    app = SideboarderApp()
    async with app.run_test(size=(120, 40)) as pilot:
        app.push_screen(import_screen.ImportScreen("Deck"))
        await pilot.pause()
        await pilot.press("ctrl+v")
        await pilot.pause()
        area = app.screen.query_one("#decklist-text", import_screen.DecklistArea)
        assert area.text == ""
