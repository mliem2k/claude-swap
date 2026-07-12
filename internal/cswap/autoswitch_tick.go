package cswap

import (
	"fmt"
	"sort"
	"strconv"
	"time"
)

// IdleHoldMaxS mirrors IDLE_HOLD_MAX_S: an owned-and-expired token
// normally means Claude Code is idle and will self-heal on next use, but
// a dead refresh token with an active user would look identical forever,
// so after this long the engine falls back to normal unhealthy counting.
const IdleHoldMaxS = 30 * 60.0

// Tick mirrors tick: evaluate once, poll usage, maybe switch. Never
// returns a raw error, every failure becomes an ErrorEvent. Collapses
// Python's two-clause except ClaudeSwitchError / except Exception into
// one: Go has no equivalent of an arbitrary uncaught exception type
// surfacing as a second flavor of error (a Go bug of that shape would
// panic, not return here), so every non-nil error from tickInner is
// reported identically.
func (e *AutoSwitchEngine) Tick() TickOutcome {
	outcome, err := e.tickInner()
	if err != nil {
		e.Emit(NewErrorEvent(err.Error(), true))
		return TickError
	}
	return outcome
}

// tickInner mirrors _tick_inner: evaluate once, poll usage, maybe switch.
func (e *AutoSwitchEngine) tickInner() (TickOutcome, error) {
	e.sleepUntilTs = nil
	e.blockedWaitLong = false
	e.idleHoldSlow = false
	settings := e.currentSettings()
	state := e.readState()
	if !e.DryRun {
		// Dry-run must not write anything, so recovered quarantines are
		// only released (state mutation) on real ticks.
		var err error
		state, err = e.releaseRecoveredQuarantines(state)
		if err != nil {
			return TickError, err
		}
	}
	quarantined := map[string]bool{}
	if q, ok := state["quarantine"].(map[string]any); ok {
		for num := range q {
			quarantined[num] = true
		}
	}

	current := e.Switcher.CurrentAccountNumber()
	if current == "" {
		e.Emit(NewPollEvent(nil, nil, nil, settings.Threshold, nil, nil))
		if e.Switcher.HasLiveLogin() {
			// Live login exists but cswap doesn't manage it: never act,
			// a switch would overwrite it without a backup.
			e.Emit(NewNoSwitchEvent("unmanaged-active-account", "run 'cswap --add-account' to include it in rotation"))
		} else {
			e.Emit(NewNoSwitchEvent("no-active-account", "log in and run 'cswap --add-account' first"))
		}
		return TickNoAction, nil
	}

	currentEmail := e.Switcher.AccountEmail(current)
	activeRef := accountRefFromNumString(current, currentEmail)

	entries, usage, headroom := e.collectScheduledUsage(current, quarantined, settings.Threshold)

	fetchErrors := map[string]string{}
	for num, entry := range entries {
		if usage[num] == nil && entry.LastError != "" {
			fetchErrors[num] = entry.LastError
		}
	}
	windows := map[string][]WindowPct{}
	for num, value := range usage {
		valueMap, _ := value.(map[string]any)
		if pcts := windowPcts(valueMap, e.models); len(pcts) > 0 {
			windows[num] = pcts
		}
	}
	var numbersOrder []string
	for num := range headroom {
		numbersOrder = append(numbersOrder, num)
	}
	sort.Slice(numbersOrder, func(i, j int) bool {
		ni, _ := strconv.Atoi(numbersOrder[i])
		nj, _ := strconv.Atoi(numbersOrder[j])
		return ni < nj
	})
	e.Emit(NewPollEvent(numbersOrder, activeRef, headroom, settings.Threshold, fetchErrors, windows))

	if !e.modelCheckDone {
		e.checkModelNames(quarantined, usage)
	}

	if e.Switcher.AccountKind(current) == "api_key" && !settings.IncludeAPIKeyAccounts {
		e.Emit(NewNoSwitchEvent("active-api-key", "API-key accounts have no quota to watch"))
		return TickNoAction, nil
	}

	activeHeadroomPtr := headroom[current]
	var trigger string
	if activeHeadroomPtr != nil {
		activeHeadroom := *activeHeadroomPtr
		e.unhealthyTicks = 0
		e.idleHoldSince = nil
		utilization := 100.0 - activeHeadroom
		if utilization < settings.Threshold {
			e.Emit(NewNoSwitchEvent("below-threshold", fmt.Sprintf("%s%% < %s%%", pctLabel(utilization), pctLabel(settings.Threshold))))
			return TickNoAction, nil
		}
		if activeHeadroom <= 0 {
			trigger = "at-limit"
		} else {
			trigger = "proactive"
		}
	} else {
		if isUsageTokenExpired(usage[current]) {
			// Expired while an owner (Claude Code / live session) holds
			// the credential: CC refreshes on every API request, so
			// expired + owner present proves Claude has been idle since
			// expiry, no quota burn, nothing to switch for.
			now := float64(e.Clock().Unix())
			if e.idleHoldSince == nil {
				e.idleHoldSince = &now
			}
			if now-*e.idleHoldSince <= IdleHoldMaxS {
				e.unhealthyTicks = 0
				e.idleHoldSlow = true
				e.Emit(NewNoSwitchEvent("active-idle", "token expired while Claude Code is idle; resumes on next use"))
				return TickNoAction, nil
			}
			// Held far longer than any idle nap should need, likely a
			// dead refresh token with an active user. Fall through to
			// normal unhealthy counting so failover can still happen.
			e.Switcher.Logger.Warn("active token expired and owned past idle-hold cap; resuming unhealthy counting", "idleHoldMaxMinutes", IdleHoldMaxS/60)
		} else {
			e.idleHoldSince = nil
		}
		e.unhealthyTicks++
		if e.unhealthyTicks < settings.UnhealthyTicks {
			e.Emit(NewNoSwitchEvent("active-usage-unknown", fmt.Sprintf("%d/%d before failover", e.unhealthyTicks, settings.UnhealthyTicks)))
			return TickNoAction, nil
		}
		trigger = "failover"
	}

	if trigger == "proactive" && e.inCooldown(state) {
		e.Emit(NewNoSwitchEvent("cooldown", ""))
		return TickNoAction, nil
	}

	// -- candidate selection --
	var candidates []string
	for _, num := range e.Switcher.SwitchableAccountNumbers() {
		if num != current && !quarantined[num] {
			candidates = append(candidates, num)
		}
	}
	var oauthCandidates, apiKeyCandidates []string
	for _, n := range candidates {
		if e.Switcher.AccountKind(n) != "api_key" {
			oauthCandidates = append(oauthCandidates, n)
		}
	}
	if settings.IncludeAPIKeyAccounts {
		for _, n := range candidates {
			if e.Switcher.AccountKind(n) == "api_key" {
				apiKeyCandidates = append(apiKeyCandidates, n)
			}
		}
	}
	if len(oauthCandidates) == 0 && len(apiKeyCandidates) == 0 {
		// Won't change until the user adds/recovers an account, no point
		// re-polling at full cadence.
		e.blockedWaitLong = true
		e.Emit(NewNoSwitchEvent("no-candidates", ""))
		return TickBlocked, nil
	}

	type qualifyingCandidate struct {
		headroom float64
		number   string
	}
	var qualifying []qualifyingCandidate
	anyKnown := false
	for _, num := range oauthCandidates {
		hPtr := headroom[num]
		if hPtr == nil {
			continue
		}
		h := *hPtr
		anyKnown = true
		if h <= 0 {
			continue // itself at its limit, never a target
		}
		if trigger == "proactive" && activeHeadroomPtr != nil {
			// Hysteresis guards only the proactive case: the candidate
			// must beat the active account by the full margin, and the
			// landing must be healthy.
			if (100.0 - h) >= settings.Threshold {
				continue
			}
			if h-*activeHeadroomPtr < settings.HysteresisPct {
				continue
			}
		}
		qualifying = append(qualifying, qualifyingCandidate{h, num})
	}
	// Best headroom first; stable sort preserves sequence order for ties.
	sort.SliceStable(qualifying, func(i, j int) bool { return qualifying[i].headroom > qualifying[j].headroom })
	var ordered []string
	for _, q := range qualifying {
		ordered = append(ordered, q.number)
	}
	if len(ordered) == 0 && len(apiKeyCandidates) > 0 {
		// Last resort: metered API-key accounts (unmeasurable headroom).
		ordered = apiKeyCandidates
	}

	if len(ordered) == 0 {
		if !anyKnown {
			e.Emit(NewNoSwitchEvent("no-comparison", "no candidate has readable usage"))
			return TickBlocked, nil
		}
		trulyExhausted := true
		for _, n := range oauthCandidates {
			hPtr := headroom[n]
			if hPtr == nil || *hPtr > 0 {
				trulyExhausted = false
				break
			}
		}
		if !trulyExhausted {
			e.Emit(NewNoSwitchEvent("no-qualifying-candidate", "no candidate is below the threshold and better than the active account by the hysteresis margin, or usage is unreadable this tick"))
			return TickBlocked, nil
		}
		e.blockedWaitLong = true
		earliestTs, ok := e.earliestRecovery(usage)
		var earliestResetAt *string
		if ok {
			sleepUntil := earliestTs + ResetSlackS
			e.sleepUntilTs = &sleepUntil
			iso := time.Unix(int64(earliestTs), 0).UTC().Format("2006-01-02T15:04:05Z")
			earliestResetAt = &iso
		}
		e.Emit(NewAllExhaustedEvent(earliestResetAt))
		return TickBlocked, nil
	}

	// -- freshen + switch --
	transientFailure := false
	for _, num := range ordered {
		email := e.Switcher.AccountEmail(num)
		if e.DryRun {
			// Dry-run stops at the decision: no token refresh, no
			// quarantine writes, freshening is a mutation.
			return e.perform(num, email, trigger)
		}
		status, err := e.freshenTarget(num, email)
		if err != nil {
			return TickError, err
		}
		switch status {
		case "identity-conflict":
			// The slot's credential is alive but belongs to a different
			// account, switching onto it would silently run the wrong
			// account. Quarantine (auto-released once a re-add replaces
			// the credential).
			if err := e.quarantine(num, email, "identity-conflict"); err != nil {
				return TickError, err
			}
			continue
		case "invalid_grant":
			if err := e.quarantine(num, email, "invalid_grant"); err != nil {
				return TickError, err
			}
			continue
		case "transient":
			transientFailure = true
			continue
		case "skip-live-session":
			continue
		}
		return e.perform(num, email, trigger)
	}

	if transientFailure {
		e.Emit(NewErrorEvent("could not freshen any candidate (network?)", true))
		return TickError, nil
	}
	e.Emit(NewNoSwitchEvent("no-viable-target", ""))
	return TickBlocked, nil
}
