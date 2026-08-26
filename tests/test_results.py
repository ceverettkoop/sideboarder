import pytest

from sideboarder.models import (
    CardEntry,
    Deck,
    MatchResult,
    SideboardDocument,
    normalize_result,
)
from sideboarder.results import build_records, overall_record, results_to_csv


def _results():
    return [
        MatchResult(date="2026-08-01", event="FNM", archetype="Azorius Control", result="W"),
        MatchResult(date="2026-08-01", event="FNM", archetype="azorius control", result="L"),
        MatchResult(date="2026-08-08", event="RCQ", archetype="Azorius Control", result="W"),
        MatchResult(date="2026-08-08", event="RCQ", archetype="Burn", result="D"),
        MatchResult(date="2026-08-08", event="RCQ", archetype="", result="L"),
    ]


def test_normalize_result_accepts_words_and_case():
    assert normalize_result("w") == "W"
    assert normalize_result("Loss") == "L"
    assert normalize_result(" draw ") == "D"
    with pytest.raises(ValueError):
        normalize_result("2-1")


def test_match_result_round_trip():
    res = MatchResult(date="2026-08-01", event="FNM", archetype="Burn", result="L", notes="fast")
    assert MatchResult.from_dict(res.to_dict()) == res


def test_document_round_trip_with_results():
    doc = SideboardDocument(results=_results())
    assert SideboardDocument.from_dict(doc.to_dict()) == doc


def test_document_without_results_key_loads():
    doc = SideboardDocument.from_dict({"schema_version": 1, "deck": {}, "archetypes": []})
    assert doc.results == []


def test_build_records_groups_case_insensitively():
    records = build_records(_results())
    by_name = {r.name: r for r in records}
    azorius = by_name["Azorius Control"]
    assert (azorius.wins, azorius.losses, azorius.draws) == (2, 1, 0)
    assert azorius.winrate == pytest.approx(2 / 3)
    assert azorius.record_text == "2-1-0"
    assert by_name["Burn"].winrate is None  # draws only: no decisive matches
    assert by_name["Burn"].winrate_text == "—"
    assert "(unknown)" in by_name  # blank archetype gets a bucket
    assert records[0].name == "Azorius Control"  # sorted by matches desc


def test_overall_record():
    total = overall_record(_results())
    assert (total.wins, total.losses, total.draws) == (2, 2, 1)
    assert total.matches == 5
    assert total.winrate == pytest.approx(0.5)


def test_results_to_csv():
    results = _results()[:1]
    results[0].deck_revision = 2
    text = results_to_csv(results)
    lines = text.strip().splitlines()
    assert lines[0] == "date,event,archetype,result,deck_revision,notes"
    assert lines[1] == "2026-08-01,FNM,Azorius Control,W,2,"


def _doc_with_result() -> SideboardDocument:
    doc = SideboardDocument(
        deck=Deck(name="Burn", mainboard=[CardEntry("Lightning Bolt", 4)])
    )
    doc.results.append(
        MatchResult(archetype="Control", result="W", deck_revision=doc.deck_revision)
    )
    return doc


def test_deck_change_snapshots_but_does_not_bump_revision():
    doc = _doc_with_result()
    doc.note_deck_change()
    doc.deck.mainboard[0].qty = 3

    # A deck edit alone never advances the revision; it only freezes the old
    # list so the pinned result keeps resolving to what was actually played.
    assert doc.deck_revision == 1
    assert doc.deck_modified is True
    assert len(doc.revisions) == 1
    snap = doc.revisions[0]
    assert snap.revision == 1
    assert snap.deck.mainboard == [CardEntry("Lightning Bolt", 4)]  # frozen copy
    assert snap.saved_at  # stamped
    assert doc.deck_for_revision(doc.results[0].deck_revision) is snap.deck
    assert doc.deck_for_revision(None) is None
    assert doc.deck_for_revision(99) is None


def test_commit_finalizes_pending_deck_changes():
    doc = _doc_with_result()
    doc.note_deck_change()
    doc.deck.mainboard[0].qty = 3

    # Using the revised deck (a plan edit or a new result) commits revision 2.
    assert doc.commit_deck_revision() == 2
    assert doc.deck_modified is False
    assert doc.deck_for_revision(1) is doc.revisions[0].deck
    assert doc.deck_for_revision(2) is doc.deck
    # Committing again without pending changes is a no-op.
    assert doc.commit_deck_revision() == 2
    assert doc.deck_revision == 2


def test_many_deck_edits_become_one_revision():
    doc = _doc_with_result()
    for _ in range(3):  # a whole editing session between tournaments
        doc.note_deck_change()
    assert doc.deck_revision == 1
    assert len(doc.revisions) == 1
    assert doc.commit_deck_revision() == 2
    assert len(doc.revisions) == 1


def test_deck_change_without_results_keeps_revision():
    doc = SideboardDocument(deck=Deck(name="Burn"))
    doc.note_deck_change()
    assert doc.deck_revision == 1
    assert doc.deck_modified is False
    assert doc.revisions == []
    # Nothing pending: using the deck doesn't bump either.
    assert doc.commit_deck_revision() == 1


def test_edits_after_commit_without_new_results_stay_in_place():
    doc = _doc_with_result()
    doc.note_deck_change()
    doc.commit_deck_revision()
    # No results pinned to rev 2 yet: further edits morph rev 2 in place.
    doc.note_deck_change()
    assert doc.deck_revision == 2
    assert doc.deck_modified is False
    assert len(doc.revisions) == 1


def test_document_round_trip_with_pending_deck_changes():
    doc = _doc_with_result()
    doc.note_deck_change()
    doc.deck.mainboard[0].qty = 3
    restored = SideboardDocument.from_dict(doc.to_dict())
    assert restored == doc
    assert restored.deck_modified is True


def test_old_document_defaults_to_revision_one():
    doc = SideboardDocument.from_dict({"schema_version": 1, "deck": {}, "archetypes": []})
    assert doc.deck_revision == 1
    assert doc.deck_modified is False
    assert doc.revisions == []
