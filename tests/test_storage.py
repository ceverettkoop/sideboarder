import pytest

from sideboarder.models import Archetype, CardEntry, Deck, Plan, SideboardDocument
from sideboarder.storage import default_filename, load_document, save_document


def test_save_and_load_roundtrip(tmp_path):
    doc = SideboardDocument(
        deck=Deck(name="Burn", mainboard=[CardEntry("Lightning Bolt", 4)]),
        archetypes=[Archetype(name="Control", base=Plan(out=[CardEntry("Bolt", 1)]))],
    )
    path = tmp_path / "burn.sbd.json"
    save_document(doc, path)
    assert load_document(path) == doc


def test_load_rejects_future_schema(tmp_path):
    path = tmp_path / "future.sbd.json"
    path.write_text('{"schema_version": 999, "deck": {}, "archetypes": []}', encoding="utf-8")
    with pytest.raises(ValueError):
        load_document(path)


def test_default_filename_sanitizes():
    assert default_filename("Mono-Red Burn!") == "Mono-Red_Burn.sbd.json"
    assert default_filename("").endswith(".sbd.json")
