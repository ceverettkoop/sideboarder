"""Best-effort system clipboard reader.

Textual can *write* the system clipboard (via OSC 52) but cannot read it, so we
shell out to whichever platform tool is available. Returns ``None`` when no
working clipboard tool is found.
"""

from __future__ import annotations

import shutil
import subprocess
import sys


def _candidates() -> list[list[str]]:
    if sys.platform == "darwin":
        return [["pbpaste"]]
    if sys.platform == "win32":
        return [["powershell", "-NoProfile", "-Command", "Get-Clipboard"]]
    # Linux / BSD: prefer Wayland, then X11 utilities.
    return [
        ["wl-paste", "--no-newline"],
        ["xclip", "-selection", "clipboard", "-out"],
        ["xsel", "--clipboard", "--output"],
    ]


def read_clipboard() -> str | None:
    """Return the system clipboard text, or ``None`` if it can't be read."""
    for cmd in _candidates():
        if shutil.which(cmd[0]) is None:
            continue
        try:
            result = subprocess.run(
                cmd, capture_output=True, text=True, timeout=2, check=False
            )
        except (OSError, subprocess.SubprocessError):
            continue
        if result.returncode == 0:
            return result.stdout
    return None
