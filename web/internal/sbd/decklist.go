package sbd

import (
	"regexp"
	"strconv"
	"strings"
)

// Plaintext decklist parsing (port of src/sideboarder/decklist.py).
//
// The canonical shape is `<qty> <name>` lines, the mainboard first, then a
// blank line, then the sideboard (Moxfield "MTGO" export). Explicit section
// headers (Deck, Sideboard, Commander …), `SB:` line prefixes and trailing
// set/collector annotations such as `(2X2) 117` or foil markers `*F*` are
// tolerated.

var (
	// "4 Lightning Bolt", "4x Lightning Bolt", "4 Lightning Bolt (2X2) 117 *F*"
	cardRE = regexp.MustCompile(`^(\d+)\s*[xX]?\s+(.+?)\s*$`)
	// trailing " (SET) 123" or " (SET)" annotations
	setAnnotRE = regexp.MustCompile(`\s+\([^)]+\)(?:\s+\S+)?\s*$`)
	// trailing foil/etched markers like "*F*"
	foilRE = regexp.MustCompile(`\s+\*[A-Za-z]+\*\s*$`)

	mainHeaders = map[string]bool{
		"deck": true, "maindeck": true, "main": true, "mainboard": true,
		"commander": true, "companion": true,
	}
	sideHeaders = map[string]bool{"sideboard": true, "side": true, "sb": true}
)

// ParseResult is a parsed deck plus any lines that were not understood.
type ParseResult struct {
	Deck     Deck     `json:"deck"`
	Unparsed []string `json:"unparsed"`
}

func cleanName(name string) string {
	prev := ""
	for prev != name {
		prev = name
		name = setAnnotRE.ReplaceAllString(name, "")
		name = foilRE.ReplaceAllString(name, "")
	}
	return strings.TrimSpace(name)
}

// header returns "main" or "side" if the line is a section header, else "".
func header(line string) string {
	token := Fold(strings.TrimRight(strings.TrimSpace(line), ":"))
	switch {
	case mainHeaders[token]:
		return "main"
	case sideHeaders[token]:
		return "side"
	}
	return ""
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// ParseDecklist parses decklist text into a Deck plus any unparsed lines.
func ParseDecklist(text, deckName string) ParseResult {
	var main, side []CardEntry
	unparsed := []string{}

	section := "main"
	seenCardInSection := false
	explicitSection := false // a header was given, so blank lines no longer split

	for _, raw := range splitLines(text) {
		line := strings.TrimSpace(raw)
		if line == "" {
			// A blank line after mainboard cards implies the sideboard follows,
			// unless explicit headers are driving sectioning.
			if !explicitSection && section == "main" && seenCardInSection {
				section = "side"
				seenCardInSection = false
			}
			continue
		}

		if h := header(line); h != "" {
			section = h
			explicitSection = true
			seenCardInSection = false
			continue
		}

		target := section
		if len(line) >= 3 && Fold(line[:3]) == "sb:" {
			target = "side"
			line = strings.TrimSpace(line[3:])
		}

		m := cardRE.FindStringSubmatch(line)
		if m == nil {
			unparsed = append(unparsed, raw)
			continue
		}
		name := cleanName(m[2])
		qty, err := strconv.Atoi(m[1])
		if name == "" || err != nil {
			unparsed = append(unparsed, raw)
			continue
		}
		entry := CardEntry{Name: name, Qty: qty}
		if target == "side" {
			side = append(side, entry)
		} else {
			main = append(main, entry)
		}
		if target == section {
			seenCardInSection = true
		}
	}

	return ParseResult{
		Deck: Deck{
			Name:      deckName,
			Mainboard: MergeEntries(main),
			Sideboard: MergeEntries(side),
		},
		Unparsed: unparsed,
	}
}

// DeckText renders a plain-text decklist: mainboard, blank line, sideboard.
func DeckText(deck Deck) string {
	var lines []string
	for _, e := range deck.Mainboard {
		lines = append(lines, strconv.Itoa(e.Qty)+" "+e.Name)
	}
	lines = append(lines, "")
	for _, e := range deck.Sideboard {
		lines = append(lines, strconv.Itoa(e.Qty)+" "+e.Name)
	}
	return strings.Join(lines, "\n")
}
