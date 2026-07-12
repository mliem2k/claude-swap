package cswap

import (
	"testing"
)

func TestReplanNewActivePullsPlanToActiveFloor(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	// Seed a fetched entry with a slow, candidate-style plan (as if it
	// was an idle alternate before this switch).
	s.UsageStore.Record(map[string]FetchRecord{"1": {Usage: map[string]any{"ok": true}}}, identities)
	farFuture := float64(s.UsageStore.clock().Unix()) + CandidateMaxIntervalS
	slowInterval := CandidateMaxIntervalS
	s.UsageStore.SetPollPlan(map[string][2]*float64{"1": {&farFuture, &slowInterval}}, identities)

	s.ReplanNewActive("1", "a@example.com", "")

	entry := s.UsageStore.Entries(identities)["1"]
	if entry.NextPollAt == nil || *entry.NextPollAt >= farFuture {
		t.Fatalf("expected the plan pulled earlier than the old candidate-style deadline, got %#v", entry.NextPollAt)
	}
}

func TestReplanNewActiveNeverFetchedIsNoop(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	// No prior fetch at all: must not create a plan out of nothing (a
	// never-measured account is left plan-less so nothing blocks its
	// first fetch).
	s.ReplanNewActive("1", "a@example.com", "")
	entry := s.UsageStore.Entries(map[string]Identity{"1": {Email: "a@example.com"}})["1"]
	if entry.NextPollAt != nil {
		t.Fatalf("expected no plan created for a never-fetched account, got %#v", entry.NextPollAt)
	}
}

func TestReplanNewActiveNeverPushesLater(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	s.UsageStore.Record(map[string]FetchRecord{"1": {Usage: map[string]any{"ok": true}}}, identities)
	// An already-tight, near-immediate plan (tighter than MinIntervalS).
	soon := float64(s.UsageStore.clock().Unix()) + 5.0
	tight := 5.0
	s.UsageStore.SetPollPlan(map[string][2]*float64{"1": {&soon, &tight}}, identities)

	s.ReplanNewActive("1", "a@example.com", "")

	entry := s.UsageStore.Entries(identities)["1"]
	if entry.NextPollAt == nil || *entry.NextPollAt != soon {
		t.Fatalf("expected the already-tight plan left untouched, got %#v want %v", entry.NextPollAt, soon)
	}
}

func TestReplanNewActiveIsBestEffortNeverPanics(t *testing.T) {
	s := newTestSwitcher(t)
	// No SetupDirectories at all: UsageStore reads/writes against a
	// nonexistent cache dir. Must not panic; the switch it rides on has
	// already committed.
	s.ReplanNewActive("1", "a@example.com", "")
}
