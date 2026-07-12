package cswap

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestCliImportReadsFile(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s
	path := filepath.Join(t.TempDir(), "in.json")
	writeTestEnvelope(t, path, oneAccountEnvelope())
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "import", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
}

func TestCliImportForceFlag(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s
	path := filepath.Join(t.TempDir(), "in.json")
	writeTestEnvelope(t, path, oneAccountEnvelope())
	var stdout, stderr bytes.Buffer
	RunCLI([]string{"cswap", "import", path}, &stdout, &stderr)
	stdout.Reset()
	stderr.Reset()
	code := RunCLI([]string{"cswap", "import", path, "--force"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
}

func TestCliImportLegacyFlagForm(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s
	path := filepath.Join(t.TempDir(), "in.json")
	writeTestEnvelope(t, path, oneAccountEnvelope())
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "--import", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
}
