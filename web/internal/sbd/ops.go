package sbd

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Edits are expressed as small intent-based operations (the same actions the
// TUI's key bindings perform) so several clients can edit one file: each op
// is applied to the latest copy on disk rather than overwriting it wholesale.

// ResultInput is the editable part of a match result, as typed in a form.
type ResultInput struct {
	Date      string `json:"date"`
	Event     string `json:"event"`
	Archetype string `json:"archetype"`
	PlayDraw  string `json:"play_draw"`
	Games     string `json:"games"` // e.g. "2-1"
	Notes     string `json:"notes"`
}

// Op is one edit to a document. Only the fields the op names are used.
type Op struct {
	Op string `json:"op"`

	Text   string `json:"text,omitempty"`   // import_deck: decklist text
	Name   string `json:"name,omitempty"`   // new card / deck / archetype name
	Format string `json:"format,omitempty"` // set_deck_info
	Notes  string `json:"notes,omitempty"`  // update_archetype

	Board string `json:"board,omitempty"` // "main" | "side"
	Card  string `json:"card,omitempty"`  // existing card being edited
	Qty   int    `json:"qty,omitempty"`
	Delta int    `json:"delta,omitempty"`

	ArchetypeID string `json:"archetype_id,omitempty"`
	Layer       string `json:"layer,omitempty"` // "base" | "play" | "draw"
	List        string `json:"list,omitempty"`  // "out" | "in"

	ResultID string       `json:"result_id,omitempty"`
	Result   *ResultInput `json:"result,omitempty"`

	Share *float64 `json:"share,omitempty"` // set_meta_share: percent; null clears
}

// OpResult carries op-specific feedback (e.g. lines an import couldn't parse).
type OpResult struct {
	Message  string   `json:"message,omitempty"`
	Unparsed []string `json:"unparsed,omitempty"`
}

// Apply performs op on doc in place.
func Apply(doc *Document, op Op) (OpResult, error) {
	switch op.Op {
	case "import_deck":
		return importDeck(doc, op)
	case "set_deck_info":
		doc.Deck.Name = orDefault(strings.TrimSpace(op.Name), "Untitled")
		doc.Deck.Format = strings.TrimSpace(op.Format)
		return OpResult{}, nil
	case "deck_add", "deck_edit", "deck_delete", "deck_qty":
		return OpResult{}, deckOp(doc, op)
	case "add_archetype":
		name := strings.TrimSpace(op.Name)
		if name == "" {
			return OpResult{}, errors.New("archetype name is required")
		}
		doc.Archetypes = append(doc.Archetypes, NewArchetype(name))
		return OpResult{Message: doc.Archetypes[len(doc.Archetypes)-1].ID}, nil
	case "update_archetype":
		arch, err := doc.Archetype(op.ArchetypeID)
		if err != nil {
			return OpResult{}, err
		}
		if name := strings.TrimSpace(op.Name); name != "" {
			arch.Name = name
		}
		arch.Notes = strings.TrimSpace(op.Notes)
		return OpResult{}, nil
	case "remove_archetype":
		if _, err := doc.Archetype(op.ArchetypeID); err != nil {
			return OpResult{}, err
		}
		kept := doc.Archetypes[:0]
		for _, a := range doc.Archetypes {
			if a.ID != op.ArchetypeID {
				kept = append(kept, a)
			}
		}
		doc.Archetypes = kept
		return OpResult{}, nil
	case "set_meta_share":
		arch, err := doc.Archetype(op.ArchetypeID)
		if err != nil {
			return OpResult{}, err
		}
		if op.Share != nil && (*op.Share < 0 || *op.Share > 100 || math.IsNaN(*op.Share)) {
			return OpResult{}, errors.New("metagame share must be between 0 and 100%")
		}
		arch.MetaShare = op.Share
		return OpResult{}, nil
	case "set_target_deck":
		return setTargetDeck(doc, op)
	case "apply_suggestion":
		return applySuggestion(doc)
	case "plan_add", "plan_remove", "plan_qty":
		return OpResult{}, planOp(doc, op)
	case "result_add":
		res, err := buildResult(op.Result, MatchResult{ID: NewID()})
		if err != nil {
			return OpResult{}, err
		}
		// Recording a result with a revised deck finalizes it as a new revision.
		rev := doc.CommitDeckRevision()
		res.DeckRevision = &rev
		doc.Results = append(doc.Results, res)
		return OpResult{}, nil
	case "result_update":
		cur, err := doc.Result(op.ResultID)
		if err != nil {
			return OpResult{}, err
		}
		res, err := buildResult(op.Result, *cur)
		if err != nil {
			return OpResult{}, err
		}
		*cur = res
		return OpResult{}, nil
	case "result_delete":
		if _, err := doc.Result(op.ResultID); err != nil {
			return OpResult{}, err
		}
		kept := doc.Results[:0]
		for _, r := range doc.Results {
			if r.ID != op.ResultID {
				kept = append(kept, r)
			}
		}
		doc.Results = kept
		return OpResult{}, nil
	}
	return OpResult{}, fmt.Errorf("unknown op %q", op.Op)
}

func importDeck(doc *Document, op Op) (OpResult, error) {
	parsed := ParseDecklist(op.Text, orDefault(strings.TrimSpace(op.Name), "Untitled"))
	if len(parsed.Deck.Mainboard) == 0 && len(parsed.Deck.Sideboard) == 0 {
		return OpResult{}, errors.New("no cards found in the pasted decklist")
	}
	doc.NoteDeckChange()
	doc.Deck = parsed.Deck
	return OpResult{
		Message: fmt.Sprintf("Imported %d+%d cards.",
			parsed.Deck.MainboardCount(), parsed.Deck.SideboardCount()),
		Unparsed: parsed.Unparsed,
	}, nil
}

// setTargetDeck stores the deck wanted after boarding against an archetype.
// Pasted text may be split by blank lines (e.g. creatures / spells / lands);
// only a list that already has a full main deck has its sideboard ignored.
func setTargetDeck(doc *Document, op Op) (OpResult, error) {
	arch, err := doc.Archetype(op.ArchetypeID)
	if err != nil {
		return OpResult{}, err
	}
	if strings.TrimSpace(op.Text) == "" {
		arch.TargetDeck = nil
		return OpResult{Message: "Post-board deck cleared."}, nil
	}
	parsed := ParseDecklist(op.Text, "")
	entries := parsed.Deck.Mainboard
	msg := ""
	if parsed.Deck.MainboardCount() >= MainSize {
		if n := parsed.Deck.SideboardCount(); n > 0 {
			msg = fmt.Sprintf(" Ignored the %d-card sideboard.", n)
		}
	} else {
		entries = MergeEntries(parsed.Deck.Mainboard, parsed.Deck.Sideboard)
	}
	if len(entries) == 0 {
		return OpResult{}, errors.New("no cards found in the pasted list")
	}
	arch.TargetDeck = entries
	return OpResult{
		Message:  fmt.Sprintf("Saved a %d-card post-board deck for %s.%s", TotalQty(entries), arch.Name, msg),
		Unparsed: parsed.Unparsed,
	}, nil
}

// applySuggestion replaces the deck with the suggested 75 and each targeted
// matchup's base plan with the plan that reaches its post-board deck.
func applySuggestion(doc *Document) (OpResult, error) {
	s := Suggest(doc)
	if !s.Ready {
		return OpResult{}, errors.New("enter a post-board deck for at least one matchup first")
	}
	doc.NoteDeckChange()
	doc.Deck.Mainboard = s.Main
	doc.Deck.Sideboard = s.Side
	for _, fit := range s.Matchups {
		arch, err := doc.Archetype(fit.ArchetypeID)
		if err != nil {
			return OpResult{}, err
		}
		arch.Base = fit.Plan
	}
	// The plans now use the new list, which makes it a revision in use.
	doc.CommitDeckRevision()
	return OpResult{Message: fmt.Sprintf("Deck updated to %d+%d cards with %d sideboard plans.",
		TotalQty(s.Main), TotalQty(s.Side), len(s.Matchups))}, nil
}

func deckOp(doc *Document, op Op) error {
	entries, err := doc.Deck.Board(op.Board)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(op.Name)
	if op.Op != "deck_add" && indexOf(*entries, op.Card) < 0 {
		return fmt.Errorf("%q is not in the %sboard", op.Card, op.Board)
	}
	if (op.Op == "deck_add" || op.Op == "deck_edit") && name == "" {
		return errors.New("card name is required")
	}
	doc.NoteDeckChange()
	switch op.Op {
	case "deck_add":
		*entries = MergeEntries(*entries, []CardEntry{{Name: name, Qty: max(1, op.Qty)}})
	case "deck_edit":
		// Replace the edited entry, then merge in case the new name collides.
		rebuilt := make([]CardEntry, 0, len(*entries))
		for _, e := range *entries {
			if e.Name == op.Card {
				e = CardEntry{Name: name, Qty: max(1, op.Qty)}
			}
			rebuilt = append(rebuilt, e)
		}
		*entries = MergeEntries(rebuilt)
	case "deck_delete":
		*entries = without(*entries, op.Card)
	case "deck_qty":
		adjustQty(*entries, op.Card, op.Delta)
	}
	return nil
}

func planOp(doc *Document, op Op) error {
	arch, err := doc.Archetype(op.ArchetypeID)
	if err != nil {
		return err
	}
	plan, err := arch.Layer(op.Layer, op.Op == "plan_add")
	if err != nil {
		return err
	}
	if plan == nil {
		return fmt.Errorf("the %s layer has no cards yet", op.Layer)
	}
	var target *[]CardEntry
	switch op.List {
	case "out":
		target = &plan.Out
	case "in":
		target = &plan.In
	default:
		return fmt.Errorf("unknown plan list %q", op.List)
	}
	switch op.Op {
	case "plan_add":
		name := strings.TrimSpace(op.Name)
		if name == "" {
			return errors.New("card name is required")
		}
		*target = MergeEntries(*target, []CardEntry{{Name: name, Qty: max(1, op.Qty)}})
	case "plan_remove":
		if indexOf(*target, op.Card) < 0 {
			return fmt.Errorf("%q is not in the plan", op.Card)
		}
		*target = without(*target, op.Card)
	case "plan_qty":
		if indexOf(*target, op.Card) < 0 {
			return fmt.Errorf("%q is not in the plan", op.Card)
		}
		adjustQty(*target, op.Card, op.Delta)
	}
	// Editing a plan against a revised deck is what makes that deck a new
	// revision: results logged from here on pin to the updated list.
	doc.CommitDeckRevision()
	return nil
}

func buildResult(in *ResultInput, base MatchResult) (MatchResult, error) {
	if in == nil {
		return base, errors.New("result fields are required")
	}
	won, lost, err := ParseGameScore(in.Games)
	if err != nil {
		return base, err
	}
	playDraw, err := ParsePlayDraw(in.PlayDraw)
	if err != nil {
		return base, err
	}
	return MatchResult{
		ID:           base.ID,
		DeckRevision: base.DeckRevision,
		Date:         strings.TrimSpace(in.Date),
		Event:        strings.TrimSpace(in.Event),
		Archetype:    strings.TrimSpace(in.Archetype),
		PlayDraw:     playDraw,
		GamesWon:     won,
		GamesLost:    lost,
		Notes:        strings.TrimSpace(in.Notes),
	}, nil
}

func indexOf(entries []CardEntry, name string) int {
	for i, e := range entries {
		if e.Name == name {
			return i
		}
	}
	return -1
}

func without(entries []CardEntry, name string) []CardEntry {
	out := []CardEntry{}
	for _, e := range entries {
		if e.Name != name {
			out = append(out, e)
		}
	}
	return out
}

func adjustQty(entries []CardEntry, name string, delta int) {
	for i := range entries {
		if entries[i].Name == name {
			entries[i].Qty = max(1, entries[i].Qty+delta)
		}
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
