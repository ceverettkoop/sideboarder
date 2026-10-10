// Package sbd is a Go port of the Sideboarder data model (src/sideboarder/models.py).
//
// A document = one deck (mainboard + sideboard) plus a list of opponent
// archetypes. Each archetype holds a *base* sideboard plan and optional
// *play* / *draw* override deltas. The effective plan for a given game is the
// base combined with the relevant override, summing quantities per card.
//
// Documents read and write the same *.sbd.json format as the Python TUI, so
// the two front ends can share files.
package sbd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the newest document schema this package understands.
const SchemaVersion = 1

// Fold is the case-insensitive key used for card / archetype names
// (Python's str.casefold).
func Fold(s string) string { return strings.ToLower(s) }

// Today returns the local ISO date; a variable so tests can pin the clock.
var Today = func() string { return time.Now().Format("2006-01-02") }

// NewID returns a random UUID4 as 32 hex characters (Python's uuid4().hex).
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}

// CardEntry is a card name with a quantity.
type CardEntry struct {
	Name string `json:"name"`
	Qty  int    `json:"qty"`
}

// MergeEntries combines entry lists, summing quantities per (case-insensitive)
// name. Order is preserved by first appearance; entries with a non-positive
// total are dropped.
func MergeEntries(lists ...[]CardEntry) []CardEntry {
	var order []string
	totals := map[string]int{}
	display := map[string]string{}
	for _, entries := range lists {
		for _, e := range entries {
			key := Fold(e.Name)
			if _, ok := totals[key]; !ok {
				order = append(order, key)
				display[key] = e.Name
			}
			totals[key] += e.Qty
		}
	}
	out := []CardEntry{}
	for _, k := range order {
		if totals[k] > 0 {
			out = append(out, CardEntry{Name: display[k], Qty: totals[k]})
		}
	}
	return out
}

// TotalQty sums the quantities of entries.
func TotalQty(entries []CardEntry) int {
	n := 0
	for _, e := range entries {
		n += e.Qty
	}
	return n
}

// Plan is a set of cards to take OUT and bring IN for a matchup.
type Plan struct {
	Out []CardEntry `json:"out"`
	In  []CardEntry `json:"in"`
}

// IsEmpty reports whether the plan moves no cards.
func (p Plan) IsEmpty() bool { return len(p.Out) == 0 && len(p.In) == 0 }

func (p Plan) normalized() Plan {
	return Plan{Out: nonNil(p.Out), In: nonNil(p.In)}
}

// Combine returns the effective plan: base summed with override per card.
func Combine(base Plan, override *Plan) Plan {
	if override == nil || override.IsEmpty() {
		return Plan{Out: append([]CardEntry{}, base.Out...), In: append([]CardEntry{}, base.In...)}
	}
	return Plan{Out: MergeEntries(base.Out, override.Out), In: MergeEntries(base.In, override.In)}
}

// Archetype is an opponent deck and the plan(s) against it.
type Archetype struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Notes        string `json:"notes"`
	Base         Plan   `json:"base"`
	PlayOverride *Plan  `json:"play_override,omitempty"`
	DrawOverride *Plan  `json:"draw_override,omitempty"`
	// Metagame builder inputs: the archetype's share of the field (percent)
	// and the 60-card deck wanted after sideboarding against it.
	MetaShare  *float64    `json:"meta_share,omitempty"`
	TargetDeck []CardEntry `json:"target_deck,omitempty"`
}

// NewArchetype returns an archetype with a fresh ID and an empty base plan.
func NewArchetype(name string) Archetype {
	return Archetype{ID: NewID(), Name: name, Base: Plan{Out: []CardEntry{}, In: []CardEntry{}}}
}

// Effective is the plan that actually applies on the play (true) or draw (false).
func (a Archetype) Effective(onPlay bool) Plan {
	if onPlay {
		return Combine(a.Base, a.PlayOverride)
	}
	return Combine(a.Base, a.DrawOverride)
}

// Plan layers, as named by the plan editor.
const (
	LayerBase = "base"
	LayerPlay = "play"
	LayerDraw = "draw"
)

// Layer returns the plan for a layer, creating an empty override if asked.
func (a *Archetype) Layer(layer string, create bool) (*Plan, error) {
	var slot **Plan
	switch layer {
	case LayerBase:
		return &a.Base, nil
	case LayerPlay:
		slot = &a.PlayOverride
	case LayerDraw:
		slot = &a.DrawOverride
	default:
		return nil, fmt.Errorf("unknown plan layer %q", layer)
	}
	if *slot == nil && create {
		*slot = &Plan{Out: []CardEntry{}, In: []CardEntry{}}
	}
	return *slot, nil
}

// Match outcomes and play/draw values.
const (
	ResultWin  = "W"
	ResultLoss = "L"
	ResultDraw = "D"

	Play = "play"
	Draw = "draw"
)

var gameScoreRE = regexp.MustCompile(`^(\d+)\s*[-–/:]\s*(\d+)$`)

// Legacy W/L/D match results become the game score they most likely stood for.
var legacyScores = map[string][2]int{ResultWin: {2, 0}, ResultLoss: {0, 2}, ResultDraw: {1, 1}}

// ParseGameScore parses '2-1', '1–2', '1/0' into (games won, games lost).
func ParseGameScore(value string) (int, int, error) {
	m := gameScoreRE.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return 0, 0, fmt.Errorf("Games must look like '2-1', got %q", value)
	}
	won, err1 := strconv.Atoi(m[1])
	lost, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("Games must look like '2-1', got %q", value)
	}
	return won, lost, nil
}

// ParsePlayDraw coerces user input ('p', 'Play', 'draw', ”) to Play/Draw or ""
// (unknown).
func ParsePlayDraw(value string) (string, error) {
	text := Fold(strings.TrimSpace(value))
	if text == "" || text == "—" {
		return "", nil
	}
	if strings.HasPrefix(Play, text) {
		return Play, nil
	}
	if strings.HasPrefix(Draw, text) {
		return Draw, nil
	}
	return "", fmt.Errorf("Play/draw must be 'play' or 'draw' (or blank), got %q", value)
}

// MatchResult is one recorded tournament match, scored in games won/lost.
type MatchResult struct {
	ID           string `json:"id"`
	Date         string `json:"date"` // ISO yyyy-mm-dd (kept as text; not validated)
	Event        string `json:"event"`
	Archetype    string `json:"archetype"` // opponent archetype name (free text)
	GamesWon     int    `json:"games_won"`
	GamesLost    int    `json:"games_lost"`
	Notes        string `json:"notes"`
	PlayDraw     string `json:"play_draw,omitempty"`     // Play, Draw, or "" when not recorded
	DeckRevision *int   `json:"deck_revision,omitempty"` // revision played with (nil: unknown)
}

// Result is the match outcome (W/L/D) implied by the game score.
func (r MatchResult) Result() string {
	switch {
	case r.GamesWon > r.GamesLost:
		return ResultWin
	case r.GamesWon < r.GamesLost:
		return ResultLoss
	}
	return ResultDraw
}

// GamesText renders the score as "2-1".
func (r MatchResult) GamesText() string { return fmt.Sprintf("%d-%d", r.GamesWon, r.GamesLost) }

// Deck is the player's deck: a mainboard and a sideboard.
type Deck struct {
	Name      string      `json:"name"`
	Format    string      `json:"format"`
	Mainboard []CardEntry `json:"mainboard"`
	Sideboard []CardEntry `json:"sideboard"`
}

// NewDeck returns an empty deck named "Untitled".
func NewDeck() Deck {
	return Deck{Name: "Untitled", Mainboard: []CardEntry{}, Sideboard: []CardEntry{}}
}

// MainboardCount is the number of mainboard cards.
func (d Deck) MainboardCount() int { return TotalQty(d.Mainboard) }

// SideboardCount is the number of sideboard cards.
func (d Deck) SideboardCount() int { return TotalQty(d.Sideboard) }

// Clone returns a deep copy of the deck.
func (d Deck) Clone() Deck {
	return Deck{
		Name:      d.Name,
		Format:    d.Format,
		Mainboard: append([]CardEntry{}, d.Mainboard...),
		Sideboard: append([]CardEntry{}, d.Sideboard...),
	}
}

// Board returns a pointer to the "main" or "side" entry list.
func (d *Deck) Board(which string) (*[]CardEntry, error) {
	switch which {
	case "main":
		return &d.Mainboard, nil
	case "side":
		return &d.Sideboard, nil
	}
	return nil, fmt.Errorf("unknown board %q", which)
}

// DeckRevision is a frozen snapshot of an earlier version of the deck.
type DeckRevision struct {
	Revision int    `json:"revision"`
	Deck     Deck   `json:"deck"`
	SavedAt  string `json:"saved_at"` // ISO date the snapshot was taken
}

// Document is the top-level value persisted to a *.sbd.json file.
type Document struct {
	SchemaVersion int            `json:"schema_version"`
	Deck          Deck           `json:"deck"`
	DeckRevision  int            `json:"deck_revision"` // revision number of the *current* deck
	Revisions     []DeckRevision `json:"revisions"`     // earlier snapshots
	Archetypes    []Archetype    `json:"archetypes"`
	Results       []MatchResult  `json:"results"`
	// DeckModified: deck edited since results were pinned to this revision.
	DeckModified bool `json:"deck_modified,omitempty"`
}

// NewDocument returns an empty document.
func NewDocument() *Document {
	return &Document{
		SchemaVersion: SchemaVersion,
		Deck:          NewDeck(),
		DeckRevision:  1,
		Revisions:     []DeckRevision{},
		Archetypes:    []Archetype{},
		Results:       []MatchResult{},
	}
}

// NoteDeckChange must be called before mutating the deck's composition.
//
// Deck edits never advance the revision by themselves — they accumulate as
// pending changes. If recorded results reference the current revision, its
// decklist is frozen into Revisions (once) so those results stay tied to the
// exact list they were played with; the revision number only advances when
// the revised deck is *used* (see CommitDeckRevision).
func (doc *Document) NoteDeckChange() {
	if doc.DeckModified {
		return // already pending; keep accumulating edits
	}
	pinned := false
	for _, r := range doc.Results {
		if r.DeckRevision != nil && *r.DeckRevision == doc.DeckRevision {
			pinned = true
			break
		}
	}
	if !pinned {
		return // nothing pinned to the current deck: edit it in place
	}
	doc.Revisions = append(doc.Revisions, DeckRevision{
		Revision: doc.DeckRevision,
		Deck:     doc.Deck.Clone(),
		SavedAt:  Today(),
	})
	doc.DeckModified = true
}

// CommitDeckRevision finalizes pending deck changes into a new revision and
// returns the current one. Called when the (possibly revised) deck is actually
// used: a match result is recorded with it, or a sideboard plan is edited
// against it.
func (doc *Document) CommitDeckRevision() int {
	if doc.DeckModified {
		doc.DeckRevision++
		doc.DeckModified = false
	}
	return doc.DeckRevision
}

// DeckForRevision is the decklist a revision number refers to (snapshot or current).
func (doc *Document) DeckForRevision(revision *int) *Deck {
	if revision == nil {
		return nil
	}
	// Snapshots first: while deck edits are pending, the current revision
	// number still refers to the frozen list, not the working deck.
	for i := range doc.Revisions {
		if doc.Revisions[i].Revision == *revision {
			return &doc.Revisions[i].Deck
		}
	}
	if *revision == doc.DeckRevision {
		return &doc.Deck
	}
	return nil
}

// Archetype returns the archetype with the given ID.
func (doc *Document) Archetype(id string) (*Archetype, error) {
	for i := range doc.Archetypes {
		if doc.Archetypes[i].ID == id {
			return &doc.Archetypes[i], nil
		}
	}
	return nil, fmt.Errorf("no archetype with id %q", id)
}

// Result returns the match result with the given ID.
func (doc *Document) Result(id string) (*MatchResult, error) {
	for i := range doc.Results {
		if doc.Results[i].ID == id {
			return &doc.Results[i], nil
		}
	}
	return nil, fmt.Errorf("no result with id %q", id)
}

// PlanValidation is the result of checking a plan against the deck's sideboard.
type PlanValidation struct {
	OutTotal  int      `json:"out_total"`
	InTotal   int      `json:"in_total"`
	IllegalIn []string `json:"illegal_in"` // IN names not present in the sideboard
	Balanced  bool     `json:"balanced"`
	OK        bool     `json:"ok"`
}

// ValidatePlan checks OUT/IN balance and that IN cards exist in the sideboard.
func ValidatePlan(plan Plan, deck Deck) PlanValidation {
	sb := map[string]bool{}
	for _, e := range deck.Sideboard {
		sb[Fold(e.Name)] = true
	}
	illegal := []string{}
	for _, e := range plan.In {
		if !sb[Fold(e.Name)] {
			illegal = append(illegal, e.Name)
		}
	}
	v := PlanValidation{OutTotal: TotalQty(plan.Out), InTotal: TotalQty(plan.In), IllegalIn: illegal}
	v.Balanced = v.OutTotal == v.InTotal
	v.OK = v.Balanced && len(illegal) == 0
	return v
}

func nonNil(entries []CardEntry) []CardEntry {
	if entries == nil {
		return []CardEntry{}
	}
	return entries
}

// Normalize replaces nil slices with empty ones so the document serializes
// like the Python app (`[]`, never `null`).
func (doc *Document) Normalize() {
	normDeck := func(d *Deck) {
		d.Mainboard, d.Sideboard = nonNil(d.Mainboard), nonNil(d.Sideboard)
	}
	normDeck(&doc.Deck)
	if doc.Revisions == nil {
		doc.Revisions = []DeckRevision{}
	}
	for i := range doc.Revisions {
		normDeck(&doc.Revisions[i].Deck)
	}
	if doc.Archetypes == nil {
		doc.Archetypes = []Archetype{}
	}
	for i := range doc.Archetypes {
		a := &doc.Archetypes[i]
		a.Base = a.Base.normalized()
		if a.PlayOverride != nil {
			p := a.PlayOverride.normalized()
			a.PlayOverride = &p
		}
		if a.DrawOverride != nil {
			p := a.DrawOverride.normalized()
			a.DrawOverride = &p
		}
	}
	if doc.Results == nil {
		doc.Results = []MatchResult{}
	}
}
