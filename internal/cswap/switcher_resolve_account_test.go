package cswap

import (
	"errors"
	"testing"
)

func seedOneAccountForResolve(t *testing.T) *ClaudeAccountSwitcher {
	t.Helper()
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Sequence = []int{1}
	data.Accounts["1"] = AccountRecord{Email: "a@example.com", OrganizationUUID: "org1"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestResolveAccountByNumber(t *testing.T) {
	s := seedOneAccountForResolve(t)
	num, email, org, err := s.ResolveAccount("1")
	if err != nil {
		t.Fatal(err)
	}
	if num != "1" || email != "a@example.com" || org != "org1" {
		t.Fatalf("got %q %q %q", num, email, org)
	}
}

func TestResolveAccountByEmail(t *testing.T) {
	s := seedOneAccountForResolve(t)
	num, email, _, err := s.ResolveAccount("a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if num != "1" || email != "a@example.com" {
		t.Fatalf("got %q %q", num, email)
	}
}

func TestResolveAccountNotFoundIsAccountNotFoundError(t *testing.T) {
	s := seedOneAccountForResolve(t)
	_, _, _, err := s.ResolveAccount("nobody@example.com")
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestResolveAccountDigitNotPresentIsAccountNotFoundError(t *testing.T) {
	s := seedOneAccountForResolve(t)
	_, _, _, err := s.ResolveAccount("99")
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestResolveAccountAmbiguousEmailPropagatesConfigError(t *testing.T) {
	s := seedOneAccountForResolve(t)
	data := s.GetSequenceData()
	data.Sequence = []int{1, 2}
	data.Accounts["2"] = AccountRecord{Email: "a@example.com", OrganizationUUID: "org2"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := s.ResolveAccount("a@example.com")
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("got %v", err)
	}
}
