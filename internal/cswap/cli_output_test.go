package cswap

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmPromptYesAccepts(t *testing.T) {
	in := strings.NewReader("y\n")
	var out bytes.Buffer
	confirm := confirmPrompt(in, &out)
	if !confirm("Overwrite slot 1?") {
		t.Fatal("expected y to confirm")
	}
	if !strings.Contains(out.String(), "Overwrite slot 1? [y/N] ") {
		t.Fatalf("expected the prompt text with a [y/N] suffix, got %q", out.String())
	}
}

func TestConfirmPromptUppercaseYAccepts(t *testing.T) {
	in := strings.NewReader("Y\n")
	var out bytes.Buffer
	confirm := confirmPrompt(in, &out)
	if !confirm("Proceed?") {
		t.Fatal("expected uppercase Y to confirm, matching Python's .lower() == 'y'")
	}
}

func TestConfirmPromptEmptyAndNoDecline(t *testing.T) {
	for _, answer := range []string{"\n", "n\n", "no\n"} {
		in := strings.NewReader(answer)
		var out bytes.Buffer
		confirm := confirmPrompt(in, &out)
		if confirm("Proceed?") {
			t.Fatalf("expected %q to decline (default-no)", answer)
		}
	}
}

func TestPrintJSONFormatsWithTwoSpaceIndent(t *testing.T) {
	var out bytes.Buffer
	printJSON(&out, map[string]any{"schemaVersion": 1})
	if !strings.Contains(out.String(), "\"schemaVersion\": 1") {
		t.Fatalf("got %q", out.String())
	}
}

func TestPrintLinesOneLinePerEntry(t *testing.T) {
	var out bytes.Buffer
	printLines(&out, []string{"first", "second"})
	got := out.String()
	if got != "first\nsecond\n" {
		t.Fatalf("got %q", got)
	}
}
