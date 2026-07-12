// internal/cswap/tui/widgets_test.go
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/mliem2k/claude-swap/internal/cswap"
)

func TestBarCellsNilPctIsAllTrack(t *testing.T) {
	got := BarCells(nil, 10, false, nil)
	if strings.Count(got, "─") != 10 {
		t.Errorf("BarCells(nil, 10, ...) = %q, want 10 track cells", got)
	}
}

func TestBarCellsFullWidthAtHundred(t *testing.T) {
	pct := 100.0
	got := BarCells(&pct, 10, false, nil)
	if strings.Count(got, "━") != 10 {
		t.Errorf("BarCells(100, 10, ...) = %q, want 10 filled cells", got)
	}
}

func TestBarCellsThresholdTick(t *testing.T) {
	pct, threshold := 10.0, 50.0
	got := BarCells(&pct, 10, false, &threshold)
	if !strings.Contains(got, "┃") {
		t.Errorf("BarCells with threshold=50 over width=10 should contain a tick mark, got %q", got)
	}
}

// TestBarCellsThresholdTickRoundsHalfToEven is the regression test for a
// real bug: threshold=25%, width=10 lands the raw tick position exactly
// on 2.5, where int(x+0.5) (round-half-up) and Python's round() builtin
// (round-half-to-even) disagree: 3 vs 2. Cell index 2 is the third
// rendered rune ("╸", "─", "┃", ...); no ANSI styling wraps individual
// cells in a non-tty test environment, so a plain rune-index check is
// reliable here.
func TestBarCellsThresholdTickRoundsHalfToEven(t *testing.T) {
	pct, threshold := 5.0, 25.0
	got := []rune(BarCells(&pct, 10, false, &threshold))
	if len(got) != 10 {
		t.Fatalf("expected 10 rendered cells, got %d (%q)", len(got), string(got))
	}
	if got[2] != '┃' {
		t.Fatalf("expected the tick at cell index 2 (round-half-to-even of 2.5), got %q at index 2 in %q", got[2], string(got))
	}
}

func TestUsageRowsFiveHourAndSevenDay(t *testing.T) {
	lastGood := map[string]any{
		"five_hour": map[string]any{"pct": 47.0},
		"seven_day": map[string]any{"pct": 12.0},
	}
	rows := UsageRows(lastGood)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Label != "5h" || *rows[0].Pct != 47.0 {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if rows[1].Label != "7d" || *rows[1].Pct != 12.0 {
		t.Errorf("row 1 = %+v", rows[1])
	}
}

func TestUsageRowsScopedMaxedGetsMarker(t *testing.T) {
	lastGood := map[string]any{
		"scoped": []map[string]any{{"name": "Fable", "pct": 100.0}},
	}
	rows := UsageRows(lastGood)
	if len(rows) != 1 || rows[0].Label != "Fable" {
		t.Fatalf("got %+v", rows)
	}
	if !strings.Contains(rows[0].Suffix, "(!)") {
		t.Errorf("maxed scoped window suffix = %q, want it to contain (!)", rows[0].Suffix)
	}
}

func TestUsageRowsSuffixFullIncludesClock(t *testing.T) {
	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	lastGood := map[string]any{
		"five_hour": map[string]any{"pct": 47.0, "resets_at": resetsAt},
	}
	rows := UsageRows(lastGood)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if strings.Contains(rows[0].Suffix, "·") {
		t.Errorf("Suffix = %q, want the plain countdown with no clock", rows[0].Suffix)
	}
	if !strings.Contains(rows[0].SuffixFull, "·") {
		t.Errorf("SuffixFull = %q, want it to include the absolute clock time", rows[0].SuffixFull)
	}
	if !strings.HasPrefix(rows[0].SuffixFull, rows[0].Suffix) {
		t.Errorf("SuffixFull = %q, want it to extend Suffix = %q", rows[0].SuffixFull, rows[0].Suffix)
	}
}

func TestUsageRowsSuffixFullEqualsSuffixWhenNoClockKnown(t *testing.T) {
	lastGood := map[string]any{
		"five_hour": map[string]any{"pct": 47.0}, // no resets_at
	}
	rows := UsageRows(lastGood)
	if len(rows) != 1 || rows[0].Suffix != "" || rows[0].SuffixFull != "" {
		t.Fatalf("got %+v, want an empty suffix and suffixFull with no reset info", rows)
	}
}

func TestAccountCardTextUsesFullSuffixWhenWidthAllows(t *testing.T) {
	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	acc := cswap.AccountSnapshot{
		Number: "1", Email: "a@example.com",
		Usage: cswap.UsageEntry{LastGood: map[string]any{
			"five_hour": map[string]any{"pct": 47.0, "resets_at": resetsAt},
		}},
	}
	got := AccountCardText(acc, 200, nil)
	if !strings.Contains(got, "·") {
		t.Errorf("AccountCardText at width=200 = %q, want it to include the absolute reset clock", got)
	}
}

func TestAccountCardTextDropsClockWhenWidthTight(t *testing.T) {
	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	acc := cswap.AccountSnapshot{
		Number: "1", Email: "a@example.com",
		Usage: cswap.UsageEntry{LastGood: map[string]any{
			"five_hour": map[string]any{"pct": 47.0, "resets_at": resetsAt},
		}},
	}
	// barWidth floors at 12 regardless of how small width goes, so
	// rowOverhead has a fixed minimum; width must be well below that floor
	// plus the full suffix's length to actually force a drop (width=50
	// above the floor still fit the clock, which caught a first draft of
	// this test being accidentally non-discriminating).
	got := AccountCardText(acc, 30, nil)
	if strings.Contains(got, "·") {
		t.Errorf("AccountCardText at width=30 = %q, want the clock dropped for width", got)
	}
	if !strings.Contains(got, "resets") {
		t.Errorf("AccountCardText at width=30 = %q, want the plain countdown kept", got)
	}
}

func TestAccountCardTextShowsEmailAndActiveMarker(t *testing.T) {
	acc := cswap.AccountSnapshot{
		Number: "1", Email: "a@example.com", IsActive: true, Switchable: true,
		Usage: cswap.UsageEntry{LastGood: map[string]any{"five_hour": map[string]any{"pct": 10.0}}},
	}
	got := AccountCardText(acc, 80, nil)
	if !strings.Contains(got, "a@example.com") || !strings.Contains(got, "active") {
		t.Errorf("AccountCardText = %q, want email and an active marker", got)
	}
}

func TestAccountCardTextSentinelReplacesBars(t *testing.T) {
	sentinel := cswap.UsageAPIKey
	acc := cswap.AccountSnapshot{
		Number: "2", Email: "b@example.com", Switchable: false,
		Usage: cswap.UsageEntry{Sentinel: &sentinel},
	}
	got := AccountCardText(acc, 80, nil)
	if !strings.Contains(got, "API key (no quota)") {
		t.Errorf("AccountCardText with API-key sentinel = %q", got)
	}
}

// TestAccountCardTextSentinelShowsLastSeenNote is the regression test for a
// real gap: Python's account_card_text appends a supplementary
// "└ last seen NN% used · Ns ago" line under a non-API-key sentinel when an
// older measurement exists (the same line `cswap list` prints), which the
// Go port previously dropped entirely.
func TestAccountCardTextSentinelShowsLastSeenNote(t *testing.T) {
	sentinel := cswap.UsageKeychainUnavailable
	fetchedAt := 0.0 // epoch, so FormatAge renders a large-but-defined age
	acc := cswap.AccountSnapshot{
		Number: "2", Email: "b@example.com", Switchable: false,
		Usage: cswap.UsageEntry{
			Sentinel:  &sentinel,
			LastGood:  map[string]any{"five_hour": map[string]any{"pct": 40.0}},
			FetchedAt: &fetchedAt,
		},
	}
	got := AccountCardText(acc, 80, nil)
	if !strings.Contains(got, "└") || !strings.Contains(got, "last seen") {
		t.Errorf("AccountCardText with a non-API-key sentinel and a prior measurement = %q, want a last-seen note", got)
	}
}

// TestAccountCardTextAPIKeySentinelHasNoLastSeenNote confirms the API-key
// sentinel is still excluded, matching Python's `if sentinel != USAGE_API_KEY`
// guard: an API-key account has no quota to have "seen".
func TestAccountCardTextAPIKeySentinelHasNoLastSeenNote(t *testing.T) {
	sentinel := cswap.UsageAPIKey
	fetchedAt := 0.0
	acc := cswap.AccountSnapshot{
		Number: "2", Email: "b@example.com", Switchable: false,
		Usage: cswap.UsageEntry{
			Sentinel:  &sentinel,
			LastGood:  map[string]any{"five_hour": map[string]any{"pct": 40.0}},
			FetchedAt: &fetchedAt,
		},
	}
	got := AccountCardText(acc, 80, nil)
	if strings.Contains(got, "└") {
		t.Errorf("AccountCardText with an API-key sentinel = %q, want no last-seen note", got)
	}
}

func TestMiniAccountTextShowsShortSummary(t *testing.T) {
	acc := cswap.AccountSnapshot{
		Number: "3", Email: "c@example.com",
		Usage: cswap.UsageEntry{LastGood: map[string]any{"five_hour": map[string]any{"pct": 92.0}}},
	}
	got := MiniAccountText(acc)
	if !strings.Contains(got, "c@example.com") || !strings.Contains(got, "92") {
		t.Errorf("MiniAccountText = %q", got)
	}
}

func TestAccountsPanelTextNoAccounts(t *testing.T) {
	got := AccountsPanelText(nil, true, nil, 80)
	if !strings.Contains(got, "No managed accounts") {
		t.Errorf("AccountsPanelText(nil) = %q", got)
	}
}

// TestAccountsPanelTextBreathesAroundActiveCard is the regression test for a
// real gap: Python's AccountsPanel.render inserts a blank line ("\n\n")
// around the expanded (multiline) active-account card so it doesn't run
// straight into a neighboring one-line mini, while two adjacent minis stay
// tight on a single "\n". The Go port previously used a flat
// strings.Join(blocks, "\n") that never varied the separator.
func TestAccountsPanelTextBreathesAroundActiveCard(t *testing.T) {
	accounts := []cswap.AccountSnapshot{
		{Number: "1", Email: "active@example.com", IsActive: true, Switchable: true,
			Usage: cswap.UsageEntry{LastGood: map[string]any{"five_hour": map[string]any{"pct": 10.0}}}},
		{Number: "2", Email: "mini-a@example.com",
			Usage: cswap.UsageEntry{LastGood: map[string]any{"five_hour": map[string]any{"pct": 20.0}}}},
		{Number: "3", Email: "mini-b@example.com",
			Usage: cswap.UsageEntry{LastGood: map[string]any{"five_hour": map[string]any{"pct": 30.0}}}},
	}
	got := AccountsPanelText(accounts, true, nil, 80)
	if !strings.Contains(got, "\n\n") {
		t.Fatalf("AccountsPanelText = %q, want a blank line around the expanded active card", got)
	}
	if strings.Contains(got, "mini-a@example.com\n\nmini-b") {
		t.Errorf("AccountsPanelText = %q, want two adjacent one-line minis joined by a single newline", got)
	}
}
