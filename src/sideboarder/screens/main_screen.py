"""The main three-pane screen: archetypes · plan editor · deck."""

from __future__ import annotations

from textual import on
from textual.app import ComposeResult
from textual.containers import Horizontal, Vertical
from textual.screen import Screen
from textual.widgets import Button, Footer, Header, Label, ListItem, ListView

from ..models import Archetype
from ..widgets.deck_pane import DeckPane
from ..widgets.plan_editor import PlanEditor


class MainScreen(Screen):
    """Top-level workspace."""

    def compose(self) -> ComposeResult:
        yield Header()
        with Horizontal(id="body"):
            with Vertical(id="arch-pane"):
                yield Label("ARCHETYPES", classes="pane-title")
                yield ListView(id="arch-list")
                yield Button("Add archetype", id="add-arch", classes="mini")
            yield PlanEditor()
            yield DeckPane()
        yield Footer()

    def on_mount(self) -> None:
        self.refresh_archetypes()

    # ----- refreshers ------------------------------------------------------

    def refresh_archetypes(self, select_index: int | None = None) -> None:
        lv = self.query_one("#arch-list", ListView)
        prev = lv.index if select_index is None else select_index
        lv.clear()
        for arch in self.app.document.archetypes:
            lv.append(ListItem(Label(arch.name)))
        if self.app.document.archetypes:
            target = 0 if prev is None else min(prev, len(self.app.document.archetypes) - 1)
            lv.index = target
            self._load_selected(target)
        else:
            self.query_one(PlanEditor).load(None)

    def refresh_deck(self) -> None:
        self.query_one(DeckPane).refresh_deck()

    def _load_selected(self, index: int | None) -> None:
        archs = self.app.document.archetypes
        valid = index is not None and 0 <= index < len(archs)
        arch: Archetype | None = archs[index] if valid else None
        self.query_one(PlanEditor).load(arch)

    # ----- events ----------------------------------------------------------

    @on(ListView.Highlighted, "#arch-list")
    def _arch_highlighted(self, event: ListView.Highlighted) -> None:
        self._load_selected(event.list_view.index)

    @on(Button.Pressed, "#add-arch")
    def _add_arch(self) -> None:
        self.app.action_add_archetype()
