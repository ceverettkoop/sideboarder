package sbd

import (
	"fmt"
	"strings"
)

// View is a document plus everything derived from it that the web client
// displays, so the rules (effective plans, validation, records) live here in
// one place rather than being re-implemented in JavaScript.
type View struct {
	Doc        *Document                `json:"doc"`
	Matchups   map[string]MatchupView   `json:"matchups"` // keyed by archetype ID
	Records    []RecordView             `json:"records"`
	Overall    RecordView               `json:"overall"`
	Outcomes   map[string]string        `json:"outcomes"` // result ID -> W/L/D
	Candidates []string                 `json:"archetype_candidates"`
	Revisions  map[int]RevisionDeckView `json:"revision_decks"`
	Suggestion Suggestion               `json:"suggestion"`
}

// EffectiveView is the effective plan for one side of the coin flip.
type EffectiveView struct {
	Plan       Plan           `json:"plan"`
	Validation PlanValidation `json:"validation"`
}

// MatchupView holds the effective plans on the play and on the draw.
type MatchupView struct {
	Play EffectiveView `json:"play"`
	Draw EffectiveView `json:"draw"`
	// The current main deck after the base plan, as decklist text: a
	// starting point for the matchup's post-board target.
	BasePostboard string `json:"base_postboard"`
}

// RecordView is an ArchetypeRecord with its display strings.
type RecordView struct {
	ArchetypeRecord
	Matches     int    `json:"matches"`
	RecordText  string `json:"record_text"`
	GamesText   string `json:"games_text"`
	WinrateText string `json:"winrate_text"`
}

// RevisionDeckView is the decklist a result's revision number refers to.
type RevisionDeckView struct {
	Deck Deck   `json:"deck"`
	Text string `json:"text"`
}

func recordView(r ArchetypeRecord) RecordView {
	return RecordView{
		ArchetypeRecord: r,
		Matches:         r.Matches(),
		RecordText:      r.RecordText(),
		GamesText:       r.GamesText(),
		WinrateText:     r.WinrateText(),
	}
}

// BuildView derives the client view of doc.
func BuildView(doc *Document) View {
	doc.Normalize()
	v := View{
		Doc:       doc,
		Matchups:  map[string]MatchupView{},
		Records:   []RecordView{},
		Overall:   recordView(OverallRecord(doc.Results)),
		Outcomes:  map[string]string{},
		Revisions: map[int]RevisionDeckView{},
	}
	for _, a := range doc.Archetypes {
		play, draw := a.Effective(true), a.Effective(false)
		v.Matchups[a.ID] = MatchupView{
			Play:          EffectiveView{Plan: play.normalized(), Validation: ValidatePlan(play, doc.Deck)},
			Draw:          EffectiveView{Plan: draw.normalized(), Validation: ValidatePlan(draw, doc.Deck)},
			BasePostboard: listText(PostboardDeck(doc.Deck, a.Base)),
		}
	}
	for _, r := range BuildRecords(doc.Results) {
		v.Records = append(v.Records, recordView(r))
	}
	for _, r := range doc.Results {
		v.Outcomes[r.ID] = r.Result()
		if r.DeckRevision == nil {
			continue
		}
		if _, done := v.Revisions[*r.DeckRevision]; done {
			continue
		}
		if deck := doc.DeckForRevision(r.DeckRevision); deck != nil {
			v.Revisions[*r.DeckRevision] = RevisionDeckView{Deck: *deck, Text: DeckText(*deck)}
		}
	}
	v.Candidates = ArchetypeCandidates(doc)
	v.Suggestion = Suggest(doc)
	return v
}

// ArchetypeCandidates lists opponent names for autocomplete: the planned
// archetypes, then any other opponents already logged (deduplicated).
func ArchetypeCandidates(doc *Document) []string {
	names := []string{}
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" && !seen[Fold(name)] {
			seen[Fold(name)] = true
			names = append(names, name)
		}
	}
	for _, a := range doc.Archetypes {
		add(a.Name)
	}
	for _, r := range doc.Results {
		add(r.Archetype)
	}
	return names
}

// PostboardDeck is the main deck after a plan: OUT cards removed, IN cards added.
func PostboardDeck(deck Deck, plan Plan) []CardEntry {
	out := make([]CardEntry, len(plan.Out))
	for i, e := range plan.Out {
		out[i] = CardEntry{Name: e.Name, Qty: -e.Qty}
	}
	return MergeEntries(deck.Mainboard, out, plan.In)
}

func listText(entries []CardEntry) string {
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = fmt.Sprintf("%d %s", e.Qty, e.Name)
	}
	return strings.Join(lines, "\n")
}
