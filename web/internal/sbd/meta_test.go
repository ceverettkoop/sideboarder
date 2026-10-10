package sbd

import (
	"reflect"
	"strings"
	"testing"
)

func f64(x float64) *float64 { return &x }

// Two matchups sharing a 52-card core:
//
//	Aggro   60%: 48 Mountain, 4 Bolt, 4 Spike, 4 Blaze
//	Control 40%: 48 Mountain, 4 Bolt, 4 Smash, 2 Spike, 2 Rest in Peace
//
// Copies wanted by the whole field (value 100%): 48 Mountain, 4 Bolt and the
// first 2 Spikes = 54. Six slots remain for copies valued 60% (Spike 3-4,
// Blaze 1-4); Spike wins the tie on average copies (3.2 vs 2.4). The
// sideboard gets the 40% copies: Smash (avg 1.6) before Rest in Peace (0.8).
func builderDoc() *Document {
	doc := NewDocument()
	doc.Archetypes = []Archetype{
		{ID: "aggro", Name: "Aggro", MetaShare: f64(30), TargetDeck: []CardEntry{
			ce("Mountain", 48), ce("Bolt", 4), ce("Spike", 4), ce("Blaze", 4)}},
		{ID: "control", Name: "Control", MetaShare: f64(20), TargetDeck: []CardEntry{
			ce("Mountain", 48), ce("Bolt", 4), ce("Smash", 4), ce("Spike", 2), ce("Rest in Peace", 2)}},
		{ID: "unplanned", Name: "Combo", MetaShare: f64(10)},
	}
	return doc
}

func TestSuggestMainAndSide(t *testing.T) {
	s := Suggest(builderDoc())
	if !s.Ready {
		t.Fatal("not ready")
	}
	wantMain := []CardEntry{ce("Mountain", 48), ce("Bolt", 4), ce("Spike", 4), ce("Blaze", 4)}
	wantSide := []CardEntry{ce("Smash", 4), ce("Rest in Peace", 2)}
	if !reflect.DeepEqual(s.Main, wantMain) || !reflect.DeepEqual(s.Side, wantSide) {
		t.Fatalf("main %v\nside %v", s.Main, s.Side)
	}
	aggro, control := s.Matchups[0], s.Matchups[1]
	if aggro.Weight != 60 || aggro.Game1 != 60 || aggro.Postboard != 60 || aggro.Swaps != 0 {
		t.Fatalf("aggro %+v", aggro)
	}
	wantPlan := Plan{Out: []CardEntry{ce("Spike", 2), ce("Blaze", 4)}, In: []CardEntry{ce("Smash", 4), ce("Rest in Peace", 2)}}
	if control.Game1 != 54 || control.Postboard != 60 || control.Swaps != 6 || !reflect.DeepEqual(control.Plan, wantPlan) {
		t.Fatalf("control %+v", control)
	}
	if s.Game1Rate != 96 || s.PostRate != 100 || s.AvgSwaps != 2.4 {
		t.Fatalf("rates %v %v %v", s.Game1Rate, s.PostRate, s.AvgSwaps)
	}
	var spike SuggestedCard
	for _, c := range s.Cards {
		if c.Name == "Spike" {
			spike = c
		}
	}
	if spike.Average != 3.2 || !reflect.DeepEqual(spike.Wanted, []float64{100, 100, 60, 60}) {
		t.Fatalf("spike %+v", spike)
	}
	joined := strings.Join(s.Warnings, "\n")
	if !strings.Contains(joined, "Combo (10%") || !strings.Contains(joined, "9 are free") {
		t.Fatalf("warnings %q", joined)
	}
}

// When the targets need more than 75 cards, the least-wanted copies are left
// out and show up as missing in the matchups that wanted them.
func TestSuggestReportsMissingCards(t *testing.T) {
	doc := NewDocument()
	doc.Archetypes = []Archetype{
		{ID: "a", Name: "A", MetaShare: f64(70), TargetDeck: []CardEntry{ce("Island", 60)}},
		{ID: "b", Name: "B", MetaShare: f64(30), TargetDeck: []CardEntry{ce("Swamp", 60)}},
	}
	s := Suggest(doc)
	if !reflect.DeepEqual(s.Main, []CardEntry{ce("Island", 60)}) || !reflect.DeepEqual(s.Side, []CardEntry{ce("Swamp", 15)}) {
		t.Fatalf("main %v side %v", s.Main, s.Side)
	}
	b := s.Matchups[1]
	if b.Postboard != 15 || b.Swaps != 15 || !reflect.DeepEqual(b.Missing, []CardEntry{ce("Swamp", 45)}) {
		t.Fatalf("b %+v", b)
	}
	// Short of its target, B keeps 45 main-deck Islands to make 60 cards.
	if !reflect.DeepEqual(b.Plan, Plan{Out: []CardEntry{ce("Island", 15)}, In: []CardEntry{ce("Swamp", 15)}}) {
		t.Fatalf("plan %+v", b.Plan)
	}
}

func TestSuggestWithoutSharesWeighsEqually(t *testing.T) {
	doc := builderDoc()
	doc.Archetypes[0].MetaShare, doc.Archetypes[1].MetaShare = nil, nil
	s := Suggest(doc)
	if s.Matchups[0].Weight != 50 || !strings.Contains(strings.Join(s.Warnings, " "), "counts equally") {
		t.Fatalf("got %+v", s)
	}
	if Suggest(NewDocument()).Ready {
		t.Fatal("ready without targets")
	}
}

func TestBuilderOps(t *testing.T) {
	doc := builderDoc()
	doc.Results = []MatchResult{{ID: "r", GamesWon: 2, DeckRevision: intp(1)}}

	// Blank-line groups in a short list are all part of the deck…
	res := mustApply(t, doc, Op{Op: "set_target_deck", ArchetypeID: "unplanned", Text: "40 Mountain\n\n20 Bolt\n"})
	if TotalQty(doc.Archetypes[2].TargetDeck) != 60 || !strings.Contains(res.Message, "60-card") {
		t.Fatalf("got %v %+v", doc.Archetypes[2].TargetDeck, res)
	}
	// …but a full 60 followed by a sideboard drops the sideboard.
	res = mustApply(t, doc, Op{Op: "set_target_deck", ArchetypeID: "unplanned", Text: "60 Mountain\n\n15 Smash\n"})
	if !reflect.DeepEqual(doc.Archetypes[2].TargetDeck, []CardEntry{ce("Mountain", 60)}) || !strings.Contains(res.Message, "15-card sideboard") {
		t.Fatalf("got %v %+v", doc.Archetypes[2].TargetDeck, res)
	}
	mustApply(t, doc, Op{Op: "set_target_deck", ArchetypeID: "unplanned", Text: "  "})
	if doc.Archetypes[2].TargetDeck != nil {
		t.Fatal("not cleared")
	}

	if _, err := Apply(doc, Op{Op: "set_meta_share", ArchetypeID: "aggro", Share: f64(120)}); err == nil {
		t.Fatal("accepted 120%")
	}
	mustApply(t, doc, Op{Op: "set_meta_share", ArchetypeID: "unplanned", Share: nil})
	if doc.Archetypes[2].MetaShare != nil {
		t.Fatal("share not cleared")
	}

	mustApply(t, doc, Op{Op: "apply_suggestion"})
	if doc.Deck.MainboardCount() != 60 || doc.Deck.SideboardCount() != 6 {
		t.Fatalf("deck %+v", doc.Deck)
	}
	control, _ := doc.Archetype("control")
	if control.Base.In[0] != ce("Smash", 4) {
		t.Fatalf("plan %+v", control.Base)
	}
	// The old list was frozen for the logged result; the new one is revision 2.
	if doc.DeckRevision != 2 || doc.DeckModified || len(doc.Revisions) != 1 {
		t.Fatalf("revisions %d %v %d", doc.DeckRevision, doc.DeckModified, len(doc.Revisions))
	}
	if _, err := Apply(NewDocument(), Op{Op: "apply_suggestion"}); err == nil {
		t.Fatal("applied without targets")
	}
}

func TestBuilderFieldsRoundTrip(t *testing.T) {
	doc := builderDoc()
	back := roundTrip(t, doc)
	if !reflect.DeepEqual(back, doc) {
		t.Fatalf("got %+v", back.Archetypes)
	}
	data, _ := Marshal(NewDocument())
	if strings.Contains(string(data), "meta_share") || strings.Contains(string(data), "target_deck") {
		t.Fatal("empty builder fields serialized")
	}
}

func TestPostboardDeck(t *testing.T) {
	deck := Deck{Mainboard: []CardEntry{ce("Bolt", 4), ce("Spike", 4)}}
	got := PostboardDeck(deck, Plan{Out: []CardEntry{ce("Spike", 4)}, In: []CardEntry{ce("Smash", 2), ce("Bolt", 1)}})
	if !reflect.DeepEqual(got, []CardEntry{ce("Bolt", 5), ce("Smash", 2)}) {
		t.Fatalf("got %v", got)
	}
}
