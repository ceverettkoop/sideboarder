"""Frequency report modal: how often each card is boarded in / out."""

from __future__ import annotations

from pathlib import Path

from textual import on
from textual.app import ComposeResult
from textual.containers import Horizontal, Vertical
from textual.screen import ModalScreen
from textual.widgets import Button, DataTable, Label, RadioButton, RadioSet

from ..models import Archetype
from ..report import MODE_BASE, MODE_DRAW, MODE_PLAY, build_frequency, to_csv
from .dialogs import PromptScreen

_MODE_BY_INDEX = [MODE_BASE, MODE_PLAY, MODE_DRAW]


class ReportScreen(ModalScreen[None]):
    """Show per-card OUT/IN frequencies with a counting-mode toggle."""

    BINDINGS = [("escape", "close", "Close")]

    def __init__(self, archetypes: list[Archetype]) -> None:
        super().__init__()
        self._archetypes = archetypes
        self._mode = MODE_BASE

    def compose(self) -> ComposeResult:
        with Vertical(classes="dialog dialog-wide"):
            yield Label("Frequency report", classes="dialog-title")
            with RadioSet(id="mode"):
                yield RadioButton("Base plans", value=True)
                yield RadioButton("Effective (on the play)")
                yield RadioButton("Effective (on the draw)")
            yield DataTable(id="freq-table")
            with Horizontal(classes="dialog-buttons"):
                yield Button("Export CSV", id="export")
                yield Button("Close", variant="primary", id="close")

    def on_mount(self) -> None:
        table = self.query_one("#freq-table", DataTable)
        table.cursor_type = "row"
        table.add_columns("Card", "OUT (matchups)", "IN (matchups)", "OUT qty", "IN qty")
        self._refresh()

    def _refresh(self) -> None:
        table = self.query_one("#freq-table", DataTable)
        table.clear()
        rows = build_frequency(self._archetypes, self._mode)
        if not rows:
            table.add_row("(no plans yet)", "", "", "", "")
            return
        for r in rows:
            table.add_row(r.name, str(r.out_count), str(r.in_count), str(r.out_qty), str(r.in_qty))

    @on(RadioSet.Changed, "#mode")
    def _mode_changed(self, event: RadioSet.Changed) -> None:
        self._mode = _MODE_BY_INDEX[event.index]
        self._refresh()

    @on(Button.Pressed, "#export")
    def _export(self) -> None:
        def write_csv(path: str | None) -> None:
            if not path:
                return
            csv_text = to_csv(build_frequency(self._archetypes, self._mode))
            Path(path).expanduser().write_text(csv_text, encoding="utf-8")
            self.notify(f"Exported to {path}")

        self.app.push_screen(
            PromptScreen("Export CSV to path:", value="frequency.csv"), write_csv
        )

    @on(Button.Pressed, "#close")
    def action_close(self) -> None:
        self.dismiss(None)
