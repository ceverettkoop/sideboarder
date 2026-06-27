"""Parse plaintext decklists (Moxfield "MTGO" export and close relatives).

The canonical shape is::

    4 Lightning Bolt
    4 Goblin Guide
    <blank line>
    3 Smash to Smithereens
    2 Rest in Peace

i.e. ``<qty> <name>`` lines, the mainboard first, then a blank line, then the
sideboard. We also tolerate explicit section headers (``Deck``, ``Sideboard``,
``Commander`` …), ``SB:`` line prefixes, and trailing set/collector annotations
such as ``(2X2) 117`` or foil markers ``*F*``.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

from .models import CardEntry, Deck, merge_entries

# "4 Lightning Bolt", "4x Lightning Bolt", "4 Lightning Bolt (2X2) 117 *F*"
_CARD_RE = re.compile(r"^(?P<qty>\d+)\s*[xX]?\s+(?P<name>.+?)\s*$")
# trailing " (SET) 123" or " (SET)" annotations
_SET_ANNOT_RE = re.compile(r"\s+\([^)]+\)(?:\s+\S+)?\s*$")
# trailing foil/etched markers like "*F*"
_FOIL_RE = re.compile(r"\s+\*[A-Za-z]+\*\s*$")

_MAIN_HEADERS = {"deck", "maindeck", "main", "mainboard", "commander", "companion"}
_SIDE_HEADERS = {"sideboard", "side", "sb"}


@dataclass
class ParseResult:
    deck: Deck
    unparsed: list[str]


def _clean_name(name: str) -> str:
    prev = None
    while prev != name:
        prev = name
        name = _SET_ANNOT_RE.sub("", name)
        name = _FOIL_RE.sub("", name)
    return name.strip()


def _header(line: str) -> str | None:
    """Return 'main' or 'side' if the line is a section header, else None."""
    token = line.strip().rstrip(":").casefold()
    if token in _MAIN_HEADERS:
        return "main"
    if token in _SIDE_HEADERS:
        return "side"
    return None


def parse_decklist(text: str, deck_name: str = "Untitled") -> ParseResult:
    """Parse decklist ``text`` into a :class:`Deck` plus any unparsed lines."""
    main: list[CardEntry] = []
    side: list[CardEntry] = []
    unparsed: list[str] = []

    section = "main"
    seen_card_in_section = False
    explicit_section = False  # a header was given, so blank lines no longer split

    for raw in text.splitlines():
        line = raw.strip()
        if not line:
            # A blank line after mainboard cards implies the sideboard follows,
            # unless explicit headers are driving sectioning.
            if not explicit_section and section == "main" and seen_card_in_section:
                section = "side"
                seen_card_in_section = False
            continue

        header = _header(line)
        if header is not None:
            section = header
            explicit_section = True
            seen_card_in_section = False
            continue

        target_section = section
        if line[:3].casefold() == "sb:":
            target_section = "side"
            line = line[3:].strip()

        match = _CARD_RE.match(line)
        if not match:
            unparsed.append(raw)
            continue

        name = _clean_name(match.group("name"))
        if not name:
            unparsed.append(raw)
            continue
        entry = CardEntry(name=name, qty=int(match.group("qty")))
        (side if target_section == "side" else main).append(entry)
        if target_section == section:
            seen_card_in_section = True

    return ParseResult(
        deck=Deck(
            name=deck_name,
            mainboard=merge_entries(main),
            sideboard=merge_entries(side),
        ),
        unparsed=unparsed,
    )
