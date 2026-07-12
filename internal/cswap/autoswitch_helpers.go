package cswap

import "time"

// BindingPct mirrors binding_pct: utilization of the binding (worst)
// relevant window, or (0, false) when unknown.
func BindingPct(usage map[string]any, models []string) (float64, bool) {
	headroom, ok := AccountHeadroom(usage, models)
	if !ok {
		return 0, false
	}
	return 100.0 - headroom, true
}

// windowPcts mirrors _window_pcts: ordered window label -> pct pairs,
// deliberately restricted to the windows the decision reads (same models
// filter): showing an unconfigured scoped window at 100% next to a switch
// onto that account would look like a bug, when the engine correctly
// ignored it. Full per-model usage lives in `cswap list`.
func windowPcts(usage map[string]any, models []string) []WindowPct {
	windows := RelevantWindows(usage, models)
	out := make([]WindowPct, len(windows))
	for i, w := range windows {
		out[i] = WindowPct{Label: w.Label, Pct: w.Pct}
	}
	return out
}

// limitingResetTs mirrors _limiting_reset_ts: epoch when the last of the
// >=100% relevant windows resets (account usable again), or (0, false)
// when nothing is at cap.
func limitingResetTs(usage map[string]any, models []string) (float64, bool) {
	var latest float64
	found := false
	for _, w := range RelevantWindows(usage, models) {
		if w.Pct < 100.0 {
			continue
		}
		ts, ok := parseResetTs(w.ResetsAt)
		if ok && (!found || ts > latest) {
			latest = ts
			found = true
		}
	}
	return latest, found
}

// earliestFutureResetTs mirrors _earliest_future_reset_ts: epoch of the
// next relevant-window reset ahead of now, any utilization, or (0, false)
// when none is ahead of now.
func earliestFutureResetTs(usage map[string]any, now float64, models []string) (float64, bool) {
	var earliest float64
	found := false
	for _, w := range RelevantWindows(usage, models) {
		ts, ok := parseResetTs(w.ResetsAt)
		if ok && ts > now && (!found || ts < earliest) {
			earliest = ts
			found = true
		}
	}
	return earliest, found
}

// parseResetTs mirrors _parse_reset_ts: epoch seconds from an RFC3339
// "...Z" timestamp, or (0, false) when absent/unparseable.
func parseResetTs(resetsAt string) (float64, bool) {
	if resetsAt == "" {
		return 0, false
	}
	t, err := time.Parse(time.RFC3339, resetsAt)
	if err != nil {
		return 0, false
	}
	return float64(t.Unix()), true
}
