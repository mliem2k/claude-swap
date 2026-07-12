package menubar

import (
	"strings"
	"testing"
	"time"
)

func TestTightestPct(t *testing.T) {
	tests := []struct {
		name    string
		u       DisplayUsage
		wantPct float64
		wantOK  bool
	}{
		{"nil last good", DisplayUsage{}, 0, false},
		{"picks the higher window", DisplayUsage{LastGood: map[string]any{
			"five_hour": map[string]any{"pct": 47.0},
			"seven_day": map[string]any{"pct": 62.0},
		}}, 62.0, true},
		{"spend excluded", DisplayUsage{LastGood: map[string]any{
			"spend": map[string]any{"pct": 99.0},
		}}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pct, ok := TightestPct(tt.u)
			if ok != tt.wantOK || (ok && pct != tt.wantPct) {
				t.Errorf("TightestPct(%+v) = (%v, %v), want (%v, %v)", tt.u, pct, ok, tt.wantPct, tt.wantOK)
			}
		})
	}
}

func TestUsageSummarySentinel(t *testing.T) {
	got := UsageSummary(DisplayUsage{Sentinel: "API key (no quota)"}, time.Now())
	if got != "API key (no quota)" {
		t.Errorf("UsageSummary(sentinel) = %q", got)
	}
}

func TestUsageSummaryUnavailable(t *testing.T) {
	got := UsageSummary(DisplayUsage{}, time.Now())
	if got != "usage unavailable" {
		t.Errorf("UsageSummary(empty) = %q", got)
	}
}

func TestUsageSummaryWindowsAndSpend(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	u := DisplayUsage{LastGood: map[string]any{
		"five_hour": map[string]any{"pct": 47.0},
		"spend":     map[string]any{"pct": 12.0},
	}}
	got := UsageSummary(u, now)
	if !strings.Contains(got, "5h 47%") || !strings.Contains(got, "$ 12%") {
		t.Errorf("UsageSummary = %q, want it to contain '5h 47%%' and '$ 12%%'", got)
	}
}

func TestUsageSummaryMaxedScopedWindowGetsMarker(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	u := DisplayUsage{LastGood: map[string]any{
		"scoped": []map[string]any{{"name": "Fable", "pct": 100.0}},
	}}
	got := UsageSummary(u, now)
	if !strings.Contains(got, "Fable 100% (!)") {
		t.Errorf("UsageSummary(maxed scoped) = %q, want it to contain 'Fable 100%% (!)'", got)
	}
}

func TestFormatAccountLabel(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	u := DisplayUsage{LastGood: map[string]any{"five_hour": map[string]any{"pct": 47.0}}}
	got := FormatAccountLabel("2", "a@example.com", u, now)
	if !strings.Contains(got, "2") || !strings.Contains(got, "a@example.com") || !strings.Contains(got, "5h 47%") {
		t.Errorf("FormatAccountLabel = %q", got)
	}
}

func TestFormatTitleNoActiveAccount(t *testing.T) {
	got := FormatTitle("", DisplayUsage{}, DefaultSettings(), time.Now())
	if got != "⇄" {
		t.Errorf("FormatTitle(no active) = %q, want the bare icon", got)
	}
}

func TestFormatTitleShowsNameAndPercentages(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	u := DisplayUsage{LastGood: map[string]any{
		"five_hour": map[string]any{"pct": 47.0},
		"seven_day": map[string]any{"pct": 12.0, "resets_at": now.Add(48 * time.Hour).UTC().Format(time.RFC3339)},
	}}
	got := FormatTitle("someone@example.com", u, DefaultSettings(), now)
	if !strings.Contains(got, "someone") || !strings.Contains(got, "47%") || !strings.Contains(got, "12%") {
		t.Errorf("FormatTitle = %q", got)
	}
}

func TestFormatTitleTitlePctOffShowsNoPercentages(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := DefaultSettings()
	s.TitlePct = "off"
	u := DisplayUsage{LastGood: map[string]any{"five_hour": map[string]any{"pct": 47.0}}}
	got := FormatTitle("someone@example.com", u, s, now)
	if strings.Contains(got, "47%") {
		t.Errorf("FormatTitle(titlePct=off) = %q, should not contain a percentage", got)
	}
}

func TestFormatUsageLog(t *testing.T) {
	u := DisplayUsage{LastGood: map[string]any{
		"five_hour": map[string]any{"pct": 47.0, "clock": "20:39"},
	}}
	got, ok := FormatUsageLog("a@example.com", u)
	if !ok || !strings.Contains(got, "5h 47%") || !strings.Contains(got, "resets 20:39") {
		t.Errorf("FormatUsageLog = (%q, %v)", got, ok)
	}
}

func TestFormatUsageLogNoNumericWindow(t *testing.T) {
	_, ok := FormatUsageLog("a@example.com", DisplayUsage{Sentinel: "token expired"})
	if ok {
		t.Error("FormatUsageLog(sentinel-only) should return ok=false")
	}
}

func TestParseSwitchHistory(t *testing.T) {
	log := "2026-07-12 09:00:00 - INFO - Switched from account 1 to 2\n" +
		"2026-07-12 09:05:12 - INFO - some unrelated line\n" +
		"2026-07-12 09:10:00 - INFO - Switched from account 2 to 3\n"
	got := ParseSwitchHistory(log, 10)
	want := []string{"2 → 3   2026-07-12 09:10", "1 → 2   2026-07-12 09:00"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ParseSwitchHistory = %v, want %v", got, want)
	}
}

func TestParseSwitchHistoryRespectsLimit(t *testing.T) {
	var log strings.Builder
	for i := 0; i < 5; i++ {
		log.WriteString("2026-07-12 09:00:00 - INFO - Switched from account 1 to 2\n")
	}
	got := ParseSwitchHistory(log.String(), 3)
	if len(got) != 3 {
		t.Errorf("ParseSwitchHistory with limit 3 returned %d entries, want 3", len(got))
	}
}
