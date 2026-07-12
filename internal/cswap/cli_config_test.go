package cswap

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLIConfigListHuman(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "autoswitch.threshold") {
		t.Fatalf("got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "(default)") {
		t.Fatalf("expected (default) marker on an unset key, got %q", stdout.String())
	}
}

func TestCLIConfigListJSON(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "list", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"settings"`) || !strings.Contains(stdout.String(), `"isSet"`) {
		t.Fatalf("got %q", stdout.String())
	}
}

// TestCLIConfigBareJSONDefaultsToListJSON mirrors Python argparse
// registering --json on the parent `config` parser itself (not just its
// list/get children), so `cswap config --json` behaves like
// `cswap config list --json` instead of failing with "unknown flag".
func TestCLIConfigBareJSONDefaultsToListJSON(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"settings"`) || !strings.Contains(stdout.String(), `"isSet"`) {
		t.Fatalf("got %q", stdout.String())
	}
}

func TestCLIConfigBareDefaultsToList(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "autoswitch.threshold") {
		t.Fatalf("got %q", stdout.String())
	}
}

func TestCLIConfigGetHuman(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "get", "autoswitch.threshold"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "90" {
		t.Fatalf("got %q", stdout.String())
	}
}

func TestCLIConfigGetJSON(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "get", "autoswitch.threshold", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"key": "autoswitch.threshold"`) {
		t.Fatalf("got %q", stdout.String())
	}
}

func TestCLIConfigGetUnknownKeyErrors(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "get", "autoswitch.bogus"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("got exit code %d, want 1, stderr=%q", code, stderr.String())
	}
}

func TestCLIConfigPath(t *testing.T) {
	s := newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "path"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != SettingsPath(s.BackupDir) {
		t.Fatalf("got %q want %q", stdout.String(), SettingsPath(s.BackupDir))
	}
}

func TestCLIConfigSetSuccess(t *testing.T) {
	s := newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "set", "autoswitch.threshold", "80"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "autoswitch.threshold = 80") {
		t.Fatalf("got %q", stdout.String())
	}
	got := LoadSettings(s.BackupDir)
	if got.Threshold != 80.0 {
		t.Fatalf("got %#v", got)
	}
}

func TestCLIConfigSetInvalidValueErrors(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "set", "autoswitch.threshold", "bogus"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("got exit code %d, want 1, stderr=%q", code, stderr.String())
	}
}

func TestCLIConfigUnsetPresent(t *testing.T) {
	s := newTestSwitcher(t)
	if _, err := SetSetting(s.BackupDir, "autoswitch.threshold", "80"); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "unset", "autoswitch.threshold"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "unset") || !strings.Contains(stdout.String(), "default: 90") {
		t.Fatalf("got %q", stdout.String())
	}
	got := LoadSettings(s.BackupDir)
	if got.Threshold != 90.0 {
		t.Fatalf("got %#v", got)
	}
}

func TestCLIConfigUnsetNotPresent(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "config", "unset", "autoswitch.threshold"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not set") {
		t.Fatalf("expected the not-set note on stderr, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
