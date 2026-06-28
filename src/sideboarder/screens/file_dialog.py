"""A simple file-browser modal for opening documents."""

from __future__ import annotations

from pathlib import Path

from textual import on
from textual.app import ComposeResult
from textual.containers import Horizontal, Vertical
from textual.screen import ModalScreen
from textual.widgets import Button, DataTable, Label

from ..storage import FILE_SUFFIX


class FileDialog(ModalScreen[str | None]):
    """Browse directories and pick a file. Dismisses with the path or None."""

    BINDINGS = [("escape", "cancel", "Cancel")]

    def __init__(
        self,
        start_dir: str | Path,
        suffix: str = FILE_SUFFIX,
        title: str = "Open file",
    ) -> None:
        super().__init__()
        start = Path(start_dir).expanduser()
        if not start.is_dir():
            start = start.parent if start.parent.is_dir() else Path.home()
        self._dir = start.resolve()
        self._suffix = suffix
        self._title = title
        # Row index -> (path, is_dir) for the currently shown listing.
        self._rows: list[tuple[Path, bool]] = []

    def compose(self) -> ComposeResult:
        with Vertical(classes="dialog dialog-wide"):
            yield Label(self._title, classes="dialog-title")
            yield Label("", id="fd-path", classes="summary")
            yield DataTable(id="fd-table")
            yield Label(f"Showing folders and *{self._suffix} files.", classes="summary")
            with Horizontal(classes="dialog-buttons"):
                yield Button("Open", variant="primary", id="fd-open")
                yield Button("Cancel", id="fd-cancel")

    def on_mount(self) -> None:
        table = self.query_one("#fd-table", DataTable)
        table.cursor_type = "row"
        table.add_columns("Name")
        self._populate()
        table.focus()

    def _populate(self) -> None:
        self.query_one("#fd-path", Label).update(str(self._dir))
        table = self.query_one("#fd-table", DataTable)
        table.clear()
        self._rows.clear()

        if self._dir.parent != self._dir:
            self._rows.append((self._dir.parent, True))
            table.add_row("../")

        try:
            children = sorted(
                self._dir.iterdir(), key=lambda p: (not p.is_dir(), p.name.lower())
            )
        except OSError:
            children = []

        for child in children:
            try:
                is_dir = child.is_dir()
            except OSError:
                continue
            if is_dir:
                self._rows.append((child, True))
                table.add_row(f"{child.name}/")
            elif child.name.endswith(self._suffix):
                self._rows.append((child, False))
                table.add_row(child.name)

    def _activate_row(self, index: int) -> None:
        if not (0 <= index < len(self._rows)):
            return
        path, is_dir = self._rows[index]
        if is_dir:
            self._dir = path.resolve()
            self._populate()
            table = self.query_one("#fd-table", DataTable)
            table.move_cursor(row=0)
        else:
            self.dismiss(str(path))

    @on(DataTable.RowSelected, "#fd-table")
    def _row_selected(self, event: DataTable.RowSelected) -> None:
        self._activate_row(event.cursor_row)

    @on(Button.Pressed, "#fd-open")
    def _open_pressed(self) -> None:
        table = self.query_one("#fd-table", DataTable)
        self._activate_row(table.cursor_row)

    @on(Button.Pressed, "#fd-cancel")
    def action_cancel(self) -> None:
        self.dismiss(None)
