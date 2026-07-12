package cswap

// earliestRecovery mirrors _earliest_recovery: earliest moment (epoch
// seconds) any account becomes usable again, or (0, false) when that
// moment can't be proven. Per account that's the latest reset among its
// >=100% relevant windows (an account blocked on both 5h and a scoped
// weekly limit isn't usable when the 5h rolls over), then the minimum
// across accounts. A blocked account whose exhausted windows carry no
// reset time at all makes the whole answer unprovable.
func (e *AutoSwitchEngine) earliestRecovery(usage map[string]any) (float64, bool) {
	var earliest float64
	found := false
	for _, value := range usage {
		v, ok := value.(map[string]any)
		if !ok {
			continue
		}
		blocked := false
		for _, w := range RelevantWindows(v, e.models) {
			if w.Pct >= 100.0 {
				blocked = true
				break
			}
		}
		if !blocked {
			continue // not exhausted, doesn't gate the blocked state
		}
		usableAt, ok := limitingResetTs(v, e.models)
		if !ok {
			return 0, false // blocked with unprovable recovery, don't oversleep
		}
		if !found || usableAt < earliest {
			earliest = usableAt
			found = true
		}
	}
	return earliest, found
}
