package cswap

import (
	"strings"
	"testing"
)

func TestFirstRunSetupNoActiveAccount(t *testing.T) {
	s := newTestSwitcher(t)
	msg, err := s.FirstRunSetup(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "No active Claude account found") {
		t.Fatalf("got %q", msg)
	}
}

func TestFirstRunSetupDeclinedLeavesNoAccounts(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)

	msg, err := s.FirstRunSetup(func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "cancelled") && !strings.Contains(msg, "Cancelled") {
		t.Fatalf("got %q", msg)
	}
	data := s.GetSequenceData()
	if data != nil && len(data.Accounts) != 0 {
		t.Fatalf("expected no accounts added, got %#v", data.Accounts)
	}
}

func TestFirstRunSetupAcceptedAddsAccount(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)

	_, err := s.FirstRunSetup(func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	if data == nil || len(data.Accounts) != 1 {
		t.Fatalf("expected one account added, got %#v", data)
	}
}
