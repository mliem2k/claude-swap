package cswap

// inCooldown mirrors _in_cooldown: whether the cooldown window since the
// last real switch is still active.
func (e *AutoSwitchEngine) inCooldown(state map[string]any) bool {
	last, ok := state["lastSwitchAt"].(float64)
	if !ok {
		return false
	}
	return float64(e.Clock().Unix())-last < e.currentSettings().CooldownSeconds
}
