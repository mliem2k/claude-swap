package cswap

// collectScheduledUsage mirrors _collect_scheduled_usage: two-phase usage
// collection with an O(1) baseline. Phase A fetches the active account
// (when its persisted poll plan says it is due, poll_policy's urgent
// mode is what tightens that cadence near the band) plus ONE due
// candidate (stalest data first); everyone else is served from the
// usage store. Phase B refetches ALL candidates and recomputes before
// any switch decision when a switch could be near: active utilization
// within EscalationMarginPct of the threshold, or active usage unknown
// (failover must not run on stale candidate data).
//
// Adapted cadences are persisted by the collector itself after each
// fetch (shared with every other surface via persistPollPlans), not by
// the engine, matching upstream commit 245f5ba; this function no longer
// calls anything poll-plan-related itself.
//
// threshold is the caller's tick-snapshotted value (see tickInner), so
// one tick fetches and decides on the same value even if a future
// TUI-driven threshold change lands mid-tick.
//
// Returns (entries, usage, headroom): entries is the raw store-backed
// per-account read model; usage carries each account's decision value
// (a map, a sentinel string, or nil); headroom is the derived headroom
// per account (nil when unknown).
func (e *AutoSwitchEngine) collectScheduledUsage(current string, quarantined map[string]bool, threshold float64) (map[string]UsageEntry, map[string]any, map[string]*float64) {
	now := float64(e.Clock().Unix())
	var candidates []string
	for _, n := range e.Switcher.SwitchableAccountNumbers() {
		if n != current && !quarantined[n] {
			candidates = append(candidates, n)
		}
	}

	pre := e.Switcher.UsageEntriesByAccount(map[string]bool{})
	plan := map[string]bool{}
	activePre, hasActivePre := pre[current]

	// The active account is nominated when never fetched, poll-due per
	// its persisted plan, or (no plan yet) past the normal cadence
	// floor. Reserve honors due-ness even inside the serve TTL, so an
	// urgent plan (60s while burning near the band) actually fetches. A
	// candidate-style plan left over from a role change the switcher
	// never saw (e.g. a manual login) is overridden past the active age
	// cap, but an exhausted account stays parked at its reset: its
	// numbers cannot move until then, and the passed reset itself makes
	// the plan due.
	staleCandidatePlan := false
	if hasActivePre && activePre.AgeS != nil && *activePre.AgeS >= ActiveMaxIntervalS &&
		activePre.PollIntervalS != nil && *activePre.PollIntervalS > ActiveMaxIntervalS {
		pct, ok := BindingPct(activePre.LastGood, e.models)
		// Matches Python's `(binding_pct(...) or 0.0) < 100.0`: an
		// unreadable/unknown pct counts as 0 (not "at the limit, stay
		// parked"), so !ok also makes this true.
		staleCandidatePlan = !ok || pct < 100.0
	}
	switch {
	case !hasActivePre || activePre.AgeS == nil:
		plan[current] = true
	case staleCandidatePlan:
		plan[current] = true
	case activePre.NextPollAt != nil && now >= *activePre.NextPollAt:
		plan[current] = true
	case activePre.NextPollAt == nil && *activePre.AgeS >= MinIntervalS:
		plan[current] = true
	}

	if e.idleHoldSince == nil {
		if pick := DueCandidate(candidates, pre, e.Clock()); pick != "" {
			plan[pick] = true
		}
	}
	entries := e.Switcher.UsageEntriesByAccount(plan)
	usage := map[string]any{}
	for num, entry := range entries {
		usage[num] = entry.DecisionValue()
	}

	activeValue := usage[current]
	activeUsageMap, _ := activeValue.(map[string]any)
	activeHeadroom, activeOK := AccountHeadroom(activeUsageMap, e.models)
	escalate := len(candidates) > 0 && ((!activeOK && !isUsageTokenExpired(activeValue)) ||
		(activeOK && 100.0-activeHeadroom >= threshold-EscalationMarginPct))
	if escalate {
		fetchAll := map[string]bool{current: true}
		for _, n := range candidates {
			fetchAll[n] = true
		}
		entries = e.Switcher.UsageEntriesByAccount(fetchAll)
		usage = map[string]any{}
		for num, entry := range entries {
			usage[num] = entry.DecisionValue()
		}
	}

	headroom := map[string]*float64{}
	for num, value := range usage {
		valueMap, _ := value.(map[string]any)
		if h, ok := AccountHeadroom(valueMap, e.models); ok {
			hCopy := h
			headroom[num] = &hCopy
		} else {
			headroom[num] = nil
		}
	}
	return entries, usage, headroom
}

// isUsageTokenExpired reports whether a decision value is the
// UsageTokenExpired sentinel string, mirroring Python's
// `usage.get(current) == USAGE_TOKEN_EXPIRED` comparison.
func isUsageTokenExpired(value any) bool {
	s, ok := value.(string)
	return ok && s == UsageTokenExpired
}
