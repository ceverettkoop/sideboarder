package sbd

import (
	"bytes"
	"encoding/csv"
	"sort"
	"strconv"
)

// Frequency report (port of src/sideboarder/report.py): how often each card
// is boarded in / out across matchups.

// Counting modes for the frequency report.
const (
	ModeBase = "base" // count each archetype's base plan once
	ModePlay = "play" // count effective plan on the play
	ModeDraw = "draw" // count effective plan on the draw
)

// CardFrequency is one row of the frequency report.
type CardFrequency struct {
	Name     string `json:"name"`
	OutCount int    `json:"out_count"` // number of matchups this card goes OUT in
	InCount  int    `json:"in_count"`  // number of matchups this card comes IN in
	OutQty   int    `json:"out_qty"`   // total copies removed across matchups
	InQty    int    `json:"in_qty"`    // total copies added across matchups
}

func planFor(a Archetype, mode string) Plan {
	switch mode {
	case ModePlay:
		return a.Effective(true)
	case ModeDraw:
		return a.Effective(false)
	}
	return a.Base
}

// BuildFrequency returns per-card frequencies, sorted by total involvement
// (desc), then name.
func BuildFrequency(archetypes []Archetype, mode string) []CardFrequency {
	table := map[string]*CardFrequency{}
	var rows []*CardFrequency
	row := func(name string) *CardFrequency {
		key := Fold(name)
		if r, ok := table[key]; ok {
			return r
		}
		r := &CardFrequency{Name: name}
		table[key] = r
		rows = append(rows, r)
		return r
	}
	for _, a := range archetypes {
		plan := planFor(a, mode)
		for _, e := range plan.Out {
			r := row(e.Name)
			r.OutCount++
			r.OutQty += e.Qty
		}
		for _, e := range plan.In {
			r := row(e.Name)
			r.InCount++
			r.InQty += e.Qty
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ti, tj := rows[i].OutCount+rows[i].InCount, rows[j].OutCount+rows[j].InCount
		if ti != tj {
			return ti > tj
		}
		return Fold(rows[i].Name) < Fold(rows[j].Name)
	})
	out := make([]CardFrequency, len(rows))
	for i, r := range rows {
		out[i] = *r
	}
	return out
}

// FrequencyCSV renders frequency rows as CSV text.
func FrequencyCSV(rows []CardFrequency) string {
	records := [][]string{{"card", "out_count", "in_count", "out_qty", "in_qty"}}
	for _, r := range rows {
		records = append(records, []string{
			r.Name, strconv.Itoa(r.OutCount), strconv.Itoa(r.InCount),
			strconv.Itoa(r.OutQty), strconv.Itoa(r.InQty),
		})
	}
	return writeCSV(records)
}

// writeCSV matches Python's csv.writer defaults (CRLF line endings).
func writeCSV(records [][]string) string {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.UseCRLF = true
	_ = w.WriteAll(records)
	return buf.String()
}
