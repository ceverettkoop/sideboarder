from sideboarder.decklist import parse_decklist
from sideboarder.models import CardEntry

MOXFIELD_MTGO = """\
4 Lightning Bolt
4 Goblin Guide
4 Monastery Swiftspear

3 Smash to Smithereens
2 Rest in Peace
"""

WITH_HEADERS = """\
Deck
4 Lightning Bolt
2 Fireblast

Sideboard
3 Smash to Smithereens
"""

WITH_ANNOTATIONS = """\
4 Lightning Bolt (2X2) 117 *F*
1 Fable of the Mirror-Breaker // Reflection of Kiki-Rikki (NEO) 141
"""


def test_blank_line_splits_main_and_side():
    result = parse_decklist(MOXFIELD_MTGO)
    assert result.deck.mainboard_count() == 12
    assert result.deck.sideboard_count() == 5
    assert CardEntry("Lightning Bolt", 4) in result.deck.mainboard
    assert CardEntry("Rest in Peace", 2) in result.deck.sideboard
    assert result.unparsed == []


def test_explicit_headers():
    result = parse_decklist(WITH_HEADERS)
    assert result.deck.mainboard_count() == 6
    assert result.deck.sideboard_count() == 3


def test_strips_set_and_foil_annotations():
    result = parse_decklist(WITH_ANNOTATIONS)
    names = [e.name for e in result.deck.mainboard]
    assert "Lightning Bolt" in names
    assert "Fable of the Mirror-Breaker // Reflection of Kiki-Rikki" in names


def test_sb_prefix_routes_to_sideboard():
    result = parse_decklist("4 Lightning Bolt\nSB: 2 Smash to Smithereens\n")
    assert result.deck.mainboard_count() == 4
    assert result.deck.sideboard_count() == 2


def test_duplicate_lines_merge():
    result = parse_decklist("2 Lightning Bolt\n2 Lightning Bolt\n")
    assert result.deck.mainboard == [CardEntry("Lightning Bolt", 4)]


def test_x_quantity_and_unparsed_lines():
    result = parse_decklist("4x Lightning Bolt\nnot a card line\n")
    assert result.deck.mainboard == [CardEntry("Lightning Bolt", 4)]
    assert result.unparsed == ["not a card line"]
