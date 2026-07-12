package cswap

import (
	"reflect"
	"testing"
)

func TestTranslateLegacyArgsKnownFlags(t *testing.T) {
	cases := map[string][]string{
		"--list":           {"list"},
		"--status":         {"status"},
		"--add-account":    {"add"},
		"--add-token":      {"add-token"},
		"--remove-account": {"remove"},
		"--purge":          {"purge"},
		"--upgrade":        {"upgrade"},
		"--tui":            {"tui"},
		"--watch":          {"watch"},
	}
	for flag, want := range cases {
		got := translateLegacyArgs([]string{flag})
		if !reflect.DeepEqual(got, want) {
			t.Errorf("translateLegacyArgs([%q]) = %v, want %v", flag, got, want)
		}
	}
}

func TestTranslateLegacyArgsPreservesTrailingArgs(t *testing.T) {
	got := translateLegacyArgs([]string{"--remove-account", "3"})
	want := []string{"remove", "3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTranslateLegacyArgsSwitchBareRotate(t *testing.T) {
	got := translateLegacyArgs([]string{"--switch"})
	want := []string{"switch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTranslateLegacyArgsSwitchWithFlagStaysBare(t *testing.T) {
	got := translateLegacyArgs([]string{"--switch", "--strategy", "best"})
	want := []string{"switch", "--strategy", "best"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTranslateLegacyArgsSwitchToPositional(t *testing.T) {
	got := translateLegacyArgs([]string{"--switch-to", "2"})
	want := []string{"switch", "2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTranslateLegacyArgsUnknownFlagsPassThroughUnchanged(t *testing.T) {
	got := translateLegacyArgs([]string{"list", "--json"})
	want := []string{"list", "--json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTranslateLegacyArgsEmptyArgsUnchanged(t *testing.T) {
	got := translateLegacyArgs([]string{})
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}
