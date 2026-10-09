//go:build !js

// Command sideboarder-web serves the Sideboarder sideboard planner as a
// mobile-friendly web app, intended for use over a Tailscale network.
//
// It reads and writes the same *.sbd.json documents as the Textual TUI (one
// directory of them) and shares its card-name database for autocomplete.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ceverettkoop/sideboarder/web/internal/carddb"
	"github.com/ceverettkoop/sideboarder/web/internal/config"
)

//go:embed static
var staticFiles embed.FS

func defaultSaveDir() string {
	if dir := config.LoadSettings().DefaultSaveDir; dir != "" {
		return config.ExpandUser(dir)
	}
	return "saves"
}

func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

func main() {
	addr := flag.String("addr", "tailscale:8080",
		`listen address; host "tailscale" binds to this machine's tailnet IP only`)
	dir := flag.String("dir", defaultSaveDir(),
		"directory of *.sbd.json documents (default: the TUI's default save dir, else ./saves)")
	cards := flag.String("cardnames", config.CardNamesPath(), "card-name database (shared with the TUI)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags]\n\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
		fmt.Fprint(flag.CommandLine.Output(), `
Examples:
  sideboarder-web                         # http://<tailnet-ip>:8080, tailnet only
  sideboarder-web -addr 127.0.0.1:8080    # then: tailscale serve --bg 8080  (HTTPS + MagicDNS)
  sideboarder-web -addr :8080             # every interface (trusted networks only)
`)
	}
	flag.Parse()

	listen, err := resolveListenAddr(*addr)
	if err != nil {
		log.Fatalf("resolve listen address %q: %v\n"+
			"Start Tailscale, or pass -addr explicitly (e.g. -addr 127.0.0.1:8080 with `tailscale serve`).",
			*addr, err)
	}
	absDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		log.Fatalf("create document dir: %v", err)
	}

	db := carddb.New(*cards)
	if n := db.Load(); n > 0 {
		log.Printf("loaded %d card names from %s", n, *cards)
	} else {
		log.Printf("no card database at %s yet; update it from Settings to enable autocomplete", *cards)
	}

	static, _ := fs.Sub(staticFiles, "static")
	srv := &http.Server{
		Addr:              listen,
		Handler:           logRequests(newServer(dirStore{dir: absDir}, db, static).routes()),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("documents in %s", absDir)
	log.Printf("serving Sideboarder on http://%s", listen)
	log.Fatal(srv.ListenAndServe())
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if r.URL.Path != "/api/carddb/status" {
			log.Printf("%s %s %s (%s)", r.RemoteAddr, r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
