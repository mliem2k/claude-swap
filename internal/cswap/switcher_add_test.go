package cswap

import (
	"errors"
	"strings"
	"testing"
)

func TestAddAccountNoActiveLoginErrors(t *testing.T) {
	s := newTestSwitcher(t)
	_, err := s.AddAccount(nil, nil)
	if err == nil {
		t.Fatal("expected an error with no active claude.json login")
	}
}

func TestAddAccountFreshRegistration(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com","accountUuid":"u1","organizationUuid":"org1","organizationName":"Acme"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"tok"}}`)

	msg, err := s.AddAccount(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "Added") {
		t.Fatalf("expected an Added message, got %q", msg)
	}
	data := s.GetSequenceData()
	if data.Accounts["1"].Email != "a@example.com" {
		t.Fatalf("expected slot 1 registered, got %#v", data.Accounts)
	}
}

func TestAddAccountRefreshesInPlaceWhenAlreadyRegistered(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com","accountUuid":"u1"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"old"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"new"}}`)
	msg, err := s.AddAccount(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "Updated") {
		t.Fatalf("expected an Updated message, got %q", msg)
	}
	if got := s.ReadAccountCredentials("1", "a@example.com"); !strings.Contains(got, "new") {
		t.Fatalf("expected refreshed creds, got %q", got)
	}
}

func TestAddAccountOccupiedSlotDeclinedLeavesUnchanged(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"tok"}}`)
	slot1 := 1
	if _, err := s.AddAccount(&slot1, nil); err != nil {
		t.Fatal(err)
	}

	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"b@example.com"}}`)
	_, err := s.AddAccount(&slot1, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	if data.Accounts["1"].Email != "a@example.com" {
		t.Fatalf("declining the overwrite should leave slot 1 unchanged, got %#v", data.Accounts["1"])
	}
}

func TestAddAccountFromTokenSetupToken(t *testing.T) {
	s := newTestSwitcher(t)
	msg, err := s.AddAccountFromToken("sk-ant-oat01-setup-token", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "Added") {
		t.Fatalf("expected Added, got %q", msg)
	}
	data := s.GetSequenceData()
	if len(data.Accounts) != 1 {
		t.Fatalf("expected one registered account, got %#v", data.Accounts)
	}
}

func TestAddAccountFromTokenAPIKey(t *testing.T) {
	s := newTestSwitcher(t)
	msg, err := s.AddAccountFromToken("sk-ant-api03-abcdefghijklmnop", "custom@example.com", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "Added") {
		t.Fatalf("expected Added, got %q", msg)
	}
	data := s.GetSequenceData()
	if data.Accounts["1"].Kind != "api_key" {
		t.Fatalf("expected api_key kind recorded, got %#v", data.Accounts["1"])
	}
}

func TestAddAccountFromTokenEmptyTokenErrors(t *testing.T) {
	s := newTestSwitcher(t)
	_, err := s.AddAccountFromToken("   ", "", nil, nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}
