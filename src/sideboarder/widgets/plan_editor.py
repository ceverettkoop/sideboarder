"""The matchup plan editor: edit base / play / draw layers for an archetype."""

from __future__ import annotations

from textual import on
from textual.app import ComposeResult
from textual.containers import Horizontal, Vertical
from textual.widgets import Button, DataTable, Label, RadioButton, RadioSet

from ..models import Archetype, CardEntry, Plan, merge_entries, validate_plan
from ..screens.dialogs import CardEntryScreen

LAYER_BASE, LAYER_PLAY, LAYER_DRAW = "base", "play", "draw"
_LAYER_BY_INDEX = [LAYER_BASE, LAYER_PLAY, LAYER_DRAW]


class PlanEditor(Vertical):
    """Edits the plan for a single archetype. Call :meth:`load` to (re)bind."""

    BINDINGS = [
        ("delete", "remove", "Remove card"),
        ("backspace", "remove", "Remove card"),
        ("plus", "qty(1)", "Qty +"),
        ("equals_sign", "qty(1)", "Qty +"),
        ("minus", "qty(-1)", "Qty -"),
    ]

    def __init__(self) -> None:
        super().__init__()
        self._arch: Archetype | None = None
        self._layer = LAYER_BASE

    def compose(self) -> ComposeResult:
        yield Label("No matchup selected", id="matchup-title", classes="pane-title")
        with RadioSet(id="layer"):
            yield RadioButton("Base", value=True)
            yield RadioButton("On the play (+)")
            yield RadioButton("On the draw (+)")
        with Horizontal(classes="editor-head"):
            yield Label("OUT (mainboard)", classes="col-title")
            yield Button("Add OUT", id="add-out", classes="mini")
        yield DataTable(id="out-table")
        with Horizontal(classes="editor-head"):
            yield Label("IN (sideboard)", classes="col-title")
            yield Button("Add IN", id="add-in", classes="mini")
        yield DataTable(id="in-table")
        yield Label("", id="plan-summary", classes="summary")

    def on_mount(self) -> None:
        for tid in ("#out-table", "#in-table"):
            table = self.query_one(tid, DataTable)
            table.cursor_type = "row"
            table.add_columns("Qty", "Card")
        self._refresh_view()

    # ----- binding helpers -------------------------------------------------

    def load(self, arch: Archetype | None) -> None:
        self._arch = arch
        self._refresh_view()

    def refresh_summary(self) -> None:
        self._render_summary()

    def _current_plan(self, create: bool = False) -> Plan | None:
        """Return the plan object for the active layer (optionally creating it)."""
        if self._arch is None:
            return None
        if self._layer == LAYER_BASE:
            return self._arch.base
        attr = "play_override" if self._layer == LAYER_PLAY else "draw_override"
        plan = getattr(self._arch, attr)
        if plan is None and create:
            plan = Plan()
            setattr(self._arch, attr, plan)
        return plan

    def _focused_list(self) -> tuple[str, list[CardEntry]] | None:
        """Return ('out'|'in', entry list) for the focused table, or None."""
        plan = self._current_plan(create=True)
        if plan is None:
            return None
        focused = self.app.focused
        if focused is self.query_one("#out-table", DataTable):
            return "out", plan.out
        if focused is self.query_one("#in-table", DataTable):
            return "in", plan.in_
        return None

    # ----- rendering -------------------------------------------------------

    def _refresh_view(self) -> None:
        title = self.query_one("#matchup-title", Label)
        out_table = self.query_one("#out-table", DataTable)
        in_table = self.query_one("#in-table", DataTable)
        out_table.clear()
        in_table.clear()

        if self._arch is None:
            title.update("No matchup selected")
            self.query_one("#plan-summary", Label).update("")
            return

        title.update(f"MATCHUP: {self._arch.name}")
        plan = self._current_plan() or Plan()
        for entry in plan.out:
            out_table.add_row(str(entry.qty), entry.name, key=entry.name)
        for entry in plan.in_:
            in_table.add_row(str(entry.qty), entry.name, key=entry.name)
        self._render_summary()

    def _render_summary(self) -> None:
        if self._arch is None:
            return
        deck = self.app.document.deck
        parts = []
        for label, on_play in (("On play", True), ("On draw", False)):
            eff = self._arch.effective(on_play=on_play)
            v = validate_plan(eff, deck)
            line = f"{label}: OUT {v.out_total} / IN {v.in_total}"
            if not v.balanced:
                line += " ⚠ unbalanced"
            if v.illegal_in:
                line += f" ⚠ not in SB: {', '.join(v.illegal_in)}"
            parts.append(line)
        self.query_one("#plan-summary", Label).update("\n".join(parts))

    def _changed(self) -> None:
        self.app.mark_dirty()
        self._refresh_view()
        self.app.refresh_sidebars()

    # ----- events / actions ------------------------------------------------

    @on(RadioSet.Changed, "#layer")
    def _layer_changed(self, event: RadioSet.Changed) -> None:
        self._layer = _LAYER_BY_INDEX[event.index]
        self._refresh_view()

    @on(Button.Pressed, "#add-out")
    def _add_out(self) -> None:
        self._add_card("out")

    @on(Button.Pressed, "#add-in")
    def _add_in(self) -> None:
        self._add_card("in")

    def _add_card(self, which: str) -> None:
        if self._arch is None:
            return
        deck = self.app.document.deck
        if which == "out":
            candidates = [e.name for e in deck.mainboard]
            title = "Take OUT (from mainboard)"
        else:
            candidates = [e.name for e in deck.sideboard]
            title = "Bring IN (from sideboard)"

        def got(entry: CardEntry | None) -> None:
            if entry is None:
                return
            plan = self._current_plan(create=True)
            target = plan.out if which == "out" else plan.in_
            merged = merge_entries(target, [entry])
            target.clear()
            target.extend(merged)
            self._changed()

        self.app.push_screen(CardEntryScreen(title, candidates), got)

    def action_remove(self) -> None:
        found = self._focused_list()
        if found is None:
            return
        which, entries = found
        table = self.query_one(f"#{which}-table", DataTable)
        if table.row_count == 0:
            return
        try:
            row_key, _ = table.coordinate_to_cell_key(table.cursor_coordinate)
        except Exception:  # noqa: BLE001 - no selectable cell
            return
        name = row_key.value
        entries[:] = [e for e in entries if e.name != name]
        self._changed()

    def action_qty(self, delta: int) -> None:
        found = self._focused_list()
        if found is None:
            return
        which, entries = found
        table = self.query_one(f"#{which}-table", DataTable)
        if table.row_count == 0:
            return
        try:
            row_key, _ = table.coordinate_to_cell_key(table.cursor_coordinate)
        except Exception:  # noqa: BLE001
            return
        name = row_key.value
        for e in entries:
            if e.name == name:
                e.qty = max(1, e.qty + delta)
        self._changed()
