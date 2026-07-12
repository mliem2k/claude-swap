package cswap

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCliRunNoAccountArgIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "run"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected a non-zero exit code, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCliRunAccountNotFoundFails(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	claudeStub := filepath.Join(dir, "claude")
	if err := os.WriteFile(claudeStub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "run", "99"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected a non-zero exit code for a nonexistent account, stderr=%q", stderr.String())
	}
}

func TestCliRunNoShareFlagParses(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s
	dir := t.TempDir()
	claudeStub := filepath.Join(dir, "claude")
	if err := os.WriteFile(claudeStub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	// No accounts registered yet, so this still fails, but on
	// "account not found", not on flag parsing; confirms --no-share and
	// --share-history are recognized flags.
	code := RunCLI([]string{"cswap", "run", "1", "--no-share", "--share-history"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected a non-zero exit code (no accounts registered)")
	}
	if contains(stderr.String(), "unknown flag") {
		t.Fatalf("expected --no-share/--share-history to be recognized flags, got %q", stderr.String())
	}
}
