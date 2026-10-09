#!/usr/bin/env bash
# Build and start the Sideboarder web server (Go). By default it listens only
# on this machine's Tailscale address, port 8080. Extra arguments are passed
# through (see `./run-web.sh -h`).
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
BIN="$REPO_DIR/web/bin/sideboarder-web"

echo "Building sideboarder-web ..."
(cd "$REPO_DIR/web" && go build -o "$BIN" .)

cd "$REPO_DIR"   # so the default ./saves matches the TUI's
exec "$BIN" "$@"
