package cswap

import "testing"

func TestBuildListPayloadBasicShape(t *testing.T) {
	s := newTestSwitcher(t)
	accountsInfo := []AccountUsageInfo{
		{Number: 1, Email: "a@example.com", IsActive: true},
		{Number: 2, Email: "b@example.com", IsActive: false},
	}
	entries := map[string]UsageEntry{
		"1": {LastGood: map[string]any{"five_hour": map[string]any{"pct": 10.0}}},
		"2": {LastError: "boom"},
	}
	payload := s.BuildListPayload(accountsInfo, entries)
	if payload["schemaVersion"] != JSONSchemaVersion {
		t.Fatalf("got %#v", payload["schemaVersion"])
	}
	if payload["activeAccountNumber"] != 1 {
		t.Fatalf("got %#v", payload["activeAccountNumber"])
	}
	accounts, ok := payload["accounts"].([]map[string]any)
	if !ok || len(accounts) != 2 {
		t.Fatalf("got %#v", payload["accounts"])
	}
	if accounts[0]["email"] != "a@example.com" || accounts[0]["active"] != true {
		t.Fatalf("got %#v", accounts[0])
	}
}

func TestBuildListPayloadNoActiveAccountNumberIsNil(t *testing.T) {
	s := newTestSwitcher(t)
	accountsInfo := []AccountUsageInfo{{Number: 1, Email: "a@example.com", IsActive: false}}
	entries := map[string]UsageEntry{"1": {}}
	payload := s.BuildListPayload(accountsInfo, entries)
	if payload["activeAccountNumber"] != nil {
		t.Fatalf("got %#v, want nil (no active account)", payload["activeAccountNumber"])
	}
}

func TestBuildListPayloadOmitsWarningsWhenClean(t *testing.T) {
	s := newTestSwitcher(t)
	accountsInfo := []AccountUsageInfo{{Number: 1, Email: "a@example.com", IsActive: true}}
	entries := map[string]UsageEntry{"1": {}}
	payload := s.BuildListPayload(accountsInfo, entries)
	if _, ok := payload["duplicateAccountWarnings"]; ok {
		t.Fatal("expected no duplicateAccountWarnings key when clean")
	}
	if _, ok := payload["lockstepUsageWarnings"]; ok {
		t.Fatal("expected no lockstepUsageWarnings key when clean")
	}
	if _, ok := payload["unclaimedCredentials"]; ok {
		t.Fatal("expected no unclaimedCredentials key when none stashed")
	}
}
