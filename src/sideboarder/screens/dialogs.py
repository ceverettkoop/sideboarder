"""Small reusable modal dialogs."""

from __future__ import annotations

from collections.abc import Callable, Sequence

from textual import on
from textual.app import ComposeResult
from textual.containers import Horizontal, Vertical
from textual.screen import ModalScreen
from textual.widgets import Button, Input, Label
from textual_autocomplete import AutoComplete, DropdownItem, TargetState

from ..models import CardEntry

CandidateSource = Sequence[str] | Callable[[str], list[str]]


class PromptScreen(ModalScreen[str | None]):
    """Ask for a single line of text. Dismisses with the text or None."""

    BINDINGS = [("escape", "cancel", "Cancel")]

    def __init__(self, title: str, value: str = "", placeholder: str = "") -> None:
        super().__init__()
        self._title = title
        self._value = value
        self._placeholder = placeholder

    def compose(self) -> ComposeResult:
        with Vertical(classes="dialog"):
            yield Label(self._title, classes="dialog-title")
            yield Input(value=self._value, placeholder=self._placeholder, id="prompt-input")
            with Horizontal(classes="dialog-buttons"):
                yield Button("OK", variant="primary", id="ok")
                yield Button("Cancel", id="cancel")

    def on_mount(self) -> None:
        self.query_one("#prompt-input", Input).focus()

    @on(Input.Submitted, "#prompt-input")
    @on(Button.Pressed, "#ok")
    def _accept(self) -> None:
        text = self.query_one("#prompt-input", Input).value.strip()
        self.dismiss(text or None)

    @on(Button.Pressed, "#cancel")
    def action_cancel(self) -> None:
        self.dismiss(None)


class ConfirmScreen(ModalScreen[bool]):
    """A yes/no confirmation. Dismisses with True/False."""

    BINDINGS = [("escape", "no", "No")]

    def __init__(self, question: str, yes_label: str = "Yes", no_label: str = "No") -> None:
        super().__init__()
        self._question = question
        self._yes = yes_label
        self._no = no_label

    def compose(self) -> ComposeResult:
        with Vertical(classes="dialog"):
            yield Label(self._question, classes="dialog-title")
            with Horizontal(classes="dialog-buttons"):
                yield Button(self._yes, variant="primary", id="yes")
                yield Button(self._no, id="no")

    @on(Button.Pressed, "#yes")
    def _yes_pressed(self) -> None:
        self.dismiss(True)

    @on(Button.Pressed, "#no")
    def action_no(self) -> None:
        self.dismiss(False)


class SaveChangesScreen(ModalScreen[str]):
    """Prompt on exit with unsaved changes. Dismisses 'save' / 'discard' / 'cancel'."""

    BINDINGS = [("escape", "cancel", "Cancel")]

    def __init__(self, message: str = "You have unsaved changes.") -> None:
        super().__init__()
        self._message = message

    def compose(self) -> ComposeResult:
        with Vertical(classes="dialog"):
            yield Label(self._message, classes="dialog-title")
            with Horizontal(classes="dialog-buttons"):
                yield Button("Save", variant="primary", id="save")
                yield Button("Discard", variant="error", id="discard")
                yield Button("Cancel", id="cancel")

    @on(Button.Pressed, "#save")
    def _save(self) -> None:
        self.dismiss("save")

    @on(Button.Pressed, "#discard")
    def _discard(self) -> None:
        self.dismiss("discard")

    @on(Button.Pressed, "#cancel")
    def action_cancel(self) -> None:
        self.dismiss("cancel")


class CardEntryScreen(ModalScreen[CardEntry | None]):
    """Pick a card name (with autocomplete) and a quantity."""

    BINDINGS = [("escape", "cancel", "Cancel")]

    def __init__(
        self,
        title: str,
        candidates: CandidateSource,
        name: str = "",
        qty: int = 1,
    ) -> None:
        super().__init__()
        self._title = title
        self._candidates = candidates
        self._name = name
        self._qty = qty

    def compose(self) -> ComposeResult:
        with Vertical(classes="dialog"):
            yield Label(self._title, classes="dialog-title")
            yield Label("Card name")
            yield Input(value=self._name, placeholder="Start typing…", id="card-name")
            yield Label("Quantity")
            yield Input(value=str(self._qty), id="card-qty", type="integer")
            with Horizontal(classes="dialog-buttons"):
                yield Button("OK", variant="primary", id="ok")
                yield Button("Cancel", id="cancel")

    def on_mount(self) -> None:
        name_input = self.query_one("#card-name", Input)
        candidates = self._candidates
        if callable(candidates):
            provider = candidates

            def factory(state: TargetState) -> list[DropdownItem]:
                return [DropdownItem(n) for n in provider(state.text)]

            ac_candidates: object = factory
        else:
            ac_candidates = list(candidates)
        self.mount(AutoComplete(name_input, candidates=ac_candidates))
        name_input.focus()

    @on(Button.Pressed, "#ok")
    @on(Input.Submitted, "#card-qty")
    def _accept(self) -> None:
        name = self.query_one("#card-name", Input).value.strip()
        if not name:
            self.query_one("#card-name", Input).focus()
            return
        try:
            qty = int(self.query_one("#card-qty", Input).value or "1")
        except ValueError:
            qty = 1
        qty = max(1, qty)
        self.dismiss(CardEntry(name=name, qty=qty))

    @on(Button.Pressed, "#cancel")
    def action_cancel(self) -> None:
        self.dismiss(None)
