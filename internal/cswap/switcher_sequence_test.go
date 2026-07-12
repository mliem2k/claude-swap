package cswap

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInitSequenceFileCreatesDefault(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	if data == nil || data.ActiveAccountNumber != nil || len(data.Sequence) != 0 {
		t.Fatalf("expected fresh default sequence, got %#v", data)
	}
}

func TestInitSequenceFileNoopWhenExists(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	one := 1
	data := s.GetSequenceData()
	data.ActiveAccountNumber = &one
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	got := s.GetSequenceData()
	if got.ActiveAccountNumber == nil || *got.ActiveAccountNumber != 1 {
		t.Fatalf("re-init should not overwrite existing data, got %#v", got)
	}
}

func TestGetNextAccountNumber(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	if got := s.GetNextAccountNumber(); got != 1 {
		t.Fatalf("expected 1 for an empty registry, got %d", got)
	}
	data := s.GetSequenceData()
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"}
	data.Accounts["3"] = AccountRecord{Email: "b@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	if got := s.GetNextAccountNumber(); got != 4 {
		t.Fatalf("expected max+1=4, got %d", got)
	}
}

func TestGetCurrentAccountFromClaudeJSON(t *testing.T) {
	s := newTestSwitcher(t)
	if err := os.MkdirAll(filepath.Dir(s.GetClaudeConfigPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com","organizationUuid":"org1"}}`)
	email, org, ok := s.GetCurrentAccount()
	if !ok || email != "a@example.com" || org != "org1" {
		t.Fatalf("got email=%q org=%q ok=%v", email, org, ok)
	}
}

func TestGetCurrentAccountMissingIsNotOK(t *testing.T) {
	s := newTestSwitcher(t)
	_, _, ok := s.GetCurrentAccount()
	if ok {
		t.Fatal("expected ok=false with no claude.json")
	}
}

func TestFindAccountSlot(t *testing.T) {
	data := &SequenceData{Accounts: map[string]AccountRecord{
		"1": {Email: "a@example.com", OrganizationUUID: "org1"},
		"2": {Email: "b@example.com"},
	}}
	if got := FindAccountSlot(data, "a@example.com", "org1"); got != "1" {
		t.Fatalf("got %q", got)
	}
	if got := FindAccountSlot(data, "b@example.com", ""); got != "2" {
		t.Fatalf("got %q", got)
	}
	if got := FindAccountSlot(data, "nope@example.com", ""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestAccountKindDefaultsToOAuth(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"}
	data.Accounts["2"] = AccountRecord{Email: "b@example.com", Kind: "api_key"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	if got := s.AccountKind("1"); got != "oauth" {
		t.Fatalf("expected default oauth, got %q", got)
	}
	if got := s.AccountKind("2"); got != "api_key" {
		t.Fatalf("expected api_key, got %q", got)
	}
}

func TestGetDisplayTag(t *testing.T) {
	if got := GetDisplayTag("a@example.com", "Acme", "org1"); got != "Acme" {
		t.Fatalf("got %q", got)
	}
	if got := GetDisplayTag("a@example.com", "", ""); got != "personal" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveAccountIdentifierByNumber(t *testing.T) {
	s := newTestSwitcher(t)
	got, err := s.ResolveAccountIdentifier("42")
	if err != nil || got != "42" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestResolveAccountIdentifierAmbiguousEmailErrors(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Accounts["1"] = AccountRecord{Email: "a@example.com", OrganizationName: "Acme"}
	data.Accounts["2"] = AccountRecord{Email: "a@example.com", OrganizationName: "Other"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	_, err := s.ResolveAccountIdentifier("a@example.com")
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("expected ErrConfig on ambiguous email, got %v", err)
	}
}

func TestMigrateOrgFieldsBackfillsMissingFields(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"} // no org fields set
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}

	migrated := s.GetSequenceDataMigrated()
	if migrated == nil {
		t.Fatal("expected migrated data")
	}
	acct := migrated.Accounts["1"]
	if acct.OrganizationUUID != "" {
		t.Fatalf("expected empty org uuid backfilled, got %q", acct.OrganizationUUID)
	}
}
