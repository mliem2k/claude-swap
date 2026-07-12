package cswap

import "testing"

func usageWithWindows(fiveHourPct, sevenDayPct float64, fiveHourResetsAt, sevenDayResetsAt string) map[string]any {
	return map[string]any{
		"five_hour": map[string]any{"pct": fiveHourPct, "resets_at": fiveHourResetsAt},
		"seven_day": map[string]any{"pct": sevenDayPct, "resets_at": sevenDayResetsAt},
	}
}

// usageWithFiveHourPct was previously defined in the now-deleted
// autoswitch_poll_interval_test.go; kept here (this package's shared
// test-helpers file) since autoswitch_recovery_test.go still depends on
// it and that file is outside this task's scope.
func usageWithFiveHourPct(pct float64) map[string]any {
	return usageWithWindows(pct, 0.0, "", "")
}

func TestBindingPctKnown(t *testing.T) {
	usage := usageWithWindows(80.0, 40.0, "", "")
	pct, ok := BindingPct(usage, nil)
	if !ok || pct != 80.0 {
		t.Fatalf("got %v %v", pct, ok)
	}
}

func TestBindingPctUnknownWhenNoUsage(t *testing.T) {
	_, ok := BindingPct(nil, nil)
	if ok {
		t.Fatal("expected unknown for nil usage")
	}
}

func TestWindowPctsOrderedFiveHourThenSevenDay(t *testing.T) {
	usage := usageWithWindows(80.0, 40.0, "", "")
	got := windowPcts(usage, nil)
	if len(got) != 2 || got[0].Label != "5h" || got[0].Pct != 80.0 || got[1].Label != "7d" || got[1].Pct != 40.0 {
		t.Fatalf("got %#v", got)
	}
}

func TestLimitingResetTsOnlyConsidersAtOrAboveCap(t *testing.T) {
	usage := usageWithWindows(100.0, 40.0, "2026-07-11T01:00:00Z", "2026-07-12T00:00:00Z")
	ts, ok := limitingResetTs(usage, nil)
	if !ok {
		t.Fatal("expected a limiting reset ts")
	}
	want, _ := parseResetTs("2026-07-11T01:00:00Z")
	if ts != want {
		t.Fatalf("got %v want %v", ts, want)
	}
}

func TestLimitingResetTsNoneWhenNothingAtCap(t *testing.T) {
	usage := usageWithWindows(80.0, 40.0, "2026-07-11T01:00:00Z", "2026-07-12T00:00:00Z")
	_, ok := limitingResetTs(usage, nil)
	if ok {
		t.Fatal("expected no limiting reset ts when nothing is at 100%")
	}
}

func TestEarliestFutureResetTsPicksNearestAheadOfNow(t *testing.T) {
	usage := usageWithWindows(80.0, 40.0, "2026-07-11T01:00:00Z", "2026-07-12T00:00:00Z")
	now, _ := parseResetTs("2026-07-11T00:30:00Z")
	ts, ok := earliestFutureResetTs(usage, now, nil)
	if !ok {
		t.Fatal("expected a future reset ts")
	}
	want, _ := parseResetTs("2026-07-11T01:00:00Z")
	if ts != want {
		t.Fatalf("got %v want %v", ts, want)
	}
}

func TestEarliestFutureResetTsIgnoresPastResets(t *testing.T) {
	usage := usageWithWindows(80.0, 40.0, "2026-07-11T01:00:00Z", "")
	now, _ := parseResetTs("2026-07-11T02:00:00Z")
	_, ok := earliestFutureResetTs(usage, now, nil)
	if ok {
		t.Fatal("expected no future reset ts when the only one is in the past")
	}
}

func TestParseResetTsValidAndInvalid(t *testing.T) {
	ts, ok := parseResetTs("2026-07-11T00:00:00Z")
	if !ok || ts <= 0 {
		t.Fatalf("got %v %v", ts, ok)
	}
	if _, ok := parseResetTs(""); ok {
		t.Fatal("expected false for empty string")
	}
	if _, ok := parseResetTs("not-a-timestamp"); ok {
		t.Fatal("expected false for unparseable string")
	}
}
