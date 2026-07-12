package menubar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSettings(t *testing.T) {
	got := DefaultSettings()
	want := Settings{ShowAccountName: true, TitlePct: "both", RefreshInterval: 60, AutoSwitchEnabled: false}
	if got != want {
		t.Errorf("DefaultSettings() = %+v, want %+v", got, want)
	}
}

func TestLoadSettingsMissingFileYieldsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	got := LoadSettings(path)
	if got != DefaultSettings() {
		t.Errorf("LoadSettings(missing) = %+v, want defaults", got)
	}
}

func TestLoadSettingsUnparseableYieldsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadSettings(path)
	if got != DefaultSettings() {
		t.Errorf("LoadSettings(unparseable) = %+v, want defaults", got)
	}
}

func TestLoadSettingsPartialOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"refresh_interval": 300, "unknown_key": "ignored"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadSettings(path)
	want := DefaultSettings()
	want.RefreshInterval = 300
	if got != want {
		t.Errorf("LoadSettings(partial) = %+v, want %+v", got, want)
	}
}

func TestLoadSettingsWrongTypeKeepsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	// refresh_interval should be a number; a string value must be
	// ignored, leaving the field at its default rather than erroring.
	if err := os.WriteFile(path, []byte(`{"refresh_interval": "not-a-number"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadSettings(path)
	if got != DefaultSettings() {
		t.Errorf("LoadSettings(wrong type) = %+v, want defaults", got)
	}
}

// TestSettingsJSONFieldNamesMatchPython locks in the snake_case field
// names menubar_settings.json must use: Python's MenuBarSettings.save
// writes asdict(self) verbatim, and both implementations target the
// identical file path, so a mismatch here means a settings file written
// by one is silently invisible to the other.
func TestSettingsJSONFieldNamesMatchPython(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Settings{ShowAccountName: false, TitlePct: "5h", RefreshInterval: 30, AutoSwitchEnabled: true}
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"show_account_name"`, `"title_pct"`, `"refresh_interval"`, `"auto_switch_enabled"`} {
		if !strings.Contains(string(body), key) {
			t.Errorf("expected saved JSON to contain %s, got %s", key, body)
		}
	}
}

func TestSettingsSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	s := Settings{ShowAccountName: false, TitlePct: "5h", RefreshInterval: 30, AutoSwitchEnabled: true}
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := LoadSettings(path)
	if got != s {
		t.Errorf("round-tripped = %+v, want %+v", got, s)
	}
}
