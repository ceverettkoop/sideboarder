#!/usr/bin/env bash
# Launch sideboarder from the repo's .venv, creating it (and installing the
# package) on first run. Extra arguments are passed through to the app.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
VENV_DIR="$REPO_DIR/.venv"

if [ ! -x "$VENV_DIR/bin/python" ]; then
    echo "Creating virtualenv at $VENV_DIR ..."
    python3 -m venv "$VENV_DIR"
fi

if [ ! -x "$VENV_DIR/bin/sideboarder" ]; then
    echo "Installing sideboarder into the virtualenv ..."
    "$VENV_DIR/bin/pip" install --quiet --upgrade pip
    "$VENV_DIR/bin/pip" install --quiet -e "$REPO_DIR"
fi

exec "$VENV_DIR/bin/sideboarder" "$@"
