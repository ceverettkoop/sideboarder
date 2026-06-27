"""Entry point: ``python -m sideboarder`` / ``sideboarder``."""

from __future__ import annotations

import sys

from .app import SideboarderApp


def main() -> int:
    initial = sys.argv[1] if len(sys.argv) > 1 else None
    SideboarderApp(initial_path=initial).run()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
