package cswap

import (
	"testing"
	"time"
)

func TestUsageEntryFresh(t *testing.T) {
	now := time.Now()
	e := UsageEntry{FetchedAt: floatPtr(float64(now.Add(-10 * time.Second).Unix()))}
	if !e.Fresh(now, 30*time.Second) {
		t.Fatal("10s old should be fresh under a 30s ttl")
	}
	if e.Fresh(now, 5*time.Second) {
		t.Fatal("10s old should not be fresh under a 5s ttl")
	}
}

func TestUsageEntryInBackoff(t *testing.T) {
	now := time.Now()
	e := UsageEntry{BackoffUntil: floatPtr(float64(now.Add(time.Minute).Unix()))}
	if !e.InBackoff(now) {
		t.Fatal("expected in backoff")
	}
	if (UsageEntry{}).InBackoff(now) {
		t.Fatal("no backoffUntil should mean not in backoff")
	}
}

func TestUsageEntryTokenDead(t *testing.T) {
	e := UsageEntry{AuthDeadStrikes: 1}
	if !e.TokenDead(AuthDeadStrikes) {
		t.Fatal("expected token dead at the strike threshold")
	}
	if (UsageEntry{}).TokenDead(AuthDeadStrikes) {
		t.Fatal("zero strikes should not be dead")
	}
}

func TestUsageEntryDecisionValueSentinelWins(t *testing.T) {
	e := UsageEntry{Sentinel: strPtr("api key"), LastGood: map[string]any{"x": 1.0}}
	if e.DecisionValue() != "api key" {
		t.Fatalf("sentinel should win, got %#v", e.DecisionValue())
	}
}

func TestUsageEntryDecisionValueFreshLastGood(t *testing.T) {
	e := UsageEntry{LastGood: map[string]any{"x": 1.0}, AgeS: floatPtr(10)}
	got, ok := e.DecisionValue().(map[string]any)
	if !ok || got["x"] != 1.0 {
		t.Fatalf("expected last-good to win, got %#v", e.DecisionValue())
	}
}

func TestUsageEntryDecisionValueUnknownWhenStaleAndNotTrusted(t *testing.T) {
	e := UsageEntry{LastGood: map[string]any{"x": 1.0}, AgeS: floatPtr(float64(StaleOKS/float64(time.Second)) + 1)}
	if e.DecisionValue() != nil {
		t.Fatalf("expected nil (unknown) for stale untrusted data, got %#v", e.DecisionValue())
	}
}

// TestUsageEntryDecisionValueSubSecondBoundaryNotFloored is the regression
// test for a truncate-then-multiply bug: time.Duration(*e.AgeS)*time.Second
// used to floor AgeS's fractional seconds before comparing against
// StaleOKS, so 300.4s (just past the 300s trust boundary) was silently
// treated as exactly 300s (still trusted) instead of correctly stale.
// Python's bare `self.age_s <= STALE_OK_S` float compare has no such
// flooring.
func TestUsageEntryDecisionValueSubSecondBoundaryNotFloored(t *testing.T) {
	e := UsageEntry{LastGood: map[string]any{"x": 1.0}, AgeS: floatPtr(float64(StaleOKS/float64(time.Second)) + 0.4)}
	if e.DecisionValue() != nil {
		t.Fatalf("expected nil (unknown/stale) for AgeS 0.4s past the trust boundary, got %#v", e.DecisionValue())
	}
}

func TestDueCandidatePicksStalestFetched(t *testing.T) {
	now := time.Now()
	entries := map[string]UsageEntry{
		"1": {FetchedAt: floatPtr(float64(now.Add(-2 * time.Hour).Unix()))},
		"2": {FetchedAt: floatPtr(float64(now.Add(-1 * time.Hour).Unix()))},
	}
	got := DueCandidate([]string{"1", "2"}, entries, now)
	if got != "1" {
		t.Fatalf("expected the stalest entry (1), got %q", got)
	}
}

func TestDueCandidateSkipsBackoffAndDeadTokens(t *testing.T) {
	now := time.Now()
	entries := map[string]UsageEntry{
		"1": {BackoffUntil: floatPtr(float64(now.Add(time.Hour).Unix()))},
		"2": {AuthDeadStrikes: AuthDeadStrikes},
		"3": {},
	}
	got := DueCandidate([]string{"1", "2", "3"}, entries, now)
	if got != "3" {
		t.Fatalf("expected the only eligible entry (3), got %q", got)
	}
}

// TestDueCandidateFullTieBreaksLexicographically mirrors due.sort()'s
// (rank, fetched_at, num) tuple sort: Python's third element is the
// account number as a *string*, so two never-fetched (rank=0,
// fetchedAt=0) candidates break the tie lexicographically ("10" < "9"),
// not numerically and not by candidates' input order (which a bare
// min-scan would otherwise default to).
func TestDueCandidateFullTieBreaksLexicographically(t *testing.T) {
	now := time.Now()
	entries := map[string]UsageEntry{
		"9":  {},
		"10": {},
	}
	got := DueCandidate([]string{"9", "10"}, entries, now)
	if got != "10" {
		t.Fatalf("expected the lexicographically-smaller \"10\" (< \"9\") on a full tie, got %q", got)
	}
	// Order-independence: the same tie resolves the same way regardless
	// of which candidate appears first in the input slice.
	got = DueCandidate([]string{"10", "9"}, entries, now)
	if got != "10" {
		t.Fatalf("expected \"10\" regardless of input order, got %q", got)
	}
}

func floatPtr(v float64) *float64 { return &v }
func strPtr(s string) *string     { return &s }
