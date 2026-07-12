package cswap

import (
	"testing"
	"time"
)

func TestInCooldownNoLastSwitchIsFalse(t *testing.T) {
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: time.Now}
	if e.inCooldown(map[string]any{}) {
		t.Fatal("expected false with no lastSwitchAt")
	}
}

func TestInCooldownNonNumericLastSwitchIsFalse(t *testing.T) {
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: time.Now}
	if e.inCooldown(map[string]any{"lastSwitchAt": "not-a-number"}) {
		t.Fatal("expected false for a non-numeric lastSwitchAt")
	}
}

func TestInCooldownRecentSwitchIsTrue(t *testing.T) {
	now := time.Unix(1000, 0)
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: func() time.Time { return now }}
	e.Settings.CooldownSeconds = 300
	state := map[string]any{"lastSwitchAt": float64(now.Unix() - 100)}
	if !e.inCooldown(state) {
		t.Fatal("expected true, switch was 100s ago with a 300s cooldown")
	}
}

func TestInCooldownExpiredCooldownIsFalse(t *testing.T) {
	now := time.Unix(1000, 0)
	e := &AutoSwitchEngine{Settings: DefaultAutoSwitchSettings(), Clock: func() time.Time { return now }}
	e.Settings.CooldownSeconds = 300
	state := map[string]any{"lastSwitchAt": float64(now.Unix() - 400)}
	if e.inCooldown(state) {
		t.Fatal("expected false, switch was 400s ago with a 300s cooldown")
	}
}
