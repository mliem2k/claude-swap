package cswap

import (
	"testing"
	"time"
)

func TestStopIsIdempotent(t *testing.T) {
	s := newTestSwitcher(t)
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(AutoSwitchEvent) {}, false, "", nil)
	e.Stop()
	e.Stop() // must not panic
}

func TestNextDelayBlockedWithSleepUntilTs(t *testing.T) {
	now := time.Unix(1000, 0)
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: func() time.Time { return now }}
	future := float64(now.Unix() + 500)
	e.sleepUntilTs = &future
	got := e.nextDelay(TickBlocked)
	if got != 500.0 {
		t.Fatalf("got %v", got)
	}
}

func TestNextDelayBlockedSleepUntilTsFlooredAtInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: func() time.Time { return now }}
	past := float64(now.Unix() - 10) // already in the past
	e.sleepUntilTs = &past
	got := e.nextDelay(TickBlocked)
	if got != e.Settings.IntervalSeconds {
		t.Fatalf("got %v want %v", got, e.Settings.IntervalSeconds)
	}
}

func TestNextDelayBlockedSleepUntilTsCappedAtMaxSleep(t *testing.T) {
	now := time.Unix(1000, 0)
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: func() time.Time { return now }}
	farFuture := float64(now.Unix()) + MaxSleepS + 1000
	e.sleepUntilTs = &farFuture
	got := e.nextDelay(TickBlocked)
	if got != MaxSleepS {
		t.Fatalf("got %v want %v", got, MaxSleepS)
	}
}

func TestNextDelayBlockedWaitLongNoSleepUntilTs(t *testing.T) {
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: time.Now}
	e.blockedWaitLong = true
	got := e.nextDelay(TickBlocked)
	if got != NoResetFallbackS {
		t.Fatalf("got %v want %v", got, NoResetFallbackS)
	}
}

func TestNextDelayBlockedNormalCadenceIsJittered(t *testing.T) {
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: time.Now}
	got := e.nextDelay(TickBlocked)
	interval := e.Settings.IntervalSeconds
	if got < interval*0.9 || got > interval*1.1 {
		t.Fatalf("got %v, expected within +/-10%% of %v", got, interval)
	}
}

func TestNextDelayNoActionIdleHoldSlow(t *testing.T) {
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: time.Now}
	e.idleHoldSlow = true
	got := e.nextDelay(TickNoAction)
	if got != NoResetFallbackS {
		t.Fatalf("got %v want %v", got, NoResetFallbackS)
	}
}

func TestNextDelaySwitchedIsJittered(t *testing.T) {
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: time.Now}
	got := e.nextDelay(TickSwitched)
	interval := e.Settings.IntervalSeconds
	if got < interval*0.9 || got > interval*1.1 {
		t.Fatalf("got %v, expected within +/-10%% of %v", got, interval)
	}
}

func TestRunLoopStopsPromptlyAndReturnsZero(t *testing.T) {
	s := newTestSwitcher(t)
	settings := DefaultAutoSwitchSettings()
	settings.IntervalSeconds = 0.01 // fast enough for a real test, no active account so ticks are cheap
	e := NewAutoSwitchEngine(s, settings, func(AutoSwitchEvent) {}, false, "", nil)

	done := make(chan int, 1)
	go func() { done <- e.RunLoop() }()

	time.Sleep(30 * time.Millisecond)
	e.Stop()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunLoop did not return after Stop")
	}
}

// TestRunLoopSecondCallOnStoppedEngineReturnsImmediately locks in the
// engines-are-single-use invariant RunLoop's stop-before-start race fix
// depends on: once Stop() has been observed, stopCh is never reopened, so
// a later RunLoop() call on the SAME instance must return 0 without
// ticking, exactly like Python's run_loop (_stop is set once, never
// cleared). Restarting for real means constructing a fresh
// AutoSwitchEngine, which every real caller (TUI/menu bar startEngine)
// already does; nothing in this codebase relies on re-running a stopped
// engine.
func TestRunLoopSecondCallOnStoppedEngineReturnsImmediately(t *testing.T) {
	s := newTestSwitcher(t)
	settings := DefaultAutoSwitchSettings()
	settings.IntervalSeconds = 0.01
	e := NewAutoSwitchEngine(s, settings, func(AutoSwitchEvent) {}, false, "", nil)

	done1 := make(chan int, 1)
	go func() { done1 <- e.RunLoop() }()
	time.Sleep(20 * time.Millisecond)
	e.Stop()
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("first RunLoop did not return")
	}

	done2 := make(chan int, 1)
	go func() { done2 <- e.RunLoop() }()
	select {
	case code := <-done2:
		if code != 0 {
			t.Fatalf("got %d", code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second RunLoop call on an already-stopped engine did not return immediately")
	}
}

// TestRunLoopStopBeforeStartIsNotLost is the regression test for the race
// Python's abd4aef fixed: Stop() called before RunLoop() has ever run on
// this engine must not be silently discarded. Previously RunLoop()
// unconditionally recreated stopCh on entry, overwriting the already-closed
// channel with a fresh open one and ticking anyway.
func TestRunLoopStopBeforeStartIsNotLost(t *testing.T) {
	s := newTestSwitcher(t)
	settings := DefaultAutoSwitchSettings()
	settings.IntervalSeconds = 5 // deliberately not fast: a regression would hang this test for seconds
	e := NewAutoSwitchEngine(s, settings, func(AutoSwitchEvent) {}, false, "", nil)

	e.Stop() // stop BEFORE RunLoop is ever called

	done := make(chan int, 1)
	go func() { done <- e.RunLoop() }()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("got %d", code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RunLoop did not return promptly after a pre-start Stop(): the stop signal was lost")
	}
}

// TestRunLoopEmitsSleepEventWhenDelayExceedsIntervalBy50Percent drives a
// real tickInner "no-candidates" BLOCKED outcome (one account added, no
// other switchable accounts) rather than hand-setting blockedWaitLong:
// tickInner resets blockedWaitLong = false at the top of every tick and
// only this decision path (autoswitch_tick.go's no-candidates branch) sets
// it back to true, so hand-setting the field before RunLoop starts would be
// overwritten by the very first tick and prove nothing about RunLoop's own
// SleepEvent-emission threshold.
func TestRunLoopEmitsSleepEventWhenDelayExceedsIntervalBy50Percent(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	// Active account's usage reads at 95% (above the 90% default threshold,
	// so the engine wants to switch) with no other account to switch to:
	// tickInner's no-candidates branch sets blockedWaitLong = true and
	// returns TickBlocked every tick.
	withUsageURL(t, usageServer(t, 95.0, 0.0).URL)

	settings := DefaultAutoSwitchSettings()
	settings.IntervalSeconds = 0.01
	var events []AutoSwitchEvent
	e := NewAutoSwitchEngine(s, settings, func(ev AutoSwitchEvent) { events = append(events, ev) }, false, "", nil)

	done := make(chan int, 1)
	go func() { done <- e.RunLoop() }()
	time.Sleep(30 * time.Millisecond)
	e.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLoop did not return after Stop")
	}

	found := false
	for _, ev := range events {
		if _, ok := ev.(*SleepEvent); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("expected at least one SleepEvent given blockedWaitLong forces a >1.5x-interval delay")
	}
}
