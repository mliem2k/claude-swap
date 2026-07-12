package cswap

import "strings"

// checkModelNames mirrors _check_model_names: a one-shot autoswitch.model
// typo guard. A configured name that no account reports means the filter
// looks active while gating nothing, provable only once every relevant
// oauth account has readable usage this tick (adaptive polling
// legitimately leaves gaps before that).
func (e *AutoSwitchEngine) checkModelNames(quarantined map[string]bool, usage map[string]any) {
	hasWanted := false
	for _, m := range e.models {
		if strings.ToLower(m) != "all" {
			hasWanted = true
			break
		}
	}
	if !hasWanted {
		e.modelCheckDone = true // bare "all" needs no name match
		return
	}

	var relevant []string
	for _, n := range e.Switcher.SwitchableAccountNumbers() {
		if quarantined[n] {
			continue
		}
		if e.Switcher.AccountKind(n) == "api_key" {
			continue
		}
		relevant = append(relevant, n)
	}
	var readable []map[string]any
	for _, n := range relevant {
		if v, ok := usage[n].(map[string]any); ok {
			readable = append(readable, v)
		}
	}
	if len(readable) == 0 || len(readable) != len(relevant) {
		return // not every account observed yet, re-check next tick
	}

	seen := map[string]bool{}
	for _, v := range readable {
		for _, s := range ScopedWindows(v) {
			name, ok := s["name"].(string)
			if !ok {
				continue
			}
			seen[strings.ToLower(name)] = true
		}
	}

	e.modelCheckDone = true
	var missing []string
	for _, m := range e.models {
		low := strings.ToLower(m)
		if low == "all" {
			continue
		}
		if !seen[low] {
			missing = append(missing, m)
		}
	}
	if len(missing) > 0 {
		e.Emit(NewConfigWarningEvent(
			"autoswitch.model: " + strings.Join(missing, ", ") +
				" matches no account's usage windows, only the 5h/7d limits are being watched for it (typo?)",
		))
	}
}
