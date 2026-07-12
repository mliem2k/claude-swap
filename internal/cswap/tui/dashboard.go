// internal/cswap/tui/dashboard.go
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mliem2k/claude-swap/internal/cswap"
)

type menuEntry struct{ label, actionID string }

var backEntry = menuEntry{"← back", "back"}

// dashboardScreen mirrors DashboardScreen: an always-visible accounts
// panel on top, a nested action menu below.
type dashboardScreen struct {
	app       *App
	menuStack [][]menuEntry
	titles    []string
	cursor    int
}

func newDashboardScreen(app *App) screen {
	s := &dashboardScreen{app: app}
	s.pushMenu("menu", s.rootEntries())
	return s
}

func (s *dashboardScreen) rootEntries() []menuEntry {
	return []menuEntry{
		{"Switch account…", "switch"},
		{"Watch accounts", "watch"},
		{"Auto-switch view", "auto"},
		{"Add account…", "add-menu"},
		{"Remove account…", "remove-menu"},
		{"Quit", "quit"},
	}
}

func (s *dashboardScreen) addEntries() []menuEntry {
	return []menuEntry{{"From current Claude Code login", "add-login"}, {"From a setup-token / API key…", "add-token"}, backEntry}
}

func (s *dashboardScreen) removeEntries() []menuEntry {
	var entries []menuEntry
	if s.app.Snapshot != nil {
		for _, acc := range s.app.Snapshot.Accounts {
			entries = append(entries, menuEntry{acc.Number + "  " + acc.Email + "  [" + acc.DisplayTag() + "]", "remove:" + acc.Number})
		}
	}
	return append(entries, backEntry)
}

func (s *dashboardScreen) pushMenu(title string, entries []menuEntry) {
	s.titles = append(s.titles, title)
	s.menuStack = append(s.menuStack, entries)
	s.cursor = 0
}

func (s *dashboardScreen) popMenu() {
	if len(s.menuStack) > 1 {
		s.titles = s.titles[:len(s.titles)-1]
		s.menuStack = s.menuStack[:len(s.menuStack)-1]
		s.cursor = 0
	}
}

func (s *dashboardScreen) currentEntries() []menuEntry { return s.menuStack[len(s.menuStack)-1] }

func (s *dashboardScreen) Init() tea.Cmd { return nil }

func (s *dashboardScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	entries := s.currentEntries()
	switch keyMsg.String() {
	case "down", "j":
		if s.cursor < len(entries)-1 {
			s.cursor++
		}
		return s, nil
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
		return s, nil
	case "s":
		return s, func() tea.Msg { return pushScreenMsg{newSwitchScreen(s.app)} }
	case "w":
		return s, func() tea.Msg { return pushScreenMsg{newWatchScreen(s.app)} }
	case "g":
		return s, func() tea.Msg { return pushScreenMsg{newAutoScreen(s.app)} }
	case "f":
		return s, s.app.ActionRefreshFull()
	case "q":
		return s, tea.Quit
	case "esc", "left":
		s.popMenu()
		return s, nil
	case "enter":
		return s, s.dispatch(entries[s.cursor].actionID)
	}
	return s, nil
}

func (s *dashboardScreen) dispatch(actionID string) tea.Cmd {
	switch {
	case actionID == "back":
		s.popMenu()
		return nil
	case actionID == "add-menu":
		s.pushMenu("add account", s.addEntries())
		return nil
	case actionID == "remove-menu":
		s.pushMenu("remove account", s.removeEntries())
		return nil
	case strings.HasPrefix(actionID, "remove:"):
		number := strings.TrimPrefix(actionID, "remove:")
		email := "?"
		if s.app.Snapshot != nil {
			for _, acc := range s.app.Snapshot.Accounts {
				if acc.Number == number {
					email = acc.Email
				}
			}
		}
		return s.app.ConfirmRemove(number, email)
	case actionID == "switch":
		return func() tea.Msg { return pushScreenMsg{newSwitchScreen(s.app)} }
	case actionID == "watch":
		return func() tea.Msg { return pushScreenMsg{newWatchScreen(s.app)} }
	case actionID == "auto":
		return func() tea.Msg { return pushScreenMsg{newAutoScreen(s.app)} }
	case actionID == "add-login":
		return s.app.ActionAddCurrent()
	case actionID == "add-token":
		return s.app.ActionAddToken()
	case actionID == "quit":
		return tea.Quit
	}
	return nil
}

func (s *dashboardScreen) View() string {
	var b strings.Builder
	b.WriteString(s.panelView())
	b.WriteString("\n\n")
	b.WriteString(strings.Join(s.titles, " › "))
	b.WriteString("\n")
	for i, e := range s.currentEntries() {
		prefix := "  "
		if i == s.cursor {
			prefix = "> "
		}
		style := StyleForeground
		if e.actionID == "back" {
			style = StyleMuted
		}
		b.WriteString(prefix + style.Render(e.label) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(renderFooter("s switch accounts", "w watch", "q quit"))
	return b.String()
}

func (s *dashboardScreen) panelView() string {
	if s.app.Snapshot == nil {
		return StyleMuted.Render("loading…")
	}
	return AccountsPanelText(s.app.Snapshot.Accounts, true, s.app.ThresholdPct, s.app.ContentWidth())
}

// flashS mirrors FLASH_S: how long a just-refreshed row stays highlighted.
const flashS = 1500 * time.Millisecond

// accountListState is the cursor/selection state SwitchScreen and
// WatchScreen both drive, folded into a small shared helper in place of
// Python's AccountListScreen base class (Go has no inheritance).
type accountListState struct {
	app      *App
	cursor   int
	selected bool // WatchScreen only: whether selection is armed

	// stamps and flashUntil back updateFlash's "briefly highlight rows
	// whose stored measurement just advanced" (mirrors _flash_updated).
	// stamps is nil until the first render, so the very first paint never
	// flashes every row.
	stamps     map[string]*float64
	flashUntil map[string]time.Time
}

// updateFlash mirrors _flash_updated: diffs each account's FetchedAt
// against the value seen at the previous render and arms a flashS window
// for any account whose measurement just advanced. There is no per-row
// expiry timer (unlike Notify's clearNoticeMsg): isFlashing checks
// wall-clock time at render time instead, so a flash can occasionally
// linger up to one extra poll cycle past flashS on an otherwise idle
// screen (no keypress or engine event to force an earlier repaint) rather
// than clearing at the exact instant. Acceptable for a purely cosmetic cue.
func (s *accountListState) updateFlash(accounts []cswap.AccountSnapshot) {
	newStamps := make(map[string]*float64, len(accounts))
	for _, acc := range accounts {
		newStamps[acc.Number] = acc.Usage.FetchedAt
	}
	if s.stamps != nil {
		if s.flashUntil == nil {
			s.flashUntil = make(map[string]time.Time)
		}
		now := time.Now()
		for num, ts := range newStamps {
			if ts == nil {
				continue
			}
			old, existed := s.stamps[num]
			if !existed || old == nil || *old != *ts {
				s.flashUntil[num] = now.Add(flashS)
			}
		}
	}
	s.stamps = newStamps
}

func (s *accountListState) isFlashing(number string) bool {
	until, ok := s.flashUntil[number]
	return ok && time.Now().Before(until)
}

func activeIndex(accounts []cswap.AccountSnapshot) int {
	for i, a := range accounts {
		if a.IsActive {
			return i
		}
	}
	return 0
}

func (s *accountListState) accounts() []cswap.AccountSnapshot {
	if s.app.Snapshot == nil {
		return nil
	}
	return s.app.Snapshot.Accounts
}

// clampCursor keeps the cursor within the bounds of the current accounts
// slice. App's poll loop (app.go, roughly every 3s) can swap in a fresh,
// shorter Accounts slice at any time - e.g. the user ran `cswap remove` in
// another terminal while this screen was open, a normal workflow for this
// tool - and the cursor is otherwise only set once at construction and
// re-clamped on explicit up/down navigation, so it can be left pointing
// past the end of a shrunk list.
func (s *accountListState) clampCursor() {
	n := len(s.accounts())
	switch {
	case n == 0:
		s.cursor = 0
	case s.cursor < 0:
		s.cursor = 0
	case s.cursor >= n:
		s.cursor = n - 1
	}
}

// current clamps the cursor and returns the account under it. ok is false
// when there are no accounts to select, so every Enter handler that would
// otherwise index accounts[s.cursor] can go through this instead of
// indexing directly.
func (s *accountListState) current() (cswap.AccountSnapshot, bool) {
	s.clampCursor()
	accounts := s.accounts()
	if len(accounts) == 0 {
		return cswap.AccountSnapshot{}, false
	}
	return accounts[s.cursor], true
}

func (s *accountListState) render(title string) string {
	accounts := s.accounts()
	s.updateFlash(accounts)
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	if len(accounts) == 0 {
		b.WriteString(StyleMuted.Render("no managed accounts"))
		return b.String()
	}
	for i, acc := range accounts {
		prefix := "  "
		if s.selected && i == s.cursor {
			prefix = "> "
		}
		card := AccountCardText(acc, s.app.ContentWidth(), s.app.ThresholdPct)
		if s.isFlashing(acc.Number) {
			card = lipgloss.NewStyle().Background(Panel).Width(s.app.ContentWidth()).Render(card)
		}
		b.WriteString(prefix + card + "\n\n")
	}
	return b.String()
}

// switchScreen mirrors SwitchScreen: every account full-size, arrows
// pick, enter switches.
type switchScreen struct{ list accountListState }

func newSwitchScreen(app *App) screen {
	s := &switchScreen{list: accountListState{app: app, selected: true}}
	s.list.cursor = activeIndex(s.list.accounts())
	return s
}

func (s *switchScreen) Init() tea.Cmd { return nil }

func (s *switchScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	s.list.clampCursor()
	accounts := s.list.accounts()
	switch keyMsg.String() {
	case "down", "j":
		if s.list.cursor < len(accounts)-1 {
			s.list.cursor++
		}
	case "up", "k":
		if s.list.cursor > 0 {
			s.list.cursor--
		}
	case "enter":
		if acc, ok := s.list.current(); ok {
			return s, tea.Batch(s.list.app.DoSwitch(acc.Number), popScreen())
		}
	case "b":
		// Mirrors Binding("b", "app.switch_best", ...): action_switch_best
		// is an App-level action that never touches the screen stack, so
		// SwitchScreen stays open, unlike "enter" (whose
		// on_list_view_selected handler pops explicitly). An earlier Go
		// draft popped here too.
		return s, s.list.app.ActionSwitchBest()
	case "esc", "q", "s":
		return s, popScreen()
	}
	return s, nil
}

func (s *switchScreen) View() string {
	return s.list.render("switch to which account?") + "\n\n" +
		renderFooter("enter switch", "b best pick", "esc back")
}

// watchScreen mirrors WatchScreen: live monitor, hands-off by default; s
// arms selection, enter switches and stays watching, esc disarms first.
// viewport backs monitor-mode scrolling: Python's ListView scrolls its own
// content automatically, but Bubble Tea's alt-screen buffer doesn't, so an
// account list taller than the terminal needs its own scroll mechanism
// (mirrors action_nav_down/action_nav_up's listview.scroll_down/up calls
// when selection isn't armed).
type watchScreen struct {
	list     accountListState
	viewport viewport.Model
}

// newWatchScreen deliberately does not touch app.storeOnly: Python's
// WatchScreen/SwitchScreen never call set_store_only at all (only
// AutoScreen does, since it hosts its own live-fetching engine). An
// earlier Go draft called SetStoreOnly(true) here, which made Watch's
// "live monitor" screen stop refreshing over the network entirely, the
// opposite of its purpose.
func newWatchScreen(app *App) screen {
	return &watchScreen{list: accountListState{app: app, selected: false}}
}

func (s *watchScreen) Init() tea.Cmd { return nil }

func (s *watchScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	s.list.clampCursor()
	accounts := s.list.accounts()
	switch keyMsg.String() {
	case "s":
		s.list.selected = !s.list.selected
		if s.list.selected {
			s.list.cursor = activeIndex(accounts)
		}
	case "enter":
		if s.list.selected {
			if acc, ok := s.list.current(); ok {
				s.list.selected = false
				return s, s.list.app.DoSwitch(acc.Number)
			}
		}
	case "down", "j":
		if s.list.selected {
			if s.list.cursor < len(accounts)-1 {
				s.list.cursor++
			}
		} else {
			s.viewport.ScrollDown(1)
		}
	case "up", "k":
		if s.list.selected {
			if s.list.cursor > 0 {
				s.list.cursor--
			}
		} else {
			s.viewport.ScrollUp(1)
		}
	case "f":
		return s, s.list.app.ActionRefreshFull()
	case "esc", "q":
		if s.list.selected {
			s.list.selected = false
			return s, nil
		}
		return s, popScreen()
	}
	return s, nil
}

func (s *watchScreen) View() string {
	title := "watching all accounts"
	if s.list.selected {
		title = "switch to which account? · enter confirm · esc cancel"
	}
	content := s.list.render(title)
	if s.list.selected {
		// Cursor navigation always renders the full card list directly, no
		// viewport: SwitchScreen (the only other user of accountListState)
		// behaves the same way, and selection mode is expected to be a
		// short-lived, focused pick rather than a long scroll session.
		return content
	}
	// Monitor mode's own footer only: the selected-mode title above already
	// spells out "enter confirm · esc cancel" inline, so a second footer
	// line there would just repeat it.
	footerHeight := 2 // blank line + the footer line itself
	height := max(s.list.app.ContentHeight()-footerHeight, 1)
	s.viewport.Width = s.list.app.ContentWidth()
	s.viewport.Height = height
	s.viewport.SetContent(content)
	return s.viewport.View() + "\n\n" + renderFooter("s switch", "esc back")
}
