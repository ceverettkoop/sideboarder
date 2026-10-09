package sbd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Tournament result aggregation (port of src/sideboarder/results.py):
// record and winrate per archetype.

// OverallLabel names the combined record across every match.
const OverallLabel = "(overall)"

// ArchetypeRecord is the W-L-D and game record against one archetype.
type ArchetypeRecord struct {
	Name      string `json:"name"`
	Wins      int    `json:"wins"`
	Losses    int    `json:"losses"`
	Draws     int    `json:"draws"`
	GamesWon  int    `json:"games_won"`
	GamesLost int    `json:"games_lost"`
}

// Matches is the number of matches in the record.
func (r ArchetypeRecord) Matches() int { return r.Wins + r.Losses + r.Draws }

// Winrate is wins / decisive matches (draws excluded); ok is false with no
// decisive matches.
func (r ArchetypeRecord) Winrate() (rate float64, ok bool) {
	decisive := r.Wins + r.Losses
	if decisive == 0 {
		return 0, false
	}
	return float64(r.Wins) / float64(decisive), true
}

// GamesText renders games won-lost, e.g. "5-3".
func (r ArchetypeRecord) GamesText() string { return fmt.Sprintf("%d-%d", r.GamesWon, r.GamesLost) }

// RecordText renders W-L-D, e.g. "3-1-0".
func (r ArchetypeRecord) RecordText() string {
	return fmt.Sprintf("%d-%d-%d", r.Wins, r.Losses, r.Draws)
}

// WinrateText renders the winrate as a whole percentage, or "—".
func (r ArchetypeRecord) WinrateText() string {
	rate, ok := r.Winrate()
	if !ok {
		return "—"
	}
	return strconv.FormatFloat(rate*100, 'f', 0, 64) + "%"
}

// BuildRecords returns per-archetype records (case-insensitive grouping),
// sorted by matches desc, then name.
func BuildRecords(results []MatchResult) []ArchetypeRecord {
	table := map[string]*ArchetypeRecord{}
	var recs []*ArchetypeRecord
	for _, res := range results {
		name := strings.TrimSpace(res.Archetype)
		if name == "" {
			name = "(unknown)"
		}
		key := Fold(name)
		rec, ok := table[key]
		if !ok {
			rec = &ArchetypeRecord{Name: name}
			table[key] = rec
			recs = append(recs, rec)
		}
		switch res.Result() {
		case ResultWin:
			rec.Wins++
		case ResultLoss:
			rec.Losses++
		case ResultDraw:
			rec.Draws++
		}
		rec.GamesWon += res.GamesWon
		rec.GamesLost += res.GamesLost
	}
	sort.SliceStable(recs, func(i, j int) bool {
		if mi, mj := recs[i].Matches(), recs[j].Matches(); mi != mj {
			return mi > mj
		}
		return Fold(recs[i].Name) < Fold(recs[j].Name)
	})
	out := make([]ArchetypeRecord, len(recs))
	for i, r := range recs {
		out[i] = *r
	}
	return out
}

// OverallRecord is one combined record across every match.
func OverallRecord(results []MatchResult) ArchetypeRecord {
	total := ArchetypeRecord{Name: OverallLabel}
	for _, rec := range BuildRecords(results) {
		total.Wins += rec.Wins
		total.Losses += rec.Losses
		total.Draws += rec.Draws
		total.GamesWon += rec.GamesWon
		total.GamesLost += rec.GamesLost
	}
	return total
}

// ResultsCSV renders raw match results as CSV text.
func ResultsCSV(results []MatchResult) string {
	records := [][]string{{
		"date", "event", "archetype", "play_draw", "games_won", "games_lost",
		"result", "deck_revision", "notes",
	}}
	for _, r := range results {
		rev := ""
		if r.DeckRevision != nil {
			rev = strconv.Itoa(*r.DeckRevision)
		}
		records = append(records, []string{
			r.Date, r.Event, r.Archetype, r.PlayDraw,
			strconv.Itoa(r.GamesWon), strconv.Itoa(r.GamesLost),
			r.Result(), rev, r.Notes,
		})
	}
	return writeCSV(records)
}
