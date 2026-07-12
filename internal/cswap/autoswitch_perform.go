package cswap

import (
	"strconv"
	"time"
)

// accountRefFromNumString mirrors _ref for the case this plan needs it:
// building an *AccountRef from a string account number (Plan 10 covered
// the map[string]any JSON-boundary shape via AccountRefDict; this is the
// typed-struct shape SwitchEvent's from/to fields use). Returns nil if
// numStr does not parse as an integer.
func accountRefFromNumString(numStr, email string) *AccountRef {
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return nil
	}
	return &AccountRef{Number: &n, Email: email}
}

// perform mirrors _perform: executes (or, in dry-run, simulates) a switch
// to number/email, under the state lock for the whole
// recheck-switch-record sequence so two concurrent engines (loop + cron
// --once) make one serialized decision.
//
// The locked section is a closure, not the whole function body under a
// single top-level defer, so the lock is released before the final
// success-path Emit, exactly matching Python's `with self._state_lock():`
// block, which ends right after the atomic_write_json call and emits its
// SwitchEvent outside the lock. OnEvent is an arbitrary caller-supplied
// callback (its own doc comment: "exceptions it raises are not caught... a
// broken frontend should fail loudly"), so holding the lock across it would
// risk self-contention if a handler ever called back into a method that
// takes the same lock (mutateState, or another perform).
func (e *AutoSwitchEngine) perform(number, email, trigger string) (TickOutcome, error) {
	if e.DryRun {
		current := e.Switcher.CurrentAccountNumber()
		var fromRef *AccountRef
		if current != "" {
			fromRef = accountRefFromNumString(current, e.Switcher.AccountEmail(current))
		}
		toRef := accountRefFromNumString(number, email)
		e.Emit(NewSwitchEvent(trigger, fromRef, toRef, nil, true))
		return TickSwitched, nil
	}

	var result *SwitchResult
	outcome, err := func() (TickOutcome, error) {
		lock := NewFileLock(e.stateLockPath(), 10*time.Second)
		if err := lock.Lock(10 * time.Second); err != nil {
			return TickError, err
		}
		defer lock.Release()

		state := e.readState()
		if trigger == "proactive" && e.inCooldown(state) {
			e.Emit(NewNoSwitchEvent("cooldown", ""))
			return TickNoAction, nil
		}

		var switchErr error
		result, switchErr = e.Switcher.SwitchTo(number, false, nil)
		if switchErr != nil {
			return TickError, switchErr
		}
		if result == nil || !result.Switched {
			detail := ""
			if result != nil {
				detail = result.Reason
			}
			e.Emit(NewNoSwitchEvent("already-active", detail))
			return TickNoAction, nil
		}

		state["schemaVersion"] = StateSchemaVersion
		state["lastSwitchAt"] = float64(e.Clock().Unix())
		state["lastSwitchTo"] = number
		if err := atomicWriteJSON(e.StatePath, state); err != nil {
			return TickError, err
		}
		return TickSwitched, nil
	}()
	if err != nil || outcome != TickSwitched {
		return outcome, err
	}

	e.Emit(NewSwitchEvent(trigger, result.From, result.To, result.Warnings, false))
	return TickSwitched, nil
}
