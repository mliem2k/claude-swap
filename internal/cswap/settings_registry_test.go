package cswap

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultAutoSwitchSettings(t *testing.T) {
	d := DefaultAutoSwitchSettings()
	if d.Threshold != 90.0 || d.IntervalSeconds != 60.0 || d.CooldownSeconds != 300.0 ||
		d.HysteresisPct != 10.0 || d.Strategy != "best" || d.IncludeAPIKeyAccounts != false ||
		d.UnhealthyTicks != 3 || d.Model != nil {
		t.Fatalf("got %#v", d)
	}
}

func TestSettingSpecDotted(t *testing.T) {
	spec := SettingSpec{Section: "autoswitch", JSONKey: "intervalSeconds"}
	if got := spec.Dotted(); got != "autoswitch.intervalSeconds" {
		t.Fatalf("got %q", got)
	}
}

func TestSettingSpecDefaultReadsLiveField(t *testing.T) {
	spec, err := SettingSpecFor("autoswitch.threshold")
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.Default(); got != 90.0 {
		t.Fatalf("got %#v", got)
	}
	specModel, err := SettingSpecFor("autoswitch.model")
	if err != nil {
		t.Fatal(err)
	}
	if got := specModel.Default(); got != nil {
		t.Fatalf("expected nil default for model, got %#v", got)
	}
}

func TestSettingSpecForAllEightKeysResolve(t *testing.T) {
	keys := []string{
		"autoswitch.threshold", "autoswitch.intervalSeconds", "autoswitch.cooldownSeconds",
		"autoswitch.hysteresisPct", "autoswitch.strategy", "autoswitch.includeApiKeyAccounts",
		"autoswitch.unhealthyTicks", "autoswitch.model",
	}
	for _, k := range keys {
		if _, err := SettingSpecFor(k); err != nil {
			t.Errorf("SettingSpecFor(%q): %v", k, err)
		}
	}
}

func TestSettingSpecForUnknownKeyErrors(t *testing.T) {
	_, err := SettingSpecFor("autoswitch.bogus")
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("expected ErrConfig, got %v", err)
	}
	if !strings.Contains(err.Error(), "unknown setting") {
		t.Fatalf("got %q", err.Error())
	}
}

func TestSettingsPathJoinsBackupRoot(t *testing.T) {
	got := SettingsPath("/backup")
	want := filepath.Join("/backup", "settings.json")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseModelNamesSplitsTrimsDedupsCaseInsensitive(t *testing.T) {
	got := ParseModelNames("Opus, fable,OPUS, Fable ")
	want := []string{"Opus", "fable"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %#v want %#v (first spelling wins on case-insensitive dedup)", got, want)
	}
}

func TestParseModelNamesEmptyStringIsEmpty(t *testing.T) {
	if got := ParseModelNames(""); len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}
