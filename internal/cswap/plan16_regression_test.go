package cswap

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestPlan16NoThunderingHerdAfterEdge429 is the whole-plan regression guard.
//
// It reproduces the exact scenario this plan exists to fix: an active account
// whose usage fetch always answers HTTP 429 with Retry-After: 0 (the
// saturated rolling-window edge). Such an account must NOT be re-fetched on
// every scheduled collection pass. After the first 429 the store's failure
// backoff floors to EdgeBackoffS (300s) via failureBackoffS's edge rule, and
// Reserve's under-lock eligibility check honors that backoff, so repeated
// passes inside the window never re-probe.
//
// This exercises the full wiring end to end, not any piece in isolation:
//
//	collectScheduledUsage -> UsageEntriesByAccount -> CollectUsageEntries ->
//	Reserve (backoff gate) -> RunUsageFetches -> real HTTP 429 ->
//	classifyUsageError -> Record -> failureBackoffS (edge floor).
//
// A controllable clock advances to +150s, a point where the OLD model's
// 30s/120s backoff would already have re-fetched, proving the new 300s edge
// floor is what actually suppresses the re-fetch, then past EdgeBackoffS to
// prove the backoff is bounded (the account is eventually probed again).
func TestPlan16NoThunderingHerdAfterEdge429(t *testing.T) {
	s := setupOneActiveAccountForCollectScheduled(t)

	base := time.Unix(1_000_000, 0)
	cur := base
	clock := func() time.Time { return cur }
	// Replace the switcher's store with one on the SAME cache dir but a
	// controllable clock, so backoff windows advance deterministically.
	s.UsageStore = NewUsageStore(filepath.Join(s.BackupDir, "cache"), clock)
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(AutoSwitchEvent) {}, false, "", clock)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	withUsageURL(t, srv.URL)

	identities := map[string]Identity{"1": {Email: "a@example.com"}}

	// Pass 1: never fetched -> nominated -> fetch attempted -> 429.
	e.collectScheduledUsage("1", map[string]bool{}, 90)
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("first pass: expected exactly 1 fetch attempt, got %d", got)
	}
	entry := s.UsageStore.Entries(identities)["1"]
	if entry.BackoffUntil == nil {
		t.Fatal("expected a backoff recorded after the 429")
	}
	wantBackoff := float64(base.Unix()) + EdgeBackoffS
	if *entry.BackoffUntil != wantBackoff {
		t.Fatalf("edge backoff floored to the wrong value: got now+%v, want now+EdgeBackoffS (now+%v)",
			*entry.BackoffUntil-float64(base.Unix()), EdgeBackoffS)
	}
	if entry.Last429At == nil {
		t.Fatal("expected last429At stamped after the 429")
	}

	// Advance 150s: past the OLD model's 30s/120s backoff (which would have
	// re-fetched here) but well inside the new 300s edge floor. Several rapid
	// passes must all be suppressed.
	cur = base.Add(150 * time.Second)
	for i := 0; i < 4; i++ {
		e.collectScheduledUsage("1", map[string]bool{}, 90)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("inside EdgeBackoffS window (+150s): expected NO re-fetch (still 1), got %d attempts", got)
	}

	// Advance just past EdgeBackoffS: the backoff is bounded, so the account
	// is probed again exactly once (proving it is a backoff, not a lockout).
	cur = base.Add(time.Duration(EdgeBackoffS+1) * time.Second)
	e.collectScheduledUsage("1", map[string]bool{}, 90)
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("after EdgeBackoffS elapsed (+301s): expected exactly one more probe (2 total), got %d", got)
	}
}
