package cswap

import (
	"testing"
)

func TestPollPolicyInputsDefaultToSettingsFile(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	threshold, models := s.pollPolicyInputs()
	want := DefaultAutoSwitchSettings()
	if threshold != want.Threshold {
		t.Fatalf("got threshold %v, want the default %v", threshold, want.Threshold)
	}
	if len(models) != 0 {
		t.Fatalf("got models %v, want none configured", models)
	}
}

func TestPollPolicyInputsOverridePinsEngineValues(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	s.SetPollPolicyInputs(77, []string{"Fable"})
	threshold, models := s.pollPolicyInputs()
	if threshold != 77 || len(models) != 1 || models[0] != "Fable" {
		t.Fatalf("got %v %v", threshold, models)
	}
	s.ClearPollPolicyInputs()
	threshold, _ = s.pollPolicyInputs()
	if threshold == 77 {
		t.Fatal("expected ClearPollPolicyInputs to drop the pin, falling back to the settings file")
	}
}

func TestCollectUsageEntriesRespectsFreshEntryOnDemand(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	info := []AccountUsageInfo{{Number: 1, Email: "a@example.com", IsActive: true, Credentials: `{"claudeAiOauth":{"accessToken":"x"}}`}}
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	s.UsageStore.Record(map[string]FetchRecord{"1": {Usage: map[string]any{"five_hour": map[string]any{"utilization": 10.0}}}}, identities)

	entries := s.CollectUsageEntries(info, nil)
	if entries["1"].LastGood == nil {
		t.Fatalf("expected the fresh entry served without a fetch, got %#v", entries["1"])
	}
}

// TestCollectUsageEntriesPersistsPollPlanAfterSuccessfulFetch fakes the
// underlying network call by redirecting usageAPIURL (withUsageURL,
// oauth_usage_test.go) at a local usageServer (autoswitch_tick_test.go),
// this port's established seam for faking a usage fetch; there is no
// separate records-map seam to reuse instead.
func TestCollectUsageEntriesPersistsPollPlanAfterSuccessfulFetch(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	withUsageURL(t, usageServer(t, 10.0, 10.0).URL)
	info := []AccountUsageInfo{{Number: 1, Email: "a@example.com", IsActive: true, Credentials: `{"claudeAiOauth":{"accessToken":"x"}}`}}

	s.CollectUsageEntries(info, nil)

	entry := s.UsageStore.Entries(map[string]Identity{"1": {Email: "a@example.com"}})["1"]
	if entry.NextPollAt == nil || entry.PollIntervalS == nil {
		t.Fatalf("expected a poll plan persisted after a successful fetch, got %#v", entry)
	}
}
