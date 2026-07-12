// internal/cswap/tui/integration_test.go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mliem2k/claude-swap/internal/cswap"
)

// TestDashboardToSwitchToWatchToAutoNavigation drives the real App through
// every screen this plan ported, proving the screens compose through the
// actual stack (Task 7), not just in each task's own isolated test.
func TestDashboardToSwitchToWatchToAutoNavigation(t *testing.T) {
	// NewClaudeAccountSwitcher takes a debug bool, not a directory: BackupDir
	// is derived from the environment (GetBackupRoot), so isolation happens
	// via HOME/XDG_DATA_HOME, not a constructor argument. This mirrors the
	// established pattern in autoview_test.go's setupAppForAutoScreen, the
	// only other place in this package that builds a real switcher.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", "")
	switcher, err := cswap.NewClaudeAccountSwitcher(false)
	if err != nil {
		t.Fatalf("NewClaudeAccountSwitcher: %v", err)
	}
	app := NewApp(switcher, "dashboard")
	app.Init()

	if !strings.Contains(app.View(), "Switch account") {
		t.Fatalf("dashboard root view = %q", app.View())
	}

	// step sends one message through app.Update and, if that produced a
	// tea.Cmd, executes it once and feeds any resulting message back through
	// app.Update - mirroring what the real bubbletea runtime loop does.
	// This matters here: dashboardScreen/switchScreen/watchScreen never
	// mutate App's screen stack directly on a key press. Every screen
	// transition (the "s"/"w" shortcuts, "esc" pops) instead returns a
	// pushScreenMsg- or popScreenMsg-producing tea.Cmd for App.Update to act
	// on later, so a test that inspects app.top() right after app.Update()
	// without also running that returned cmd would still see the *previous*
	// top of stack.
	step := func(msg tea.Msg) {
		model, cmd := app.Update(msg)
		app = model.(*App)
		if cmd == nil {
			return
		}
		result := cmd()
		if result == nil {
			return
		}
		model, _ = app.Update(result)
		app = model.(*App)
	}
	key := func(r rune) { step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}) }

	// s -> switch screen
	key('s')
	if got := app.top().View(); !strings.Contains(got, "switch to which account?") {
		t.Fatalf("after 's', top screen view = %q", got)
	}

	// esc -> back to dashboard
	step(tea.KeyMsg{Type: tea.KeyEscape})
	if !strings.Contains(app.top().View(), "Switch account") {
		t.Fatal("esc from switch screen should return to the dashboard root menu")
	}

	// w -> watch screen
	key('w')
	if !strings.Contains(app.top().View(), "watching all accounts") {
		t.Fatal("'w' from the dashboard should open the watch screen")
	}
	step(tea.KeyMsg{Type: tea.KeyEscape})
	if !strings.Contains(app.top().View(), "Switch account") {
		t.Fatal("esc from watch screen should return to the dashboard root menu")
	}

	// menu down to "Auto-switch view" (root entries: Switch, Watch,
	// Auto-switch, Add account…, Remove account…, Quit - index 2, so two "j"
	// presses from the cursor at index 0), enter -> auto screen
	for i := 0; i < 2; i++ {
		key('j')
	}
	step(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(app.top().View(), "DRY-RUN") {
		t.Fatalf("after opening auto-switch view, top screen view = %q, want the DRY-RUN badge", app.top().View())
	}
}
