"""Aggregate tournament match results: record and winrate per archetype."""

from __future__ import annotations

import csv
import io
from dataclasses import dataclass

from .models import RESULT_DRAW, RESULT_LOSS, RESULT_WIN, MatchResult

OVERALL_LABEL = "(overall)"


@dataclass
class ArchetypeRecord:
    name: str
    wins: int = 0
    losses: int = 0
    draws: int = 0

    @property
    def matches(self) -> int:
        return self.wins + self.losses + self.draws

    @property
    def winrate(self) -> float | None:
        """Wins / decisive matches (draws excluded); None with no decisive matches."""
        decisive = self.wins + self.losses
        return self.wins / decisive if decisive else None

    @property
    def record_text(self) -> str:
        return f"{self.wins}-{self.losses}-{self.draws}"

    @property
    def winrate_text(self) -> str:
        rate = self.winrate
        return "—" if rate is None else f"{rate:.0%}"


def build_records(results: list[MatchResult]) -> list[ArchetypeRecord]:
    """Per-archetype records (case-insensitive grouping), sorted by matches desc, name."""
    table: dict[str, ArchetypeRecord] = {}
    for res in results:
        name = res.archetype.strip() or "(unknown)"
        key = name.casefold()
        if key not in table:
            table[key] = ArchetypeRecord(name=name)
        rec = table[key]
        if res.result == RESULT_WIN:
            rec.wins += 1
        elif res.result == RESULT_LOSS:
            rec.losses += 1
        elif res.result == RESULT_DRAW:
            rec.draws += 1
    return sorted(table.values(), key=lambda r: (-r.matches, r.name.casefold()))


def overall_record(results: list[MatchResult]) -> ArchetypeRecord:
    """One combined record across every match."""
    total = ArchetypeRecord(name=OVERALL_LABEL)
    for rec in build_records(results):
        total.wins += rec.wins
        total.losses += rec.losses
        total.draws += rec.draws
    return total


def results_to_csv(results: list[MatchResult]) -> str:
    """Render raw match results as CSV text."""
    buf = io.StringIO()
    writer = csv.writer(buf)
    writer.writerow(["date", "event", "archetype", "result", "deck_revision", "notes"])
    for r in results:
        rev = "" if r.deck_revision is None else r.deck_revision
        writer.writerow([r.date, r.event, r.archetype, r.result, rev, r.notes])
    return buf.getvalue()
