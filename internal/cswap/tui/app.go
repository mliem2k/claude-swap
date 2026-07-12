// internal/cswap/tui/app.go
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mliem2k/claude-swap/internal/cswap"
)

// pollIntervalS mirrors POLL_INTERVAL_S: matches the old watch view's
// recapture cadence.
const pollIntervalS = 3 * time.Second

// screen is what every TUI screen (Task 8's dashboard/switch/watch, Task
// 9's auto screen, Task 6's modals via screenAdapter) implements. It
// differs from tea.Model only in Update's return type, so a screen's
// Update never accidentally hands back a bare tea.Model that forgets it
// is also a screen.
type screen interface {
	Init() tea.Cmd
	Update(tea.Msg) (screen, tea.Cmd)
	View() string
}

// screenAdapter lifts a tea.Model (Task 6's modals, which are written
// against the plain tea.Model interface since Bubbles components expect
// that shape) into a screen.
type screenAdapter struct{ tea.Model }

func (a screenAdapter) Update(msg tea.Msg) (screen, tea.Cmd) {
	next, cmd := a.Model.Update(msg)
	return screenAdapter{next}, cmd
}

func adaptScreen(m tea.Model) screen { return screenAdapter{m} }

type snapshotMsg struct {
	snap *cswap.AccountsSnapshotResult
}

type actionDoneMsg struct {
	label      string
	result     ActionResult
	showOutput bool
}

// notification mirrors one self.notify(...) toast call: a transient
// status message with Textual's severity vocabulary ("information"
// (default) | "warning" | "error"), rendered under the current screen
// until it expires.
type notification struct {
	message  string
	title    string
	severity string
}

// defaultNotifyTimeout mirrors Textual's own default toast timeout (5s);
// Python overrides it per-call (timeout=2, timeout=6) where noted at each
// Notify call site below.
const defaultNotifyTimeout = 5 * time.Second

// clearNoticeMsg carries the generation of the notice it should clear,
// so a still-pending timer for an older notice can never clear a newer
// one that replaced it before the old timer fired.
type clearNoticeMsg struct{ gen int }

// App mirrors CswapApp: owns the snapshot poll loop and every mutating
// action, so every screen drives the same code paths.
type App struct {
	Switcher     *cswap.ClaudeAccountSwitcher
	Snapshot     *cswap.AccountsSnapshotResult
	ThresholdPct *float64

	start  string // "dashboard" | "watch"
	source *cswap.SnapshotSource

	stack []screen

	// width and height are the live terminal size from the most recent
	// tea.WindowSizeMsg, mirroring Python's self.size.width/height
	// (Textual's own live layout size); see ContentWidth/ContentHeight
	// for the pre-any-resize defaults. Bubble Tea sends an initial
	// WindowSizeMsg on startup, so these are populated before the first
	// real render in practice; the defaults only matter for tests that
	// never dispatch one.
	width  int
	height int

	// notice and noticeGen back Notify's toast system (see clearNoticeMsg).
	notice    *notification
	noticeGen int

	refreshing bool
	storeOnly  bool
	fullNext   bool

	busy       bool
	pendingCmd tea.Cmd
}

// NewApp mirrors CswapApp.__init__.
func NewApp(switcher *cswap.ClaudeAccountSwitcher, start string) *App {
	a := &App{
		Switcher: switcher,
		start:    start,
		source:   cswap.NewSnapshotSource(switcher),
	}
	settings := cswap.LoadSettings(switcher.BackupDir)
	a.ThresholdPct = &settings.Threshold
	return a
}

func (a *App) Init() tea.Cmd {
	a.PushScreen(newDashboardScreen(a))
	if a.start == "watch" {
		a.PushScreen(newWatchScreen(a))
	}
	return tea.Batch(a.top().Init(), a.tickCmd(), a.pollCmd())
}

func (a *App) top() screen { return a.stack[len(a.stack)-1] }

// PushScreen mirrors push_screen.
func (a *App) PushScreen(s screen) { a.stack = append(a.stack, s) }

// PopScreen mirrors pop_screen; a no-op on the root screen (mirrors
// Textual, where the base dashboard screen is never popped).
func (a *App) PopScreen() {
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
	}
}

func (a *App) tickCmd() tea.Cmd {
	return tea.Tick(pollIntervalS, func(time.Time) tea.Msg { return tickMsg{} })
}

type tickMsg struct{}

func (a *App) pollCmd() tea.Cmd {
	if a.refreshing {
		return nil
	}
	a.refreshing = true
	full, storeOnly := a.fullNext, a.storeOnly
	a.fullNext = false
	source := a.source
	return func() tea.Msg {
		snap := source.Take(full, storeOnly)
		return snapshotMsg{snap: &snap}
	}
}

func (a *App) applySnapshot(msg snapshotMsg) {
	a.refreshing = false
	a.Snapshot = msg.snap
}

// RequestRefresh mirrors request_refresh.
func (a *App) RequestRefresh(full bool) {
	if full {
		a.fullNext = true
	}
}

// SetStoreOnly mirrors set_store_only.
func (a *App) SetStoreOnly(v bool) { a.storeOnly = v }

// ActionRefreshFull mirrors action_refresh_full: an explicit "f" keypress
// forces a full refresh out of cadence, unlike the RequestRefresh(false)
// calls elsewhere in this file that just flag fullNext and let the next
// natural tickMsg (up to pollIntervalS away) pick it up. Python's
// request_refresh calls self._tick() immediately, so this batches pollCmd
// itself the same way rather than waiting on the timer.
func (a *App) ActionRefreshFull() tea.Cmd {
	a.RequestRefresh(true)
	return tea.Batch(a.pollCmd(), a.Notify("Refreshing usage…", "", "", 2*time.Second))
}

// Notify mirrors self.notify(message, title=, severity=, timeout=):
// shows a transient toast under the current screen (see App.View),
// auto-clearing after timeout (defaultNotifyTimeout when 0). severity is
// Textual's vocabulary: "information" (default, pass "" for it),
// "warning", or "error".
func (a *App) Notify(message, title, severity string, timeout time.Duration) tea.Cmd {
	if timeout <= 0 {
		timeout = defaultNotifyTimeout
	}
	a.noticeGen++
	gen := a.noticeGen
	a.notice = &notification{message: message, title: title, severity: severity}
	return tea.Tick(timeout, func(time.Time) tea.Msg { return clearNoticeMsg{gen: gen} })
}

// defaultContentWidth matches the width every render path hardcoded
// before terminal-resize responsiveness existed, used as the fallback
// when no tea.WindowSizeMsg has arrived yet (construction, or a test that
// never dispatches one).
const defaultContentWidth = 80

// ContentWidth is the live terminal width every screen's View() renders
// against, mirroring Python's account_card_text/account_list_screen
// reading self.size.width live from Textual's layout instead of a fixed
// constant. An earlier Go draft hardcoded 80/78 at every render call
// site, so bars/cards never adapted to a narrow or wide terminal at all.
func (a *App) ContentWidth() int {
	if a.width <= 0 {
		return defaultContentWidth
	}
	return a.width
}

// defaultContentHeight is the fallback terminal height (a common default
// row count) used before any tea.WindowSizeMsg has arrived.
const defaultContentHeight = 24

// ContentHeight is the live terminal height, the vertical counterpart of
// ContentWidth. Used to size WatchScreen's viewport for scrolling a
// monitor-mode account list that overflows the terminal (mirrors
// Textual's ListView, which scrolls its own content automatically; Bubble
// Tea's alt-screen buffer does not, so this is the Go port's own
// mechanism for it).
func (a *App) ContentHeight() int {
	if a.height <= 0 {
		return defaultContentHeight
	}
	return a.height
}

// startAction mirrors _start_action's busy gate; returns false (and does
// nothing) if an action is already running.
func (a *App) startAction() bool {
	if a.busy {
		return false
	}
	a.busy = true
	return true
}

// runActionCmd mirrors _start_action's busy gate; when an action is
// already running, it now notifies (matching Python's "Another action is
// still running" warning) instead of silently doing nothing.
func (a *App) runActionCmd(label string, fn func() ActionResult, showOutput bool) tea.Cmd {
	if !a.startAction() {
		return a.Notify("Another action is still running", "", "warning", 0)
	}
	return func() tea.Msg {
		return actionDoneMsg{label: label, result: fn(), showOutput: showOutput}
	}
}

// actionDone mirrors _action_done: a switch/switch_to result (detected
// via result.Switch, Go's equivalent of Python's `"switched" in payload`,
// since only switch actions populate that field) gets a toast ("Switched
// to X" / "No switch: reason"), never the OutputModal every other action
// gets. Otherwise mirrors Python's own if/elif exactly: showOutput with
// non-empty output pushes the modal; else a non-empty first line
// notifies instead (e.g. remove-account's "Removed Account-N" message).
func (a *App) actionDone(msg actionDoneMsg) tea.Cmd {
	a.busy = false
	a.RequestRefresh(false)
	if !msg.result.OK {
		return func() tea.Msg {
			return pushScreenMsg{adaptScreen(NewOutputModal(msg.label+", failed", msg.result.Message, func() {}))}
		}
	}
	if sr := msg.result.Switch; sr != nil {
		if sr.Switched {
			return a.Notify("Switched to "+switchTargetLabel(sr.To), "Switch", "", 0)
		}
		reason := sr.Reason
		if reason == "" {
			reason = "no switch performed"
		}
		return a.Notify(reason, "No switch", "warning", 0)
	}
	if msg.showOutput && strings.TrimSpace(msg.result.Message) != "" {
		return func() tea.Msg {
			return pushScreenMsg{adaptScreen(NewOutputModal(msg.label, msg.result.Message, func() {}))}
		}
	}
	if line := firstLineOf(msg.result.Message); line != "" {
		return a.Notify(line, "", "", 0)
	}
	return nil
}

// switchTargetLabel mirrors _action_done's `to.get("email") or
// f"account {to.get('number')}"`.
func switchTargetLabel(ref *cswap.AccountRef) string {
	if ref == nil {
		return ""
	}
	if ref.Email != "" {
		return ref.Email
	}
	if ref.Number != nil {
		return fmt.Sprintf("account %d", *ref.Number)
	}
	return ""
}

// firstLineOf mirrors ActionResult.first_line: the first non-empty line
// (Go's ActionResult.Message is already a plain string, not raw captured
// ANSI-colored terminal output the way Python's is, so no ANSI stripping
// is needed here).
func firstLineOf(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type pushScreenMsg struct{ s screen }

// DoSwitch mirrors do_switch.
func (a *App) DoSwitch(number string) tea.Cmd {
	return a.runActionCmd("Switch to account "+number, func() ActionResult {
		return RunSwitchAction(func() (*cswap.SwitchResult, error) {
			return a.Switcher.SwitchTo(number, false, nil)
		})
	}, false)
}

// ActionSwitchBest mirrors action_switch_best.
func (a *App) ActionSwitchBest() tea.Cmd {
	return a.runActionCmd("Switch (best)", func() ActionResult {
		return RunSwitchAction(func() (*cswap.SwitchResult, error) {
			return a.Switcher.Switch("best", nil)
		})
	}, false)
}

// ConfirmRemove mirrors confirm_remove: pushes a ConfirmModal, removing
// on confirm.
func (a *App) ConfirmRemove(number, email string) tea.Cmd {
	return func() tea.Msg {
		return pushScreenMsg{adaptScreen(NewConfirmModal(
			"Remove account "+number+" ("+email+")?\n\nIts stored credentials and config backup are deleted.",
			"Remove account", "Remove",
			func(confirmed bool) {
				if confirmed {
					a.pendingCmd = a.runActionCmd("Remove account "+number, func() ActionResult {
						return RunAction(func() (string, error) {
							return a.Switcher.RemoveAccount(number, nil, nil)
						})
					}, false)
				}
			},
		))}
	}
}

// ActionAddCurrent mirrors action_add_current.
func (a *App) ActionAddCurrent() tea.Cmd {
	return func() tea.Msg {
		return pushScreenMsg{adaptScreen(NewConfirmModal(
			"Back up the current Claude Code login as a managed account?\n\nIf this account is already managed, its stored credentials are refreshed in place.",
			"Add account", "Add",
			func(confirmed bool) {
				if confirmed {
					a.pendingCmd = a.runActionCmd("Add current login", func() ActionResult {
						return RunAction(func() (string, error) { return a.Switcher.AddAccount(nil, nil) })
					}, true)
				}
			},
		))}
	}
}

// SlotOccupant mirrors _slot_occupant: the email of the account currently
// in slot, or "" when slot is nil, unoccupied, or the snapshot isn't
// loaded yet.
func (a *App) SlotOccupant(slot *int) string {
	if slot == nil || a.Snapshot == nil {
		return ""
	}
	target := strconv.Itoa(*slot)
	for _, acc := range a.Snapshot.Accounts {
		if acc.Number == target {
			return acc.Email
		}
	}
	return ""
}

// ActionAddToken mirrors action_add_token/_on_token_form: confirms first
// when the target slot is already occupied ("Slot X is occupied by
// email. Overwrite?"), matching Python exactly. An earlier Go draft ran
// AddAccountFromToken unconditionally, silently overwriting an existing
// account's stored credentials.
func (a *App) ActionAddToken() tea.Cmd {
	return func() tea.Msg {
		return pushScreenMsg{adaptScreen(NewAddTokenModal(func(form *TokenForm) {
			if form == nil {
				return
			}
			run := func() {
				a.pendingCmd = a.runActionCmd("Add account from token", func() ActionResult {
					return RunAction(func() (string, error) {
						return a.Switcher.AddAccountFromToken(form.Token, form.Email, form.Slot, nil)
					})
				}, true)
			}
			occupant := a.SlotOccupant(form.Slot)
			if occupant == "" {
				run()
				return
			}
			a.pendingCmd = func() tea.Msg {
				return pushScreenMsg{adaptScreen(NewConfirmModal(
					fmt.Sprintf("Slot %d is occupied by %s. Overwrite?", *form.Slot, occupant),
					"Overwrite slot", "Overwrite",
					func(confirmed bool) {
						if confirmed {
							run()
						}
					},
				))}
			}
		}))}
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		return a, nil
	case tea.KeyMsg:
		if m.String() == "ctrl+c" {
			return a, tea.Quit
		}
	case tickMsg:
		return a, tea.Batch(a.tickCmd(), a.pollCmd())
	case snapshotMsg:
		a.applySnapshot(m)
		return a, nil
	case actionDoneMsg:
		cmd := a.actionDone(m)
		return a, cmd
	case clearNoticeMsg:
		if m.gen == a.noticeGen {
			a.notice = nil
		}
		return a, nil
	case pushScreenMsg:
		a.PushScreen(m.s)
		return a, a.top().Init()
	case popScreenMsg:
		a.PopScreen()
		if a.pendingCmd != nil {
			cmd := a.pendingCmd
			a.pendingCmd = nil
			return a, cmd
		}
		return a, nil
	}

	next, cmd := a.top().Update(msg)
	a.stack[len(a.stack)-1] = next
	return a, cmd
}

func (a *App) View() string {
	if len(a.stack) == 0 {
		return ""
	}
	view := a.top().View()
	if a.notice != nil {
		view += "\n\n" + renderNotice(*a.notice)
	}
	return view
}

// renderNotice mirrors Textual's toast rendering: a severity-colored
// line, title prefixed when set. No Bubble Tea/Lip Gloss equivalent of
// Textual's built-in toast widget exists, so this is a plain styled line
// appended under the current screen (see App.View) rather than a
// separately-positioned overlay.
func renderNotice(n notification) string {
	color := SevOK
	switch n.severity {
	case "warning":
		color = SevWarn
	case "error":
		color = SevCrit
	}
	style := lipgloss.NewStyle().Foreground(color)
	text := n.message
	if n.title != "" {
		text = n.title + ": " + text
	}
	return style.Render(text)
}
