package cswap

import (
	"os"
	"testing"
)

func TestBareInvocationSkipsTUIWhenNotATTY(t *testing.T) {
	// A pipe is never a TTY on either end, so this must fall through to
	// the normal help/usage path rather than trying to start the TUI
	// (which would hang waiting for a real terminal in a test process).
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isInteractiveTerminal(r, w) {
		t.Fatal("a pipe must never be reported as an interactive terminal")
	}
}
