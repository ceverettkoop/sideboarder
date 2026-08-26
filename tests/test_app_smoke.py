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
            MatchResult(date="2026-08-01", event="FNM", archetype="Burn", result="W")
        )
        await pilot.press("t")
        await pilot.pause()
        assert isinstance(app.screen, ResultsScreen)
        screen = app.screen
        table = screen.query_one("#results-table", DataTable)
        assert table.row_count == 1

        # New rows show up and stats aggregate.
        app.document.results.append(
            MatchResult(date="2026-08-02", event="FNM", archetype="Burn", result="L")
        )
        screen.refresh_results()
        await pilot.pause()
        assert table.row_count == 2
        assert "1-1-0" in str(screen.query_one("#overall-line", Label).render())

        # The entry form mounts (new and edit modes).
        for modal in (
            ResultEntryScreen(["Burn"]),
            ResultEntryScreen(["Burn"], existing=app.document.results[0]),
        ):
            app.push_screen(modal)
            await pilot.pause()
            app.pop_screen()
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
