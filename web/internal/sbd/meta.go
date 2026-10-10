package sbd

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Metagame builder: from the deck you want after sideboarding in each
// matchup (its "target") and each matchup's share of the field, suggest a
// 60-card main deck and 15-card sideboard, plus the plan for every matchup.
//
// The model counts a "miss" for each target card you can't play. Against
// matchup m (weight w_m, target d_m) game 1 is played with the main deck M,
// and after boarding the best reachable list is drawn from M+S, so the
// expected misses are
//
//	Σ_m w_m · ( Σ_c max(0, d_m[c] − M[c])  +  k · Σ_c max(0, d_m[c] − (M+S)[c]) )
//
// for some weight k on post-board games. Both sums split per card copy: the
// j-th copy of card c saves one miss in exactly the matchups whose target
// runs at least j copies, so its value is v(c, j) = Σ_{m: d_m[c] ≥ j} w_m, the
// share of the field that wants it. A card's values only fall as j grows, so
// giving the main deck the 60 most valuable copies and the sideboard the next
// 15 minimises both sums at once, whatever k is. The 4-copy limit holds
// automatically: no copy beyond what some target runs has any value.

// Deck sizes the builder fills.
const (
	MainSize = 60
	SideSize = 15
)

// SuggestedCard is one card of the suggested 75 with the numbers behind it.
type SuggestedCard struct {
	Name        string    `json:"name"`
	Main        int       `json:"main"`
	Side        int       `json:"side"`
	CurrentMain int       `json:"current_main"`
	CurrentSide int       `json:"current_side"`
	Average     float64   `json:"average"` // share-weighted copies across targets
	Wanted      []float64 `json:"wanted"`  // % of the field wanting ≥1, ≥2, … copies
}

// MatchupFit says how well the suggested 75 serves one matchup.
type MatchupFit struct {
	ArchetypeID string      `json:"archetype_id"`
	Name        string      `json:"name"`
	Weight      float64     `json:"weight"` // % of the planned field
	TargetSize  int         `json:"target_size"`
	Game1       int         `json:"game1"`     // target cards already in the main deck
	Postboard   int         `json:"postboard"` // target cards playable after boarding
	Swaps       int         `json:"swaps"`
	Plan        Plan        `json:"plan"`    // OUT/IN from the suggested main deck
	Missing     []CardEntry `json:"missing"` // target cards the 75 can't supply
}

// Suggestion is the builder's output.
type Suggestion struct {
	Ready     bool            `json:"ready"`
	Main      []CardEntry     `json:"main"`
	Side      []CardEntry     `json:"side"`
	Cards     []SuggestedCard `json:"cards"`
	Matchups  []MatchupFit    `json:"matchups"`
	Game1Rate float64         `json:"game1_rate"`     // weighted % of target cards in game 1
	PostRate  float64         `json:"postboard_rate"` // weighted % after boarding
	AvgSwaps  float64         `json:"avg_swaps"`      // weighted cards swapped per matchup
	Warnings  []string        `json:"warnings"`
}

type copyUnit struct {
	key   string
	copy  int
	value float64
	avg   float64
}

const eps = 1e-9

// Suggest computes the suggested main deck, sideboard and plans for doc.
func Suggest(doc *Document) Suggestion {
	s := Suggestion{
		Main: []CardEntry{}, Side: []CardEntry{}, Cards: []SuggestedCard{},
		Matchups: []MatchupFit{}, Warnings: []string{},
	}
	var targeted []*Archetype
	for i := range doc.Archetypes {
		a := &doc.Archetypes[i]
		if len(a.TargetDeck) > 0 {
			targeted = append(targeted, a)
		} else if a.MetaShare != nil && *a.MetaShare > 0 {
			s.Warnings = append(s.Warnings, fmt.Sprintf(
				"%s (%s of the field) has no post-board deck yet, so it isn't counted.", a.Name, pct(*a.MetaShare)))
		}
	}
	if len(targeted) == 0 {
		return s
	}
	s.Ready = true

	weights := matchupWeights(targeted, &s.Warnings)

	// Per-card target counts, keyed by folded name; display names prefer the
	// current deck's spelling.
	display := map[string]string{}
	for _, e := range append(append([]CardEntry{}, doc.Deck.Mainboard...), doc.Deck.Sideboard...) {
		if _, ok := display[Fold(e.Name)]; !ok {
			display[Fold(e.Name)] = e.Name
		}
	}
	counts := make([]map[string]int, len(targeted))
	var keys []string
	for i, a := range targeted {
		counts[i] = map[string]int{}
		for _, e := range MergeEntries(a.TargetDeck) {
			k := Fold(e.Name)
			counts[i][k] = e.Qty
			if _, ok := display[k]; !ok {
				display[k] = e.Name
			}
			if !contains(keys, k) {
				keys = append(keys, k)
			}
		}
		if size := TotalQty(a.TargetDeck); size != MainSize {
			s.Warnings = append(s.Warnings, fmt.Sprintf(
				"The post-board deck for %s has %d cards, not %d.", a.Name, size, MainSize))
		}
	}

	// Value every copy of every card, best first.
	var units []copyUnit
	avg := map[string]float64{}
	wanted := map[string][]float64{}
	for _, k := range keys {
		most := 0
		for i := range targeted {
			avg[k] += weights[i] * float64(counts[i][k])
			most = max(most, counts[i][k])
		}
		for j := 1; j <= most; j++ {
			v := 0.0
			for i := range targeted {
				if counts[i][k] >= j {
					v += weights[i]
				}
			}
			wanted[k] = append(wanted[k], round1(v*100))
			units = append(units, copyUnit{key: k, copy: j, value: v})
		}
	}
	sort.SliceStable(units, func(a, b int) bool {
		ua, ub := units[a], units[b]
		if math.Abs(ua.value-ub.value) > eps {
			return ua.value > ub.value
		}
		if math.Abs(avg[ua.key]-avg[ub.key]) > eps {
			return avg[ua.key] > avg[ub.key]
		}
		if ua.key != ub.key {
			return ua.key < ub.key
		}
		return ua.copy < ub.copy
	})

	main, side := map[string]int{}, map[string]int{}
	var order []string // cards by their most valuable copy
	for i, u := range units {
		if !contains(order, u.key) {
			order = append(order, u.key)
		}
		switch {
		case i < MainSize:
			main[u.key]++
		case i < MainSize+SideSize:
			side[u.key]++
		}
	}
	if n := min(len(units), MainSize); n < MainSize {
		s.Warnings = append(s.Warnings, fmt.Sprintf(
			"The post-board decks only use %d different card copies, so the main deck is %d short.", n, MainSize-n))
	} else if extra := len(units) - MainSize; extra < SideSize {
		s.Warnings = append(s.Warnings, fmt.Sprintf(
			"Only %d sideboard slots are needed; the other %d are free.", extra, SideSize-extra))
	}

	cur := func(entries []CardEntry) map[string]int {
		m := map[string]int{}
		for _, e := range entries {
			m[Fold(e.Name)] += e.Qty
		}
		return m
	}
	curMain, curSide := cur(doc.Deck.Mainboard), cur(doc.Deck.Sideboard)
	for _, k := range order {
		if main[k] > 0 {
			s.Main = append(s.Main, CardEntry{Name: display[k], Qty: main[k]})
		}
		if side[k] > 0 {
			s.Side = append(s.Side, CardEntry{Name: display[k], Qty: side[k]})
		}
		s.Cards = append(s.Cards, SuggestedCard{
			Name: display[k], Main: main[k], Side: side[k],
			CurrentMain: curMain[k], CurrentSide: curSide[k],
			Average: round1(avg[k]), Wanted: wanted[k],
		})
	}

	for i, a := range targeted {
		fit := fitMatchup(a, counts[i], main, side, order, display)
		fit.Weight = round1(weights[i] * 100)
		s.Matchups = append(s.Matchups, fit)
		if fit.TargetSize > 0 {
			s.Game1Rate += weights[i] * float64(fit.Game1) / float64(fit.TargetSize)
			s.PostRate += weights[i] * float64(fit.Postboard) / float64(fit.TargetSize)
		}
		s.AvgSwaps += weights[i] * float64(fit.Swaps)
	}
	s.Game1Rate, s.PostRate, s.AvgSwaps = round1(s.Game1Rate*100), round1(s.PostRate*100), round1(s.AvgSwaps)
	return s
}

// matchupWeights normalises the targeted matchups' shares to sum to 1. With no
// shares entered, every matchup counts equally.
func matchupWeights(targeted []*Archetype, warnings *[]string) []float64 {
	w := make([]float64, len(targeted))
	total := 0.0
	for i, a := range targeted {
		if a.MetaShare != nil && *a.MetaShare > 0 {
			w[i] = *a.MetaShare
			total += w[i]
		}
	}
	if total == 0 {
		for i := range w {
			w[i] = 1 / float64(len(w))
		}
		if len(w) > 1 {
			*warnings = append(*warnings, "No metagame shares entered, so every matchup counts equally.")
		}
		return w
	}
	for i, a := range targeted {
		if w[i] == 0 {
			*warnings = append(*warnings, fmt.Sprintf(
				"%s has a post-board deck but no metagame share, so it isn't counted.", a.Name))
		}
		w[i] /= total
	}
	return w
}

// fitMatchup builds the closest reachable post-board deck for one matchup and
// the sideboard plan that gets there from the suggested main deck.
func fitMatchup(a *Archetype, want, main, side map[string]int, order []string, display map[string]string) MatchupFit {
	fit := MatchupFit{
		ArchetypeID: a.ID, Name: a.Name, TargetSize: TotalQty(a.TargetDeck),
		Plan: Plan{Out: []CardEntry{}, In: []CardEntry{}}, Missing: []CardEntry{},
	}
	post := map[string]int{}
	size := 0
	for _, k := range order {
		post[k] = min(want[k], main[k]+side[k])
		size += post[k]
		fit.Game1 += min(want[k], main[k])
	}
	fit.Postboard = size
	// Short of a full deck (the 75 lacks some target cards): keep main-deck
	// cards first so the plan swaps as little as possible, then use spare
	// sideboard cards.
	for _, k := range order {
		for size < MainSize && post[k] < main[k] {
			post[k]++
			size++
		}
	}
	for _, k := range order {
		for size < MainSize && post[k] < main[k]+side[k] {
			post[k]++
			size++
		}
	}
	for _, k := range order {
		if d := main[k] - post[k]; d > 0 {
			fit.Plan.Out = append(fit.Plan.Out, CardEntry{Name: display[k], Qty: d})
		}
		if d := post[k] - main[k]; d > 0 {
			fit.Plan.In = append(fit.Plan.In, CardEntry{Name: display[k], Qty: d})
			fit.Swaps += d
		}
	}
	for _, e := range MergeEntries(a.TargetDeck) {
		if d := e.Qty - post[Fold(e.Name)]; d > 0 {
			fit.Missing = append(fit.Missing, CardEntry{Name: e.Name, Qty: d})
		}
	}
	return fit
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

func pct(x float64) string {
	return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", x), "0"), ".") + "%"
}
