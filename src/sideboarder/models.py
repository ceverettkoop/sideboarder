"""Data model for Sideboarder.

A document = one deck (mainboard + sideboard) plus a list of opponent
archetypes. Each archetype holds a *base* sideboard plan and optional
*play* / *draw* override deltas. The effective plan for a given game is the
base combined with the relevant override, summing quantities per card.

Everything serializes to/from plain dicts (stdlib ``json``).
"""

from __future__ import annotations

import uuid
from dataclasses import dataclass, field

SCHEMA_VERSION = 1


@dataclass
class CardEntry:
    """A card name with a quantity."""

    name: str
    qty: int = 1

    def to_dict(self) -> dict:
        return {"name": self.name, "qty": self.qty}

    @classmethod
    def from_dict(cls, data: dict) -> CardEntry:
        return cls(name=str(data["name"]), qty=int(data.get("qty", 1)))


def _entries_to_list(entries: list[CardEntry]) -> list[dict]:
    return [e.to_dict() for e in entries]


def _entries_from_list(data: list[dict] | None) -> list[CardEntry]:
    return [CardEntry.from_dict(d) for d in (data or [])]


def merge_entries(*entry_lists: list[CardEntry]) -> list[CardEntry]:
    """Combine several entry lists, summing quantities per (case-insensitive) name.

    Order is preserved by first appearance. Entries with a non-positive total
    quantity are dropped.
    """
    order: list[str] = []
    totals: dict[str, int] = {}
    display: dict[str, str] = {}
    for entries in entry_lists:
        for entry in entries:
            key = entry.name.casefold()
            if key not in totals:
                order.append(key)
                totals[key] = 0
                display[key] = entry.name
            totals[key] += entry.qty
    return [CardEntry(name=display[k], qty=totals[k]) for k in order if totals[k] > 0]


@dataclass
class Plan:
    """A set of cards to take OUT and bring IN for a matchup."""

    out: list[CardEntry] = field(default_factory=list)
    in_: list[CardEntry] = field(default_factory=list)

    def to_dict(self) -> dict:
        return {"out": _entries_to_list(self.out), "in": _entries_to_list(self.in_)}

    @classmethod
    def from_dict(cls, data: dict | None) -> Plan:
        data = data or {}
        return cls(
            out=_entries_from_list(data.get("out")),
            in_=_entries_from_list(data.get("in")),
        )

    def is_empty(self) -> bool:
        return not self.out and not self.in_


def combine(base: Plan, override: Plan | None) -> Plan:
    """Return the effective plan: ``base`` summed with ``override`` per card."""
    if override is None or override.is_empty():
        return Plan(out=list(base.out), in_=list(base.in_))
    return Plan(
        out=merge_entries(base.out, override.out),
        in_=merge_entries(base.in_, override.in_),
    )


@dataclass
class Archetype:
    """An opponent deck and the plan(s) against it."""

    name: str
    id: str = field(default_factory=lambda: uuid.uuid4().hex)
    notes: str = ""
    base: Plan = field(default_factory=Plan)
    play_override: Plan | None = None
    draw_override: Plan | None = None

    def effective(self, on_play: bool) -> Plan:
        """The plan that actually applies, given play (True) or draw (False)."""
        override = self.play_override if on_play else self.draw_override
        return combine(self.base, override)

    def to_dict(self) -> dict:
        data = {
            "id": self.id,
            "name": self.name,
            "notes": self.notes,
            "base": self.base.to_dict(),
        }
        if self.play_override is not None:
            data["play_override"] = self.play_override.to_dict()
        if self.draw_override is not None:
            data["draw_override"] = self.draw_override.to_dict()
        return data

    @classmethod
    def from_dict(cls, data: dict) -> Archetype:
        return cls(
            id=str(data.get("id") or uuid.uuid4().hex),
            name=str(data["name"]),
            notes=str(data.get("notes", "")),
            base=Plan.from_dict(data.get("base")),
            play_override=(
                Plan.from_dict(data["play_override"]) if data.get("play_override") else None
            ),
            draw_override=(
                Plan.from_dict(data["draw_override"]) if data.get("draw_override") else None
            ),
        )


@dataclass
class Deck:
    """The player's deck: a mainboard and a sideboard."""

    name: str = "Untitled"
    format: str = ""
    mainboard: list[CardEntry] = field(default_factory=list)
    sideboard: list[CardEntry] = field(default_factory=list)

    def mainboard_count(self) -> int:
        return sum(e.qty for e in self.mainboard)

    def sideboard_count(self) -> int:
        return sum(e.qty for e in self.sideboard)

    def sideboard_names(self) -> set[str]:
        return {e.name.casefold() for e in self.sideboard}

    def to_dict(self) -> dict:
        return {
            "name": self.name,
            "format": self.format,
            "mainboard": _entries_to_list(self.mainboard),
            "sideboard": _entries_to_list(self.sideboard),
        }

    @classmethod
    def from_dict(cls, data: dict) -> Deck:
        return cls(
            name=str(data.get("name", "Untitled")),
            format=str(data.get("format", "")),
            mainboard=_entries_from_list(data.get("mainboard")),
            sideboard=_entries_from_list(data.get("sideboard")),
        )


@dataclass
class SideboardDocument:
    """Top-level document persisted to a ``*.sbd.json`` file."""

    deck: Deck = field(default_factory=Deck)
    archetypes: list[Archetype] = field(default_factory=list)
    schema_version: int = SCHEMA_VERSION

    def to_dict(self) -> dict:
        return {
            "schema_version": self.schema_version,
            "deck": self.deck.to_dict(),
            "archetypes": [a.to_dict() for a in self.archetypes],
        }

    @classmethod
    def from_dict(cls, data: dict) -> SideboardDocument:
        version = int(data.get("schema_version", SCHEMA_VERSION))
        if version > SCHEMA_VERSION:
            raise ValueError(
                f"Document schema_version {version} is newer than supported "
                f"{SCHEMA_VERSION}; upgrade Sideboarder."
            )
        return cls(
            deck=Deck.from_dict(data.get("deck", {})),
            archetypes=[Archetype.from_dict(a) for a in data.get("archetypes", [])],
            schema_version=version,
        )


@dataclass
class PlanValidation:
    """Result of checking a plan against the deck's sideboard."""

    out_total: int
    in_total: int
    illegal_in: list[str]  # IN names not present in the sideboard

    @property
    def balanced(self) -> bool:
        return self.out_total == self.in_total

    @property
    def ok(self) -> bool:
        return self.balanced and not self.illegal_in


def validate_plan(plan: Plan, deck: Deck) -> PlanValidation:
    """Check OUT/IN balance and that IN cards exist in the sideboard."""
    sb = deck.sideboard_names()
    illegal = [e.name for e in plan.in_ if e.name.casefold() not in sb]
    return PlanValidation(
        out_total=sum(e.qty for e in plan.out),
        in_total=sum(e.qty for e in plan.in_),
        illegal_in=illegal,
    )
