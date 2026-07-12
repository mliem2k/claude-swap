package cswap

import (
	"strings"
	"testing"
)

func TestFormatUsageLinesFiveHourAndSevenDay(t *testing.T) {
	usage := map[string]any{
		"five_hour": map[string]any{"pct": 42.0},
		"seven_day": map[string]any{"pct": 10.0},
	}
	lines := FormatUsageLines(usage)
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %#v", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "5h:") || !strings.Contains(lines[0], "42%") {
		t.Fatalf("got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "7d:") || !strings.Contains(lines[1], "10%") {
		t.Fatalf("got %q", lines[1])
	}
}

func TestFormatUsageLinesSpendWithoutReset(t *testing.T) {
	usage := map[string]any{
		"spend": map[string]any{"used": 12.5, "limit": 20.0, "pct": 62.5, "currency": "USD"},
	}
	lines := FormatUsageLines(usage)
	if len(lines) != 1 {
		t.Fatalf("got %#v", lines)
	}
	if !strings.Contains(lines[0], "62%") || !strings.Contains(lines[0], "$12.50") || !strings.Contains(lines[0], "$20.00") {
		t.Fatalf("got %q", lines[0])
	}
	if strings.Contains(lines[0], "resets") {
		t.Fatalf("expected no reset clause without resets_at, got %q", lines[0])
	}
}

func TestFormatUsageLinesLabelsPadToWidestLabel(t *testing.T) {
	usage := map[string]any{
		"scoped":    []map[string]any{{"name": "Fable", "pct": 5.0}},
		"five_hour": map[string]any{"pct": 1.0},
	}
	lines := FormatUsageLines(usage)
	// "Fable:" is the widest label (6 chars); "5h:" must be padded to match
	// so the two lines' bodies start in the same column.
	var fable, fiveHour string
	for _, l := range lines {
		if strings.HasPrefix(l, "Fable:") {
			fable = l
		}
		if strings.HasPrefix(l, "5h:") {
			fiveHour = l
		}
	}
	if fable == "" || fiveHour == "" {
		t.Fatalf("got %#v", lines)
	}
	fableBodyStart := strings.Index(fable, "5%")
	fiveHourBodyStart := strings.Index(fiveHour, "1%")
	if fableBodyStart != fiveHourBodyStart {
		t.Fatalf("expected aligned columns, got fable at %d, 5h at %d (%q / %q)", fableBodyStart, fiveHourBodyStart, fable, fiveHour)
	}
}

func TestFormatUsageLinesSpendWithThousandsSeparator(t *testing.T) {
	usage := map[string]any{
		"spend": map[string]any{"used": 1234.5, "limit": 2000.0, "pct": 61.7, "currency": "USD"},
	}
	lines := FormatUsageLines(usage)
	if len(lines) != 1 || !strings.Contains(lines[0], "$1,234.50") || !strings.Contains(lines[0], "$2,000.00") {
		t.Fatalf("got %q", lines[0])
	}
}

func TestFormatUsageLinesSpendRoundingCarriesIntoWhole(t *testing.T) {
	// 999.999 rounds to 1000.00, not the malformed "999.100": the
	// fractional-part rounding can carry into the whole part.
	usage := map[string]any{
		"spend": map[string]any{"used": 999.999, "limit": 2000.0, "pct": 50.0, "currency": "USD"},
	}
	lines := FormatUsageLines(usage)
	if len(lines) != 1 || !strings.Contains(lines[0], "$1,000.00") {
		t.Fatalf("got %q", lines[0])
	}
}

func TestFormatUsageLinesScopedOverLimitMarksBang(t *testing.T) {
	usage := map[string]any{
		"scoped": []map[string]any{{"name": "Fable", "pct": 100.0}},
	}
	lines := FormatUsageLines(usage)
	if len(lines) != 1 || !strings.Contains(lines[0], "(!)") {
		t.Fatalf("got %#v", lines)
	}
}

func TestLastSeenNoteEmptyWithoutFetchedAt(t *testing.T) {
	entry := UsageEntry{LastGood: map[string]any{"five_hour": map[string]any{"pct": 1.0}}}
	if got := LastSeenNote(entry); got != "" {
		t.Fatalf("got %q, want empty (no fetched_at)", got)
	}
}

func TestLastSeenNotePresent(t *testing.T) {
	// AccountHeadroom returns 100-max(pct) (70 here); LastSeenNote displays
	// 100-headroom, which is max(pct) again (30): the "% used" figure is the
	// original pct, not the headroom. Verified against oauth.py's
	// account_headroom docstring before writing this expectation.
	fetchedAt := float64(nowUnixForTest())
	entry := UsageEntry{
		LastGood:  map[string]any{"five_hour": map[string]any{"pct": 30.0}},
		FetchedAt: &fetchedAt,
	}
	got := LastSeenNote(entry)
	if !strings.HasPrefix(got, "last seen 30% used") {
		t.Fatalf("got %q", got)
	}
}

func TestLastSeenNoteUsesMiddleDotSeparator(t *testing.T) {
	fetchedAt := float64(nowUnixForTest() - 30) // 30s ago, inside the "just now" bucket
	entry := UsageEntry{
		LastGood:  map[string]any{"five_hour": map[string]any{"pct": 30.0}},
		FetchedAt: &fetchedAt,
	}
	got := LastSeenNote(entry)
	want := "last seen 30% used · just now"
	if got != want {
		t.Fatalf("got %q, want %q (middle dot U+00B7, not a comma)", got, want)
	}
}

func TestUsageEntryLinesSentinelShowsNote(t *testing.T) {
	sentinel := UsageAPIKey
	entry := UsageEntry{Sentinel: &sentinel}
	lines := UsageEntryLines(entry)
	if len(lines) != 1 || !strings.Contains(lines[0], "API key") {
		t.Fatalf("got %#v", lines)
	}
}

func TestUsageEntryLinesSentinelWithLastSeenAppendsSecondLine(t *testing.T) {
	fetchedAt := float64(nowUnixForTest())
	sentinel := UsageTokenExpired
	entry := UsageEntry{
		Sentinel:  &sentinel,
		LastGood:  map[string]any{"five_hour": map[string]any{"pct": 20.0}},
		FetchedAt: &fetchedAt,
	}
	lines := UsageEntryLines(entry)
	if len(lines) != 2 {
		t.Fatalf("got %#v, want a note line plus a last-seen line", lines)
	}
	if !strings.Contains(lines[1], "last seen") {
		t.Fatalf("got %q", lines[1])
	}
}

func TestUsageEntryLinesNoMeasurementShowsUnavailable(t *testing.T) {
	entry := UsageEntry{LastError: "connection refused"}
	lines := UsageEntryLines(entry)
	if len(lines) != 1 || !strings.Contains(lines[0], "usage unavailable") || !strings.Contains(lines[0], "connection refused") {
		t.Fatalf("got %#v", lines)
	}
}

func TestUsageEntryLinesGoodMeasurement(t *testing.T) {
	entry := UsageEntry{LastGood: map[string]any{"five_hour": map[string]any{"pct": 15.0}}}
	lines := UsageEntryLines(entry)
	if len(lines) != 1 || !strings.Contains(lines[0], "15%") {
		t.Fatalf("got %#v", lines)
	}
}
