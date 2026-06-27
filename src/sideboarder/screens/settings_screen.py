"""Settings modal: card-database source, manual update, default save dir."""

from __future__ import annotations

from datetime import UTC, datetime

from textual import on, work
from textual.app import ComposeResult
from textual.containers import Horizontal, Vertical
from textual.screen import ModalScreen
from textual.widgets import Button, Input, Label, RadioButton, RadioSet

from ..carddb import SOURCES

_SOURCE_BY_INDEX = ["mtgjson", "scryfall"]


class SettingsScreen(ModalScreen[None]):
    """Edit settings and trigger a manual card-database update."""

    BINDINGS = [("escape", "close", "Close")]

    def compose(self) -> ComposeResult:
        with Vertical(classes="dialog"):
            yield Label("Settings", classes="dialog-title")
            yield Label("Card database source")
            with RadioSet(id="source"):
                yield RadioButton("MTGJSON", value=self.app.settings.card_source == "mtgjson")
                yield RadioButton("Scryfall", value=self.app.settings.card_source == "scryfall")
            yield Label("", id="db-status")
            yield Button("Update card database now", id="update")
            yield Label("Default save directory")
            yield Input(value=self.app.settings.default_save_dir, id="save-dir")
            with Horizontal(classes="dialog-buttons"):
                yield Button("Save & close", variant="primary", id="close")

    def on_mount(self) -> None:
        self._refresh_status()

    def _refresh_status(self) -> None:
        db = self.app.carddb
        if db.available:
            src = SOURCES.get(db.source, db.source or "?")
            status = f"Loaded {db.count} card names (source: {src}, updated: {db.updated or '?'})."
        else:
            status = "No card database yet — update to enable name autocomplete."
        self.query_one("#db-status", Label).update(status)

    @on(RadioSet.Changed, "#source")
    def _source_changed(self, event: RadioSet.Changed) -> None:
        self.app.settings.card_source = _SOURCE_BY_INDEX[event.index]

    @on(Button.Pressed, "#update")
    def _update_pressed(self) -> None:
        self.query_one("#db-status", Label).update("Updating… this may take a minute.")
        self.query_one("#update", Button).disabled = True
        self._do_update(self.app.settings.card_source)

    @work(thread=True, exclusive=True)
    def _do_update(self, source: str) -> None:
        stamp = datetime.now(UTC).isoformat(timespec="seconds")
        try:
            count = self.app.carddb.update(source, stamp)
        except Exception as exc:  # noqa: BLE001 - surface any network/parse failure
            self.app.call_from_thread(self._update_failed, str(exc))
            return
        self.app.call_from_thread(self._update_done, count)

    def _update_done(self, count: int) -> None:
        self.app.settings.cardnames_updated = self.app.carddb.updated
        self.app.settings.cardnames_count = count
        self.app.settings.save()
        self.query_one("#update", Button).disabled = False
        self._refresh_status()
        self.notify(f"Card database updated: {count} names.")

    def _update_failed(self, message: str) -> None:
        self.query_one("#update", Button).disabled = False
        self._refresh_status()
        self.notify(f"Update failed: {message}", severity="error")

    @on(Button.Pressed, "#close")
    def action_close(self) -> None:
        self.app.settings.default_save_dir = self.query_one("#save-dir", Input).value.strip()
        self.app.settings.save()
        self.dismiss(None)
