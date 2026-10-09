// Package config locates the TUI's settings and data files (same paths as the
// Python app's platformdirs usage) so the web server shares them.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AppName is the directory name used under the platform config/data roots.
const AppName = "sideboarder"

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func xdg(env, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return v
	}
	return filepath.Join(home(), fallback)
}

// ConfigDir mirrors platformdirs.user_config_dir("sideboarder").
func ConfigDir() string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home(), "Library", "Application Support", AppName)
	case "windows":
		return windowsDir()
	}
	return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), AppName)
}

// DataDir mirrors platformdirs.user_data_dir("sideboarder").
func DataDir() string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home(), "Library", "Application Support", AppName)
	case "windows":
		return windowsDir()
	}
	return filepath.Join(xdg("XDG_DATA_HOME", filepath.Join(".local", "share")), AppName)
}

func windowsDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(home(), "AppData", "Local")
	}
	return filepath.Join(base, AppName, AppName) // appauthor defaults to appname
}

// SettingsPath is the TUI's settings.json.
func SettingsPath() string { return filepath.Join(ConfigDir(), "settings.json") }

// CardNamesPath is the local card-name database used for autocomplete.
func CardNamesPath() string { return filepath.Join(DataDir(), "cardnames.json") }

// Settings are the persisted settings the web server reads or updates.
// Unknown keys in the file (e.g. the TUI's last_file) are preserved on save.
type Settings struct {
	CardSource       string `json:"card_source"`
	DefaultSaveDir   string `json:"default_save_dir"`
	LastFile         string `json:"last_file"`
	CardnamesUpdated string `json:"cardnames_updated"`
	CardnamesCount   int    `json:"cardnames_count"`
}

// LoadSettings reads settings.json, returning defaults when it is missing or bad.
func LoadSettings() Settings {
	s := Settings{CardSource: "mtgjson"}
	data, err := os.ReadFile(SettingsPath())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	if s.CardSource == "" {
		s.CardSource = "mtgjson"
	}
	return s
}

// UpdateSettings merges fields into settings.json, keeping keys it doesn't know.
func UpdateSettings(fields map[string]any) error {
	merged := map[string]any{}
	if data, err := os.ReadFile(SettingsPath()); err == nil {
		_ = json.Unmarshal(data, &merged)
	}
	for k, v := range fields {
		merged[k] = v
	}
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(SettingsPath(), data, 0o644)
}

// ExpandUser expands a leading "~" like Python's Path.expanduser().
func ExpandUser(p string) string {
	if p == "~" {
		return home()
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(home(), p[2:])
	}
	return p
}
