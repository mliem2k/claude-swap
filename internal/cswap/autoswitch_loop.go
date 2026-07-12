package cswap

import (
	"math/rand"
	"time"
)

// MaxSleepS mirrors MAX_SLEEP_S: never trust one long sleep alone (laptops
// suspend, clocks drift), cap and re-evaluate.
const MaxSleepS = 6 * 3600.0

// NoResetFallbackS mirrors NO_RESET_FALLBACK_S: the crawl cadence when
// blocked with no provable recovery time.
const NoResetFallbackS = 300.0

// Stop mirrors stop: asks RunLoop to exit, wakes it from any sleep. Safe
// to call more than once (Python's threading.Event.set() is idempotent;
// a bare close(stopCh) is not, so this checks before closing under a
// lock shared with RunLoop's channel-recreate).
func (e *AutoSwitchEngine) Stop() {
	e.stopMu.Lock()
	defer e.stopMu.Unlock()
	select {
	case <-e.stopCh:
		// already stopped
	default:
		close(e.stopCh)
	}
}

// Wake mirrors wake: cuts the current inter-tick sleep short and ticks
// now. A no-op if RunLoop has not started yet (mirrors Python's run_loop
// clearing _wake before it could possibly matter, since no tick has
// happened for it to interrupt). Safe to call from any goroutine.
func (e *AutoSwitchEngine) Wake() {
	e.stopMu.Lock()
	ch := e.wakeCh
	e.stopMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// nextDelay mirrors _next_delay.
func (e *AutoSwitchEngine) nextDelay(outcome TickOutcome) float64 {
	interval := e.currentSettings().IntervalSeconds
	if outcome == TickBlocked {
		if e.sleepUntilTs != nil {
			delay := *e.sleepUntilTs - float64(e.Clock().Unix())
			return min(max(delay, interval), MaxSleepS)
		}
		if e.blockedWaitLong {
			// Truly exhausted with no reset time known / no candidates.
			return max(interval, NoResetFallbackS)
		}
		// Blocked on something that can resolve any tick (hysteresis,
		// unreadable usage), keep the normal cadence so the at-limit
		// escape isn't missed.
	} else if outcome == TickNoAction && e.idleHoldSlow {
		// Idle-hold: Claude is idle on an expired token, nothing changes
		// until the user comes back, so crawl. Worst case protection
		// resumes one slow tick after they do.
		return max(interval, NoResetFallbackS)
	}
	// +/-10% jitter so multiple machines don't synchronize their API hits.
	return interval * (0.9 + 0.2*rand.Float64())
}

// RunLoop mirrors run_loop: ticks forever (until Stop), a failing tick
// never kills it (Tick itself already guards against panics/errors and
// always returns a TickOutcome, matching Python's own "tick() already
// guards" comment on the analogous try/except it does not need here
// either).
//
// stopCh is never recreated here (only wakeCh is): it is set once, by the
// constructor, and only ever closed, matching Python's abd4aef fix for a
// real stop-before-start race (Stop() called, then RunLoop() overwrote the
// already-closed channel with a fresh open one, silently discarding the
// pending stop and ticking anyway). Recreating stopCh on every call would
// also let a SECOND RunLoop() call on an already-stopped engine run again;
// every real caller (the TUI's and menu bar's startEngine/restartEngine)
// already constructs a brand new AutoSwitchEngine per start, so this loses
// no real capability, and it makes a second call on an already-stopped
// instance return 0 immediately without ticking, matching Python's own
// engines-are-single-use invariant exactly (once _stop is set, it is never
// cleared, so any later run_loop call sees it set immediately too).
func (e *AutoSwitchEngine) RunLoop() int {
	e.stopMu.Lock()
	e.wakeCh = make(chan struct{}, 1)
	stopCh := e.stopCh
	wakeCh := e.wakeCh
	e.stopMu.Unlock()

	for {
		select {
		case <-stopCh:
			return 0
		default:
		}

		outcome := e.Tick()
		delay := e.nextDelay(outcome)
		if delay > e.currentSettings().IntervalSeconds*1.5 {
			until := time.Now().UTC().Add(time.Duration(delay * float64(time.Second)))
			e.Emit(NewSleepEvent(delay, until.Format("2006-01-02T15:04:05Z")))
		}

		select {
		case <-stopCh:
			return 0
		case <-wakeCh:
		case <-time.After(time.Duration(delay * float64(time.Second))):
		}
	}
}
