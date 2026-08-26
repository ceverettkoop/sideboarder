import pytest

from sideboarder.models import MatchResult, SideboardDocument, normalize_result
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
    text = results_to_csv(_results()[:1])
    lines = text.strip().splitlines()
    assert lines[0] == "date,event,archetype,result,notes"
    assert lines[1] == "2026-08-01,FNM,Azorius Control,W,"
