package cswap

import (
	"errors"
	"strings"
	"testing"
)

func TestRemoveAccountNoAccountsErrors(t *testing.T) {
	s := newTestSwitcher(t)
	_, err := s.RemoveAccount("1", nil, nil)
	if err == nil {
		t.Fatal("expected an error with no managed accounts")
	}
}

func TestRemoveAccountByNumber(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	msg, err := s.RemoveAccount("1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "Removed") {
		t.Fatalf("expected Removed, got %q", msg)
	}
	data := s.GetSequenceData()
	if _, ok := data.Accounts["1"]; ok {
		t.Fatal("expected slot 1 removed")
	}
}

func TestRemoveAccountDeclinedConfirmLeavesAccount(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	_, err := s.RemoveAccount("1", func(string) bool { return false }, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	if _, ok := data.Accounts["1"]; !ok {
		t.Fatal("declining removal should leave the account in place")
	}
}

func TestRemoveAccountUnknownIdentifierErrors(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	_, err := s.RemoveAccount("999", nil, nil)
	if err == nil {
		t.Fatal("expected AccountNotFound for an unknown number")
	}
}

func seedTwoAccountsSameEmailForRemove(t *testing.T) *ClaudeAccountSwitcher {
	t.Helper()
	s := newTestSwitcher(t)
	data := &SequenceData{Sequence: []int{1, 2}, Accounts: map[string]AccountRecord{
		"1": {Email: "shared@example.com", OrganizationUUID: "org1", OrganizationName: "Org One"},
		"2": {Email: "shared@example.com", OrganizationUUID: "org2", OrganizationName: "Org Two"},
	}}
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestRemoveAccountAmbiguousEmailNilDisambiguateFallsThroughToInformativeError
// mirrors SwitchTo's own nil-disambiguate contract (Python's
// `if not json_output:` guard): a caller with no interactive capability
// gets ResolveAccountIdentifier's own error, listing every candidate and
// its org tag, not a bare uninformative failure.
func TestRemoveAccountAmbiguousEmailNilDisambiguateFallsThroughToInformativeError(t *testing.T) {
	s := seedTwoAccountsSameEmailForRemove(t)
	_, err := s.RemoveAccount("shared@example.com", nil, nil)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("got %v, want an ErrConfig-wrapped ambiguous-match error", err)
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected the error to name the ambiguity, got %q", err.Error())
	}
}

// TestRemoveAccountAmbiguousEmailDisambiguateCancelReturnsCancelledMessage
// mirrors remove_account's own "Cancelled" print-and-return-None path:
// unlike SwitchTo's error-based cancel, this is a success carrying a
// "Cancelled" message, matching this function's own sibling
// confirm-declined path.
func TestRemoveAccountAmbiguousEmailDisambiguateCancelReturnsCancelledMessage(t *testing.T) {
	s := seedTwoAccountsSameEmailForRemove(t)
	disambiguate := func(candidates []string) string { return "" }
	msg, err := s.RemoveAccount("shared@example.com", nil, disambiguate)
	if err != nil {
		t.Fatalf("expected no error on a cancelled disambiguation, got %v", err)
	}
	if msg != "Cancelled" {
		t.Fatalf("got %q", msg)
	}
	data := s.GetSequenceData()
	if len(data.Accounts) != 2 {
		t.Fatal("a cancelled disambiguation must not remove either account")
	}
}

// TestRemoveAccountAmbiguousEmailDisambiguateChoosesAccount confirms a
// disambiguate callback's chosen number actually drives which of the
// ambiguous accounts gets removed.
func TestRemoveAccountAmbiguousEmailDisambiguateChoosesAccount(t *testing.T) {
	s := seedTwoAccountsSameEmailForRemove(t)
	var gotCandidates []string
	disambiguate := func(candidates []string) string {
		gotCandidates = candidates
		return "2"
	}
	msg, err := s.RemoveAccount("shared@example.com", nil, disambiguate)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "Account-2") {
		t.Fatalf("expected the chosen account (2) removed, got %q", msg)
	}
	if len(gotCandidates) != 2 {
		t.Fatalf("expected both ambiguous candidates offered, got %v", gotCandidates)
	}
	data := s.GetSequenceData()
	if _, ok := data.Accounts["2"]; ok {
		t.Fatal("account 2 should have been removed")
	}
	if _, ok := data.Accounts["1"]; !ok {
		t.Fatal("account 1 should have been left in place")
	}
}
