"""Paste-a-decklist modal with a live parse preview."""

from __future__ import annotations

from textual import on
from textual.app import ComposeResult
from textual.containers import Horizontal, Vertical
from textual.screen import ModalScreen
from textual.widgets import Button, Input, Label, TextArea

from ..clipboard import read_clipboard
from ..decklist import parse_decklist
from ..models import Deck

SAMPLE_HINT = "Paste a Moxfield 'MTGO' export (qty name lines; blank line before sideboard)."
PASTE_HINT = "Paste: terminal paste (Cmd+V / Ctrl+Shift+V, works over SSH), Ctrl+V, or the button."
SSH_PASTE_WARNING = (
    "Can't read the system clipboard (common over SSH). "
    "Use your terminal's own paste instead: Cmd+V (macOS) or Ctrl+Shift+V (Linux/Windows)."
)


class DecklistArea(TextArea):
    """TextArea whose ctrl+v falls back to the *system* clipboard.

    Textual's built-in ctrl+v pastes from the app-internal clipboard, which is
    empty unless something was copied inside the app — so for users it looks
    like paste is broken. Terminal-level paste (bracketed paste) still arrives
    as a normal Paste event and is handled by TextArea itself.
    """

    def action_paste(self) -> None:
        if self.app.clipboard:
            super().action_paste()
            return
        text = read_clipboard()
        if text:
            if result := self._replace_via_keyboard(text, *self.selection):
                self.move_cursor(result.end_location)
        else:
            self.notify(SSH_PASTE_WARNING, severity="warning")


class ImportScreen(ModalScreen[Deck | None]):
    """Collect pasted decklist text and return a parsed Deck."""

    BINDINGS = [
        ("escape", "cancel", "Cancel"),
        ("ctrl+shift+v", "paste_clipboard", "Paste clipboard"),
    ]

    def __init__(self, deck_name: str = "Untitled") -> None:
        super().__init__()
        self._deck_name = deck_name

    def compose(self) -> ComposeResult:
        with Vertical(classes="dialog dialog-wide"):
            yield Label("Import decklist", classes="dialog-title")
            yield Label("Deck name")
            yield Input(value=self._deck_name, id="deck-name")
            yield Label(SAMPLE_HINT)
            yield Label(PASTE_HINT, classes="dialog-hint")
            yield DecklistArea(id="decklist-text")
            yield Label("", id="preview")
            with Horizontal(classes="dialog-buttons"):
                yield Button("Paste clipboard", id="paste")
                yield Button("Import", variant="primary", id="import")
                yield Button("Cancel", id="cancel")

    def on_mount(self) -> None:
        self.query_one("#decklist-text", TextArea).focus()
        self._update_preview()

    @on(TextArea.Changed, "#decklist-text")
    def _update_preview(self) -> None:
        text = self.query_one("#decklist-text", TextArea).text
        result = parse_decklist(text, self.query_one("#deck-name", Input).value or "Untitled")
        deck = result.deck
        msg = (
            f"Mainboard: {deck.mainboard_count()} cards ({len(deck.mainboard)} unique)   "
            f"Sideboard: {deck.sideboard_count()} cards ({len(deck.sideboard)} unique)"
        )
        if result.unparsed:
            msg += f"\n⚠ {len(result.unparsed)} unparsed line(s): " + "; ".join(result.unparsed[:3])
        self.query_one("#preview", Label).update(msg)

    @on(Button.Pressed, "#paste")
    def action_paste_clipboard(self) -> None:
        text = read_clipboard()
        if not text:
            self.app.bell()
            self.notify(SSH_PASTE_WARNING, severity="warning")
            return
        area = self.query_one("#decklist-text", TextArea)
        area.replace(text, area.selection.start, area.selection.end)
        area.focus()
        self._update_preview()

    @on(Button.Pressed, "#import")
    def _do_import(self) -> None:
        text = self.query_one("#decklist-text", TextArea).text
        name = self.query_one("#deck-name", Input).value or "Untitled"
        deck = parse_decklist(text, name).deck
        if not deck.mainboard and not deck.sideboard:
            self.app.bell()
            return
        self.dismiss(deck)

    @on(Button.Pressed, "#cancel")
    def action_cancel(self) -> None:
        self.dismiss(None)
