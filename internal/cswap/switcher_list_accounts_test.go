package cswap

import (
	"strings"
	"testing"
)

func TestListAccountsNoSequenceFileJSONReturnsEmptyPayload(t *testing.T) {
	s := newTestSwitcher(t)
	res, err := s.ListAccounts(false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Payload["activeAccountNumber"] != nil {
		t.Fatalf("got %#v", res.Payload["activeAccountNumber"])
	}
	accounts, ok := res.Payload["accounts"].([]map[string]any)
	if !ok || len(accounts) != 0 {
		t.Fatalf("got %#v", res.Payload["accounts"])
	}
	if res.NeedsFirstRunSetup {
		t.Fatal("JSON mode must never signal first-run setup")
	}
}

func TestListAccountsNoSequenceFileHumanSignalsFirstRunSetup(t *testing.T) {
	s := newTestSwitcher(t)
	res, err := s.ListAccounts(false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsFirstRunSetup {
		t.Fatal("expected NeedsFirstRunSetup when no accounts are managed yet")
	}
	if len(res.Lines) != 1 || !strings.Contains(res.Lines[0], "No accounts are managed yet") {
		t.Fatalf("got %#v", res.Lines)
	}
}

func TestListAccountsHumanShowsAccountsAndActiveMarker(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	res, err := s.ListAccounts(false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(res.Lines, "\n")
	if !strings.Contains(joined, "Accounts:") {
		t.Fatalf("got %q", joined)
	}
	if !strings.Contains(joined, "a@example.com") {
		t.Fatalf("got %q", joined)
	}
	if !strings.Contains(joined, "(active)") {
		t.Fatalf("expected active marker, got %q", joined)
	}
}

func TestListAccountsJSONMatchesBuildListPayload(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	res, err := s.ListAccounts(false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Payload == nil {
		t.Fatal("expected a populated JSON payload")
	}
	accounts, ok := res.Payload["accounts"].([]map[string]any)
	if !ok || len(accounts) != 1 {
		t.Fatalf("got %#v", res.Payload["accounts"])
	}
	if len(res.Lines) != 0 {
		t.Fatalf("expected no human Lines in JSON mode, got %#v", res.Lines)
	}
}
