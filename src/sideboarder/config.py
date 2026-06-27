"""Application paths and persisted settings."""

from __future__ import annotations

import json
from dataclasses import asdict, dataclass
from pathlib import Path

from platformdirs import user_config_dir, user_data_dir

APP_NAME = "sideboarder"

CONFIG_DIR = Path(user_config_dir(APP_NAME))
DATA_DIR = Path(user_data_dir(APP_NAME))

SETTINGS_PATH = CONFIG_DIR / "settings.json"
CARDNAMES_PATH = DATA_DIR / "cardnames.json"


@dataclass
class AppSettings:
    """User-tweakable settings, persisted as JSON."""

    card_source: str = "mtgjson"  # "mtgjson" | "scryfall"
    default_save_dir: str = ""
    cardnames_updated: str = ""  # ISO timestamp of last card DB update
    cardnames_count: int = 0

    @classmethod
    def load(cls) -> AppSettings:
        try:
            data = json.loads(SETTINGS_PATH.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            return cls()
        known = {f for f in cls.__dataclass_fields__}
        return cls(**{k: v for k, v in data.items() if k in known})

    def save(self) -> None:
        CONFIG_DIR.mkdir(parents=True, exist_ok=True)
        SETTINGS_PATH.write_text(json.dumps(asdict(self), indent=2), encoding="utf-8")
