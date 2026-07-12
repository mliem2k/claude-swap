// internal/cswap/tui/widgets.go
package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mliem2k/claude-swap/internal/cswap"
)

const (
	barFilled = "━"
	barHalf   = "╸"
	barEmpty  = "─"
	barTick   = "┃"
)

// BarCells mirrors bar_cells: just the bar glyphs, severity-colored fill,
// track, and an optional threshold tick.
func BarCells(pct *float64, width int, stale bool, threshold *float64) string {
	if pct == nil {
		return lipgloss.NewStyle().Foreground(Track).Render(strings.Repeat(barEmpty, width))
	}
	clamped := *pct
	if clamped < 0 {
		clamped = 0
	}
	if clamped > 100 {
		clamped = 100
	}
	cells := clamped / 100.0 * float64(width)
	full := int(cells)
	half := (cells-float64(full)) >= 0.5 && full < width
	tickAt := -1
	if threshold != nil {
		// math.RoundToEven mirrors Python's round() builtin (round-half-
		// to-even), not int(x+0.5)'s round-half-up: for a threshold/width
		// combination landing exactly on a .5 cell, the two disagree by
		// one column.
		t := int(math.RoundToEven(*threshold / 100.0 * float64(width)))
		if t < 0 {
			t = 0
		}
		if t > width-1 {
			t = width - 1
		}
		tickAt = t
	}
	color := SeverityColor(pct)
	fillStyle := lipgloss.NewStyle().Foreground(color)
	if stale {
		fillStyle = fillStyle.Faint(true)
	}
	trackStyle := lipgloss.NewStyle().Foreground(Track)
	tickStyle := lipgloss.NewStyle().Foreground(SevWarn)

	var b strings.Builder
	for i := 0; i < width; i++ {
		switch {
		case i == tickAt:
			b.WriteString(tickStyle.Render(barTick))
		case i < full:
			b.WriteString(fillStyle.Render(barFilled))
		case i == full && half:
			b.WriteString(fillStyle.Render(barHalf))
		default:
			b.WriteString(trackStyle.Render(barEmpty))
		}
	}
	return b.String()
}

// UsageBar mirrors usage_bar: one full bar line, "5h ━━━━╸────┃──  47%
// resets 2h 13m · 20:39".
func UsageBar(label string, pct *float64, suffix string, width int, stale bool, threshold *float64) string {
	var b strings.Builder
	b.WriteString(StyleMuted.Render(label + " "))
	b.WriteString(BarCells(pct, width, stale, threshold))
	if pct == nil {
		b.WriteString(StyleMuted.Render("  usage unknown"))
	} else {
		style := lipgloss.NewStyle().Foreground(SeverityColor(pct))
		if stale {
			style = style.Faint(true)
		}
		b.WriteString(style.Render(fmt.Sprintf(" %3.0f%%", *pct)))
	}
	if suffix != "" {
		b.WriteString(StyleMuted.Render("  " + suffix))
	}
	return b.String()
}

// UsageRow is one (label, pct, suffix, suffixFull) row mirroring
// usage_rows' per-window tuples. SuffixFull extends the reset countdown
// with the absolute clock time ("resets 2h 13m · 20:39") for rows that
// have the width for it; AccountCardText picks whichever fits.
type UsageRow struct {
	Label      string
	Pct        *float64
	Suffix     string
	SuffixFull string
}

func floatFromAny(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// resetSuffixParts mirrors _reset_parts: the countdown suffix and its
// clock-extended variant for one window ("resets 2h 13m", "resets 2h 13m ·
// 20:39"), equal when no clock is known.
func resetSuffixParts(window map[string]any) (suffix, suffixFull string) {
	countdown, clock, ok := cswap.FreshResetStrings(window)
	if !ok {
		return "", ""
	}
	suffix = "resets " + countdown
	suffixFull = suffix
	if clock != "" {
		suffixFull = suffix + " · " + clock
	}
	return suffix, suffixFull
}

// UsageRows mirrors usage_rows: (label, pct, suffix, suffixFull) rows for
// one account's usage dict, same map shape and window order
// FormatUsageLines (switcher_list.go) already reads: spend, 5h, 7d, then
// per-model scoped windows, the latter marked "(!)" at/over their limit.
func UsageRows(lastGood map[string]any) []UsageRow {
	if lastGood == nil {
		return nil
	}
	var rows []UsageRow
	if spend, ok := lastGood["spend"].(map[string]any); ok && spend != nil {
		pct, _ := floatFromAny(spend["pct"])
		used, _ := floatFromAny(spend["used"])
		limit, _ := floatFromAny(spend["limit"])
		amounts := fmt.Sprintf("$%.2f / $%.2f", used, limit)
		reset, resetFull := resetSuffixParts(spend)
		suffix, suffixFull := amounts, amounts
		if reset != "" {
			suffix = reset + "  " + amounts
		}
		if resetFull != "" {
			suffixFull = resetFull + "  " + amounts
		}
		rows = append(rows, UsageRow{Label: "$$", Pct: &pct, Suffix: suffix, SuffixFull: suffixFull})
	}
	for _, lw := range []struct{ key, label string }{{"five_hour", "5h"}, {"seven_day", "7d"}} {
		w, ok := lastGood[lw.key].(map[string]any)
		if !ok || w == nil {
			continue
		}
		pct, _ := floatFromAny(w["pct"])
		suffix, suffixFull := resetSuffixParts(w)
		rows = append(rows, UsageRow{Label: lw.label, Pct: &pct, Suffix: suffix, SuffixFull: suffixFull})
	}
	for _, w := range cswap.ScopedWindows(lastGood) {
		name, _ := w["name"].(string)
		pct, _ := floatFromAny(w["pct"])
		suffix, suffixFull := resetSuffixParts(w)
		if pct >= 100 {
			if suffix != "" {
				suffix += "  (!)"
			} else {
				suffix = "(!)"
			}
			if suffixFull != "" {
				suffixFull += "  (!)"
			} else {
				suffixFull = "(!)"
			}
		}
		rows = append(rows, UsageRow{Label: name, Pct: &pct, Suffix: suffix, SuffixFull: suffixFull})
	}
	return rows
}

// staleAge mirrors the account_card_text/mini_account_text "stale" check:
// acc.usage.age_s is not None and acc.usage.age_s > STALE_OK_S. Python's
// STALE_OK_S (usage_store.py, 300s — "trusted for switch decisions; older
// -> headroom unknown") is a bar-dimming threshold distinct from both
// ServeTTLS (poll_policy.go's fetch-cadence floor, 180s) and UsageAgeNoteS
// (the separate age-note-text threshold data.go's FormatAge uses, 90s):
// three different constants for three different display decisions, not
// interchangeable. Go's cswap.StaleOKS carries the same 300s value but
// pre-scaled to Duration-nanosecond units (used directly against
// time.Duration elsewhere, e.g. UsageEntry.Fresh), so it's divided back to
// plain seconds here to compare against AgeS, the same idiom
// usage_store_entry_test.go already uses.
func staleAge(ageS *float64) bool {
	return ageS != nil && *ageS > cswap.StaleOKS/float64(time.Second)
}

// AccountCardText mirrors account_card_text: the full account card,
// header line plus per-window bar rows (or a sentinel line replacing the
// bars entirely).
func AccountCardText(acc cswap.AccountSnapshot, width int, threshold *float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%2s  ", acc.Number)
	b.WriteString(StyleForeground.Render(acc.Email))
	b.WriteString(StyleMuted.Render(fmt.Sprintf("  [%s]", acc.DisplayTag())))
	if acc.IsActive {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(Accent).Render("   ● active"))
	}
	if age := FormatAge(acc.Usage.AgeS); age != "" {
		b.WriteString(StyleMuted.Render("   " + age))
	}

	if acc.Usage.Sentinel != nil {
		b.WriteString("\n    ")
		style := StyleMuted
		marker := "·"
		if *acc.Usage.Sentinel != cswap.UsageAPIKey {
			style = lipgloss.NewStyle().Foreground(SevWarn)
			marker = "⚠"
		}
		fmt.Fprintf(&b, "%s", style.Render(marker+" "+SentinelLabel(*acc.Usage.Sentinel)))
		// Same supplementary line `cswap list` prints: the last good
		// measurement behind the sentinel. API-key accounts have no quota
		// to have "seen", so this only applies to other sentinel states
		// (e.g. auth-error, keychain-unavailable).
		if *acc.Usage.Sentinel != cswap.UsageAPIKey {
			if lastSeen := cswap.LastSeenNote(acc.Usage); lastSeen != "" {
				b.WriteString("\n    ")
				b.WriteString(StyleMuted.Render("└ " + lastSeen))
			}
		}
		return b.String()
	}

	rows := UsageRows(acc.Usage.LastGood)
	if len(rows) == 0 {
		b.WriteString("\n    ")
		b.WriteString(StyleMuted.Render("usage unavailable"))
		if acc.Usage.LastError != "" {
			b.WriteString(StyleMuted.Render(" · " + acc.Usage.LastError))
		}
		return b.String()
	}

	stale := staleAge(acc.Usage.AgeS)
	labelWidth := 0
	for _, r := range rows {
		if len(r.Label) > labelWidth {
			labelWidth = len(r.Label)
		}
	}
	barWidth := width - 42 - labelWidth
	if barWidth < 12 {
		barWidth = 12
	}
	if barWidth > 30 {
		barWidth = 30
	}
	// Everything on a row except the suffix: indent, label, bar, " NNN%",
	// gap. Mirrors Python's row_overhead exactly, so a long spend-row
	// amount degrading to the plain countdown doesn't cost the 5h/7d rows
	// their clocks too.
	rowOverhead := 4 + labelWidth + 1 + barWidth + 5 + 2
	for _, r := range rows {
		suffix := r.Suffix
		if r.SuffixFull != r.Suffix && rowOverhead+len(r.SuffixFull) <= width {
			suffix = r.SuffixFull
		}
		b.WriteString("\n    ")
		b.WriteString(UsageBar(fmt.Sprintf("%-*s", labelWidth, r.Label), r.Pct, suffix, barWidth, stale, threshold))
	}
	return b.String()
}

// MiniAccountText mirrors mini_account_text: one minimized line for an
// inactive account, "2  work@acme.dev [personal]   5h 92% · 7d 63%".
func MiniAccountText(acc cswap.AccountSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%2s  ", acc.Number)
	b.WriteString(StyleForeground.Render(acc.Email))
	b.WriteString(StyleMuted.Render(fmt.Sprintf("  [%s]", acc.DisplayTag())))
	b.WriteString("   ")

	if acc.Usage.Sentinel != nil {
		style := StyleMuted
		if *acc.Usage.Sentinel != cswap.UsageAPIKey {
			style = lipgloss.NewStyle().Foreground(SevWarn)
		}
		b.WriteString(style.Render(SentinelLabel(*acc.Usage.Sentinel)))
		return b.String()
	}

	stale := staleAge(acc.Usage.AgeS)
	parts := 0
	for _, lw := range []struct{ key, label string }{{"five_hour", "5h"}, {"seven_day", "7d"}} {
		w, ok := acc.Usage.LastGood[lw.key].(map[string]any)
		if !ok || w == nil {
			continue
		}
		pct, _ := floatFromAny(w["pct"])
		if parts > 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(Track).Render(" · "))
		}
		b.WriteString(StyleMuted.Render(lw.label + " "))
		style := lipgloss.NewStyle().Foreground(SeverityColor(&pct))
		if stale {
			style = style.Faint(true)
		}
		b.WriteString(style.Render(fmt.Sprintf("%.0f%%", pct)))
		parts++
	}
	for _, w := range cswap.ScopedWindows(acc.Usage.LastGood) {
		pct, _ := floatFromAny(w["pct"])
		if pct < 100 {
			continue
		}
		name, _ := w["name"].(string)
		if parts > 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(Track).Render(" · "))
		}
		b.WriteString(lipgloss.NewStyle().Foreground(SevCrit).Render(name + " (!)"))
		parts++
	}
	if parts == 0 {
		b.WriteString(StyleMuted.Render("usage unknown"))
	}
	return b.String()
}

// AccountsPanelText mirrors AccountsPanel.render: the active account
// full-size, others as one-line minis (showMinis=false for the auto
// screen's active-only panel).
func AccountsPanelText(accounts []cswap.AccountSnapshot, showMinis bool, threshold *float64, width int) string {
	if len(accounts) == 0 {
		return StyleMuted.Render(
			"No managed accounts yet.\nUse the menu below: Add account, from your current Claude Code login, or from a setup-token / API key.")
	}
	var blocks []string
	for _, acc := range accounts {
		if acc.IsActive {
			blocks = append(blocks, AccountCardText(acc, width, threshold))
		} else if showMinis {
			blocks = append(blocks, MiniAccountText(acc))
		}
	}
	if len(blocks) == 0 {
		return StyleMuted.Render("no active managed login")
	}
	var b strings.Builder
	previousMultiline := false
	for i, block := range blocks {
		multiline := strings.Contains(block, "\n")
		if i > 0 {
			// Breathe around the expanded active card: a blank line
			// separates it from its neighbors on either side, but two
			// adjacent one-line minis stay tight.
			if multiline || previousMultiline {
				b.WriteString("\n\n")
			} else {
				b.WriteString("\n")
			}
		}
		b.WriteString(block)
		previousMultiline = multiline
	}
	return b.String()
}
