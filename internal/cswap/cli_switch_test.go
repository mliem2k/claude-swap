package cswap

import (
	"bytes"
	"strings"
	"testing"
)

func setupTwoAccountsForSwitch(t *testing.T) *ClaudeAccountSwitcher {
	t.Helper()
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	// "sk-ant-oat01-..." with no config/credentials fixture is the verified-
	// valid setup-token shape (TestAddAccountFromTokenSetupToken,
	// internal/cswap/switcher_add_test.go); GetNextAccountNumber auto-assigns
	// slot 2 since slot 1 is already taken.
	if _, err := s.AddAccountFromToken("sk-ant-oat01-second-token", "b@example.com", nil, nil); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCLISwitchBareRotateHuman(t *testing.T) {
	setupTwoAccountsForSwitch(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "switch"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
}

func TestCLISwitchToIdentifierJSON(t *testing.T) {
	setupTwoAccountsForSwitch(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "switch", "1", "--json", "--force"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"schemaVersion"`) {
		t.Fatalf("got %q", stdout.String())
	}
}

func TestCLISwitchWithModelFlagAugmentsJSONPayload(t *testing.T) {
	setupTwoAccountsForSwitch(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "switch", "--strategy", "best", "--model", "Opus,Fable", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"models"`) {
		t.Fatalf("expected a models key in the JSON payload, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"modelSource": "cli"`) {
		t.Fatalf("expected modelSource \"cli\" for an explicit --model, got %q", stdout.String())
	}
}

// TestCLISwitchModelWithoutStrategyIsRejected mirrors main()'s
// parser.error() check: --model is meaningless outside a usage-aware
// strategy (nothing reads it there), so it must be rejected outright
// (exit 2, no switch attempted) rather than silently ignored.
func TestCLISwitchModelWithoutStrategyIsRejected(t *testing.T) {
	setupTwoAccountsForSwitch(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "switch", "--model", "Opus", "--json"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("got exit code %d, want 2 (usage error); stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--model") {
		t.Fatalf("expected the error to mention --model, got stderr=%q", stderr.String())
	}
}

// TestCLISwitchStrategyWithIdentifierIsRejected mirrors main()'s
// parser.error(): --strategy only applies to a bare rotation, not a
// direct-target switch.
func TestCLISwitchStrategyWithIdentifierIsRejected(t *testing.T) {
	setupTwoAccountsForSwitch(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "switch", "1", "--strategy", "best"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("got exit code %d, want 2 (usage error); stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

// TestCLISwitchForceOnBareRotateIsRejected mirrors main()'s parser.error():
// --force only applies to a direct-target switch, not a bare rotation.
func TestCLISwitchForceOnBareRotateIsRejected(t *testing.T) {
	setupTwoAccountsForSwitch(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "switch", "--force"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("got exit code %d, want 2 (usage error); stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

// TestCLISwitchStrategyFallsBackToConfiguredModel mirrors main()'s
// settings-derived model_source branch: --strategy without --model still
// applies the persistent autoswitch.model setting (announced, never
// silent), instead of ignoring it the way an earlier Go draft did.
func TestCLISwitchStrategyFallsBackToConfiguredModel(t *testing.T) {
	s := setupTwoAccountsForSwitch(t)
	if _, err := SetSetting(s.BackupDir, "autoswitch.model", "Opus,Fable"); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "switch", "--strategy", "best", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"modelSource": "autoswitch.model"`) {
		t.Fatalf("expected modelSource \"autoswitch.model\" from the configured setting, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"Opus"`) || !strings.Contains(stdout.String(), `"Fable"`) {
		t.Fatalf("expected both configured models in the payload, got %q", stdout.String())
	}
}

// TestCLISwitchStrategyFallbackPrintsNoticeInHumanMode mirrors switch()'s
// own pre-switch "Using configured model limits" announcement in
// human-mode output.
func TestCLISwitchStrategyFallbackPrintsNoticeInHumanMode(t *testing.T) {
	s := setupTwoAccountsForSwitch(t)
	if _, err := SetSetting(s.BackupDir, "autoswitch.model", "Opus"); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "switch", "--strategy", "best"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Using configured model limits: Opus (from autoswitch.model)") {
		t.Fatalf("expected the pre-switch model-limits notice, got %q", stdout.String())
	}
}

// TestCLISwitchToAmbiguousEmailPromptsAndSwitchesToChosen exercises the
// full interactive disambiguation flow end to end for `cswap switch
// <email>`, mirroring TestCLIRemoveAccountAmbiguousEmailPromptsAndRemovesChosen.
func TestCLISwitchToAmbiguousEmailPromptsAndSwitchesToChosen(t *testing.T) {
	seedTwoAccountsSameEmailForSwitchTo(t)

	var stdout, stderr bytes.Buffer
	code := runCLIWithStdin([]string{"cswap", "switch", "shared@example.com", "--force"}, strings.NewReader("2\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Multiple accounts found") {
		t.Fatalf("expected the disambiguation prompt, got %q", stdout.String())
	}
}

// TestCLISwitchToAmbiguousEmailJSONModeNeverPrompts mirrors Python's
// `if not json_output:` guard: --json must never read from stdin, falling
// straight through to the informative ambiguous-match error instead.
func TestCLISwitchToAmbiguousEmailJSONModeNeverPrompts(t *testing.T) {
	seedTwoAccountsSameEmailForSwitchTo(t)

	var stdout, stderr bytes.Buffer
	// No stdin provided at all: if this blocked on a prompt read, the
	// test would hang or read EOF and behave unpredictably instead of
	// failing fast with the expected error.
	code := runCLIWithStdin([]string{"cswap", "switch", "shared@example.com", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("got exit code %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "Multiple accounts found") {
		t.Fatalf("--json must never print the interactive disambiguation prompt, got %q", stdout.String())
	}
}
