package cswap

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLIAddAccountHappyPath(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "add", "-y"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Added") {
		t.Fatalf("expected an Added confirmation line, got %q", stdout.String())
	}
}

func TestCLIAddAccountWithSlotFlag(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "add", "--slot", "5", "-y"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	data := s.GetSequenceData()
	if data == nil || data.Accounts["5"].Email != "a@example.com" {
		t.Fatalf("expected account added at slot 5, got %#v", data)
	}
}

func TestCLIAddAccountNoActiveLoginErrors(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "add", "-y"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("got exit code %d, want 1, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "Error:") {
		t.Fatalf("expected an Error: line, got %q", stderr.String())
	}
}

func TestCLIAddTokenHappyPath(t *testing.T) {
	newTestSwitcher(t)

	// "sk-ant-oat01-setup-token" with no other fixture is exactly the
	// verified-valid setup-token shape TestAddAccountFromTokenSetupToken
	// (internal/cswap/switcher_add_test.go) already uses for a bare
	// AddAccountFromToken call: no config/credentials files needed, no
	// email required either.
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "add-token", "sk-ant-oat01-setup-token", "-y"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Added") {
		t.Fatalf("got %q", stdout.String())
	}
}

// TestCLILegacyAddTokenFlagHappyPath mirrors the same scenario through the
// legacy `--add-token TOKEN` flag form (cli.py's nargs='?' optional-value
// shape), confirming translateLegacyArgs' rewrite plus the trailing
// positional/flag args all reach newAddTokenCommand correctly end to end.
func TestCLILegacyAddTokenFlagHappyPath(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "--add-token", "sk-ant-oat01-setup-token", "-y"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Added") {
		t.Fatalf("got %q", stdout.String())
	}
}
