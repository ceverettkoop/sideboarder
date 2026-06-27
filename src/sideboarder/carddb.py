"""Local card-name database used for autocomplete.

A provider downloads a full card dataset once, from which we distill a compact,
sorted list of unique card names stored locally (``cardnames.json``). Autocomplete
then runs fully offline against that list. Updates are manual (triggered from the
Settings screen), never automatic.
"""

from __future__ import annotations

import json
import lzma
from collections.abc import Callable
from pathlib import Path

from .config import CARDNAMES_PATH

MTGJSON_ATOMIC_XZ = "https://mtgjson.com/api/v5/AtomicCards.json.xz"
SCRYFALL_BULK_INDEX = "https://api.scryfall.com/bulk-data"

# Source key -> human label
SOURCES = {"mtgjson": "MTGJSON", "scryfall": "Scryfall"}

ProgressFn = Callable[[str], None]


def _noop(_msg: str) -> None:
    pass


def fetch_mtgjson_names(progress: ProgressFn = _noop) -> list[str]:
    """Download MTGJSON AtomicCards and return its card names."""
    import httpx

    progress("Downloading MTGJSON AtomicCards…")
    with httpx.Client(timeout=120, follow_redirects=True) as client:
        resp = client.get(MTGJSON_ATOMIC_XZ)
        resp.raise_for_status()
        progress("Decompressing…")
        raw = lzma.decompress(resp.content)
    data = json.loads(raw)
    return list(data.get("data", {}).keys())


def fetch_scryfall_names(progress: ProgressFn = _noop) -> list[str]:
    """Download Scryfall's oracle-cards bulk file and return its card names."""
    import httpx

    progress("Locating Scryfall bulk data…")
    with httpx.Client(timeout=120, follow_redirects=True) as client:
        index = client.get(SCRYFALL_BULK_INDEX)
        index.raise_for_status()
        entries = index.json().get("data", [])
        oracle = next((e for e in entries if e.get("type") == "oracle_cards"), None)
        if oracle is None:
            raise RuntimeError("Scryfall bulk-data has no oracle_cards entry")
        progress("Downloading Scryfall oracle cards…")
        cards = client.get(oracle["download_uri"]).json()
    return [c["name"] for c in cards if "name" in c]


_FETCHERS: dict[str, Callable[[ProgressFn], list[str]]] = {
    "mtgjson": fetch_mtgjson_names,
    "scryfall": fetch_scryfall_names,
}


def _distill(names: list[str]) -> list[str]:
    """De-duplicate (case-insensitive) and sort card names."""
    seen: dict[str, str] = {}
    for name in names:
        name = name.strip()
        if name:
            seen.setdefault(name.casefold(), name)
    return sorted(seen.values(), key=str.casefold)


class CardDatabase:
    """In-memory card-name index backed by a local JSON file."""

    def __init__(self, path: str | Path = CARDNAMES_PATH) -> None:
        self.path = Path(path)
        self.source = ""
        self.updated = ""
        self._names: list[str] = []
        self._lower: list[str] = []

    @property
    def count(self) -> int:
        return len(self._names)

    @property
    def available(self) -> bool:
        return bool(self._names)

    def load(self) -> int:
        """Load names from disk if present. Returns the number loaded."""
        try:
            data = json.loads(self.path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            self._names, self._lower = [], []
            return 0
        self._set_names(data.get("names", []))
        self.source = data.get("source", "")
        self.updated = data.get("updated", "")
        return self.count

    def _set_names(self, names: list[str]) -> None:
        self._names = names
        self._lower = [n.casefold() for n in names]

    def save(self, source: str, updated: str) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.path.write_text(
            json.dumps({"source": source, "updated": updated, "names": self._names}),
            encoding="utf-8",
        )
        self.source, self.updated = source, updated

    def update(
        self,
        source: str,
        updated: str,
        progress: ProgressFn = _noop,
        fetch: Callable[[ProgressFn], list[str]] | None = None,
    ) -> int:
        """Fetch from ``source``, distill, persist, and reload. Returns count.

        ``updated`` is the ISO timestamp to stamp (passed in so the module stays
        free of nondeterministic clock calls). ``fetch`` overrides the provider
        (used in tests).
        """
        fetcher = fetch or _FETCHERS.get(source)
        if fetcher is None:
            raise ValueError(f"Unknown card source: {source!r}")
        names = _distill(fetcher(progress))
        progress(f"Indexing {len(names)} card names…")
        self._set_names(names)
        self.save(source, updated)
        return self.count

    def autocomplete(self, query: str, limit: int = 20) -> list[str]:
        """Return up to ``limit`` matches: prefix matches first, then substring."""
        q = query.strip().casefold()
        if not q:
            return []
        prefix: list[str] = []
        contains: list[str] = []
        for name, low in zip(self._names, self._lower, strict=False):
            if low.startswith(q):
                prefix.append(name)
            elif q in low:
                contains.append(name)
            if len(prefix) >= limit:
                break
        return (prefix + contains)[:limit]
