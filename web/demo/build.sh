#!/usr/bin/env bash
# Build the in-browser demo: the Go server compiled to WebAssembly plus the
# normal web client, in demo/dist/. Serve that folder with any static file
# server (no backend needed); documents live in the browser's localStorage.
set -euo pipefail

WEB="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/.." && pwd)"
OUT="$WEB/demo/dist"
rm -rf "$OUT" && mkdir -p "$OUT"

(cd "$WEB" && GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o "$OUT/sideboarder.wasm" .)
WASM_EXEC="$(go env GOROOT)/lib/wasm/wasm_exec.js"
[ -f "$WASM_EXEC" ] || WASM_EXEC="$(go env GOROOT)/misc/wasm/wasm_exec.js"
cp "$WASM_EXEC" "$OUT/"
cp "$WEB/demo/demo.js" "$WEB/static/app.js" "$WEB/static/style.css" "$WEB/static/icon.svg" \
   "$WEB/static/manifest.webmanifest" "$OUT/"

# Load the Go runtime and the shim before the app, and flag the page as a demo.
sed -e 's#<script src="app.js" defer></script>#<script src="wasm_exec.js"></script>\n  <script src="demo.js"></script>\n  <script src="app.js" defer></script>#' \
    -e 's#<main id="main"#<p class="demo-banner">Demo — runs entirely in your browser; documents are saved in this browser only.</p>\n  <main id="main"#' \
    "$WEB/static/index.html" > "$OUT/index.html"

echo "Built $OUT ($(du -sh "$OUT/sideboarder.wasm" | cut -f1) wasm)"
