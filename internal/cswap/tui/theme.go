package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Core palette: a subtle modern dark theme mirroring theme.py's
// "cswap-dark" — neutral charcoal backgrounds, one warm terracotta
// accent (the same tone printer.go's ANSI accent uses for the CLI), and
// desaturated severity colors so usage bars read calmly on a dark
// background.
const (
	Accent     = lipgloss.Color("#d7875f")
	Foreground = lipgloss.Color("#e8e4de")
	Muted      = lipgloss.Color("#8a8a8a")
	Background = lipgloss.Color("#141414")
	Surface    = lipgloss.Color("#1e1e1e")
	Panel      = lipgloss.Color("#262626")

	SevOK   = lipgloss.Color("#87af87")
	SevWarn = lipgloss.Color("#d7af5f")
	SevCrit = lipgloss.Color("#d75f5f")
	Track   = lipgloss.Color("#3a3a3a")
)

// Severity band edges. WarnPct mirrors where a user starts caring;
// CritPct mirrors the auto-switch default threshold so bar color and
// switch behavior agree.
const (
	WarnPct = 70.0
	CritPct = 90.0
)

// SeverityColor mirrors severity_color: bar/percentage color for a
// utilization percentage. nil (unknown) is muted.
func SeverityColor(pct *float64) lipgloss.Color {
	if pct == nil {
		return Muted
	}
	if *pct >= CritPct {
		return SevCrit
	}
	if *pct >= WarnPct {
		return SevWarn
	}
	return SevOK
}

var (
	StyleAccent     = lipgloss.NewStyle().Foreground(Accent)
	StyleMuted      = lipgloss.NewStyle().Foreground(Muted)
	StyleForeground = lipgloss.NewStyle().Foreground(Foreground)
	StyleBold       = lipgloss.NewStyle().Bold(true)
)

// renderFooter mirrors Footer(): a bottom hint bar listing the screen's
// currently visible keybindings ("key label" segments), the same plain
// muted style the modals' own hint lines already use (e.g. AddTokenModal's
// "enter add  ·  tab next field  ·  esc cancel"). Every real screen in
// Python composes a Footer widget; only bindings with show=True (and, on
// AutoScreen/WatchScreen, only those check_action currently allows) belong
// here, matching each screen's declared BINDINGS list.
func renderFooter(hints ...string) string {
	return StyleMuted.Render(strings.Join(hints, "  ·  "))
}
