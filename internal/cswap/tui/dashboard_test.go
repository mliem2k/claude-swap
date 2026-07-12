// internal/cswap/tui/dashboard_test.go
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mliem2k/claude-swap/internal/cswap"
)

// TestDashboardFooterShowsVisibleBindings and its siblings below are the
// regression tests for a real gap: every real Python screen composes a
// Footer() widget listing its visible keybindings, which the Go port had
// no equivalent of on any screen at all.
func TestDashboardFooterShowsVisibleBindings(t *testing.T) {
	app := &App{}
	s := newDashboardScreen(app)
	view := s.View()
	for _, want := range []string{"switch accounts", "watch", "quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard footer missing %q, got:\n%s", want, view)
		}
	}
}

func TestSwitchScreenFooterShowsVisibleBindings(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", Switchable: true}},
	}}
	s := newSwitchScreen(app)
	view := s.View()
	for _, want := range []string{"switch", "best pick", "back"} {
		if !strings.Contains(view, want) {
			t.Errorf("switch screen footer missing %q, got:\n%s", want, view)
		}
	}
}

func TestWatchScreenFooterShowsVisibleBindingsWhenUnselected(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", IsActive: true}},
	}}
	s := newWatchScreen(app)
	view := s.View()
	if !strings.Contains(view, "back") {
		t.Errorf("watch screen footer missing %q, got:\n%s", "back", view)
	}
}

func TestDashboardRootMenuShowsAllEntries(t *testing.T) {
	app := &App{}
	s := newDashboardScreen(app)
	view := s.View()
	for _, want := range []string{"Switch account", "Watch accounts", "Auto-switch view", "Add account", "Remove account", "Quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard root menu missing %q, got:\n%s", want, view)
		}
	}
}

func TestDashboardSDrivesToSwitchScreen(t *testing.T) {
	app := &App{}
	app.PushScreen(newDashboardScreen(app))
	next, cmd := app.top().Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	app.stack[len(app.stack)-1] = next
	if cmd == nil {
		t.Fatal("expected a push-screen command for the switch shortcut")
	}
}

func TestSwitchScreenEnterCallsDoSwitch(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", Switchable: true}},
	}}
	s := newSwitchScreen(app)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// The screen doesn't call DoSwitch directly (App does, from the msg
	// this Update returns), so the strongest thing this test can check
	// without wiring up the whole App is that enter actually produced a
	// command at all. Merely asserting the account still renders in
	// View() (the old version of this test) is vacuous: render() lists
	// every account regardless of cursor state, so it would pass even if
	// the "enter" case were deleted entirely.
	if cmd == nil {
		t.Fatal("expected enter on switch screen to return a non-nil command (switch + pop)")
	}
}

func TestSwitchScreenEscapePops(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", Switchable: true}},
	}}
	s := newSwitchScreen(app)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected esc on switch screen to return a pop-screen command")
	}
}

// TestSwitchScreenBestPickStaysOpen is the regression test for a real
// gap: Python's Binding("b", "app.switch_best", ...) maps directly to
// App-level action_switch_best, which never touches the screen stack
// (SwitchScreen stays open), unlike "enter"'s on_list_view_selected
// handler, which pops explicitly. An earlier Go draft batched a
// popScreen() alongside "b" too. Uses a real (test-isolated) Switcher,
// like autoview_test.go's setupAppForAutoScreen, since this test must
// actually invoke the returned command to distinguish "batched with a
// pop" from "not batched," and ActionSwitchBest's command reaches
// a.Switcher.Switch.
func TestSwitchScreenBestPickStaysOpen(t *testing.T) {
	app := setupAppForAutoScreen(t)
	app.Snapshot = &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", Switchable: true}},
	}
	s := newSwitchScreen(app)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if cmd == nil {
		t.Fatal("expected 'b' on switch screen to return the switch-best command")
	}
	if _, isBatch := cmd().(tea.BatchMsg); isBatch {
		t.Fatal("'b' must not batch a pop-screen command alongside switch-best")
	}
}

// TestDashboardGOpensAutoScreen and TestDashboardQQuits are regression
// tests for a real gap: Python's DashboardScreen binds "g" (app.open_auto,
// show=False) and "q" (app.quit) directly, neither of which the Go port's
// dashboardScreen.Update originally handled at all (only reachable via the
// nested menu's "Auto-switch view" / "Quit" entries).
func TestDashboardGOpensAutoScreen(t *testing.T) {
	app := &App{}
	app.PushScreen(newDashboardScreen(app))
	_, cmd := app.top().Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if cmd == nil {
		t.Fatal("expected g to return a push-screen command")
	}
	msg := cmd()
	pushMsg, ok := msg.(pushScreenMsg)
	if !ok {
		t.Fatalf("expected a pushScreenMsg, got %T", msg)
	}
	if _, ok := pushMsg.s.(*autoScreen); !ok {
		t.Fatalf("expected *autoScreen pushed, got %T", pushMsg.s)
	}
}

func TestDashboardQQuits(t *testing.T) {
	app := &App{}
	s := newDashboardScreen(app)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected q to return a command")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Fatalf("expected q to return tea.Quit, got %#v", msg)
	}
}

// TestDashboardFTriggersImmediateRefresh and
// TestWatchScreenFTriggersImmediateRefresh are the regression tests for a
// real gap: Python's action_refresh_full calls request_refresh(full=True)
// then self._tick() immediately (bypassing the poll-interval wait), plus a
// 2s "Refreshing usage…" toast; the Go port had no "f" case on either
// screen at all, so a full out-of-cadence refresh was unreachable from the
// keyboard. ActionRefreshFull sets refreshing/fullNext and the notice
// synchronously before returning its command (same pattern as Notify), so
// this checks that state directly instead of invoking the returned
// tea.Cmd, which would otherwise really block on the toast's tea.Tick.
func TestDashboardFTriggersImmediateRefresh(t *testing.T) {
	app := setupAppForAutoScreen(t)
	s := newDashboardScreen(app)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if cmd == nil {
		t.Fatal("expected f to return a refresh command")
	}
	if !app.refreshing {
		t.Error("expected f to start an immediate refresh (app.refreshing=true), not wait for the next tick")
	}
	if app.notice == nil || !strings.Contains(app.notice.message, "Refreshing") {
		t.Errorf("expected a 'Refreshing usage' notice, got %+v", app.notice)
	}
}

func TestWatchScreenFTriggersImmediateRefresh(t *testing.T) {
	app := setupAppForAutoScreen(t)
	app.Snapshot = &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", IsActive: true}},
	}
	s := newWatchScreen(app)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if cmd == nil {
		t.Fatal("expected f to return a refresh command")
	}
	if !app.refreshing {
		t.Error("expected f to start an immediate refresh (app.refreshing=true), not wait for the next tick")
	}
}

// TestWatchScreenScrollsWithoutSelection is the regression test for a real
// gap: Python's action_nav_down/action_nav_up still scroll the ListView
// (listview.scroll_down/up) when selection isn't armed, so a monitor-mode
// account list taller than the terminal stays reachable with j/k/arrows.
// The Go port's watchScreen originally no-op'd down/up entirely outside
// selected mode, since Bubble Tea's alt-screen buffer (unlike Textual's
// auto-scrolling ListView) never scrolls on its own.
func TestWatchScreenScrollsWithoutSelection(t *testing.T) {
	var accounts []cswap.AccountSnapshot
	for i := range 30 {
		accounts = append(accounts, cswap.AccountSnapshot{
			Number: strconv.Itoa(i + 1), Email: fmt.Sprintf("acct%d@example.com", i+1),
			Usage: cswap.UsageEntry{LastGood: map[string]any{"five_hour": map[string]any{"pct": 10.0}}},
		})
	}
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{Accounts: accounts}}
	s := newWatchScreen(app).(*watchScreen)
	// Size the viewport the way a real render would (App defaults to
	// defaultContentWidth/defaultContentHeight before any resize).
	s.View()
	if s.viewport.YOffset != 0 {
		t.Fatalf("YOffset before scrolling = %d, want 0", s.viewport.YOffset)
	}
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if s.viewport.YOffset == 0 {
		t.Error("expected j to scroll the viewport down while unselected, YOffset stayed 0")
	}
	afterDown := s.viewport.YOffset
	s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if s.viewport.YOffset != afterDown-1 {
		t.Errorf("YOffset after k = %d, want %d (one line back up)", s.viewport.YOffset, afterDown-1)
	}
}

// TestAccountListStateFlashesRowsWithAdvancedMeasurement and
// TestAccountListStateFlashExpiresAfterFlashS are the regression tests for a
// real gap: Python's AccountListScreen._flash_updated briefly highlights
// any row whose stored measurement (FetchedAt) just advanced, which the Go
// port's accountListState.render originally had no equivalent for at all.
func TestAccountListStateFlashesRowsWithAdvancedMeasurement(t *testing.T) {
	ts1, ts2 := 100.0, 200.0
	accounts := []cswap.AccountSnapshot{
		{Number: "1", Usage: cswap.UsageEntry{FetchedAt: &ts1}},
		{Number: "2", Usage: cswap.UsageEntry{FetchedAt: &ts1}},
	}
	s := &accountListState{}
	s.updateFlash(accounts) // first render establishes the baseline
	if s.isFlashing("1") || s.isFlashing("2") {
		t.Fatal("the first render must not flash any row (nothing 'just advanced' yet)")
	}
	accounts[0].Usage.FetchedAt = &ts2 // account 1's measurement advances
	s.updateFlash(accounts)
	if !s.isFlashing("1") {
		t.Error("expected account 1 to flash after its FetchedAt advanced")
	}
	if s.isFlashing("2") {
		t.Error("account 2's FetchedAt did not change, it should not flash")
	}
}

func TestAccountListStateFlashExpiresAfterFlashS(t *testing.T) {
	ts1, ts2 := 100.0, 200.0
	accounts := []cswap.AccountSnapshot{{Number: "1", Usage: cswap.UsageEntry{FetchedAt: &ts1}}}
	s := &accountListState{}
	s.updateFlash(accounts)
	accounts[0].Usage.FetchedAt = &ts2
	s.updateFlash(accounts)
	if !s.isFlashing("1") {
		t.Fatal("expected account 1 to be flashing right after the change")
	}
	s.flashUntil["1"] = time.Now().Add(-time.Millisecond) // force expiry, no real sleep
	if s.isFlashing("1") {
		t.Error("expected the flash to have expired")
	}
}

func TestWatchScreenStartsUnarmed(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", IsActive: true}},
	}}
	s := newWatchScreen(app)
	if !strings.Contains(s.View(), "watching all accounts") {
		t.Fatalf("watch screen initial view = %q, want the watch title", s.View())
	}
}

// TestWatchScreenNeverTouchesStoreOnly is the regression test for a real
// bug: newWatchScreen used to call app.SetStoreOnly(true), which is
// Python's AutoScreen-only mechanism (dashboard.py's WatchScreen/
// SwitchScreen never call set_store_only at all), so it made Watch's
// live-monitor screen stop refreshing over the network entirely, the
// opposite of its purpose. Confirms both entering and leaving (via esc)
// leave storeOnly at whatever it already was.
func TestWatchScreenNeverTouchesStoreOnly(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", IsActive: true}},
	}}
	if app.storeOnly {
		t.Fatal("test setup invariant broken: storeOnly should start false")
	}
	s := newWatchScreen(app)
	if app.storeOnly {
		t.Fatal("newWatchScreen must not set storeOnly")
	}
	watchScreen, ok := s.(*watchScreen)
	if !ok {
		t.Fatalf("expected *watchScreen, got %T", s)
	}
	_, _ = watchScreen.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if app.storeOnly {
		t.Fatal("leaving the watch screen must not touch storeOnly either")
	}
}

func TestWatchScreenSArmsSelection(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", IsActive: true}},
	}}
	s := newWatchScreen(app)
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !strings.Contains(s.View(), "switch to which account?") {
		t.Fatalf("after 's', view = %q, want the selection-armed title", s.View())
	}
}

// TestDashboardEscapePopsSubmenu proves the "esc" fix in
// dashboardScreen.Update: it navigates into the "add account" submenu via
// the menu's own cursor + enter (the same pattern
// TestDashboardSDrivesToSwitchScreen uses to drive keys), then sends a
// real tea.KeyMsg{Type: tea.KeyEscape} and checks the crumb trail in
// View() actually popped back up one level. Before the fix, esc's
// String() ("esc") never matched the literal "escape" case, so this
// would fail (the submenu would stay open).
func TestDashboardEscapePopsSubmenu(t *testing.T) {
	app := &App{}
	s := newDashboardScreen(app)
	// Root entries: Switch, Watch, Auto-switch, Add account…, Remove
	// account…, Quit — move the cursor down to "Add account…".
	for i := 0; i < 3; i++ {
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(s.View(), "menu › add account") {
		t.Fatalf("expected to have entered the add-account submenu, view = %q", s.View())
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if strings.Contains(s.View(), "add account") {
		t.Fatalf("expected esc to pop back out of the submenu, view = %q", s.View())
	}
}

// TestDashboardJKMovesCursor is a quick bonus check that plain j/k cursor
// movement still works alongside the esc fix.
func TestDashboardJKMovesCursor(t *testing.T) {
	app := &App{}
	var s screen = newDashboardScreen(app)
	if s.(*dashboardScreen).cursor != 0 {
		t.Fatalf("expected initial cursor 0, got %d", s.(*dashboardScreen).cursor)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if s.(*dashboardScreen).cursor != 1 {
		t.Fatalf("expected cursor 1 after j, got %d", s.(*dashboardScreen).cursor)
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if s.(*dashboardScreen).cursor != 0 {
		t.Fatalf("expected cursor 0 after k, got %d", s.(*dashboardScreen).cursor)
	}
}

// TestWatchScreenEscapeDisarmsSelection proves the "esc" fix in
// watchScreen.Update: with selection armed, esc must disarm (clear
// selection) rather than pop the screen. Before the fix, esc's String()
// never matched "escape", so this would fail (selection would stay
// armed).
func TestWatchScreenEscapeDisarmsSelection(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", IsActive: true}},
	}}
	s := newWatchScreen(app)
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !strings.Contains(s.View(), "switch to which account?") {
		t.Fatalf("expected armed selection view, got %q", s.View())
	}
	s, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd != nil {
		t.Fatalf("expected esc to disarm in place (nil command), got %v", cmd)
	}
	if !strings.Contains(s.View(), "watching all accounts") {
		t.Fatalf("expected esc to disarm selection back to the watch title, view = %q", s.View())
	}
}

// TestWatchScreenEscapePopsWhenUnarmed proves the same "esc" fix for the
// unarmed case: esc on an unarmed watch screen must return a pop-screen
// command. Before the fix, esc's String() never matched "escape", so
// this would fail (cmd would be nil and the screen would stay open).
func TestWatchScreenEscapePopsWhenUnarmed(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", IsActive: true}},
	}}
	s := newWatchScreen(app)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected esc on an unarmed watch screen to return a pop-screen command")
	}
}

// TestSwitchScreenEnterClampsCursorAfterSnapshotShrinks reproduces the
// crash from finding 1 of the whole-branch review: App's poll loop
// (app.go, every ~3s) can swap in a fresh, shorter Accounts slice at any
// time - e.g. because the user ran `cswap remove` in another terminal
// while this screen was open, a normal workflow for this tool. The cursor
// is only set once at construction (activeIndex) and re-clamped on
// explicit up/down navigation, so it can be left pointing past the end of
// the new, shorter slice. Before the fix, accounts[s.list.cursor] on
// Enter indexed out of bounds and panicked the whole TUI.
func TestSwitchScreenEnterClampsCursorAfterSnapshotShrinks(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{
			{Number: "1", Email: "a@example.com", Switchable: true},
			{Number: "2", Email: "b@example.com", Switchable: true},
			{Number: "3", Email: "c@example.com", Switchable: true},
		},
	}}
	s := newSwitchScreen(app).(*switchScreen)
	s.list.cursor = 2 // positioned at the last account

	// Simulate a poll landing mid-session after a removal: a fresh, shorter
	// Accounts slice swapped in without telling the cursor.
	app.Snapshot = &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{
			{Number: "1", Email: "a@example.com", Switchable: true},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("enter on switch screen panicked after snapshot shrank: %v", r)
		}
	}()
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected enter to still produce a switch command for the clamped account")
	}
}

// TestSwitchScreenEnterNoopWhenSnapshotBecomesEmpty covers the other edge
// of the same clamp: if the snapshot shrinks to zero accounts, Enter must
// degrade to a no-op rather than indexing an empty slice.
func TestSwitchScreenEnterNoopWhenSnapshotBecomesEmpty(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", Switchable: true}},
	}}
	s := newSwitchScreen(app).(*switchScreen)
	app.Snapshot = &cswap.AccountsSnapshotResult{Accounts: nil}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("enter on switch screen panicked with an empty snapshot: %v", r)
		}
	}()
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected enter with no accounts to no-op, got cmd = %v", cmd)
	}
}

// TestWatchScreenEnterClampsCursorAfterSnapshotShrinks is the watchScreen
// counterpart to TestSwitchScreenEnterClampsCursorAfterSnapshotShrinks:
// same crash, same fix, armed selection instead of switchScreen's
// always-selected cursor.
func TestWatchScreenEnterClampsCursorAfterSnapshotShrinks(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{
			{Number: "1", Email: "a@example.com", IsActive: true},
			{Number: "2", Email: "b@example.com"},
			{Number: "3", Email: "c@example.com"},
		},
	}}
	s := newWatchScreen(app).(*watchScreen)
	s.list.selected = true
	s.list.cursor = 2 // positioned at the last account

	app.Snapshot = &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{
			{Number: "1", Email: "a@example.com", IsActive: true},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("enter on watch screen panicked after snapshot shrank: %v", r)
		}
	}()
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected enter to still produce a switch command for the clamped account")
	}
}

// TestWatchScreenEnterNoopWhenSnapshotBecomesEmpty is the watchScreen
// counterpart to TestSwitchScreenEnterNoopWhenSnapshotBecomesEmpty.
func TestWatchScreenEnterNoopWhenSnapshotBecomesEmpty(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{
		Accounts: []cswap.AccountSnapshot{{Number: "1", Email: "a@example.com", IsActive: true}},
	}}
	s := newWatchScreen(app).(*watchScreen)
	s.list.selected = true
	app.Snapshot = &cswap.AccountsSnapshotResult{Accounts: nil}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("enter on watch screen panicked with an empty snapshot: %v", r)
		}
	}()
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected enter with no accounts to no-op, got cmd = %v", cmd)
	}
}
