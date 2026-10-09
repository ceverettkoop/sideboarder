package sbd

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Decoding mirrors the Python from_dict() methods rather than strict
// encoding/json struct decoding: missing keys take defaults, numbers may be
// given as numeric strings, and legacy fields (W/L/D match results) upgrade.

type dict = map[string]any

// ParseDocument decodes *.sbd.json bytes with the same leniency as the TUI.
func ParseDocument(data []byte) (*Document, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	m, ok := raw.(dict)
	if !ok {
		return nil, fmt.Errorf("document must be a JSON object")
	}
	return documentFromDict(m)
}

func documentFromDict(data dict) (doc *Document, err error) {
	defer recoverDecode(&err)
	version := pyInt(get(data, "schema_version", float64(SchemaVersion)))
	if version > SchemaVersion {
		return nil, fmt.Errorf(
			"Document schema_version %d is newer than supported %d; upgrade Sideboarder.",
			version, SchemaVersion)
	}
	doc = &Document{
		SchemaVersion: version,
		Deck:          deckFromDict(asDict(get(data, "deck", dict{}))),
		DeckRevision:  pyInt(get(data, "deck_revision", float64(1))),
		DeckModified:  truthy(get(data, "deck_modified", false)),
	}
	for _, a := range asList(get(data, "archetypes", nil)) {
		doc.Archetypes = append(doc.Archetypes, archetypeFromDict(asDict(a)))
	}
	for _, r := range asList(get(data, "results", nil)) {
		doc.Results = append(doc.Results, resultFromDict(asDict(r)))
	}
	for _, r := range asList(get(data, "revisions", nil)) {
		rd := asDict(r)
		doc.Revisions = append(doc.Revisions, DeckRevision{
			Revision: pyInt(require(rd, "revision")),
			Deck:     deckFromDict(asDict(get(rd, "deck", dict{}))),
			SavedAt:  pyStr(get(rd, "saved_at", "")),
		})
	}
	doc.Normalize()
	return doc, nil
}

func entriesFromList(v any) []CardEntry {
	out := []CardEntry{}
	for _, item := range asList(v) {
		d := asDict(item)
		out = append(out, CardEntry{Name: pyStr(require(d, "name")), Qty: pyInt(get(d, "qty", float64(1)))})
	}
	return out
}

func planFromDict(v any) Plan {
	d := asDict(v) // None -> {}
	return Plan{Out: entriesFromList(get(d, "out", nil)), In: entriesFromList(get(d, "in", nil))}
}

func optionalPlan(data dict, key string) *Plan {
	v := get(data, key, nil)
	if !truthy(v) {
		return nil
	}
	p := planFromDict(v)
	return &p
}

func archetypeFromDict(data dict) Archetype {
	id := get(data, "id", nil)
	idStr := NewID()
	if truthy(id) {
		idStr = pyStr(id)
	}
	return Archetype{
		ID:           idStr,
		Name:         pyStr(require(data, "name")),
		Notes:        pyStr(get(data, "notes", "")),
		Base:         planFromDict(get(data, "base", nil)),
		PlayOverride: optionalPlan(data, "play_override"),
		DrawOverride: optionalPlan(data, "draw_override"),
	}
}

func resultFromDict(data dict) MatchResult {
	var won, lost int
	_, hasWon := data["games_won"]
	_, hasLost := data["games_lost"]
	if hasWon || hasLost {
		won, lost = pyInt(get(data, "games_won", float64(0))), pyInt(get(data, "games_lost", float64(0)))
	} else { // documents written before matches were scored in games
		key := strings.ToUpper(strings.TrimSpace(pyStr(get(data, "result", ResultWin))))
		if len(key) > 1 {
			key = key[:1]
		}
		score := legacyScores[key]
		won, lost = score[0], score[1]
	}
	playDraw, err := ParsePlayDraw(pyStr(get(data, "play_draw", "")))
	if err != nil {
		panic(decodeError{err})
	}
	id := NewID()
	if v := get(data, "id", nil); truthy(v) {
		id = pyStr(v)
	}
	res := MatchResult{
		ID:        id,
		Date:      pyStr(get(data, "date", "")),
		Event:     pyStr(get(data, "event", "")),
		Archetype: pyStr(get(data, "archetype", "")),
		GamesWon:  won,
		GamesLost: lost,
		PlayDraw:  playDraw,
		Notes:     pyStr(get(data, "notes", "")),
	}
	if rev, ok := data["deck_revision"]; ok && rev != nil {
		n := pyInt(rev)
		res.DeckRevision = &n
	}
	return res
}

func deckFromDict(data dict) Deck {
	return Deck{
		Name:      pyStr(get(data, "name", "Untitled")),
		Format:    pyStr(get(data, "format", "")),
		Mainboard: entriesFromList(get(data, "mainboard", nil)),
		Sideboard: entriesFromList(get(data, "sideboard", nil)),
	}
}

// ----- Python-ish value helpers ---------------------------------------------

type decodeError struct{ err error }

func recoverDecode(err *error) {
	if r := recover(); r != nil {
		de, ok := r.(decodeError)
		if !ok {
			panic(r)
		}
		*err = de.err
	}
}

func get(d dict, key string, def any) any {
	if v, ok := d[key]; ok {
		return v
	}
	return def
}

func require(d dict, key string) any {
	v, ok := d[key]
	if !ok {
		panic(decodeError{fmt.Errorf("missing required key %q", key)})
	}
	return v
}

func asDict(v any) dict {
	if v == nil {
		return dict{}
	}
	d, ok := v.(dict)
	if !ok {
		panic(decodeError{fmt.Errorf("expected an object, got %T", v)})
	}
	return d
}

func asList(v any) []any {
	if v == nil {
		return nil
	}
	l, ok := v.([]any)
	if !ok {
		panic(decodeError{fmt.Errorf("expected a list, got %T", v)})
	}
	return l
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case dict:
		return len(x) > 0
	}
	return true
}

func pyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func pyInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			panic(decodeError{fmt.Errorf("invalid integer %q", x)})
		}
		return n
	}
	panic(decodeError{fmt.Errorf("expected an integer, got %T", v)})
}
