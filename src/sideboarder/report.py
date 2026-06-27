"""Aggregate how often each card is boarded in / out across matchups."""

from __future__ import annotations

import csv
import io
from dataclasses import dataclass

from .models import Archetype, Plan

# Counting modes for the frequency report.
MODE_BASE = "base"  # count each archetype's base plan once
MODE_PLAY = "play"  # count effective plan on the play
MODE_DRAW = "draw"  # count effective plan on the draw


@dataclass
class CardFrequency:
    name: str
    out_count: int = 0  # number of matchups this card goes OUT in
    in_count: int = 0  # number of matchups this card comes IN in
    out_qty: int = 0  # total copies removed across matchups
    in_qty: int = 0  # total copies added across matchups


def _plan_for(arch: Archetype, mode: str) -> Plan:
    if mode == MODE_PLAY:
        return arch.effective(on_play=True)
    if mode == MODE_DRAW:
        return arch.effective(on_play=False)
    return arch.base


def build_frequency(archetypes: list[Archetype], mode: str = MODE_BASE) -> list[CardFrequency]:
    """Return per-card frequencies, sorted by total involvement (desc), then name."""
    table: dict[str, CardFrequency] = {}

    def row(name: str) -> CardFrequency:
        key = name.casefold()
        if key not in table:
            table[key] = CardFrequency(name=name)
        return table[key]

    for arch in archetypes:
        plan = _plan_for(arch, mode)
        for entry in plan.out:
            r = row(entry.name)
            r.out_count += 1
            r.out_qty += entry.qty
        for entry in plan.in_:
            r = row(entry.name)
            r.in_count += 1
            r.in_qty += entry.qty

    return sorted(
        table.values(),
        key=lambda r: (-(r.out_count + r.in_count), r.name.casefold()),
    )


def to_csv(rows: list[CardFrequency]) -> str:
    """Render frequency rows as CSV text."""
    buf = io.StringIO()
    writer = csv.writer(buf)
    writer.writerow(["card", "out_count", "in_count", "out_qty", "in_qty"])
    for r in rows:
        writer.writerow([r.name, r.out_count, r.in_count, r.out_qty, r.in_qty])
    return buf.getvalue()
