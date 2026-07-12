package cswap

import (
	"errors"
	"testing"
)

func TestSwitchNoAccountsErrors(t *testing.T) {
	s := newTestSwitcher(t)
	_, err := s.Switch("", nil)
	if err == nil {
		t.Fatal("expected an error with no managed accounts")
	}
}

func TestSwitchOnlyOneAccountIsNoop(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)
	data := s.GetSequenceData()
	data.Sequence = []int{1}
	delete(data.Accounts, "2")
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}

	result, err := s.Switch("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Switched {
		t.Fatalf("expected a no-op with one account, got %#v", result)
	}
	if result.Reason != "only-one-account" {
		t.Fatalf("got reason=%q", result.Reason)
	}
}

func TestSwitchPlainRotationAdvances(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)

	result, err := s.Switch("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Switched {
		t.Fatalf("expected a real switch, got %#v", result)
	}
	if result.To.Number == nil || *result.To.Number != 2 {
		t.Fatalf("expected rotation to account 2, got %#v", result.To)
	}
}

func TestSwitchFreshMachineActivatesRecorded(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())
	// No .claude.json: fresh machine, but accounts are managed.
	data := s.GetSequenceData()
	data.Sequence = []int{1}
	one := 1
	data.ActiveAccountNumber = &one
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"x"}}`)
	_ = s.WriteAccountConfig("1", "a@example.com", `{"oauthAccount":{"emailAddress":"a@example.com"}}`)

	result, err := s.Switch("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.To.Number == nil || *result.To.Number != 1 {
		t.Fatalf("expected account 1 activated, got %#v", result)
	}
}

func TestSwitchToByNumber(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)

	result, err := s.SwitchTo("2", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Switched || result.To.Number == nil || *result.To.Number != 2 {
		t.Fatalf("got %#v", result)
	}
}

func TestSwitchToAlreadyActiveIsNoop(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)

	result, err := s.SwitchTo("1", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Switched {
		t.Fatalf("expected a no-op switching to the already-active account, got %#v", result)
	}
	if result.Reason != "already-active" {
		t.Fatalf("got reason=%q", result.Reason)
	}
}

func TestSwitchToForceActivatesEvenIfAlreadyActive(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)

	result, err := s.SwitchTo("1", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "activated" {
		t.Fatalf("expected a forced activation to report 'activated', got %#v", result)
	}
}

func seedTwoAccountsSameEmailForSwitchTo(t *testing.T) *ClaudeAccountSwitcher {
	t.Helper()
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())

	data := s.GetSequenceData()
	data.Sequence = []int{1, 2}
	data.Accounts["1"] = AccountRecord{Email: "shared@example.com", OrganizationUUID: "org1", OrganizationName: "Org One"}
	data.Accounts["2"] = AccountRecord{Email: "shared@example.com", OrganizationUUID: "org2", OrganizationName: "Org Two"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}

	_ = s.WriteAccountCredentials("2", "shared@example.com", `{"claudeAiOauth":{"accessToken":"live-2","refreshToken":"r-2"}}`)
	_ = s.WriteAccountConfig("2", "shared@example.com", `{"oauthAccount":{"emailAddress":"shared@example.com"}}`)

	return s
}

// TestSwitchToAmbiguousEmailNilDisambiguateFallsThroughToInformativeError
// mirrors Python's `if not json_output:` guard: a nil disambiguate (JSON
// mode, or any caller with no interactive capability) must not short-
// circuit to a bare "cancelled" the way an earlier Go draft did; it falls
// through to ResolveAccountIdentifier's own error naming every candidate.
func TestSwitchToAmbiguousEmailNilDisambiguateFallsThroughToInformativeError(t *testing.T) {
	s := seedTwoAccountsSameEmailForSwitchTo(t)
	_, err := s.SwitchTo("shared@example.com", false, nil)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("got %v, want an ErrConfig-wrapped ambiguous-match error", err)
	}
	if !contains(err.Error(), "ambiguous") {
		t.Fatalf("expected the error to name the ambiguity, got %q", err.Error())
	}
}

// TestSwitchToAmbiguousEmailDisambiguateChoosesAccount confirms a real
// disambiguate callback's chosen number drives which ambiguous account is
// actually switched to.
func TestSwitchToAmbiguousEmailDisambiguateChoosesAccount(t *testing.T) {
	s := seedTwoAccountsSameEmailForSwitchTo(t)
	var gotCandidates []string
	disambiguate := func(candidates []string) string {
		gotCandidates = candidates
		return "2"
	}
	result, err := s.SwitchTo("shared@example.com", true, disambiguate)
	if err != nil {
		t.Fatal(err)
	}
	if result.To == nil || result.To.Number == nil || *result.To.Number != 2 {
		t.Fatalf("expected the chosen account (2), got %#v", result.To)
	}
	if len(gotCandidates) != 2 {
		t.Fatalf("expected both ambiguous candidates offered, got %v", gotCandidates)
	}
}

func TestSwitchToUnknownAccountErrors(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)
	_, err := s.SwitchTo("999", false, nil)
	if err == nil {
		t.Fatal("expected AccountNotFound for an unknown number")
	}
}
