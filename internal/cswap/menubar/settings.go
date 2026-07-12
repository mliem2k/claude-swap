package menubar

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// RefreshChoices mirrors REFRESH_CHOICES.
var RefreshChoices = []int{30, 60, 300}

// AutoThresholdChoices mirrors AUTO_THRESHOLD_CHOICES.
var AutoThresholdChoices = []int{80, 90, 95, 98}

// TitlePctChoices mirrors TITLE_PCT_CHOICES.
var TitlePctChoices = []string{"off", "5h", "7d", "both"}

// SwitchHistoryLimit mirrors SWITCH_HISTORY_LIMIT.
const SwitchHistoryLimit = 10

// Settings mirrors MenuBarSettings: user-configurable menu bar display
// behavior, persisted as JSON. Only display preferences and the
// auto-switch on/off toggle live here; auto-switch policy (threshold,
// cooldown, hysteresis) is core config, read/written through the
// settings.go autoswitch.* keys, so the CLI/TUI/menu bar share one
// source of truth.
// JSON field names are snake_case, matching Python's asdict(self) exactly
// (menubar_settings.json is a claude-swap-internal file with no
// Claude-Code-owned schema to mirror, unlike sequence.json/usage.json's
// camelCase; using this codebase's usual camelCase convention here instead
// would make a settings file written by one implementation invisible to
// the other, since both target the identical path).
type Settings struct {
	ShowAccountName   bool   `json:"show_account_name"`
	TitlePct          string `json:"title_pct"`
	RefreshInterval   int    `json:"refresh_interval"`
	AutoSwitchEnabled bool   `json:"auto_switch_enabled"`
}

// DefaultSettings mirrors MenuBarSettings()'s field defaults.
func DefaultSettings() Settings {
	return Settings{ShowAccountName: true, TitlePct: "both", RefreshInterval: 60, AutoSwitchEnabled: false}
}

// LoadSettings mirrors MenuBarSettings.load: falls back to defaults on
// any problem. A missing/unparseable file yields all-defaults; an
// unknown key is ignored; a value whose JSON type doesn't match the
// field's Go type is dropped, leaving that field at its default.
func LoadSettings(path string) Settings {
	defaults := DefaultSettings()
	raw, err := os.ReadFile(path)
	if err != nil {
		return defaults
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return defaults
	}
	out := defaults
	if v, ok := m["show_account_name"]; ok {
		var b bool
		if json.Unmarshal(v, &b) == nil {
			out.ShowAccountName = b
		}
	}
	if v, ok := m["title_pct"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			out.TitlePct = s
		}
	}
	if v, ok := m["refresh_interval"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil {
			out.RefreshInterval = n
		}
	}
	if v, ok := m["auto_switch_enabled"]; ok {
		var b bool
		if json.Unmarshal(v, &b) == nil {
			out.AutoSwitchEnabled = b
		}
	}
	return out
}

// Save mirrors MenuBarSettings.save: writes pretty JSON, creating parent
// directories.
func (s Settings) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
