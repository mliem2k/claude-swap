// internal/cswap/tui/data.go
package tui

import (
	"fmt"
	"time"

	"github.com/mliem2k/claude-swap/internal/cswap"
)

// ActionResult is the outcome of a captured switcher action. Unlike
// Python's ActionResult (which captures printed stdout, since Python's
// switcher methods print as they run), the Go switcher methods this
// wraps are pure functions returning typed results, so there is no
// output to capture: Message is the method's own returned string (or the
// error's message on failure), and Switch carries the full structured
// result when the action is a switch.
type ActionResult struct {
	OK      bool
	Message string
	Switch  *cswap.SwitchResult
}

// RunAction runs one of the switcher's (string, error)-returning actions
// (AddAccount, AddAccountFromToken, RemoveAccount) and wraps the result.
func RunAction(fn func() (string, error)) ActionResult {
	msg, err := fn()
	if err != nil {
		return ActionResult{OK: false, Message: err.Error()}
	}
	return ActionResult{OK: true, Message: msg}
}

// RunSwitchAction runs one of the switcher's (*SwitchResult, error)-returning
// actions (Switch, SwitchTo) and wraps the result.
func RunSwitchAction(fn func() (*cswap.SwitchResult, error)) ActionResult {
	sr, err := fn()
	if err != nil {
		return ActionResult{OK: false, Message: err.Error()}
	}
	return ActionResult{OK: true, Message: sr.Message, Switch: sr}
}

// SentinelLabel re-exports cswap.SentinelLabel for tui callers, mirroring
// data.py's sentinel_label wrapper around switcher.SENTINEL_NOTES.
func SentinelLabel(sentinel string) string {
	return cswap.SentinelLabel(sentinel)
}

// formatDuration mirrors format_duration: a compact duration ("45s",
// "12m", "2h 13m", "3d 4h"), used only by FormatAge (reset countdowns
// reuse cswap.FormatReset instead, see Global Constraints).
func formatDuration(seconds float64) string {
	s := int(seconds)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		h, m := s/3600, (s%3600)/60
		if m != 0 {
			return fmt.Sprintf("%dh %dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	default:
		d, h := s/86400, (s%86400)/3600
		if h != 0 {
			return fmt.Sprintf("%dd %dh", d, h)
		}
		return fmt.Sprintf("%dd", d)
	}
}

// FormatAge mirrors format_age: a measurement-age note ("· 2m ago"), ""
// while comfortably fresh (at or below cswap.UsageAgeNoteS, the same
// staleness threshold `cswap list`'s UsageEntryLines uses, so both
// surfaces agree on when a measurement counts as "old").
func FormatAge(ageS *float64) string {
	if ageS == nil || *ageS <= cswap.UsageAgeNoteS {
		return ""
	}
	return fmt.Sprintf("· %s ago", formatDuration(*ageS))
}

// ClockStamp mirrors clock_stamp: an HH:MM:SS local-time stamp for the
// event log.
func ClockStamp() string {
	return time.Now().Format("15:04:05")
}
