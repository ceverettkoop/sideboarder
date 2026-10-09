package sbd

import (
	"reflect"
	"testing"
)

func mustApply(t *testing.T, doc *Document, op Op) OpResult {
	t.Helper()
	res, err := Apply(doc, op)
	if err != nil {
		t.Fatalf("%s: %v", op.Op, err)
	}
	return res
}

func TestImportAndDeckOps(t *testing.T) {
	doc := NewDocument()
	res := mustApply(t, doc, Op{Op: "import_deck", Name: "Burn", Text: moxfieldMTGO + "junk\n"})
	if doc.Deck.Name != "Burn" || doc.Deck.MainboardCount() != 12 || len(res.Unparsed) != 1 {
		t.Fatalf("got %+v %+v", doc.Deck, res)
	}
	if _, err := Apply(doc, Op{Op: "import_deck", Text: "nothing here"}); err == nil {
		t.Fatal("empty import accepted")
	}
	mustApply(t, doc, Op{Op: "deck_add", Board: "main", Name: "lightning bolt", Qty: 1})
	if doc.Deck.Mainboard[0] != ce("Lightning Bolt", 5) {
		t.Fatalf("add didn't merge: %v", doc.Deck.Mainboard)
	}
	mustApply(t, doc, Op{Op: "deck_qty", Board: "main", Card: "Lightning Bolt", Delta: -10})
	if doc.Deck.Mainboard[0].Qty != 1 {
		t.Fatal("qty went below 1")
	}
	// Replacing a card with one already in the list merges them.
	mustApply(t, doc, Op{Op: "deck_edit", Board: "main", Card: "Lightning Bolt", Name: "Goblin Guide", Qty: 2})
	if !reflect.DeepEqual(doc.Deck.Mainboard[:2], []CardEntry{ce("Goblin Guide", 6), ce("Monastery Swiftspear", 4)}) {
		t.Fatalf("got %v", doc.Deck.Mainboard)
	}
	mustApply(t, doc, Op{Op: "deck_delete", Board: "side", Card: "Rest in Peace"})
	if doc.Deck.SideboardCount() != 3 {
		t.Fatal("delete failed")
	}
	if _, err := Apply(doc, Op{Op: "deck_delete", Board: "side", Card: "Nope"}); err == nil {
		t.Fatal("deleting a missing card succeeded")
	}
}

func TestPlanOpsCommitPendingRevision(t *testing.T) {
	doc := docWithResult()
	res := mustApply(t, doc, Op{Op: "add_archetype", Name: "Control"})
	id := res.Message
	mustApply(t, doc, Op{Op: "deck_add", Board: "side", Name: "Smash", Qty: 2})
	if !doc.DeckModified || doc.DeckRevision != 1 {
		t.Fatal("deck edit should be pending")
	}
	mustApply(t, doc, Op{Op: "plan_add", ArchetypeID: id, Layer: LayerPlay, List: "in", Name: "Smash", Qty: 1})
	if doc.DeckModified || doc.DeckRevision != 2 {
		t.Fatal("plan edit should commit the revision")
	}
	arch, _ := doc.Archetype(id)
	if arch.PlayOverride == nil || arch.PlayOverride.In[0] != ce("Smash", 1) {
		t.Fatalf("override not created: %+v", arch)
	}
	mustApply(t, doc, Op{Op: "plan_qty", ArchetypeID: id, Layer: LayerPlay, List: "in", Card: "Smash", Delta: 1})
	mustApply(t, doc, Op{Op: "plan_remove", ArchetypeID: id, Layer: LayerPlay, List: "in", Card: "Smash"})
	if len(arch.PlayOverride.In) != 0 {
		t.Fatal("remove failed")
	}
	if _, err := Apply(doc, Op{Op: "plan_remove", ArchetypeID: id, Layer: LayerDraw, List: "in", Card: "Smash"}); err == nil {
		t.Fatal("removing from a missing layer succeeded")
	}
	if arch.DrawOverride != nil {
		t.Fatal("remove created an empty draw override")
	}
	mustApply(t, doc, Op{Op: "update_archetype", ArchetypeID: id, Name: "UW Control", Notes: " slow "})
	if arch.Name != "UW Control" || arch.Notes != "slow" {
		t.Fatalf("got %+v", arch)
	}
	mustApply(t, doc, Op{Op: "remove_archetype", ArchetypeID: id})
	if len(doc.Archetypes) != 0 {
		t.Fatal("archetype not removed")
	}
}

func TestResultOps(t *testing.T) {
	doc := docWithResult()
	mustApply(t, doc, Op{Op: "deck_qty", Board: "main", Card: "Lightning Bolt", Delta: -1})
	in := &ResultInput{Date: "2026-10-09", Event: " FNM ", Archetype: "Burn", PlayDraw: "d", Games: "1-2"}
	mustApply(t, doc, Op{Op: "result_add", Result: in})
	added := doc.Results[1]
	if *added.DeckRevision != 2 || added.PlayDraw != Draw || added.Event != "FNM" || added.Result() != ResultLoss {
		t.Fatalf("got %+v", added)
	}
	in.Games = "2-1"
	mustApply(t, doc, Op{Op: "result_update", ResultID: added.ID, Result: in})
	if doc.Results[1].GamesWon != 2 || *doc.Results[1].DeckRevision != 2 || doc.Results[1].ID != added.ID {
		t.Fatalf("got %+v", doc.Results[1])
	}
	in.Games = "W"
	if _, err := Apply(doc, Op{Op: "result_update", ResultID: added.ID, Result: in}); err == nil {
		t.Fatal("bad score accepted")
	}
	mustApply(t, doc, Op{Op: "result_delete", ResultID: added.ID})
	if len(doc.Results) != 1 {
		t.Fatal("delete failed")
	}
}

func TestBuildView(t *testing.T) {
	doc := docWithResult()
	doc.Archetypes = append(doc.Archetypes, Archetype{ID: "a", Name: "Control",
		Base: Plan{Out: []CardEntry{ce("Lightning Bolt", 1)}, In: []CardEntry{}}})
	v := BuildView(doc)
	if v.Matchups["a"].Play.Validation.Balanced || v.Overall.RecordText != "1-0-0" || v.Outcomes["r1"] != ResultWin {
		t.Fatalf("got %+v", v)
	}
	if v.Revisions[1].Text != "4 Lightning Bolt\n" {
		t.Fatalf("got %q", v.Revisions[1].Text)
	}
	if !reflect.DeepEqual(v.Candidates, []string{"Control"}) {
		t.Fatalf("got %v", v.Candidates)
	}
}
