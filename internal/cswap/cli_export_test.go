package cswap

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCliExportWritesFile(t *testing.T) {
	s, _ := setupOneOAuthAccountForTransfer(t)
	_ = s
	dest := filepath.Join(t.TempDir(), "out.json")
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "export", dest}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected the export file to exist: %v", err)
	}
}

func TestCliExportFullFlag(t *testing.T) {
	s, _ := setupOneOAuthAccountForTransfer(t)
	_ = s
	dest := filepath.Join(t.TempDir(), "out.json")
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "export", dest, "--full"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
}

func TestCliExportLegacyFlagForm(t *testing.T) {
	s, _ := setupOneOAuthAccountForTransfer(t)
	_ = s
	dest := filepath.Join(t.TempDir(), "out.json")
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "--export", dest}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected the export file to exist via the legacy flag form: %v", err)
	}
}
