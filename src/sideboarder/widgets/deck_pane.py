"""The deck pane: view and edit the mainboard / sideboard in-app."""

from __future__ import annotations

from textual import on
from textual.app import ComposeResult
from textual.containers import Horizontal, Vertical
from textual.widgets import Button, DataTable, Label

from ..models import CardEntry, merge_entries
from ..screens.dialogs import CardEntryScreen


class DeckPane(Vertical):
    """Shows mainboard + sideboard tables and supports edit / replace / qty."""

    BINDINGS = [
        ("e", "edit", "Edit/replace"),
        ("d", "delete", "Delete"),
        ("plus", "qty(1)", "Qty +"),
        ("equals_sign", "qty(1)", "Qty +"),
        ("minus", "qty(-1)", "Qty -"),
    ]

    def compose(self) -> ComposeResult:
        yield Label("DECK", classes="pane-title")
        with Horizontal(classes="editor-head"):
            yield Label("Mainboard", id="main-label", classes="col-title")
            yield Button("Add", id="add-main", classes="mini")
        yield DataTable(id="main-table")
        with Horizontal(classes="editor-head"):
            yield Label("Sideboard", id="side-label", classes="col-title")
            yield Button("Add", id="add-side", classes="mini")
        yield DataTable(id="side-table")

    def on_mount(self) -> None:
        for tid in ("#main-table", "#side-table"):
            table = self.query_one(tid, DataTable)
            table.cursor_type = "row"
            table.add_columns("Qty", "Card")
        self.refresh_deck()

    def refresh_deck(self) -> None:
        deck = self.app.document.deck
        self.query_one("#main-label", Label).update(f"Mainboard ({deck.mainboard_count()})")
        self.query_one("#side-label", Label).update(f"Sideboard ({deck.sideboard_count()})")
        # Show quantity still available for the selected matchup: deck total minus
        # what the active plan layer has already pulled (OUT for the mainboard,
        # IN for the sideboard). Underlying deck data is untouched.
        out_alloc, in_alloc = self._plan_allocations()
        for tid, entries, alloc in (
            ("#main-table", deck.mainboard, out_alloc),
            ("#side-table", deck.sideboard, in_alloc),
        ):
            table = self.query_one(tid, DataTable)
            table.clear()
            for e in entries:
                remaining = max(0, e.qty - alloc.get(e.name.casefold(), 0))
                table.add_row(str(remaining), e.name, key=e.name)

    def _plan_allocations(self) -> tuple[dict[str, int], dict[str, int]]:
        """(OUT, IN) quantities reserved by the active matchup plan, by name."""
        # Imported lazily: plan_editor imports this module at load time.
        from .plan_editor import PlanEditor

        try:
            return self.screen.query_one(PlanEditor).active_allocations()
        except Exception:  # noqa: BLE001 - plan editor not mounted / no screen
            return {}, {}

    def _board_for_focus(self) -> tuple[str, list[CardEntry]] | None:
        deck = self.app.document.deck
        focused = self.app.focused
        if focused is self.query_one("#main-table", DataTable):
            return "main", deck.mainboard
        if focused is self.query_one("#side-table", DataTable):
            return "side", deck.sideboard
        return None

    def _selected_name(self, which: str) -> str | None:
        table = self.query_one(f"#{which}-table", DataTable)
        if table.row_count == 0:
            return None
        try:
            row_key, _ = table.coordinate_to_cell_key(table.cursor_coordinate)
        except Exception:  # noqa: BLE001
            return None
        return row_key.value

    def highlighted_name(self, which: str) -> str | None:
        """Card under the cursor in the "main"/"side" table (focus-independent)."""
        return self._selected_name(which)

    def _candidates(self):
        return lambda text: self.app.carddb.autocomplete(text)

    def _changed(self) -> None:
        self.app.mark_dirty()
        self.refresh_deck()
        self.app.refresh_sidebars()

    @on(Button.Pressed, "#add-main")
    def _add_main(self) -> None:
        self._add("main")

    @on(Button.Pressed, "#add-side")
    def _add_side(self) -> None:
        self._add("side")

    def _add(self, which: str) -> None:
        deck = self.app.document.deck
        entries = deck.mainboard if which == "main" else deck.sideboard

        def got(entry: CardEntry | None) -> None:
            if entry is None:
                return
            entries[:] = merge_entries(entries, [entry])
            self._changed()

        self.app.push_screen(CardEntryScreen(f"Add to {which}board", self._candidates()), got)

    def action_edit(self) -> None:
        found = self._board_for_focus()
        if found is None:
            return
        which, entries = found
        name = self._selected_name(which)
        if name is None:
            return
        current = next((e for e in entries if e.name == name), None)
        if current is None:
            return

        def got(entry: CardEntry | None) -> None:
            if entry is None:
                return
            # Replace the edited entry, then merge in case the new name collides.
            rebuilt: list[CardEntry] = []
            for e in entries:
                if e.name == name:
                    rebuilt.append(entry)
                else:
                    rebuilt.append(e)
            entries[:] = merge_entries(rebuilt)
            self._changed()

        self.app.push_screen(
            CardEntryScreen("Edit / replace card", self._candidates(), current.name, current.qty),
            got,
        )

    def action_delete(self) -> None:
        found = self._board_for_focus()
        if found is None:
            return
        which, entries = found
        name = self._selected_name(which)
        if name is None:
            return
        entries[:] = [e for e in entries if e.name != name]
        self._changed()

    def action_qty(self, delta: int) -> None:
        found = self._board_for_focus()
        if found is None:
            return
        which, entries = found
        name = self._selected_name(which)
        if name is None:
            return
        for e in entries:
            if e.name == name:
                e.qty = max(1, e.qty + delta)
        self._changed()
