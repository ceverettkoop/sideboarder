"""The Sideboarder Textual application."""

from __future__ import annotations

from collections.abc import Callable
from pathlib import Path

from textual.app import App, ComposeResult
from textual.containers import Vertical
from textual.screen import ModalScreen
from textual.widgets import Button, Static

from .carddb import CardDatabase
from .config import AppSettings
from .models import Archetype, SideboardDocument
from .screens.dialogs import ConfirmScreen, PromptScreen, SaveChangesScreen
from .screens.file_dialog import FileDialog
from .screens.import_screen import ImportScreen
from .screens.main_screen import MainScreen
from .screens.report_screen import ReportScreen
from .screens.settings_screen import SettingsScreen
from .storage import default_filename, load_document, save_document
from .widgets.deck_pane import DeckPane
from .widgets.plan_editor import PlanEditor

HELP_TEXT = """\
[b]Sideboarder[/b] — MTG sideboard guide planner

[b]Global[/b]
  i  import / paste decklist      a  add archetype       x  remove archetype
  o  open file    ctrl+s  save    f  frequency report    ,  settings
  ?  help         ctrl+q  quit

[b]Archetypes pane[/b]  ↑/↓ select matchup
[b]Plan editor[/b]  choose Base / Play / Draw layer, Add OUT / Add IN,
   focus a list then: delete remove · + / - change qty
[b]Deck pane[/b]  focus a table then: e edit/replace · d delete · + / - qty

Effective plan = base combined with the play/draw override (qty summed per card).
"""


class HelpScreen(ModalScreen[None]):
    BINDINGS = [("escape", "close", "Close"), ("question_mark", "close", "Close")]

    def compose(self) -> ComposeResult:
        with Vertical(classes="dialog dialog-wide"):
            yield Static(HELP_TEXT)
            yield Button("Close", variant="primary", id="close")

    def on_button_pressed(self) -> None:
        self.dismiss(None)

    def action_close(self) -> None:
        self.dismiss(None)


class SideboarderApp(App):
    TITLE = "Sideboarder"

    CSS = """
    #body { height: 1fr; }
    #arch-pane { width: 30; border: round $panel; }
    #arch-list { height: 1fr; }
    PlanEditor { width: 1fr; border: round $panel; padding: 0 1; }
    DeckPane { width: 42; border: round $panel; padding: 0 1; }
    .pane-title { text-style: bold; color: $accent; }
    .col-title { text-style: bold; width: 1fr; }
    .editor-head { height: auto; }
    .summary { color: $text-muted; margin-top: 1; height: auto; }
    .mini { min-width: 12; height: auto; }
    DataTable { height: 1fr; }
    RadioSet { height: auto; }

    .dialog {
        width: 60; height: auto; padding: 1 2;
        border: thick $primary; background: $surface;
    }
    .dialog-wide { width: 90; height: 80%; }
    .dialog-title { text-style: bold; color: $accent; margin-bottom: 1; }
    .dialog-buttons { height: auto; margin-top: 1; align-horizontal: right; }
    .dialog-buttons Button { margin-left: 2; }
    #decklist-text { height: 1fr; }
    #freq-table { height: 1fr; }
    """

    BINDINGS = [
        ("i", "import", "Import"),
        ("a", "add_archetype", "Add arch"),
        ("x", "remove_archetype", "Del arch"),
        ("o", "open", "Open"),
        ("ctrl+s", "save", "Save"),
        ("f", "report", "Report"),
        ("comma", "settings", "Settings"),
        ("question_mark", "help", "Help"),
        ("ctrl+q", "request_quit", "Quit"),
    ]

    def __init__(self, initial_path: str | None = None) -> None:
        super().__init__()
        self.document = SideboardDocument()
        self.current_path: str | None = None
        self.dirty = False
        self.settings = AppSettings.load()
        self.carddb = CardDatabase()
        self._initial_path = initial_path
        self.main_screen = MainScreen()

    def on_mount(self) -> None:
        self.carddb.load()
        self.push_screen(self.main_screen)
        startup_path = self._initial_path or self.settings.last_file
        if startup_path and Path(startup_path).expanduser().is_file():
            # Defer until the main screen's widgets are mounted (_load_path
            # refreshes the archetype list / deck tables).
            self.call_after_refresh(self._load_path, startup_path)
        self.refresh_title()

    # ----- shared state helpers -------------------------------------------

    def mark_dirty(self) -> None:
        self.dirty = True
        self.refresh_title()

    def refresh_title(self) -> None:
        deck = self.document.deck
        fname = Path(self.current_path).name if self.current_path else "(unsaved)"
        flag = " ●" if self.dirty else ""
        self.sub_title = f"{deck.name or 'Untitled'} · {fname}{flag}"

    def refresh_sidebars(self) -> None:
        """Refresh deck pane + plan summary after a data change."""
        try:
            self.main_screen.query_one(DeckPane).refresh_deck()
            self.main_screen.query_one(PlanEditor).refresh_summary()
        except Exception:  # noqa: BLE001 - widgets not mounted yet
            pass
        self.refresh_title()

    # ----- actions ---------------------------------------------------------

    def action_import(self) -> None:
        def got(deck) -> None:
            if deck is None:
                return
            self.document.deck = deck
            self.mark_dirty()
            self.main_screen.refresh_deck()
            self.main_screen.query_one(PlanEditor).refresh_summary()
            self.notify(f"Imported {deck.mainboard_count()}+{deck.sideboard_count()} cards.")

        self.push_screen(ImportScreen(self.document.deck.name), got)

    def action_add_archetype(self) -> None:
        def got(name: str | None) -> None:
            if not name:
                return
            self.document.archetypes.append(Archetype(name=name))
            self.mark_dirty()
            self.main_screen.refresh_archetypes(select_index=len(self.document.archetypes) - 1)

        self.push_screen(
            PromptScreen("New archetype name:", placeholder="e.g. Azorius Control"), got
        )

    def action_remove_archetype(self) -> None:
        lv_index = self.main_screen.query_one("#arch-list").index
        if lv_index is None or not self.document.archetypes:
            return
        arch = self.document.archetypes[lv_index]

        def got(confirmed: bool) -> None:
            if not confirmed:
                return
            del self.document.archetypes[lv_index]
            self.mark_dirty()
            self.main_screen.refresh_archetypes()

        self.push_screen(ConfirmScreen(f"Remove archetype '{arch.name}'?"), got)

    def action_open(self) -> None:
        def got(path: str | None) -> None:
            if not path:
                return
            self._load_path(path)

        if self.current_path:
            start = str(Path(self.current_path).expanduser().parent)
        else:
            start = self.settings.default_save_dir or str(Path.cwd())
        self.push_screen(FileDialog(start, title="Open file"), got)

    def _load_path(self, path: str) -> None:
        try:
            self.document = load_document(path)
        except Exception as exc:  # noqa: BLE001 - surface bad file to user
            self.notify(f"Open failed: {exc}", severity="error")
            return
        self.current_path = str(Path(path).expanduser())
        self.dirty = False
        self._remember_last_file(self.current_path)
        self.main_screen.refresh_archetypes()
        self.main_screen.refresh_deck()
        self.refresh_title()
        self.notify(f"Opened {Path(path).name}")

    def _remember_last_file(self, path: str) -> None:
        if self.settings.last_file == path:
            return
        self.settings.last_file = path
        try:
            self.settings.save()
        except OSError:
            pass  # non-fatal: just won't auto-open next time

    def action_save(self) -> None:
        if self.current_path:
            self._write(self.current_path)
        else:
            self._save_as()

    def _save_as(self, after: Callable[[], None] | None = None) -> None:
        base = self.settings.default_save_dir or str(Path.cwd())
        default = str(Path(base) / default_filename(self.document.deck.name))

        def got(path: str | None) -> None:
            if not path:
                return  # save-as cancelled: stay put, run no callback
            self._write(path)
            if after is not None and not self.dirty:
                after()

        self.push_screen(PromptScreen("Save as:", value=default), got)

    def _save_then(self, after: Callable[[], None]) -> None:
        """Save (prompting for a path if needed); run ``after`` only on success."""
        if self.current_path:
            self._write(self.current_path)
            if not self.dirty:  # _write clears dirty on success
                after()
        else:
            self._save_as(after)

    def _write(self, path: str) -> None:
        try:
            saved = save_document(self.document, Path(path).expanduser())
        except Exception as exc:  # noqa: BLE001
            self.notify(f"Save failed: {exc}", severity="error")
            return
        self.current_path = str(saved)
        self.dirty = False
        self._remember_last_file(self.current_path)
        self.refresh_title()
        self.notify(f"Saved {saved.name}")

    def action_report(self) -> None:
        self.push_screen(ReportScreen(self.document.archetypes))

    def action_settings(self) -> None:
        self.push_screen(SettingsScreen())

    def action_help(self) -> None:
        self.push_screen(HelpScreen())

    def action_request_quit(self) -> None:
        if not self.dirty:
            self.exit()
            return

        def got(choice: str) -> None:
            if choice == "save":
                self._save_then(self.exit)
            elif choice == "discard":
                self.exit()
            # "cancel" -> stay in the app

        self.push_screen(SaveChangesScreen("Save changes before quitting?"), got)
