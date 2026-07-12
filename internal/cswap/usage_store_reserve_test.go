package cswap

import (
	"testing"
	"time"
)

func newTestUsageStoreWithClock(t *testing.T, now time.Time) (*UsageStore, *time.Time) {
	t.Helper()
	cur := now
	clock := func() time.Time { return cur }
	dir := t.TempDir()
	store := NewUsageStore(dir, clock)
	return store, &cur
}

func TestReserveWinsFreshSlotOnDemand(t *testing.T) {
	store, _ := newTestUsageStoreWithClock(t, time.Unix(1000, 0))
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	won := store.Reserve([]string{"1"}, identities, true)
	if len(won) != 1 || won[0] != "1" {
		t.Fatalf("got %v", won)
	}
}

func TestReserveConcurrentCollectorsNeverDoubleWinSameSlot(t *testing.T) {
	store, cur := newTestUsageStoreWithClock(t, time.Unix(1000, 0))
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	first := store.Reserve([]string{"1"}, identities, true)
	if len(first) != 1 {
		t.Fatalf("expected the first reserve to win, got %v", first)
	}
	// Immediately after, same instant: the slot was just stamped
	// lastAttemptAt, so a second collector must not also win it.
	second := store.Reserve([]string{"1"}, identities, true)
	if len(second) != 0 {
		t.Fatalf("expected the second concurrent reserve to lose, got %v", second)
	}
	_ = cur
}

func TestReserveRespectPlansRequiresBothStaleAndDue(t *testing.T) {
	store, cur := newTestUsageStoreWithClock(t, time.Unix(1000, 0))
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	// Seed a fresh fetch (not stale) with no plan yet.
	store.Record(map[string]FetchRecord{"1": {Usage: map[string]any{"ok": true}}}, identities)
	won := store.Reserve([]string{"1"}, identities, true)
	if len(won) != 0 {
		t.Fatalf("expected an on-demand caller to skip a fresh (non-stale) entry, got %v", won)
	}
	// Advance past ServeTTLS: now stale, no plan yet (poll_due via
	// next_poll_at is None branch) -> eligible.
	*cur = cur.Add(time.Duration(ServeTTLS*float64(time.Second)) + time.Second)
	won = store.Reserve([]string{"1"}, identities, true)
	if len(won) != 1 {
		t.Fatalf("expected a stale entry with no plan to be eligible on-demand, got %v", won)
	}
}

func TestReserveNonOnDemandCanBeatServeTTLWhenPlanDue(t *testing.T) {
	store, cur := newTestUsageStoreWithClock(t, time.Unix(1000, 0))
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	store.Record(map[string]FetchRecord{"1": {Usage: map[string]any{"ok": true}}}, identities)
	// An urgent plan due in 60s, well inside ServeTTLS (180s).
	next := float64(cur.Unix()) + 60.0
	interval := UrgentIntervalS
	store.SetPollPlan(map[string][2]*float64{"1": {&next, &interval}}, identities)
	*cur = cur.Add(70 * time.Second) // past the urgent plan, still inside ServeTTLS
	wonOnDemand := store.Reserve([]string{"1"}, identities, true)
	if len(wonOnDemand) != 0 {
		t.Fatalf("expected respect_plans=true to still require staleness (ServeTTLS not yet passed), got %v", wonOnDemand)
	}
	wonEngine := store.Reserve([]string{"1"}, identities, false)
	if len(wonEngine) != 1 {
		t.Fatalf("expected respect_plans=false (the auto engine) to beat the serve TTL via the due plan, got %v", wonEngine)
	}
}

func TestReserveSkipsBackoffAndDeadToken(t *testing.T) {
	store, cur := newTestUsageStoreWithClock(t, time.Unix(1000, 0))
	identities := map[string]Identity{"1": {Email: "a@example.com"}, "2": {Email: "b@example.com"}}
	store.Record(map[string]FetchRecord{"1": {Error: "network-error"}}, identities)
	won := store.Reserve([]string{"1"}, identities, false)
	if len(won) != 0 {
		t.Fatalf("expected an in-backoff slot to be ineligible, got %v", won)
	}
	for range 2 {
		store.Record(map[string]FetchRecord{"2": {Error: "invalid_grant"}}, identities)
	}
	won = store.Reserve([]string{"2"}, identities, false)
	if len(won) != 0 {
		t.Fatalf("expected a dead-token slot to be ineligible, got %v", won)
	}
	_ = cur
}

func TestUsageStoreLast429AtPersistsAcrossSuccess(t *testing.T) {
	store, _ := newTestUsageStoreWithClock(t, time.Unix(1000, 0))
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	retryAfter := 0.0
	store.Record(map[string]FetchRecord{"1": {Error: "http-429", RetryAfterS: &retryAfter}}, identities)
	entry := store.Entries(identities)["1"]
	if entry.Last429At == nil {
		t.Fatal("expected last429At to be set after an http-429")
	}
	// A later success must NOT clear last429At.
	store.Record(map[string]FetchRecord{"1": {Usage: map[string]any{"ok": true}}}, identities)
	entry = store.Entries(identities)["1"]
	if entry.Last429At == nil {
		t.Fatal("expected last429At to survive a later success (deliberately not cleared)")
	}
}

func TestFailureBackoffEdgeIsFloorNotCap(t *testing.T) {
	retryAfter := 0.0
	// Low consecutive-failure count so the exponential curve alone would
	// be well under EdgeBackoffS; the edge rule must still floor it up to
	// EdgeBackoffS, not cap it down.
	got := failureBackoffS(1, &retryAfter)
	if got != EdgeBackoffS {
		t.Fatalf("got %v, want the edge floor %v", got, EdgeBackoffS)
	}
}

func TestFailureBackoffEdgeStillRespectsCapAtHighFailureCount(t *testing.T) {
	retryAfter := 0.0
	got := failureBackoffS(30, &retryAfter) // exponential curve saturates BackoffCapS
	if got != BackoffCapS {
		t.Fatalf("got %v, want BackoffCapS (%v)", got, BackoffCapS)
	}
}
