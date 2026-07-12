package cswap

import "testing"

func TestPctLabelNoTrailingRounding(t *testing.T) {
	if got := pctLabel(85.555555); got != "85.555555" {
		// .10g: 10 significant digits, 85.555555 has 8 sig figs already
		// exact, no forced rounding artifact. Verified to match Python's
		// f"{85.555555:.10g}" == "85.555555" byte for byte.
		t.Fatalf("got %q", got)
	}
}

func TestPctLabelNeverLiesToRoundHundred(t *testing.T) {
	if got := pctLabel(99.9); got == "100" {
		t.Fatalf("got %q, a .0f-style formatter would incorrectly round this to 100", got)
	}
}

func TestPctLabelWholeNumberStaysClean(t *testing.T) {
	if got := pctLabel(90.0); got != "90" {
		t.Fatalf("got %q", got)
	}
}
