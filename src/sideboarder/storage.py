"""Load and save Sideboarder documents (``*.sbd.json``)."""

from __future__ import annotations

import json
from pathlib import Path

from .models import SideboardDocument

FILE_SUFFIX = ".sbd.json"


def load_document(path: str | Path) -> SideboardDocument:
    """Read and parse a document file."""
    data = json.loads(Path(path).read_text(encoding="utf-8"))
    return SideboardDocument.from_dict(data)


def save_document(doc: SideboardDocument, path: str | Path) -> Path:
    """Write a document to ``path`` (creating parent dirs). Returns the path."""
    p = Path(path)
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(json.dumps(doc.to_dict(), indent=2), encoding="utf-8")
    return p


def default_filename(deck_name: str) -> str:
    """A filesystem-friendly default filename for a deck."""
    safe = "".join(c if c.isalnum() or c in " -_" else "" for c in deck_name).strip()
    safe = safe.replace(" ", "_") or "deck"
    return f"{safe}{FILE_SUFFIX}"
