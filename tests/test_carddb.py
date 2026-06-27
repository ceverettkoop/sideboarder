from sideboarder.carddb import CardDatabase, _distill


def fake_fetch(_progress):
    return ["Lightning Bolt", "lightning bolt", "Goblin Guide", " Fireblast ", "Llanowar Elves"]


def test_distill_dedupes_and_sorts():
    assert _distill(["b", "B", "a"]) == ["a", "b"]


def test_update_and_autocomplete(tmp_path):
    db = CardDatabase(tmp_path / "cardnames.json")
    count = db.update("mtgjson", "2026-01-01T00:00:00", fetch=fake_fetch)
    assert count == 4  # case-insensitive dedupe of "Lightning Bolt"
    assert db.autocomplete("light") == ["Lightning Bolt"]
    # substring match falls through after prefix matches
    assert "Goblin Guide" in db.autocomplete("gob")
    assert db.autocomplete("") == []


def test_persisted_db_reloads(tmp_path):
    path = tmp_path / "cardnames.json"
    CardDatabase(path).update("scryfall", "2026-01-01T00:00:00", fetch=fake_fetch)
    db2 = CardDatabase(path)
    assert db2.load() == 4
    assert db2.source == "scryfall"
    assert db2.available is True


def test_autocomplete_prefix_before_substring(tmp_path):
    db = CardDatabase(tmp_path / "c.json")
    db.update("mtgjson", "t", fetch=lambda _p: ["Bolt", "Lightning Bolt", "Boltwave"])
    result = db.autocomplete("bolt")
    assert result[:2] == ["Bolt", "Boltwave"]  # prefix matches first
    assert "Lightning Bolt" in result
