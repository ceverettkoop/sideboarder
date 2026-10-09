// Package carddb is the local card-name database used for autocomplete
// (port of src/sideboarder/carddb.py).
//
// A provider downloads a full card dataset once, from which a compact, sorted
// list of unique card names is distilled and stored locally (cardnames.json,
// shared with the TUI). Autocomplete then runs offline against that list.
// Updates are manual, never automatic.
package carddb

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Download URLs. MTGJSON is fetched gzip-compressed (the TUI uses the .xz
// variant, which Go's standard library can't decode; the content is the same).
const (
	MTGJSONAtomicGz   = "https://mtgjson.com/api/v5/AtomicCards.json.gz"
	ScryfallBulkIndex = "https://api.scryfall.com/bulk-data"
)

// Sources maps a source key to its human label.
var Sources = map[string]string{"mtgjson": "MTGJSON", "scryfall": "Scryfall"}

// ProgressFn receives human-readable progress messages.
type ProgressFn func(string)

// FetchFn downloads card names from one provider.
type FetchFn func(client *http.Client, progress ProgressFn) ([]string, error)

var fetchers = map[string]FetchFn{
	"mtgjson":  FetchMTGJSON,
	"scryfall": FetchScryfall,
}

func get(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sideboarder-web/0.1")
	req.Header.Set("Accept", "application/json;q=0.9,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

// FetchMTGJSON streams MTGJSON AtomicCards and returns the keys of its "data"
// object (the card names) without holding the whole dataset in memory.
func FetchMTGJSON(client *http.Client, progress ProgressFn) ([]string, error) {
	progress("Downloading MTGJSON AtomicCards…")
	resp, err := get(client, MTGJSONAtomicGz)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	progress("Decompressing and reading card names…")
	return dataKeys(gz)
}

// dataKeys returns the keys of the top-level "data" object of a JSON stream.
func dataKeys(r io.Reader) ([]string, error) {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("unexpected MTGJSON format")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if key, _ := tok.(string); key != "data" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, err
			}
			continue
		}
		if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
			return nil, errors.New("unexpected MTGJSON data format")
		}
		var names []string
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			names = append(names, tok.(string))
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, err
			}
		}
		return names, nil
	}
	return nil, errors.New("MTGJSON file has no data object")
}

// FetchScryfall downloads Scryfall's oracle-cards bulk file and returns its names.
func FetchScryfall(client *http.Client, progress ProgressFn) ([]string, error) {
	progress("Locating Scryfall bulk data…")
	resp, err := get(client, ScryfallBulkIndex)
	if err != nil {
		return nil, err
	}
	var index struct {
		Data []struct {
			Type        string `json:"type"`
			DownloadURI string `json:"download_uri"`
		} `json:"data"`
	}
	err = json.NewDecoder(resp.Body).Decode(&index)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	uri := ""
	for _, e := range index.Data {
		if e.Type == "oracle_cards" {
			uri = e.DownloadURI
		}
	}
	if uri == "" {
		return nil, errors.New("Scryfall bulk-data has no oracle_cards entry")
	}
	progress("Downloading Scryfall oracle cards…")
	resp, err = get(client, uri)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil, errors.New("unexpected Scryfall format")
	}
	var names []string
	for dec.More() {
		var card struct {
			Name *string `json:"name"`
		}
		if err := dec.Decode(&card); err != nil {
			return nil, err
		}
		if card.Name != nil {
			names = append(names, *card.Name)
		}
	}
	return names, nil
}

// Distill de-duplicates (case-insensitively) and sorts card names.
func Distill(names []string) []string {
	seen := map[string]string{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(n)]; !ok {
			seen[strings.ToLower(n)] = n
		}
	}
	out := make([]string, 0, len(seen))
	for _, n := range seen {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i]), strings.ToLower(out[j])
		if li != lj {
			return li < lj
		}
		return out[i] < out[j]
	})
	return out
}

// DB is an in-memory card-name index backed by a local JSON file.
type DB struct {
	Path string

	mu      sync.RWMutex
	source  string
	updated string
	names   []string
	lower   []string
}

// New returns a DB backed by path (not yet loaded).
func New(path string) *DB { return &DB{Path: path} }

type fileFormat struct {
	Source  string   `json:"source"`
	Updated string   `json:"updated"`
	Names   []string `json:"names"`
}

// Status describes the loaded database.
type Status struct {
	Available bool   `json:"available"`
	Count     int    `json:"count"`
	Source    string `json:"source"`
	Updated   string `json:"updated"`
}

// Status reports what is currently loaded.
func (db *DB) Status() Status {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return Status{Available: len(db.names) > 0, Count: len(db.names), Source: db.source, Updated: db.updated}
}

// Load reads names from disk if present and returns the number loaded.
func (db *DB) Load() int {
	var f fileFormat
	data, err := os.ReadFile(db.Path)
	if err == nil {
		err = json.Unmarshal(data, &f)
	}
	if err != nil {
		f = fileFormat{}
	}
	db.set(f)
	return len(f.Names)
}

func (db *DB) set(f fileFormat) {
	lower := make([]string, len(f.Names))
	for i, n := range f.Names {
		lower[i] = strings.ToLower(n)
	}
	db.mu.Lock()
	db.source, db.updated, db.names, db.lower = f.Source, f.Updated, f.Names, lower
	db.mu.Unlock()
}

// Update fetches from source, distills, persists and reloads. Returns the count.
// A nil fetch uses the built-in provider for source.
func (db *DB) Update(source string, progress ProgressFn, fetch FetchFn) (int, error) {
	if fetch == nil {
		fetch = fetchers[source]
	}
	if fetch == nil {
		return 0, fmt.Errorf("unknown card source: %q", source)
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	raw, err := fetch(client, progress)
	if err != nil {
		return 0, err
	}
	names := Distill(raw)
	progress(fmt.Sprintf("Indexing %d card names…", len(names)))
	f := fileFormat{
		Source:  source,
		Updated: time.Now().UTC().Format("2006-01-02T15:04:05+00:00"),
		Names:   names,
	}
	data, err := json.Marshal(f)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(db.Path), 0o755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(db.Path, data, 0o644); err != nil {
		return 0, err
	}
	db.set(f)
	return len(names), nil
}

// Autocomplete returns up to limit matches: prefix matches first, then substring.
func (db *DB) Autocomplete(query string, limit int) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	out := []string{}
	if q == "" {
		return out
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	var prefix, contains []string
	for i, low := range db.lower {
		if strings.HasPrefix(low, q) {
			prefix = append(prefix, db.names[i])
		} else if strings.Contains(low, q) {
			contains = append(contains, db.names[i])
		}
		if len(prefix) >= limit {
			break
		}
	}
	out = append(append(out, prefix...), contains...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
