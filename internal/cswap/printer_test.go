package cswap

import "testing"

func TestForceColorStylesText(t *testing.T) {
	restore := ForceColor()
	defer restore()
	got := Accent("hi")
	if got == "hi" {
		t.Fatal("expected ForceColor to make Accent apply ANSI codes")
	}
	if got != "\033[38;5;173mhi\033[0m" {
		t.Fatalf("got %q", got)
	}
}

func TestForceColorRestoresPriorState(t *testing.T) {
	// Establish a known "off" baseline the way NO_COLOR would, then force on,
	// then restore, and confirm styling reverts to plain text.
	t.Setenv("NO_COLOR", "1")
	colorsEnabledCache = nil
	if ColorsEnabled() {
		t.Fatal("expected NO_COLOR to disable colors for this baseline")
	}
	restore := ForceColor()
	if !ColorsEnabled() {
		t.Fatal("expected ForceColor to enable colors")
	}
	restore()
	if ColorsEnabled() {
		t.Fatal("expected restore to bring back the pre-ForceColor (disabled) state")
	}
}

func TestStyleNoopWhenColorsDisabled(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	colorsEnabledCache = nil
	if got := Bolded("plain"); got != "plain" {
		t.Fatalf("got %q, want unstyled passthrough", got)
	}
}

func TestEntrypointLabelKnownAndUnknown(t *testing.T) {
	if got := EntrypointLabel("claude-vscode"); got != "VS Code" {
		t.Fatalf("got %q", got)
	}
	if got := EntrypointLabel("something-new"); got != "something-new" {
		t.Fatalf("expected passthrough for unknown entrypoint, got %q", got)
	}
}

func TestIdeShortNameKnownAndUnknown(t *testing.T) {
	if got := IdeShortName("Visual Studio Code"); got != "VS Code" {
		t.Fatalf("got %q", got)
	}
	if got := IdeShortName("Sublime Text"); got != "Sublime Text" {
		t.Fatalf("expected passthrough for unknown IDE, got %q", got)
	}
}

func TestAbbreviatePathReplacesHomePrefix(t *testing.T) {
	t.Setenv("HOME", "/Users/test")
	if got := AbbreviatePath("/Users/test/projects/x"); got != "~/projects/x" {
		t.Fatalf("got %q", got)
	}
}

func TestAbbreviatePathLeavesNonHomePaths(t *testing.T) {
	t.Setenv("HOME", "/Users/test")
	if got := AbbreviatePath("/opt/other"); got != "/opt/other" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatAgeBuckets(t *testing.T) {
	now := timeNowUnixForTest(t)
	cases := []struct {
		agoMs int64
		want  string
	}{
		{30 * 1000, "just now"},
		{5 * 60 * 1000, "5m ago"},
		{2 * 3600 * 1000, "2h ago"},
		{3 * 86400 * 1000, "3d ago"},
	}
	for _, c := range cases {
		startedAtMs := now*1000 - c.agoMs
		if got := FormatAge(startedAtMs); got != c.want {
			t.Errorf("FormatAge(%d ago) = %q, want %q", c.agoMs, got, c.want)
		}
	}
}

func timeNowUnixForTest(t *testing.T) int64 {
	t.Helper()
	return nowUnixForTest()
}
