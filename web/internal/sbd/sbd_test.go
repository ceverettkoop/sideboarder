package sbd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Ports of the Python suite (tests/test_models.py, test_decklist.py,
// test_report.py, test_results.py, test_storage.py).

func ce(name string, qty int) CardEntry { return CardEntry{Name: name, Qty: qty} }

func intp(n int) *int { return &n }

func roundTrip(t *testing.T, doc *Document) *Document {
	t.Helper()
	data, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	return back
}

func TestMergeEntriesSumsByNameCaseInsensitive(t *testing.T) {
	got := MergeEntries([]CardEntry{ce("Lightning Bolt", 2), ce("Bolt of Doom", 1)}, []CardEntry{ce("lightning bolt", 1)})
	want := []CardEntry{ce("Lightning Bolt", 3), ce("Bolt of Doom", 1)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestMergeEntriesDropsNonPositive(t *testing.T) {
	if got := MergeEntries([]CardEntry{ce("A", 2)}, []CardEntry{ce("A", -2), ce("B", 0)}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestCombineNoOverrideReturnsCopy(t *testing.T) {
	base := Plan{Out: []CardEntry{ce("X", 1)}, In: []CardEntry{ce("Y", 1)}}
	got := Combine(base, nil)
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("got %v", got)
	}
	got.Out[0].Qty = 9
	if base.Out[0].Qty != 1 {
		t.Fatal("Combine aliased the base plan")
	}
}

func TestCombineSumsOverrideDeltas(t *testing.T) {
	base := Plan{Out: []CardEntry{ce("Searing Blaze", 2)}, In: []CardEntry{ce("Smash", 2)}}
	override := &Plan{In: []CardEntry{ce("Roiling Vortex", 1), ce("Smash", 1)}}
	got := Combine(base, override)
	if !reflect.DeepEqual(got.Out, []CardEntry{ce("Searing Blaze", 2)}) ||
		!reflect.DeepEqual(got.In, []CardEntry{ce("Smash", 3), ce("Roiling Vortex", 1)}) {
		t.Fatalf("got %v", got)
	}
}

func TestArchetypeEffectivePlayVsDraw(t *testing.T) {
	a := Archetype{
		Name:         "Control",
		Base:         Plan{Out: []CardEntry{ce("A", 1)}, In: []CardEntry{ce("B", 1)}},
		PlayOverride: &Plan{In: []CardEntry{ce("C", 1)}},
	}
	if len(a.Effective(true).In) != 2 || len(a.Effective(false).In) != 1 {
		t.Fatal("wrong effective plans")
	}
}

func TestDocumentRoundTrip(t *testing.T) {
	doc := NewDocument()
	doc.Deck = Deck{Name: "Burn", Format: "Modern", Mainboard: []CardEntry{ce("Lightning Bolt", 4)}, Sideboard: []CardEntry{ce("Smash", 3)}}
	doc.Archetypes = []Archetype{{
		ID: "abc", Name: "Azorius",
		Base:         Plan{Out: []CardEntry{ce("Lightning Bolt", 1)}, In: []CardEntry{ce("Smash", 1)}},
		PlayOverride: &Plan{In: []CardEntry{ce("Smash", 1)}},
	}}
	if back := roundTrip(t, doc); !reflect.DeepEqual(back, doc) {
		t.Fatalf("round trip changed the document:\n%+v\n%+v", back, doc)
	}
}

func TestOptionalOverridesOmittedWhenNil(t *testing.T) {
	doc := NewDocument()
	doc.Archetypes = []Archetype{NewArchetype("X")}
	data, _ := Marshal(doc)
	if strings.Contains(string(data), "play_override") || strings.Contains(string(data), "draw_override") {
		t.Fatalf("overrides serialized: %s", data)
	}
	if strings.Contains(string(data), "null") {
		t.Fatalf("null in output: %s", data)
	}
}

func TestEmptyOverrideDictLoadsAsNil(t *testing.T) {
	doc, err := ParseDocument([]byte(`{"archetypes":[{"name":"X","play_override":{},"draw_override":{"out":[],"in":[]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	a := doc.Archetypes[0]
	if a.PlayOverride != nil || a.DrawOverride == nil {
		t.Fatalf("Python truthiness not mirrored: %+v", a)
	}
	if len(a.ID) != 32 {
		t.Fatalf("missing id not generated: %q", a.ID)
	}
}

func TestValidatePlanBalanceAndLegality(t *testing.T) {
	deck := Deck{Sideboard: []CardEntry{ce("Smash", 3)}}
	plan := Plan{Out: []CardEntry{ce("Bolt", 2)}, In: []CardEntry{ce("Smash", 1), ce("Rogue", 1)}}
	v := ValidatePlan(plan, deck)
	if v.OutTotal != 2 || v.InTotal != 2 || !v.Balanced || !reflect.DeepEqual(v.IllegalIn, []string{"Rogue"}) || v.OK {
		t.Fatalf("got %+v", v)
	}
}

// ----- decklist -----

const moxfieldMTGO = "4 Lightning Bolt\n4 Goblin Guide\n4 Monastery Swiftspear\n\n3 Smash to Smithereens\n2 Rest in Peace\n"

func TestBlankLineSplitsMainAndSide(t *testing.T) {
	r := ParseDecklist(moxfieldMTGO, "Untitled")
	if r.Deck.MainboardCount() != 12 || r.Deck.SideboardCount() != 5 || len(r.Unparsed) != 0 {
		t.Fatalf("got %+v", r)
	}
	if r.Deck.Sideboard[1] != ce("Rest in Peace", 2) {
		t.Fatalf("got %v", r.Deck.Sideboard)
	}
}

func TestExplicitHeaders(t *testing.T) {
	r := ParseDecklist("Deck\n4 Lightning Bolt\n2 Fireblast\n\nSideboard\n3 Smash to Smithereens\n", "x")
	if r.Deck.MainboardCount() != 6 || r.Deck.SideboardCount() != 3 {
		t.Fatalf("got %+v", r.Deck)
	}
}

func TestStripsSetAndFoilAnnotations(t *testing.T) {
	r := ParseDecklist("4 Lightning Bolt (2X2) 117 *F*\n1 Fable of the Mirror-Breaker // Reflection of Kiki-Rikki (NEO) 141\n", "x")
	names := []string{r.Deck.Mainboard[0].Name, r.Deck.Mainboard[1].Name}
	want := []string{"Lightning Bolt", "Fable of the Mirror-Breaker // Reflection of Kiki-Rikki"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("got %v", names)
	}
}

func TestSBPrefixRoutesToSideboard(t *testing.T) {
	r := ParseDecklist("4 Lightning Bolt\nSB: 2 Smash to Smithereens\n", "x")
	if r.Deck.MainboardCount() != 4 || r.Deck.SideboardCount() != 2 {
		t.Fatalf("got %+v", r.Deck)
	}
}

func TestDuplicateLinesMerge(t *testing.T) {
	r := ParseDecklist("2 Lightning Bolt\r\n2 Lightning Bolt\r\n", "x")
	if !reflect.DeepEqual(r.Deck.Mainboard, []CardEntry{ce("Lightning Bolt", 4)}) {
		t.Fatalf("got %v", r.Deck.Mainboard)
	}
}

func TestXQuantityAndUnparsedLines(t *testing.T) {
	r := ParseDecklist("4x Lightning Bolt\nnot a card line\n", "x")
	if !reflect.DeepEqual(r.Deck.Mainboard, []CardEntry{ce("Lightning Bolt", 4)}) ||
		!reflect.DeepEqual(r.Unparsed, []string{"not a card line"}) {
		t.Fatalf("got %+v", r)
	}
}

// ----- report -----

func reportArchetypes() []Archetype {
	return []Archetype{
		{Name: "Control", Base: Plan{Out: []CardEntry{ce("Bolt", 2)}, In: []CardEntry{ce("Smash", 2)}},
			PlayOverride: &Plan{In: []CardEntry{ce("Vortex", 1)}}},
		{Name: "Aggro", Base: Plan{Out: []CardEntry{ce("Bolt", 1)}, In: []CardEntry{ce("Wrath", 1)}}},
	}
}

func byName(rows []CardFrequency) map[string]CardFrequency {
	m := map[string]CardFrequency{}
	for _, r := range rows {
		m[r.Name] = r
	}
	return m
}

func TestFrequencyCountsBase(t *testing.T) {
	rows := byName(BuildFrequency(reportArchetypes(), ModeBase))
	if rows["Bolt"].OutCount != 2 || rows["Bolt"].OutQty != 3 || rows["Smash"].InCount != 1 {
		t.Fatalf("got %+v", rows)
	}
	if _, ok := rows["Vortex"]; ok {
		t.Fatal("override counted in base mode")
	}
}

func TestFrequencyIncludesOverrideInPlayMode(t *testing.T) {
	if byName(BuildFrequency(reportArchetypes(), ModePlay))["Vortex"].InCount != 1 {
		t.Fatal("override missing in play mode")
	}
}

func TestFrequencySortedByInvolvement(t *testing.T) {
	if rows := BuildFrequency(reportArchetypes(), ModeBase); rows[0].Name != "Bolt" {
		t.Fatalf("got %v", rows)
	}
}

func TestFrequencyCSV(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(FrequencyCSV(BuildFrequency(reportArchetypes(), ModeBase))), "\r\n")
	if lines[0] != "card,out_count,in_count,out_qty,in_qty" || lines[1] != "Bolt,2,0,3,0" {
		t.Fatalf("got %q", lines)
	}
}

// ----- results -----

func sampleResults() []MatchResult {
	return []MatchResult{
		{Date: "2026-08-01", Event: "FNM", Archetype: "Azorius Control", GamesWon: 2, GamesLost: 0, PlayDraw: Play},
		{Date: "2026-08-01", Event: "FNM", Archetype: "azorius control", GamesWon: 1, GamesLost: 2, PlayDraw: Draw},
		{Date: "2026-08-08", Event: "RCQ", Archetype: "Azorius Control", GamesWon: 2, GamesLost: 1},
		{Date: "2026-08-08", Event: "RCQ", Archetype: "Burn", GamesWon: 1, GamesLost: 1},
		{Date: "2026-08-08", Event: "RCQ", Archetype: "", GamesWon: 0, GamesLost: 2},
	}
}

func TestParseGameScore(t *testing.T) {
	for in, want := range map[string][2]int{"2-1": {2, 1}, " 1 – 2 ": {1, 2}, "1/0": {1, 0}, "1:1": {1, 1}} {
		w, l, err := ParseGameScore(in)
		if err != nil || w != want[0] || l != want[1] {
			t.Fatalf("%q: got %d-%d %v", in, w, l, err)
		}
	}
	if _, _, err := ParseGameScore("W"); err == nil {
		t.Fatal("accepted W")
	}
}

func TestParsePlayDraw(t *testing.T) {
	for in, want := range map[string]string{"p": Play, "Draw": Draw, "": "", "—": ""} {
		if got, err := ParsePlayDraw(in); err != nil || got != want {
			t.Fatalf("%q: got %q %v", in, got, err)
		}
	}
	if _, err := ParsePlayDraw("x"); err == nil {
		t.Fatal("accepted x")
	}
}

func TestMatchResultOutcome(t *testing.T) {
	if (MatchResult{GamesWon: 2, GamesLost: 1}).Result() != ResultWin ||
		(MatchResult{GamesWon: 1, GamesLost: 2}).Result() != ResultLoss ||
		(MatchResult{GamesWon: 1, GamesLost: 1}).Result() != ResultDraw ||
		(MatchResult{GamesWon: 2, GamesLost: 1}).GamesText() != "2-1" {
		t.Fatal("wrong outcomes")
	}
}

func TestLegacyResultFieldLoadsAsGameScore(t *testing.T) {
	doc, err := ParseDocument([]byte(`{"results":[{"archetype":"Burn","result":"L"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	r := doc.Results[0]
	if r.GamesWon != 0 || r.GamesLost != 2 || r.Result() != ResultLoss || r.PlayDraw != "" {
		t.Fatalf("got %+v", r)
	}
}

func TestDocumentRoundTripWithResults(t *testing.T) {
	doc := NewDocument()
	doc.Results = sampleResults()
	for i := range doc.Results {
		doc.Results[i].ID = NewID()
	}
	if back := roundTrip(t, doc); !reflect.DeepEqual(back, doc) {
		t.Fatalf("got %+v", back)
	}
}

func TestOldDocumentDefaults(t *testing.T) {
	doc, err := ParseDocument([]byte(`{"schema_version": 1, "deck": {}, "archetypes": []}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Results) != 0 || doc.DeckRevision != 1 || doc.DeckModified || len(doc.Revisions) != 0 || doc.Deck.Name != "Untitled" {
		t.Fatalf("got %+v", doc)
	}
}

func TestBuildRecordsGroupsCaseInsensitively(t *testing.T) {
	recs := BuildRecords(sampleResults())
	m := map[string]ArchetypeRecord{}
	for _, r := range recs {
		m[r.Name] = r
	}
	az := m["Azorius Control"]
	if az.Wins != 2 || az.Losses != 1 || az.Draws != 0 || az.RecordText() != "2-1-0" || az.GamesText() != "5-3" || az.WinrateText() != "67%" {
		t.Fatalf("got %+v", az)
	}
	if _, ok := m["Burn"].Winrate(); ok || m["Burn"].WinrateText() != "—" {
		t.Fatal("draw-only record has a winrate")
	}
	if _, ok := m["(unknown)"]; !ok {
		t.Fatal("blank archetype has no bucket")
	}
	if recs[0].Name != "Azorius Control" {
		t.Fatalf("not sorted: %v", recs)
	}
}

func TestOverallRecord(t *testing.T) {
	o := OverallRecord(sampleResults())
	if o.Wins != 2 || o.Losses != 2 || o.Draws != 1 || o.Matches() != 5 || o.WinrateText() != "50%" || o.GamesText() != "6-6" {
		t.Fatalf("got %+v", o)
	}
}

func TestResultsCSV(t *testing.T) {
	rs := sampleResults()[:1]
	rs[0].DeckRevision = intp(2)
	lines := strings.Split(strings.TrimSpace(ResultsCSV(rs)), "\r\n")
	if lines[0] != "date,event,archetype,play_draw,games_won,games_lost,result,deck_revision,notes" ||
		lines[1] != "2026-08-01,FNM,Azorius Control,play,2,0,W,2," {
		t.Fatalf("got %q", lines)
	}
}

func docWithResult() *Document {
	doc := NewDocument()
	doc.Deck = Deck{Name: "Burn", Mainboard: []CardEntry{ce("Lightning Bolt", 4)}, Sideboard: []CardEntry{}}
	doc.Results = append(doc.Results, MatchResult{ID: "r1", Archetype: "Control", GamesWon: 2, DeckRevision: intp(doc.DeckRevision)})
	return doc
}

func TestDeckChangeSnapshotsButDoesNotBumpRevision(t *testing.T) {
	doc := docWithResult()
	doc.NoteDeckChange()
	doc.Deck.Mainboard[0].Qty = 3
	if doc.DeckRevision != 1 || !doc.DeckModified || len(doc.Revisions) != 1 {
		t.Fatalf("got %+v", doc)
	}
	snap := &doc.Revisions[0]
	if snap.Revision != 1 || snap.Deck.Mainboard[0] != ce("Lightning Bolt", 4) || snap.SavedAt == "" {
		t.Fatalf("bad snapshot %+v", snap)
	}
	if doc.DeckForRevision(doc.Results[0].DeckRevision) != &snap.Deck || doc.DeckForRevision(nil) != nil || doc.DeckForRevision(intp(99)) != nil {
		t.Fatal("DeckForRevision wrong")
	}
}

func TestCommitFinalizesPendingDeckChanges(t *testing.T) {
	doc := docWithResult()
	doc.NoteDeckChange()
	doc.Deck.Mainboard[0].Qty = 3
	if doc.CommitDeckRevision() != 2 || doc.DeckModified {
		t.Fatal("commit failed")
	}
	if doc.DeckForRevision(intp(1)) != &doc.Revisions[0].Deck || doc.DeckForRevision(intp(2)) != &doc.Deck {
		t.Fatal("DeckForRevision wrong after commit")
	}
	if doc.CommitDeckRevision() != 2 || doc.DeckRevision != 2 {
		t.Fatal("second commit bumped")
	}
}

func TestManyDeckEditsBecomeOneRevision(t *testing.T) {
	doc := docWithResult()
	for i := 0; i < 3; i++ {
		doc.NoteDeckChange()
	}
	if doc.DeckRevision != 1 || len(doc.Revisions) != 1 || doc.CommitDeckRevision() != 2 || len(doc.Revisions) != 1 {
		t.Fatalf("got %+v", doc)
	}
}

func TestDeckChangeWithoutResultsKeepsRevision(t *testing.T) {
	doc := NewDocument()
	doc.NoteDeckChange()
	if doc.DeckRevision != 1 || doc.DeckModified || len(doc.Revisions) != 0 || doc.CommitDeckRevision() != 1 {
		t.Fatalf("got %+v", doc)
	}
}

func TestEditsAfterCommitWithoutNewResultsStayInPlace(t *testing.T) {
	doc := docWithResult()
	doc.NoteDeckChange()
	doc.CommitDeckRevision()
	doc.NoteDeckChange()
	if doc.DeckRevision != 2 || doc.DeckModified || len(doc.Revisions) != 1 {
		t.Fatalf("got %+v", doc)
	}
}

func TestRoundTripWithPendingDeckChanges(t *testing.T) {
	doc := docWithResult()
	doc.NoteDeckChange()
	doc.Deck.Mainboard[0].Qty = 3
	back := roundTrip(t, doc)
	if !reflect.DeepEqual(back, doc) || !back.DeckModified {
		t.Fatalf("got %+v", back)
	}
}

// ----- storage -----

func TestSaveAndLoadRoundTrip(t *testing.T) {
	doc := NewDocument()
	doc.Deck.Name = "Burn"
	doc.Deck.Mainboard = []CardEntry{ce("Lightning Bolt", 4)}
	doc.Archetypes = []Archetype{{ID: "a", Name: "Control", Base: Plan{Out: []CardEntry{ce("Bolt", 1)}, In: []CardEntry{}}}}
	path := filepath.Join(t.TempDir(), "sub", "burn.sbd.json")
	if err := SaveDocument(doc, path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadDocument(path)
	if err != nil || !reflect.DeepEqual(back, doc) {
		t.Fatalf("got %+v %v", back, err)
	}
}

func TestLoadRejectsFutureSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.sbd.json")
	_ = os.WriteFile(path, []byte(`{"schema_version": 999, "deck": {}, "archetypes": []}`), 0o644)
	if _, err := LoadDocument(path); err == nil {
		t.Fatal("loaded a future schema")
	}
}

func TestDefaultFilenameSanitizes(t *testing.T) {
	if got := DefaultFilename("Mono-Red Burn!"); got != "Mono-Red_Burn.sbd.json" {
		t.Fatalf("got %q", got)
	}
	if got := DefaultFilename(""); got != "deck.sbd.json" {
		t.Fatalf("got %q", got)
	}
}

// The serialized layout matches what the TUI writes (key order, indent).
func TestMarshalMatchesPythonLayout(t *testing.T) {
	doc := docWithResult()
	doc.Results[0].Notes = "close"
	data, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "schema_version": 1,
  "deck": {
    "name": "Burn",
    "format": "",
    "mainboard": [
      {
        "name": "Lightning Bolt",
        "qty": 4
      }
    ],
    "sideboard": []
  },
  "deck_revision": 1,
  "revisions": [],
  "archetypes": [],
  "results": [
    {
      "id": "r1",
      "date": "",
      "event": "",
      "archetype": "Control",
      "games_won": 2,
      "games_lost": 0,
      "notes": "close",
      "deck_revision": 1
    }
  ]
}`
	if string(data) != want {
		t.Fatalf("got\n%s", data)
	}
}
