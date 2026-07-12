package cswap

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLIRemoveAccountAssumeYes(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "remove", "1", "-y"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	data := s.GetSequenceData()
	if _, ok := data.Accounts["1"]; ok {
		t.Fatal("expected account removed")
	}
}

// TestCLIRemoveAccountAmbiguousEmailPromptsAndRemovesChosen exercises the
// full interactive disambiguation flow end to end: two accounts share an
// email, the prompt lists both, the user picks one by number, then
// confirms the removal.
func TestCLIRemoveAccountAmbiguousEmailPromptsAndRemovesChosen(t *testing.T) {
	s := seedTwoAccountsSameEmailForRemove(t)

	var stdout, stderr bytes.Buffer
	code := runCLIWithStdin([]string{"cswap", "remove", "shared@example.com"}, strings.NewReader("2\ny\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Multiple accounts found") {
		t.Fatalf("expected the disambiguation prompt, got %q", stdout.String())
	}
	data := s.GetSequenceData()
	if _, ok := data.Accounts["2"]; ok {
		t.Fatal("expected account 2 (the chosen one) removed")
	}
	if _, ok := data.Accounts["1"]; !ok {
		t.Fatal("expected account 1 left in place")
	}
}

func TestCLIRemoveAccountDeclinedPromptLeavesAccount(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runCLIWithStdin([]string{"cswap", "remove", "1"}, strings.NewReader("n\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	data := s.GetSequenceData()
	if _, ok := data.Accounts["1"]; !ok {
		t.Fatal("declining removal should leave the account in place")
	}
}

func TestCLIRemoveUnknownAccountErrors(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "remove", "999", "-y"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("got exit code %d, want 1, stderr=%q", code, stderr.String())
	}
}

func TestCLIPurgeAssumeYesRemovesBackupDir(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "purge", "-y"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if fileExists(s.BackupDir) {
		t.Fatal("expected backup dir removed")
	}
}

func TestCLIPurgeDeclinedLeavesBackupDir(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runCLIWithStdin([]string{"cswap", "purge"}, strings.NewReader("n\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !fileExists(s.BackupDir) {
		t.Fatal("expected backup dir to survive a declined purge")
	}
}
