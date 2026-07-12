package cswap

import (
	"errors"
	"testing"
)

func TestSetSettingWritesOnlyThatOneKey(t *testing.T) {
	dir := t.TempDir()
	v, err := SetSetting(dir, "autoswitch.threshold", "80")
	if err != nil {
		t.Fatal(err)
	}
	if v != 80.0 {
		t.Fatalf("got %#v", v)
	}
	got := LoadSettings(dir)
	if got.Threshold != 80.0 {
		t.Fatalf("got %#v", got)
	}
	// Only the one key was written; every other setting is still absent
	// from the file (LoadSettings just happens to report their defaults).
	rows, err := EffectiveSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		wantSet := row.Spec.Dotted() == "autoswitch.threshold"
		if row.IsSet != wantSet {
			t.Errorf("%s: got isSet=%v want %v", row.Spec.Dotted(), row.IsSet, wantSet)
		}
	}
}

func TestSetSettingUnknownKeyErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := SetSetting(dir, "autoswitch.bogus", "1")
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("expected ErrConfig, got %v", err)
	}
}

func TestSetSettingInvalidValueErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := SetSetting(dir, "autoswitch.threshold", "not-a-number")
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("expected ErrConfig, got %v", err)
	}
	// The bad set must not have written a corrupt/partial file.
	if fileExists(SettingsPath(dir)) {
		t.Fatal("expected no settings.json written after a failed set")
	}
}

func TestUnsetSettingRemovesKeyReturnsTrue(t *testing.T) {
	dir := t.TempDir()
	if _, err := SetSetting(dir, "autoswitch.threshold", "80"); err != nil {
		t.Fatal(err)
	}
	ok, err := UnsetSetting(dir, "autoswitch.threshold")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected true")
	}
	got := LoadSettings(dir)
	if got.Threshold != 90.0 {
		t.Fatalf("expected default after unset, got %v", got.Threshold)
	}
}

func TestUnsetSettingNotPresentReturnsFalseNoWrite(t *testing.T) {
	dir := t.TempDir()
	ok, err := UnsetSetting(dir, "autoswitch.threshold")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected false, nothing was set")
	}
	if fileExists(SettingsPath(dir)) {
		t.Fatal("expected no settings.json written for a no-op unset")
	}
}

func TestUnsetSettingRemovesEmptySection(t *testing.T) {
	dir := t.TempDir()
	if _, err := SetSetting(dir, "autoswitch.threshold", "80"); err != nil {
		t.Fatal(err)
	}
	if _, err := UnsetSetting(dir, "autoswitch.threshold"); err != nil {
		t.Fatal(err)
	}
	raw, err := readSettingsRawForWrite(SettingsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["autoswitch"]; ok {
		t.Fatalf("expected the now-empty autoswitch section removed entirely, got %#v", raw)
	}
}

func TestEffectiveSettingsOrderMatchesRegistry(t *testing.T) {
	dir := t.TempDir()
	rows, err := EffectiveSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(settingSpecsOrder) {
		t.Fatalf("got %d rows, want %d", len(rows), len(settingSpecsOrder))
	}
	for i, row := range rows {
		if row.Spec.Dotted() != settingSpecsOrder[i].Dotted() {
			t.Fatalf("row %d: got %s, want %s", i, row.Spec.Dotted(), settingSpecsOrder[i].Dotted())
		}
		if row.IsSet {
			t.Fatalf("row %d (%s): expected isSet=false on a fresh backupRoot", i, row.Spec.Dotted())
		}
	}
}

func TestMergedWithCliOverlaysOnlyNonNilFields(t *testing.T) {
	base := DefaultAutoSwitchSettings()
	threshold := 85.0
	got := MergedWithCli(base, CliOverrides{Threshold: &threshold})
	if got.Threshold != 85.0 {
		t.Fatalf("got %v", got.Threshold)
	}
	// Untouched fields keep their base values.
	if got.IntervalSeconds != base.IntervalSeconds || got.Strategy != base.Strategy {
		t.Fatalf("got %#v", got)
	}
}

func TestMergedWithCliNoOverridesReturnsUnchanged(t *testing.T) {
	base := DefaultAutoSwitchSettings()
	got := MergedWithCli(base, CliOverrides{})
	if got != base {
		t.Fatalf("got %#v want unchanged %#v", got, base)
	}
}

func TestMergedWithCliClampsAppliedOverride(t *testing.T) {
	base := DefaultAutoSwitchSettings()
	tooHigh := 500.0
	got := MergedWithCli(base, CliOverrides{Threshold: &tooHigh})
	if got.Threshold != 99.9 {
		t.Fatalf("expected clamped to hi, got %v", got.Threshold)
	}
}

func TestMergedWithCliEmptyModelOverrideRevertsToNil(t *testing.T) {
	// Python's _clamped treats an empty string as falsy for the "string"
	// kind and reverts to the field default (None); this must hold for a
	// CLI-supplied override too, not just a value loaded from
	// settings.json, which is exactly what routing MergedWithCli's clamp
	// step through the same clamped() function (rather than a separate,
	// numeric-only implementation) is meant to guarantee.
	base := DefaultAutoSwitchSettings()
	original := "Opus"
	base.Model = &original
	empty := ""
	got := MergedWithCli(base, CliOverrides{Model: &empty})
	if got.Model != nil {
		t.Fatalf("expected an empty string override to revert Model to nil, got %#v", *got.Model)
	}
}

func TestMergedWithCliPreservesUntouchedNonNumericFields(t *testing.T) {
	// Strategy and UnhealthyTicks have no CLI override at all; routing
	// through clamped() must not spuriously reset or warn about them.
	base := DefaultAutoSwitchSettings()
	base.UnhealthyTicks = 7
	threshold := 85.0
	got := MergedWithCli(base, CliOverrides{Threshold: &threshold})
	if got.Strategy != "best" || got.UnhealthyTicks != 7 {
		t.Fatalf("got %#v", got)
	}
}
