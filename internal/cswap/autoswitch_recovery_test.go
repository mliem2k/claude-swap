package cswap

import "testing"

func TestEarliestRecoveryNoBlockedAccountsIsUnprovable(t *testing.T) {
	e := &AutoSwitchEngine{}
	usage := map[string]any{"1": usageWithFiveHourPct(50.0)}
	_, ok := e.earliestRecovery(usage)
	if ok {
		t.Fatal("expected no earliest recovery, nothing is blocked")
	}
}

func TestEarliestRecoveryBlockedWithKnownResetReturnsIt(t *testing.T) {
	e := &AutoSwitchEngine{}
	usage := map[string]any{
		"1": map[string]any{
			"five_hour": map[string]any{"pct": 100.0, "resets_at": "2026-07-12T01:00:00Z"},
			"seven_day": map[string]any{"pct": 40.0, "resets_at": ""},
		},
	}
	ts, ok := e.earliestRecovery(usage)
	if !ok {
		t.Fatal("expected an earliest recovery time")
	}
	want, _ := parseResetTs("2026-07-12T01:00:00Z")
	if ts != want {
		t.Fatalf("got %v want %v", ts, want)
	}
}

func TestEarliestRecoveryBlockedWithUnknownResetIsUnprovable(t *testing.T) {
	e := &AutoSwitchEngine{}
	usage := map[string]any{
		"1": map[string]any{
			"five_hour": map[string]any{"pct": 100.0, "resets_at": ""},
			"seven_day": map[string]any{"pct": 40.0, "resets_at": ""},
		},
	}
	_, ok := e.earliestRecovery(usage)
	if ok {
		t.Fatal("expected unprovable, the blocked window has no reset time")
	}
}

func TestEarliestRecoveryPicksMinimumAcrossAccounts(t *testing.T) {
	e := &AutoSwitchEngine{}
	usage := map[string]any{
		"1": map[string]any{
			"five_hour": map[string]any{"pct": 100.0, "resets_at": "2026-07-12T05:00:00Z"},
			"seven_day": map[string]any{"pct": 40.0, "resets_at": ""},
		},
		"2": map[string]any{
			"five_hour": map[string]any{"pct": 100.0, "resets_at": "2026-07-12T01:00:00Z"},
			"seven_day": map[string]any{"pct": 40.0, "resets_at": ""},
		},
	}
	ts, ok := e.earliestRecovery(usage)
	if !ok {
		t.Fatal("expected an earliest recovery time")
	}
	want, _ := parseResetTs("2026-07-12T01:00:00Z")
	if ts != want {
		t.Fatalf("got %v want %v", ts, want)
	}
}

func TestEarliestRecoveryNonMapValuesAreSkipped(t *testing.T) {
	e := &AutoSwitchEngine{}
	usage := map[string]any{"1": UsageTokenExpired}
	_, ok := e.earliestRecovery(usage)
	if ok {
		t.Fatal("expected no earliest recovery, the only entry is a sentinel string not a dict")
	}
}
