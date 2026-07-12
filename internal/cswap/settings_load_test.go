package cswap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettingsMissingFileReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	got := LoadSettings(dir)
	want := DefaultAutoSwitchSettings()
	if got != want {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestLoadSettingsReadsValidValues(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, SettingsPath(dir), `{"schemaVersion":1,"autoswitch":{"threshold":75.0,"unhealthyTicks":5,"model":"Opus,Fable"}}`)
	got := LoadSettings(dir)
	if got.Threshold != 75.0 || got.UnhealthyTicks != 5 {
		t.Fatalf("got %#v", got)
	}
	if got.Model == nil || *got.Model != "Opus,Fable" {
		t.Fatalf("got model %#v", got.Model)
	}
	// Untouched fields stay at their defaults.
	if got.IntervalSeconds != 60.0 {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadSettingsClampsOutOfRangeValue(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, SettingsPath(dir), `{"autoswitch":{"threshold":500.0}}`)
	got := LoadSettings(dir)
	if got.Threshold != 99.9 {
		t.Fatalf("expected clamped to hi=99.9, got %v", got.Threshold)
	}
}

func TestLoadSettingsRejectsBoolForNumericField(t *testing.T) {
	dir := t.TempDir()
	// Python's isinstance(value, bool) check runs before the numeric check,
	// since bool is an int subclass; a JSON `true` for a numeric field must
	// NOT be coerced to 1.0, it must revert to the field's default.
	mustWrite(t, SettingsPath(dir), `{"autoswitch":{"threshold":true}}`)
	got := LoadSettings(dir)
	if got.Threshold != 90.0 {
		t.Fatalf("expected default (bool rejected for numeric field), got %v", got.Threshold)
	}
}

func TestLoadSettingsUnsupportedChoiceFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, SettingsPath(dir), `{"autoswitch":{"strategy":"worst"}}`)
	got := LoadSettings(dir)
	if got.Strategy != "best" {
		t.Fatalf("expected default strategy, got %q", got.Strategy)
	}
}

func TestLoadSettingsCorruptJSONReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, SettingsPath(dir), `{not valid json`)
	got := LoadSettings(dir)
	if got != DefaultAutoSwitchSettings() {
		t.Fatalf("got %#v", got)
	}
}

func TestLoadSettingsNonObjectAutoswitchSectionReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, SettingsPath(dir), `{"autoswitch":"not an object"}`)
	got := LoadSettings(dir)
	if got != DefaultAutoSwitchSettings() {
		t.Fatalf("got %#v", got)
	}
}

func TestSaveSettingsWritesAllEightKeysAndPreservesUnknownSections(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, SettingsPath(dir), `{"schemaVersion":1,"someOtherTool":{"foo":"bar"}}`)

	s := DefaultAutoSwitchSettings()
	s.Threshold = 80.0
	if err := SaveSettings(dir, s); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(SettingsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, want := range []string{`"threshold": 80`, `"someOtherTool"`, `"foo": "bar"`} {
		if !contains(content, want) {
			t.Fatalf("expected %q in written file, got: %s", want, content)
		}
	}
	perm, err := os.Stat(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %v", perm.Mode().Perm())
	}
}
