"""Shared test fixtures."""

from __future__ import annotations

import pytest

import sideboarder.config as config


@pytest.fixture(autouse=True)
def isolate_settings(tmp_path, monkeypatch):
    """Point settings at a throwaway dir so tests never read or write the real
    user config (and the app's auto-open-last-file never picks up a stray path).
    """
    cfg_dir = tmp_path / "config"
    monkeypatch.setattr(config, "CONFIG_DIR", cfg_dir)
    monkeypatch.setattr(config, "SETTINGS_PATH", cfg_dir / "settings.json")
    yield
