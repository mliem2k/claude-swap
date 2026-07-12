package cswap

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestApplyThresholdConcurrentWithRunLoopIsRaceFree is the regression
// guard for the data race a task reviewer independently reproduced under
// `go test -race`: ApplyThreshold (added so the TUI can retarget the
// trigger threshold and poll cadence mid-run, from a goroutine other than
// RunLoop's own) calls Switcher.SetPollPolicyInputs, which used to write
// ClaudeAccountSwitcher.pollInputsOverride with no synchronization at
// all, while RunLoop's own goroutine concurrently reads (and, on a cache
// refresh, also writes) that same field plus pollInputsCache/
// pollInputsCacheMtime from pollPolicyInputs on every real usage-fetch
// pass, via persistPollPlans -> CollectUsageEntries/UsageEntriesByAccount
// -> collectScheduledUsage. Before ApplyThreshold existed, nothing called
// SetPollPolicyInputs/pollPolicyInputs from more than one goroutine, so
// this was never guarded; ApplyThreshold broke that invariant.
//
// This runs a REAL RunLoop() against a real httptest usage server so
// ticks actually reach collectScheduledUsage/persistPollPlans (a direct/
// synthetic call to pollPolicyInputs would prove nothing about the actual
// cross-goroutine call path the reviewer reported), while a second
// goroutine hammers ApplyThreshold concurrently. An accelerated fake
// clock (real elapsed time scaled up ~100,000x) keeps the account's
// persisted poll plan perpetually due, so nearly every RunLoop tick
// performs a real fetch and a real pollPolicyInputs call, maximizing how
// many unsynchronized-access windows `-race` gets to observe within the
// test's short real-time budget. The active account's usage is held well
// below every threshold ApplyThreshold cycles through so ticks always
// take the fast "below-threshold" path (TickNoAction) rather than ever
// going TickBlocked/no-candidates, which would otherwise make RunLoop
// sleep for real minutes and stall the tick cadence this test depends on.
func TestApplyThresholdConcurrentWithRunLoopIsRaceFree(t *testing.T) {
	s := setupOneActiveAccountForCollectScheduled(t)

	start := time.Now()
	base := time.Unix(1_000_000, 0)
	const timeScale = 100000.0 // 1ms real ~= 100 fake seconds
	clock := func() time.Time {
		return base.Add(time.Duration(float64(time.Since(start)) * timeScale))
	}
	s.UsageStore = NewUsageStore(filepath.Join(s.BackupDir, "cache"), clock)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		future := "2099-01-01T00:00:00Z"
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":10.0,"resets_at":"` + future + `"},` +
			`"seven_day":{"utilization":0.0,"resets_at":"` + future + `"}}`))
	}))
	t.Cleanup(srv.Close)
	withUsageURL(t, srv.URL)

	settings := DefaultAutoSwitchSettings()
	settings.IntervalSeconds = 0.02 // real 20ms cadence: many ticks in a short test
	e := NewAutoSwitchEngine(s, settings, func(AutoSwitchEvent) {}, false, "", clock)

	stopped := make(chan int, 1)
	go func() { stopped <- e.RunLoop() }()

	stopApply := make(chan struct{})
	var applyWG sync.WaitGroup
	applyWG.Add(1)
	go func() {
		defer applyWG.Done()
		i := 0
		for {
			select {
			case <-stopApply:
				return
			default:
			}
			i++
			// 80/81: comfortably above the fixed 10% usage above, so every
			// tick's threshold snapshot stays on the fast below-threshold
			// path regardless of which value happens to be live.
			e.ApplyThreshold(80 + float64(i%2))
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stopApply)
	applyWG.Wait()
	e.Stop()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLoop did not exit after Stop; test setup itself is broken")
	}

	if got := atomic.LoadInt32(&hits); got < 3 {
		t.Fatalf("expected several real usage fetches (each one drives a real "+
			"persistPollPlans -> pollPolicyInputs call) during the race window, got %d; "+
			"the accelerated clock or tick cadence may not be keeping the poll plan due", got)
	}
}
