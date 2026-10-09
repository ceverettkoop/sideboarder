//go:build js && wasm

// The in-browser demo build: the same server, compiled to WebAssembly, with
// documents kept in memory (and mirrored to localStorage by demo/demo.js).
// Build it with demo/build.sh.
package main

import (
	"io"
	"net/http/httptest"
	"strings"
	"syscall/js"
	"testing/fstest"

	"github.com/ceverettkoop/sideboarder/web/internal/carddb"
	"github.com/ceverettkoop/sideboarder/web/internal/sbd"
)

func main() {
	store := newMemStore()
	if saved := js.Global().Get("sideboarderSaved"); saved.Truthy() {
		keys := js.Global().Get("Object").Call("keys", saved)
		for i := 0; i < keys.Length(); i++ {
			name := keys.Index(i).String()
			if validName(name) {
				_ = store.Write(name, []byte(saved.Get(name).String()))
			}
		}
	}
	if docs, _ := store.List(); len(docs) == 0 {
		seedDemo(store)
	}
	store.onWrite = func(name string, data []byte) {
		if save := js.Global().Get("sideboarderSave"); save.Type() == js.TypeFunction {
			save.Invoke(name, string(data))
		}
	}

	db := carddb.New("")
	db.Use("demo", "built in", demoCardNames)
	handler := newServer(store, db, fstest.MapFS{}).routes()

	// sideboarderServe(method, url, body, contentType) -> Promise<{status, contentType, disposition, body}>
	js.Global().Set("sideboarderServe", js.FuncOf(func(this js.Value, args []js.Value) any {
		method, url, contentType := args[0].String(), args[1].String(), args[3].String()
		body := ""
		if args[2].Type() == js.TypeString {
			body = args[2].String()
		}
		return js.Global().Get("Promise").New(js.FuncOf(func(this js.Value, p []js.Value) any {
			resolve := p[0]
			go func() {
				req := httptest.NewRequest(method, url, strings.NewReader(body))
				if contentType != "" {
					req.Header.Set("Content-Type", contentType)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				out, _ := io.ReadAll(rec.Result().Body)
				res := js.Global().Get("Object").New()
				res.Set("status", rec.Code)
				res.Set("contentType", rec.Header().Get("Content-Type"))
				res.Set("disposition", rec.Header().Get("Content-Disposition"))
				res.Set("body", string(out))
				resolve.Invoke(res)
			}()
			return nil
		}))
	}))
	js.Global().Call("sideboarderReady")
	select {}
}

func seedDemo(store *memStore) {
	doc := sbd.NewDocument()
	steps := []sbd.Op{
		{Op: "import_deck", Name: "Mono-Red Burn", Text: demoDecklist},
		{Op: "set_deck_info", Name: "Mono-Red Burn", Format: "Modern"},
	}
	for _, op := range steps {
		_, _ = sbd.Apply(doc, op)
	}
	plans := map[string][]sbd.Op{
		"Azorius Control": {
			{Layer: "base", List: "out", Name: "Searing Blaze", Qty: 3},
			{Layer: "base", List: "in", Name: "Roiling Vortex", Qty: 2},
			{Layer: "base", List: "in", Name: "Smash to Smithereens", Qty: 1},
			{Layer: "play", List: "out", Name: "Skewer the Critics", Qty: 1},
			{Layer: "play", List: "in", Name: "Roiling Vortex", Qty: 1},
		},
		"Hammer Time": {
			{Layer: "base", List: "out", Name: "Lava Spike", Qty: 4},
			{Layer: "base", List: "in", Name: "Smash to Smithereens", Qty: 3},
			{Layer: "base", List: "in", Name: "Kor Firewalker", Qty: 1},
		},
		"Living End": {
			{Layer: "base", List: "out", Name: "Searing Blaze", Qty: 2},
			{Layer: "base", List: "in", Name: "Rest in Peace", Qty: 2},
			{Layer: "draw", List: "out", Name: "Skewer the Critics", Qty: 1},
			{Layer: "draw", List: "in", Name: "Path to Exile", Qty: 1},
		},
	}
	for _, name := range []string{"Azorius Control", "Hammer Time", "Living End"} {
		res, _ := sbd.Apply(doc, sbd.Op{Op: "add_archetype", Name: name})
		for _, op := range plans[name] {
			op.Op, op.ArchetypeID = "plan_add", res.Message
			_, _ = sbd.Apply(doc, op)
		}
	}
	results := []sbd.ResultInput{
		{Date: "2026-09-26", Event: "FNM", Archetype: "Hammer Time", PlayDraw: "play", Games: "2-1"},
		{Date: "2026-09-26", Event: "FNM", Archetype: "Azorius Control", PlayDraw: "draw", Games: "1-2", Notes: "Supreme Verdict on 4 both games"},
		{Date: "2026-09-26", Event: "FNM", Archetype: "Living End", PlayDraw: "play", Games: "2-0"},
	}
	for i := range results {
		_, _ = sbd.Apply(doc, sbd.Op{Op: "result_add", Result: &results[i]})
	}
	data, _ := sbd.Marshal(doc)
	_ = store.Write("Mono-Red_Burn.sbd.json", data)
}

const demoDecklist = `4 Goblin Guide
4 Monastery Swiftspear
4 Eidolon of the Great Revel
4 Lightning Bolt
4 Lava Spike
4 Rift Bolt
4 Boros Charm
4 Lightning Helix
3 Searing Blaze
3 Skewer the Critics
4 Inspiring Vantage
4 Sacred Foundry
4 Sunbaked Canyon
3 Arid Mesa
3 Bloodstained Mire
4 Mountain

3 Smash to Smithereens
3 Roiling Vortex
2 Rest in Peace
2 Kor Firewalker
2 Path to Exile
2 Sanctifier en-Vec
1 Deflecting Palm`

var demoCardNames = []string{
	"Goblin Guide", "Monastery Swiftspear", "Eidolon of the Great Revel", "Lightning Bolt",
	"Lava Spike", "Rift Bolt", "Boros Charm", "Lightning Helix", "Searing Blaze",
	"Skewer the Critics", "Inspiring Vantage", "Sacred Foundry", "Sunbaked Canyon", "Arid Mesa",
	"Bloodstained Mire", "Mountain", "Plains", "Smash to Smithereens", "Roiling Vortex",
	"Rest in Peace", "Kor Firewalker", "Path to Exile", "Sanctifier en-Vec", "Deflecting Palm",
	"Fireblast", "Light Up the Stage", "Play with Fire", "Chain Lightning", "Lava Dart",
	"Wild Slash", "Burst Lightning", "Bonecrusher Giant", "Fury", "Solitude", "Leyline of the Void",
	"Relic of Progenitus", "Tormod's Crypt", "Unholy Heat", "Static Prison", "Magus of the Moon",
	"Blood Moon", "Ensnaring Bridge", "Wear // Tear", "Abrade", "Pyroclasm", "Anger of the Gods",
	"Kozilek's Return", "Lightning Strike", "Shock", "Mishra's Bauble", "Ragavan, Nimble Pilferer",
	"Dragon's Rage Channeler", "Swords to Plowshares", "Force of Vigor", "Damping Sphere",
	"Ghost Quarter", "Thalia, Guardian of Thraben", "Stony Silence", "Celestial Purge",
	"Lim-Dûl's Vault", "Grafdigger's Cage", "Surgical Extraction", "Spell Pierce",
}
