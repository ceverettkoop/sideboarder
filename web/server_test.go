package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ceverettkoop/sideboarder/web/internal/carddb"
)

type testServer struct {
	*httptest.Server
	dir string
	t   *testing.T
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // keep settings writes out of $HOME
	dir := t.TempDir()
	static := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>hi")}}
	db := carddb.New(filepath.Join(t.TempDir(), "cardnames.json"))
	ts := httptest.NewServer(newServer(dir, db, static).routes())
	t.Cleanup(ts.Close)
	return &testServer{Server: ts, dir: dir, t: t}
}

func (ts *testServer) do(method, path string, body any) (int, map[string]any) {
	ts.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestCreateEditAndReload(t *testing.T) {
	ts := newTestServer(t)
	code, res := ts.do("POST", "/api/files", map[string]string{"deck_name": "Mono-Red Burn!"})
	if code != 200 || res["file"] != "Mono-Red_Burn.sbd.json" {
		t.Fatalf("create: %d %v", code, res)
	}
	// Same name again gets a fresh file rather than overwriting.
	if _, res2 := ts.do("POST", "/api/files", map[string]string{"deck_name": "Mono-Red Burn"}); res2["file"] != "Mono-Red_Burn-2.sbd.json" {
		t.Fatalf("second create: %v", res2)
	}

	ops := "/api/files/Mono-Red_Burn.sbd.json/ops"
	code, res = ts.do("POST", ops, map[string]any{"op": "import_deck", "name": "Burn", "text": "4 Lightning Bolt\n\n3 Smash to Smithereens\n"})
	if code != 200 {
		t.Fatalf("import: %d %v", code, res)
	}
	code, res = ts.do("POST", ops, map[string]any{"op": "add_archetype", "name": "Control"})
	archID := res["result"].(map[string]any)["message"].(string)
	code, res = ts.do("POST", ops, map[string]any{"op": "plan_add", "archetype_id": archID, "layer": "base", "list": "out", "name": "Lightning Bolt", "qty": 2})
	if code != 200 {
		t.Fatalf("plan_add: %d %v", code, res)
	}
	view := res["view"].(map[string]any)
	play := view["matchups"].(map[string]any)[archID].(map[string]any)["play"].(map[string]any)
	if play["validation"].(map[string]any)["balanced"] != false {
		t.Fatalf("expected unbalanced plan: %v", play)
	}
	code, res = ts.do("POST", ops, map[string]any{"op": "result_add", "result": map[string]string{"archetype": "Control", "games": "2-1", "play_draw": "play"}})
	if code != 200 || view == nil {
		t.Fatalf("result_add: %d %v", code, res)
	}
	if code, res = ts.do("POST", ops, map[string]any{"op": "result_add", "result": map[string]string{"games": "W"}}); code != 400 {
		t.Fatalf("bad score: %d %v", code, res)
	}

	// The file on disk is a normal TUI document.
	data, err := os.ReadFile(filepath.Join(ts.dir, "Mono-Red_Burn.sbd.json"))
	if err != nil || !strings.Contains(string(data), `"games_won": 2`) {
		t.Fatalf("saved file: %s %v", data, err)
	}
	code, res = ts.do("GET", "/api/files/Mono-Red_Burn.sbd.json", nil)
	doc := res["view"].(map[string]any)["doc"].(map[string]any)
	if code != 200 || doc["deck"].(map[string]any)["name"] != "Burn" || len(doc["results"].([]any)) != 1 {
		t.Fatalf("reload: %d %v", code, res)
	}

	code, res = ts.do("GET", "/api/files", nil)
	if code != 200 || len(res["files"].([]any)) != 2 {
		t.Fatalf("list: %d %v", code, res)
	}
	code, res = ts.do("GET", "/api/files/Mono-Red_Burn.sbd.json/report?mode=base", nil)
	if code != 200 || len(res["rows"].([]any)) != 1 {
		t.Fatalf("report: %d %v", code, res)
	}
	code, res = ts.do("POST", "/api/files/Mono-Red_Burn.sbd.json/copy", map[string]string{"file": "backup"})
	if code != 200 || res["file"] != "backup.sbd.json" {
		t.Fatalf("copy: %d %v", code, res)
	}
}

func TestCSVAndDownload(t *testing.T) {
	ts := newTestServer(t)
	ts.do("POST", "/api/files", map[string]string{"deck_name": "x"})
	for path, want := range map[string]string{
		"/api/files/x.sbd.json/results.csv":          "date,event,archetype",
		"/api/files/x.sbd.json/report.csv?mode=play": "card,out_count",
		"/api/files/x.sbd.json/download":             `"schema_version": 1`,
	} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), want) ||
			!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
			t.Fatalf("%s: %d %q %q", path, resp.StatusCode, body, resp.Header.Get("Content-Disposition"))
		}
	}
}

func TestRejectsBadNamesAndNonJSONWrites(t *testing.T) {
	ts := newTestServer(t)
	for _, name := range []string{"..%2Fetc%2Fpasswd", "notes.txt", ".hidden.sbd.json"} {
		if code, _ := ts.do("GET", "/api/files/"+name, nil); code != 400 {
			t.Fatalf("%s: got %d", name, code)
		}
	}
	if code, _ := ts.do("GET", "/api/files/missing.sbd.json", nil); code != 404 {
		t.Fatalf("missing: got %d", code)
	}
	// A plain HTML form post (text/plain) from another site must not write.
	resp, err := http.Post(ts.URL+"/api/files", "text/plain", strings.NewReader(`{"deck_name":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain write: got %d", resp.StatusCode)
	}
	if entries, _ := os.ReadDir(ts.dir); len(entries) != 0 {
		t.Fatalf("files written: %v", entries)
	}
}

func TestOpensTUIDocument(t *testing.T) {
	ts := newTestServer(t)
	// A document as the Python app writes it (ASCII-escaped, legacy result field).
	tui := `{"schema_version": 1, "deck": {"name": "Burn — v2", "format": "", "mainboard": [{"name": "Lightning Bolt", "qty": 4}], "sideboard": []},
	  "archetypes": [{"id": "abc", "name": "Control", "notes": "", "base": {"out": [], "in": []}}],
	  "results": [{"id": "r", "date": "2026-08-01", "event": "", "archetype": "Control", "result": "W", "notes": ""}]}`
	_ = os.WriteFile(filepath.Join(ts.dir, "burn.sbd.json"), []byte(tui), 0o644)
	code, res := ts.do("GET", "/api/files/burn.sbd.json", nil)
	if code != 200 {
		t.Fatalf("%d %v", code, res)
	}
	v := res["view"].(map[string]any)
	if v["doc"].(map[string]any)["deck"].(map[string]any)["name"] != "Burn — v2" || v["overall"].(map[string]any)["record_text"] != "1-0-0" {
		t.Fatalf("got %v", v)
	}
}

func TestStaticAndCards(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "hi") || resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("static: %d %q", resp.StatusCode, body)
	}
	resp, err = http.Get(ts.URL + "/api/cards?q=bolt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("cards without a database: %q", body)
	}
}

func TestResolveListenAddr(t *testing.T) {
	if got, err := resolveListenAddr("127.0.0.1:9000"); err != nil || got != "127.0.0.1:9000" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := resolveListenAddr("nonsense"); err == nil {
		t.Fatal("accepted an address without a port")
	}
}
