from sideboarder.models import (
    Archetype,
    CardEntry,
    Deck,
    Plan,
    SideboardDocument,
    combine,
    merge_entries,
    validate_plan,
)


def test_merge_entries_sums_by_name_case_insensitive():
    merged = merge_entries(
        [CardEntry("Lightning Bolt", 2), CardEntry("Bolt of Doom", 1)],
        [CardEntry("lightning bolt", 1)],
    )
    assert merged == [CardEntry("Lightning Bolt", 3), CardEntry("Bolt of Doom", 1)]


def test_merge_entries_drops_non_positive():
    merged = merge_entries([CardEntry("A", 2)], [CardEntry("A", -2), CardEntry("B", 0)])
    assert merged == []


def test_combine_no_override_returns_copy():
    base = Plan(out=[CardEntry("X", 1)], in_=[CardEntry("Y", 1)])
    result = combine(base, None)
    assert result == base
    assert result.out is not base.out  # copy, not alias


def test_combine_sums_override_deltas():
    base = Plan(out=[CardEntry("Searing Blaze", 2)], in_=[CardEntry("Smash", 2)])
    override = Plan(in_=[CardEntry("Roiling Vortex", 1), CardEntry("Smash", 1)])
    result = combine(base, override)
    assert result.out == [CardEntry("Searing Blaze", 2)]
    assert result.in_ == [CardEntry("Smash", 3), CardEntry("Roiling Vortex", 1)]


def test_archetype_effective_play_vs_draw():
    arch = Archetype(
        name="Control",
        base=Plan(out=[CardEntry("A", 1)], in_=[CardEntry("B", 1)]),
        play_override=Plan(in_=[CardEntry("C", 1)]),
    )
    assert len(arch.effective(on_play=True).in_) == 2
    assert len(arch.effective(on_play=False).in_) == 1


def test_document_roundtrip():
    doc = SideboardDocument(
        deck=Deck(
            name="Burn",
            format="Modern",
            mainboard=[CardEntry("Lightning Bolt", 4)],
            sideboard=[CardEntry("Smash", 3)],
        ),
        archetypes=[
            Archetype(
                name="Azorius",
                base=Plan(out=[CardEntry("Lightning Bolt", 1)], in_=[CardEntry("Smash", 1)]),
                play_override=Plan(in_=[CardEntry("Smash", 1)]),
            )
        ],
    )
    restored = SideboardDocument.from_dict(doc.to_dict())
    assert restored == doc


def test_optional_overrides_omitted_when_none():
    arch = Archetype(name="X")
    d = arch.to_dict()
    assert "play_override" not in d
    assert "draw_override" not in d


def test_validate_plan_balance_and_legality():
    deck = Deck(sideboard=[CardEntry("Smash", 3)])
    plan = Plan(out=[CardEntry("Bolt", 2)], in_=[CardEntry("Smash", 1), CardEntry("Rogue", 1)])
    result = validate_plan(plan, deck)
    assert result.out_total == 2
    assert result.in_total == 2
    assert result.balanced is True
    assert result.illegal_in == ["Rogue"]
    assert result.ok is False
