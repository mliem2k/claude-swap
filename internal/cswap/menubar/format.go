// internal/cswap/menubar/format.go
package menubar

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mliem2k/claude-swap/internal/cswap"
)

// Icon mirrors ICON.
const Icon = "⇄"

// DisplayUsage mirrors Python's `dict | str | None` union for a usage
// value: a sentinel note (token expired / API key / keychain
// unavailable), the last-good measurement, or neither (unknown).
type DisplayUsage struct {
	Sentinel string
	LastGood map[string]any
}

func floatFromAny(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func windowPct(m map[string]any, key string) (float64, bool) {
	w, ok := m[key].(map[string]any)
	if !ok {
		return 0, false
	}
	return floatFromAny(w["pct"])
}

// TightestPct mirrors tightest_pct: the highest 5h/7d utilization
// percentage, or (0, false) if unknown. Spend is excluded, it isn't a
// rate-limit window.
func TightestPct(u DisplayUsage) (float64, bool) {
	if u.LastGood == nil {
		return 0, false
	}
	best, ok := 0.0, false
	for _, key := range []string{"five_hour", "seven_day"} {
		if p, pok := windowPct(u.LastGood, key); pok {
			if !ok || p > best {
				best, ok = p, true
			}
		}
	}
	return best, ok
}

func resetsAtTime(w map[string]any) (time.Time, bool) {
	ra, ok := w["resets_at"].(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, ra)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// liveCountdown mirrors _live_countdown: time until a window resets,
// computed live from resets_at rather than a frozen cached string.
func liveCountdown(w map[string]any, now time.Time) (string, bool) {
	ts, ok := resetsAtTime(w)
	if !ok {
		return "", false
	}
	remaining := int(ts.Sub(now).Seconds())
	if remaining <= 0 {
		return "", false
	}
	days := remaining / 86400
	rem := remaining % 86400
	hours := rem / 3600
	rem %= 3600
	minutes := rem / 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours), true
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes), true
	default:
		return fmt.Sprintf("%dm", minutes), true
	}
}

const weeklyPeriodS = 7 * 86400

// rolledWeeklyWindow mirrors _rolled_weekly_window: a weekly window with
// a passed reset advanced to its next 7-day boundary, so the menu shows
// the rollover from the static schedule alone without a fresh fetch.
func rolledWeeklyWindow(w map[string]any, now time.Time) map[string]any {
	if w == nil {
		return nil
	}
	ts, ok := resetsAtTime(w)
	if !ok || ts.After(now) {
		return w
	}
	missed := int(now.Sub(ts).Seconds())/weeklyPeriodS + 1
	newTs := ts.Add(time.Duration(missed) * weeklyPeriodS * time.Second)
	rolled := make(map[string]any, len(w))
	for k, v := range w {
		rolled[k] = v
	}
	rolled["pct"] = 0.0
	rolled["resets_at"] = newTs.UTC().Format(time.RFC3339)
	delete(rolled, "countdown")
	delete(rolled, "clock")
	return rolled
}

// UsageSummary mirrors usage_summary: one-line usage summary for an
// account row, reset countdowns computed live.
func UsageSummary(u DisplayUsage, now time.Time) string {
	if u.Sentinel != "" {
		return u.Sentinel
	}
	if u.LastGood == nil {
		return "usage unavailable"
	}
	var parts []string
	for _, lw := range []struct{ key, label string }{{"five_hour", "5h"}, {"seven_day", "7d"}} {
		w, _ := u.LastGood[lw.key].(map[string]any)
		if lw.key == "seven_day" {
			w = rolledWeeklyWindow(w, now)
		}
		if pct, ok := floatFromAny(mapGet(w, "pct")); ok {
			seg := fmt.Sprintf("%s %.0f%%", lw.label, pct)
			if cd, cok := liveCountdown(w, now); cok {
				seg += fmt.Sprintf(" (%s)", cd)
			}
			parts = append(parts, seg)
		}
	}
	for _, w := range cswap.ScopedWindows(u.LastGood) {
		w = rolledWeeklyWindow(w, now)
		pct, pok := floatFromAny(mapGet(w, "pct"))
		name, _ := mapGet(w, "name").(string)
		if pok && name != "" {
			seg := fmt.Sprintf("%s %.0f%%", name, pct)
			if pct >= 100 {
				seg += " (!)"
			}
			if cd, cok := liveCountdown(w, now); cok {
				seg += fmt.Sprintf(" (%s)", cd)
			}
			parts = append(parts, seg)
		}
	}
	if spend, ok := u.LastGood["spend"].(map[string]any); ok {
		if pct, pok := floatFromAny(spend["pct"]); pok {
			parts = append(parts, fmt.Sprintf("$ %.0f%%", pct))
		}
	}
	if len(parts) == 0 {
		return "usage unavailable"
	}
	return strings.Join(parts, " · ")
}

func mapGet(m map[string]any, key string) any {
	if m == nil {
		return nil
	}
	return m[key]
}

// FormatAccountLabel mirrors format_account_label.
func FormatAccountLabel(num, email string, u DisplayUsage, now time.Time) string {
	return fmt.Sprintf("%s  %s  %s", num, email, UsageSummary(u, now))
}

func localPart(email string, limit int) string {
	local := email
	if i := strings.IndexByte(email, '@'); i >= 0 {
		local = email[:i]
	}
	if len(local) > limit {
		return local[:limit-1] + "*"
	}
	return local
}

// FormatTitle mirrors format_title: the menu-bar title from the active
// account and settings.
func FormatTitle(activeEmail string, activeUsage DisplayUsage, s Settings, now time.Time) string {
	if activeEmail == "" {
		return Icon
	}
	var segments []string
	if s.ShowAccountName {
		segments = append(segments, localPart(activeEmail, 12))
	}
	if s.TitlePct == "5h" || s.TitlePct == "both" {
		if p, ok := windowPct(activeUsage.LastGood, "five_hour"); ok {
			segments = append(segments, fmt.Sprintf("%.0f%%", p))
		}
	}
	if s.TitlePct == "7d" || s.TitlePct == "both" {
		seven, _ := activeUsage.LastGood["seven_day"].(map[string]any)
		seven = rolledWeeklyWindow(seven, now)
		if p, ok := floatFromAny(mapGet(seven, "pct")); ok {
			segments = append(segments, fmt.Sprintf("%.0f%%", p))
		}
	}
	if len(segments) == 0 {
		return Icon
	}
	return Icon + " " + strings.Join(segments, " · ")
}

// FormatUsageLog mirrors format_usage_log: a log line of an account's
// session (5h) and weekly (7d) limits, using each window's absolute
// reset clock rather than a live countdown (log lines are already
// timestamped). ok=false when no numeric window is available.
func FormatUsageLog(email string, u DisplayUsage) (string, bool) {
	var parts []string
	for _, lw := range []struct{ key, label string }{{"five_hour", "5h"}, {"seven_day", "7d"}} {
		pct, ok := windowPct(u.LastGood, lw.key)
		if !ok {
			continue
		}
		w, _ := u.LastGood[lw.key].(map[string]any)
		seg := fmt.Sprintf("%s %.0f%%", lw.label, pct)
		if clock, cok := w["clock"].(string); cok && clock != "" {
			seg += fmt.Sprintf(" (resets %s)", clock)
		}
		parts = append(parts, seg)
	}
	if len(parts) == 0 {
		return "", false
	}
	return fmt.Sprintf("usage %s: %s", email, strings.Join(parts, " · ")), true
}

var switchLogRe = regexp.MustCompile(`Switched from account (\d+) to (\d+)`)

// ParseSwitchHistory mirrors parse_switch_history: recent account
// switches from the log, most-recent first, at most limit entries.
func ParseSwitchHistory(logText string, limit int) []string {
	var out []string
	for _, line := range strings.Split(logText, "\n") {
		m := switchLogRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		stamp := line
		if i := strings.Index(line, " - "); i >= 0 {
			stamp = line[:i]
		}
		stamp = strings.TrimSpace(stamp)
		if len(stamp) > 16 {
			stamp = stamp[:16]
		}
		out = append(out, fmt.Sprintf("%s → %s   %s", m[1], m[2], stamp))
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	// reverse: most-recent first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
