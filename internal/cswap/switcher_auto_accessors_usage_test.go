package cswap

import "testing"

func setupOneAccountForAutoAccessors(t *testing.T) *ClaudeAccountSwitcher {
	t.Helper()
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUsageEntriesByAccountReturnsOneRowPerAccount(t *testing.T) {
	// A nil fetch set means every account is fetch-eligible, so this
	// would otherwise reach the real usage API; redirect it locally
	// (nothing listens here) so the test stays offline and deterministic,
	// matching TestFetchUsageNetworkFailureReturnsNil's established
	// pattern (oauth_usage_test.go).
	withUsageURL(t, "http://127.0.0.1:1")
	s := setupOneAccountForAutoAccessors(t)
	got := s.UsageEntriesByAccount(nil)
	if _, ok := got["1"]; !ok {
		t.Fatalf("got %#v", got)
	}
}

func TestUsageEntriesByAccountRestrictsToFetchSet(t *testing.T) {
	s := setupOneAccountForAutoAccessors(t)
	// An empty (non-nil) fetch set means nothing is eligible this pass;
	// the entry for "1" must still be present (as a not-fetched
	// placeholder), just not freshly fetched. This only asserts the call
	// doesn't panic and still returns a row per managed account, matching
	// CollectUsageEntries' own already-tested fetch-gating behavior.
	got := s.UsageEntriesByAccount(map[string]bool{})
	if _, ok := got["1"]; !ok {
		t.Fatalf("got %#v", got)
	}
}

func TestUsageFetchStampsNoAccountsIsEmpty(t *testing.T) {
	s := newTestSwitcher(t)
	got := s.UsageFetchStamps()
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestUsageFetchStampsReturnsPerSlotFetchedAt(t *testing.T) {
	s := setupOneAccountForAutoAccessors(t)
	got := s.UsageFetchStamps()
	if _, ok := got["1"]; !ok {
		t.Fatalf("expected a row for slot 1, got %#v", got)
	}
	// A never-fetched slot's stamp is nil, matching Python's None.
	if got["1"] != nil {
		t.Fatalf("expected nil (never fetched), got %v", *got["1"])
	}
}

func TestSetUsagePollPlanRoundTrips(t *testing.T) {
	s := setupOneAccountForAutoAccessors(t)
	next := 123.0
	interval := 45.0
	s.SetUsagePollPlan(map[string][2]*float64{"1": {&next, &interval}})

	entries := s.UsageEntriesByAccount(map[string]bool{})
	entry, ok := entries["1"]
	if !ok {
		t.Fatalf("got %#v", entries)
	}
	if entry.NextPollAt == nil || *entry.NextPollAt != 123.0 {
		t.Fatalf("got %#v", entry.NextPollAt)
	}
	if entry.PollIntervalS == nil || *entry.PollIntervalS != 45.0 {
		t.Fatalf("got %#v", entry.PollIntervalS)
	}
}
