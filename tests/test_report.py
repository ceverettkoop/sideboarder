from sideboarder.models import Archetype, CardEntry, Plan
from sideboarder.report import (
    MODE_BASE,
    MODE_PLAY,
    build_frequency,
    to_csv,
)


def make_archetypes():
    return [
        Archetype(
            name="Control",
            base=Plan(out=[CardEntry("Bolt", 2)], in_=[CardEntry("Smash", 2)]),
            play_override=Plan(in_=[CardEntry("Vortex", 1)]),
        ),
        Archetype(
            name="Aggro",
            base=Plan(out=[CardEntry("Bolt", 1)], in_=[CardEntry("Wrath", 1)]),
        ),
    ]


def test_frequency_counts_base():
    rows = {r.name: r for r in build_frequency(make_archetypes(), MODE_BASE)}
    assert rows["Bolt"].out_count == 2
    assert rows["Bolt"].out_qty == 3
    assert rows["Smash"].in_count == 1
    assert "Vortex" not in rows  # override excluded in base mode


def test_frequency_includes_override_in_play_mode():
    rows = {r.name: r for r in build_frequency(make_archetypes(), MODE_PLAY)}
    assert rows["Vortex"].in_count == 1


def test_frequency_sorted_by_involvement():
    rows = build_frequency(make_archetypes(), MODE_BASE)
    assert rows[0].name == "Bolt"  # appears in both matchups


def test_to_csv_header_and_rows():
    csv_text = to_csv(build_frequency(make_archetypes(), MODE_BASE))
    lines = csv_text.strip().splitlines()
    assert lines[0] == "card,out_count,in_count,out_qty,in_qty"
    assert any(line.startswith("Bolt,") for line in lines)
