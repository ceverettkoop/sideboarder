"""Tournament results view: a match log spreadsheet plus per-archetype stats."""

from __future__ import annotations

import datetime
from pathlib import Path

from rich.text import Text
from textual import on
from textual.app import ComposeResult
from textual.containers import Horizontal, Vertical
from textual.coordinate import Coordinate
from textual.screen import ModalScreen, Screen
from textual.widgets import (
    Button,
    DataTable,
    Footer,
    Header,
    Input,
    Label,
    RadioButton,
    RadioSet,
)
from textual_autocomplete import AutoComplete

from ..models import (
    DRAW,
    PLAY,
    RESULT_DRAW,
    RESULT_LOSS,
    RESULT_WIN,
    Deck,
    MatchResult,
    parse_game_score,
    parse_play_draw,
)
from ..results import build_records, overall_record, results_to_csv
from .dialogs import ConfirmScreen, PromptScreen

_COLUMNS = ("Date", "Event", "Opponent", "P/D", "Games", "Rev", "Notes")
_FIELDS = ("date", "event", "archetype", "play_draw", "games", "deck_revision", "notes")
_PLAY_DRAW_BY_INDEX = [PLAY, DRAW, ""]

# Rows are tinted by match outcome; theme colours with plain-terminal fallbacks.
_RESULT_THEME_COLOR = {RESULT_WIN: "success", RESULT_LOSS: "error", RESULT_DRAW: "warning"}
_RESULT_FALLBACK_COLOR = {RESULT_WIN: "green", RESULT_LOSS: "red", RESULT_DRAW: "yellow"}


def _deck_text(deck: Deck) -> str:
    """Plain-text decklist: mainboard, blank line, sideboard."""
    lines = [f"{e.qty} {e.name}" for e in deck.mainboard]
    lines.append("")
    lines += [f"{e.qty} {e.name}" for e in deck.sideboard]
    return "\n".join(lines)


class RevisionViewScreen(ModalScreen[None]):
    """Read-only view of the decklist a result was played with."""

    BINDINGS = [("escape", "close", "Close")]

    def __init__(self, title: str, deck: Deck) -> None:
        super().__init__()
        self._title = title
        self._deck = deck

    def compose(self) -> ComposeResult:
        from textual.containers import VerticalScroll
        from textual.widgets import Static

        deck = self._deck
        with Vertical(classes="dialog dialog-wide"):
            yield Label(self._title, classes="dialog-title")
            yield Label(
                f"{deck.name or 'Untitled'} — "
                f"{deck.mainboard_count()} main / {deck.sideboard_count()} side"
            )
            with VerticalScroll(id="revision-list"):
                yield Static(_deck_text(deck))
            with Horizontal(classes="dialog-buttons"):
                yield Button("Close", variant="primary", id="close")

    @on(Button.Pressed, "#close")
    def action_close(self) -> None:
        self.dismiss(None)


class ResultEntryScreen(ModalScreen[MatchResult | None]):
    """Enter (or edit) one match result in a form."""

    BINDINGS = [("escape", "cancel", "Cancel")]

    def __init__(
        self,
        archetype_candidates: list[str],
        existing: MatchResult | None = None,
    ) -> None:
        super().__init__()
        self._candidates = archetype_candidates
        self._existing = existing

    def compose(self) -> ComposeResult:
        res = self._existing or MatchResult(date=datetime.date.today().isoformat())
        with Vertical(classes="dialog"):
            yield Label(
                "Edit match result" if self._existing else "New match result",
                classes="dialog-title",
            )
            yield Label("Date")
            yield Input(value=res.date, placeholder="YYYY-MM-DD", id="res-date")
            yield Label("Event")
            yield Input(value=res.event, placeholder="e.g. FNM, RCQ…", id="res-event")
            yield Label("Opponent archetype")
            yield Input(value=res.archetype, placeholder="e.g. Azorius Control", id="res-arch")
            yield Label("Play / draw")
            with RadioSet(id="res-play-draw"):
                yield RadioButton("On the play", value=res.play_draw == PLAY)
                yield RadioButton("On the draw", value=res.play_draw == DRAW)
                yield RadioButton("Unknown", value=not res.play_draw)
            yield Label("Games won-lost")
            yield Input(value=res.games_text, placeholder="e.g. 2-1", id="res-games")
            yield Label("Notes")
            yield Input(value=res.notes, id="res-notes")
            with Horizontal(classes="dialog-buttons"):
                yield Button("OK", variant="primary", id="ok")
                yield Button("Cancel", id="cancel")

    def on_mount(self) -> None:
        arch_input = self.query_one("#res-arch", Input)
        self.mount(AutoComplete(arch_input, candidates=self._candidates))
        self.query_one("#res-date", Input).focus()

    @on(Button.Pressed, "#ok")
    @on(Input.Submitted, "#res-notes")
    def _accept(self) -> None:
        radio = self.query_one("#res-play-draw", RadioSet)
        play_draw = _PLAY_DRAW_BY_INDEX[radio.pressed_index if radio.pressed_index >= 0 else 2]
        try:
            games_won, games_lost = parse_game_score(self.query_one("#res-games", Input).value)
        except ValueError as exc:
            self.notify(str(exc), severity="error")
            self.query_one("#res-games", Input).focus()
            return
        base = self._existing or MatchResult()
        self.dismiss(
            MatchResult(
                id=base.id,
                deck_revision=base.deck_revision,
                date=self.query_one("#res-date", Input).value.strip(),
                event=self.query_one("#res-event", Input).value.strip(),
                archetype=self.query_one("#res-arch", Input).value.strip(),
                play_draw=play_draw,
                games_won=games_won,
                games_lost=games_lost,
                notes=self.query_one("#res-notes", Input).value.strip(),
            )
        )

    @on(Button.Pressed, "#cancel")
    def action_cancel(self) -> None:
        self.dismiss(None)


class ResultsScreen(Screen):
    """Match log with direct cell editing, plus winrate-per-archetype stats."""

    BINDINGS = [
        ("n", "new_result", "New result"),
        ("e", "edit_row", "Edit row"),
        ("d", "delete_row", "Del row"),
        ("v", "view_revision", "View deck"),
        ("escape", "back", "Sideboarding"),
    ]

    def compose(self) -> ComposeResult:
        yield Header()
        with Horizontal(id="results-body"):
            with Vertical(id="results-pane"):
                with Horizontal(classes="editor-head"):
                    yield Label("MATCH RESULTS", classes="pane-title col-title")
                    yield Button("New result", id="new-result", classes="mini")
                yield DataTable(id="results-table")
                yield Label(
                    "enter edit cell · e edit row · n new · d delete · v view rev deck",
                    classes="summary",
                )
                yield Label(id="results-legend", classes="summary")
            with Vertical(id="stats-pane"):
                yield Label("BY ARCHETYPE", classes="pane-title")
                yield DataTable(id="stats-table")
                yield Label("", id="overall-line", classes="summary")
                yield Button("Export CSV", id="export-results", classes="mini")
        yield Footer()

    def on_mount(self) -> None:
        table = self.query_one("#results-table", DataTable)
        table.cursor_type = "cell"
        table.add_columns(*_COLUMNS)
        stats = self.query_one("#stats-table", DataTable)
        stats.cursor_type = "row"
        stats.add_columns("Archetype", "W", "L", "D", "Games", "Winrate")
        self.app.theme_changed_signal.subscribe(self, self._on_theme_changed)
        self._refresh_legend()
        self.refresh_results()

    def _refresh_legend(self) -> None:
        legend = Text("rows: ")
        for result, label in ((RESULT_WIN, "win"), (RESULT_LOSS, "loss"), (RESULT_DRAW, "draw")):
            if len(legend) > len("rows: "):
                legend.append(" · ")
            legend.append(label, style=self._result_style(result))
        self.query_one("#results-legend", Label).update(legend)

    def _on_theme_changed(self, _theme: object) -> None:
        """Re-tint rows when the app theme changes (colours come from the theme)."""
        self._refresh_legend()
        self.refresh_results()

    # ----- data helpers ----------------------------------------------------

    @property
    def _results(self) -> list[MatchResult]:
        return self.app.document.results

    def _archetype_candidates(self) -> list[str]:
        names: list[str] = []
        seen: set[str] = set()
        pools = [a.name for a in self.app.document.archetypes]
        pools += [r.archetype for r in self._results]
        for name in pools:
            key = name.strip().casefold()
            if name.strip() and key not in seen:
                seen.add(key)
                names.append(name.strip())
        return names

    def _result_style(self, result: str) -> str:
        theme = self.app.current_theme
        color = getattr(theme, _RESULT_THEME_COLOR[result], None)
        return color or _RESULT_FALLBACK_COLOR[result]

    def refresh_results(self) -> None:
        table = self.query_one("#results-table", DataTable)
        prev = table.cursor_coordinate
        table.clear()
        for res in self._results:
            rev = "—" if res.deck_revision is None else str(res.deck_revision)
            style = self._result_style(res.result)
            cells = (
                res.date,
                res.event,
                res.archetype,
                res.play_draw_text,
                res.games_text,
                rev,
                res.notes,
            )
            table.add_row(*(Text(text, style=style) for text in cells), key=res.id)
        if table.row_count:
            table.cursor_coordinate = Coordinate(min(prev.row, table.row_count - 1), prev.column)
        self._refresh_stats()

    def _refresh_stats(self) -> None:
        stats = self.query_one("#stats-table", DataTable)
        stats.clear()
        records = build_records(self._results)
        if not records:
            stats.add_row("(no matches yet)", "", "", "", "", "")
        for rec in records:
            stats.add_row(
                rec.name,
                str(rec.wins),
                str(rec.losses),
                str(rec.draws),
                rec.games_text,
                rec.winrate_text,
            )
        total = overall_record(self._results)
        self.query_one("#overall-line", Label).update(
            f"Overall: {total.record_text} · games {total.games_text}"
            f" · winrate {total.winrate_text} ({total.matches} matches)"
        )

    def _selected(self) -> MatchResult | None:
        table = self.query_one("#results-table", DataTable)
        if table.row_count == 0:
            return None
        try:
            row_key, _ = table.coordinate_to_cell_key(table.cursor_coordinate)
        except Exception:  # noqa: BLE001 - empty table / cursor out of range
            return None
        return next((r for r in self._results if r.id == row_key.value), None)

    def _changed(self) -> None:
        self.app.mark_dirty()
        self.refresh_results()

    # ----- actions ---------------------------------------------------------

    @on(Button.Pressed, "#new-result")
    def action_new_result(self) -> None:
        def got(res: MatchResult | None) -> None:
            if res is None:
                return
            # Recording a result with a revised deck finalizes it as a new revision.
            res.deck_revision = self.app.document.commit_deck_revision()
            self._results.append(res)
            self._changed()

        self.app.push_screen(ResultEntryScreen(self._archetype_candidates()), got)

    def action_edit_row(self) -> None:
        current = self._selected()
        if current is None:
            return

        def got(res: MatchResult | None) -> None:
            if res is None:
                return
            self._results[:] = [res if r.id == res.id else r for r in self._results]
            self._changed()

        self.app.push_screen(
            ResultEntryScreen(self._archetype_candidates(), existing=current), got
        )

    def action_delete_row(self) -> None:
        current = self._selected()
        if current is None:
            return

        def got(confirmed: bool) -> None:
            if not confirmed:
                return
            self._results[:] = [r for r in self._results if r.id != current.id]
            self._changed()

        label = f"{current.date} vs {current.archetype or '?'} ({current.games_text})"
        self.app.push_screen(ConfirmScreen(f"Delete result '{label}'?"), got)

    def action_view_revision(self) -> None:
        """Show the decklist as it was when the selected match was played."""
        current = self._selected()
        if current is None:
            return
        deck = self.app.document.deck_for_revision(current.deck_revision)
        if deck is None:
            self.notify("No deck revision recorded for this result.", severity="warning")
            return
        title = f"Deck revision {current.deck_revision} — {current.date} vs {current.archetype}"
        self.app.push_screen(RevisionViewScreen(title, deck))

    def action_back(self) -> None:
        self.app.action_toggle_results()

    # ----- direct cell editing (spreadsheet-style) -------------------------

    @on(DataTable.CellSelected, "#results-table")
    def _cell_selected(self, event: DataTable.CellSelected) -> None:
        current = self._selected()
        if current is None:
            return
        field = _FIELDS[event.coordinate.column]
        column_title = _COLUMNS[event.coordinate.column]
        if field == "deck_revision":
            # The revision is pinned when the result is recorded; enter shows
            # the decklist it refers to instead of editing.
            self.action_view_revision()
            return

        def got(text: str | None) -> None:
            if text is None:
                return
            value = text.strip()
            try:
                if field == "games":
                    current.games_won, current.games_lost = parse_game_score(value)
                elif field == "play_draw":
                    current.play_draw = parse_play_draw(value)
                else:
                    setattr(current, field, value)
            except ValueError as exc:
                self.notify(str(exc), severity="error")
                return
            self._changed()

        hints = {"games": " (e.g. 2-1)", "play_draw": " (play/draw)"}
        current_text = current.games_text if field == "games" else getattr(current, field)
        self.app.push_screen(
            PromptScreen(f"{column_title}{hints.get(field, '')}:", value=current_text), got
        )

    @on(Button.Pressed, "#export-results")
    def _export(self) -> None:
        def write_csv(path: str | None) -> None:
            if not path:
                return
            Path(path).expanduser().write_text(results_to_csv(self._results), encoding="utf-8")
            self.notify(f"Exported to {path}")

        self.app.push_screen(PromptScreen("Export CSV to path:", value="results.csv"), write_csv)
