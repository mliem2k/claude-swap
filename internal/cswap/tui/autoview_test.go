// internal/cswap/tui/autoview_test.go
package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mliem2k/claude-swap/internal/cswap"
)

func setupAppForAutoScreen(t *testing.T) *App {
	t.Helper()
	// NewClaudeAccountSwitcher takes a debug bool and derives BackupDir
	// from the environment (GetBackupRoot). Isolate it under a temp HOME so
	// LoadSettings reads defaults and nothing touches the real user data,
	// mirroring the cswap package's own newTestSwitcher env isolation (its
	// swapSecurity/newFakeSecurity are unexported and not needed here: these
	// tests never run the engine goroutine or any credential operation).
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", "")
	switcher, err := cswap.NewClaudeAccountSwitcher(false)
	if err != nil {
		t.Fatalf("NewClaudeAccountSwitcher: %v", err)
	}
	return &App{Switcher: switcher}
}

// TestCandidatesViewTiesBreakByAccountNumber is the regression test for a
// real gap: Python's sorted(ranked) sorts (pct, number) tuples, breaking
// a full pct tie by the account number string; an earlier Go draft's
// manual sort only compared pct, leaving ties in whatever order the
// snapshot happened to list them. Two accounts with no usage data at all
// both land on the same 999 ("usage unknown") sort key, a real, not
// contrived, tie. Uses single-digit numbers ("3", "9") deliberately, so
// lexicographic and numeric order agree; the tiebreaker is a *string*
// comparison (matching Python's tuple sort on the number string exactly,
// including its "10" < "9" lexicographic quirk, already covered
// separately by usage_store.go's DueCandidate tests), not the ordering
// question this test is isolating.
func TestCandidatesViewTiesBreakByAccountNumber(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{Accounts: []cswap.AccountSnapshot{
		{Number: "9", Email: "b@example.com", Switchable: true},
		{Number: "3", Email: "a@example.com", Switchable: true},
	}}}
	s := &autoScreen{app: app}
	view := s.candidatesView()
	iA := strings.Index(view, "a@example.com")
	iB := strings.Index(view, "b@example.com")
	if iA == -1 || iB == -1 {
		t.Fatalf("expected both accounts rendered, got %q", view)
	}
	if iB < iA {
		t.Fatalf("expected account 3 (lexicographically before 9) to render first on a full tie, got %q", view)
	}
}

// TestEventColorMatchesPythonSeverityMap is the regression test for a real
// gap: Python's event_text colors each log line by event kind
// (_EVENT_STYLES / _QUIET_KINDS), which the Go port's autoScreen originally
// dropped entirely (every line appended as a plain, unstyled string).
func TestEventColorMatchesPythonSeverityMap(t *testing.T) {
	cases := []struct {
		kind string
		want lipgloss.Color
	}{
		{"switch", Accent},
		{"error", SevWarn},
		{"account-quarantined", SevWarn},
		{"all-exhausted", SevCrit},
		{"poll", Muted},
		{"no-switch", Muted},
		{"sleep", Muted},
		{"account-unquarantined", Muted},
		{"config-warning", Foreground}, // no entry in either map: plain foreground
	}
	for _, c := range cases {
		if got := eventColor(c.kind); got != c.want {
			t.Errorf("eventColor(%q) = %v, want %v", c.kind, got, c.want)
		}
	}
}

func TestAutoScreenStartsDryRun(t *testing.T) {
	app := setupAppForAutoScreen(t)
	s := newAutoScreen(app)
	s.Init()
	if !strings.Contains(s.View(), "DRY-RUN") {
		t.Fatalf("initial view = %q, want DRY-RUN badge", s.View())
	}
}

// TestAutoScreenFooterShowsVisibleBindings is the regression test for a
// real gap: Python's AutoScreen composes a Footer() widget listing its
// visible keybindings, which the Go port had no equivalent of at all.
func TestAutoScreenFooterShowsVisibleBindings(t *testing.T) {
	app := setupAppForAutoScreen(t)
	s := newAutoScreen(app)
	s.Init()
	view := s.View()
	for _, want := range []string{"go live/dry-run", "threshold", "back"} {
		if !strings.Contains(view, want) {
			t.Errorf("auto screen footer missing %q, got:\n%s", want, view)
		}
	}
}

func TestAutoScreenTAdjustModeShowsHint(t *testing.T) {
	app := setupAppForAutoScreen(t)
	s := newAutoScreen(app)
	s.Init()
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if !strings.Contains(s.View(), "adjust") {
		t.Fatalf("after 't', view = %q, want the adjust-mode hint", s.View())
	}
}

func TestAutoScreenArrowStepsThresholdWithinBounds(t *testing.T) {
	app := setupAppForAutoScreen(t)
	as := newAutoScreen(app).(*autoScreen)
	as.Init()
	as.startAdjust()
	before := as.settings.Threshold
	as.stepThreshold(1)
	if as.settings.Threshold != before+1 {
		t.Fatalf("threshold after +1 step = %v, want %v", as.settings.Threshold, before+1)
	}
}

func TestAutoScreenEscAfterAdjustEndsAdjustNotScreen(t *testing.T) {
	app := setupAppForAutoScreen(t)
	s := newAutoScreen(app)
	s.Init()
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	s, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd != nil {
		t.Fatal("esc while adjusting should end adjust mode, not pop the screen")
	}
	if strings.Contains(s.View(), "← → adjust") {
		t.Fatal("adjust hint should be gone after esc ends adjust mode")
	}
}

func TestAutoScreenOnUnmountRestoresConfiguredThreshold(t *testing.T) {
	app := setupAppForAutoScreen(t)
	as := newAutoScreen(app).(*autoScreen)
	as.Init()
	configured := as.settings.Threshold
	as.stepThreshold(1) // deviate for the session
	as.onUnmount()
	if app.ThresholdPct == nil || *app.ThresholdPct != configured {
		t.Fatalf("ThresholdPct after unmount = %v, want the restored configured value %v", app.ThresholdPct, configured)
	}
}

// TestAutoScreenLRequiresConfirmationBeforeGoingLive is the guard for
// Finding 1: pressing 'l' in dry-run must NOT silently start switching
// accounts. It must push a ConfirmModal and stay dry-run until the user
// confirms; only then does the engine go live — and it must thread
// restartEngine's cmd through App.pendingCmd (dropping it would flip the
// badge to LIVE while never launching the live engine's RunLoop). Driven
// through App.Update so the whole push/confirm/pop path is exercised, the
// same way it runs in the real app.
func TestAutoScreenLRequiresConfirmationBeforeGoingLive(t *testing.T) {
	app := setupAppForAutoScreen(t)
	as := newAutoScreen(app).(*autoScreen)
	as.Init()
	app.PushScreen(as)

	// Press 'l' while dry-run: no live switch yet, just a push-screen cmd.
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if !as.dryRun {
		t.Fatal("pressing l in dry-run must not flip to live before confirmation")
	}
	if cmd == nil {
		t.Fatal("expected l to return a command that pushes the confirm modal")
	}

	// Run that cmd and route the pushScreenMsg back into App so the modal
	// actually lands on top of the stack.
	msg := cmd()
	push, ok := msg.(pushScreenMsg)
	if !ok {
		t.Fatalf("expected a pushScreenMsg from l, got %T", msg)
	}
	adapter, ok := push.s.(screenAdapter)
	if !ok {
		t.Fatalf("pushed screen is not a screenAdapter, got %T", push.s)
	}
	if _, ok := adapter.Model.(*ConfirmModal); !ok {
		t.Fatalf("pushed screen is not a ConfirmModal, got %T", adapter.Model)
	}
	app.Update(push)
	if as.dryRun != true {
		t.Fatal("must still be dry-run while the confirm modal is up")
	}
	if app.pendingCmd != nil {
		t.Fatal("no engine cmd should be pending before the user confirms")
	}

	// Confirm with 'y': the modal's onDismiss must go live and hand the
	// live-engine launch cmd to App via pendingCmd.
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if as.dryRun {
		t.Fatal("after confirming, the engine must go live (dryRun=false)")
	}
	if app.pendingCmd == nil {
		t.Fatal("going live must thread restartEngine's cmd via pendingCmd so " +
			"the live engine's RunLoop actually starts; it was dropped instead")
	}
	if cmd == nil {
		t.Fatal("confirming should return a pop-screen command")
	}

	as.onUnmount()
}

// TestAutoScreenCancelKeepsDryRun proves the other half of Finding 1: if
// the user cancels the confirm modal, the engine stays dry-run and nothing
// is queued to go live.
func TestAutoScreenCancelKeepsDryRun(t *testing.T) {
	app := setupAppForAutoScreen(t)
	as := newAutoScreen(app).(*autoScreen)
	as.Init()
	app.PushScreen(as)

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	app.Update(cmd().(pushScreenMsg))
	// Cancel with 'n'.
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if !as.dryRun {
		t.Fatal("cancelling the confirm modal must leave the engine in dry-run")
	}
	if app.pendingCmd != nil {
		t.Fatal("cancelling must not queue a go-live cmd")
	}
	as.onUnmount()
}

// TestAutoScreenEngineBridgeRestartRaceFree is the regression guard for
// Finding 2, modeled on autoswitch_pollinputs_race_test.go: a REAL engine
// RunLoop() runs concurrently with the code that reassigns the s.events
// bridge, checked under `-race`.
//
// The race the merged fix targets lives in startEngine's OnEvent closure.
// Stop() only signals RunLoop to exit (it does not join), so a running
// engine's goroutine can still Emit -> OnEvent after startEngine has
// reassigned s.events to a fresh channel. The pre-fix closure read the
// s.events field live at send time, which both data-raced that reassignment
// and misrouted the late event onto the new engine's channel; the fix
// captures the birth channel in a local so each engine always sends to the
// channel it was created with.
//
// To exercise exactly that field, one long-lived engine's RunLoop is
// launched and its channel is drained (so it never blocks on a full buffer,
// keeps emitting for the whole window, and Stops cleanly at the end), while
// this goroutine hammers startEngine — the body restartEngine calls, and the
// precise site that writes s.events. Driving startEngine rather than the
// full restartEngine keeps exactly one running engine to join deterministically
// (restartEngine would Stop this emitter on its first call, and relaunching a
// fresh engine every iteration risks a Stop landing before RunLoop recreates
// its stop channel, leaking spinning goroutines) while still reproducing the
// exact s.events read/write overlap the fix guards. Under the pre-fix code
// this fails under -race; under the fix it is clean.
func TestAutoScreenEngineBridgeRestartRaceFree(t *testing.T) {
	app := setupAppForAutoScreen(t)
	as := newAutoScreen(app).(*autoScreen)
	as.Init()

	// Tick rapidly so the running engine's OnEvent closure fires continuously
	// throughout the race window (the default 60s interval would tick once).
	as.settings.IntervalSeconds = 0.001

	// One long-lived engine whose OnEvent closure is exactly the bridge the
	// fix touched. With no managed account, each tick emits a couple of events
	// and returns fast, so it keeps calling the closure.
	as.startEngine(true)
	engine := as.engine
	ch := as.events

	emitterDone := make(chan struct{})
	go func() { engine.RunLoop(); close(emitterDone) }()

	drainStop := make(chan struct{})
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			select {
			case <-drainStop:
				return
			case <-ch:
			}
		}
	}()

	// Concurrently reassign s.events by driving the exact bridge write path as
	// fast as possible for the whole window.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		as.startEngine(true)
	}

	// Deterministic teardown: stop the emitter while the drainer is still
	// running (so it can never block on a full channel), join it, then stop
	// the drainer. Finally unmount the screen.
	engine.Stop()
	select {
	case <-emitterDone:
	case <-time.After(2 * time.Second):
		t.Fatal("emitter RunLoop did not exit after Stop; test setup is broken")
	}
	close(drainStop)
	<-drainDone
	as.onUnmount()
}

// runBatch invokes a tea.Cmd the way the real bubbletea runtime dispatches
// it: tea.Batch's Cmd returns a tea.BatchMsg (a []tea.Cmd) rather than
// running its sub-commands itself, and the runtime fires each sub-command
// on its own goroutine with no ordering guarantee. readEventCmd blocks on
// a channel receive, so it must never be invoked synchronously in the test
// goroutine - only ever via a fresh goroutine, matching production.
func runBatch(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			go c()
		}
	}
}

// TestAutoScreenRestartClosesOldEventsChannel is the regression guard for
// finding 2 of the whole-branch review: restartEngine calls engine.Stop()
// (which only signals, never joins) and then startEngine swaps s.events
// for a brand new channel. Before the fix, nothing ever closed the OLD
// channel, so the readEventCmd goroutine armed for it (launched by the
// PREVIOUS startEngine's returned cmd, exactly as Init/restartEngine wire
// it up in production) stayed parked on `<-oldEvents` forever - a leaked
// goroutine on every live/dry-run toggle. The fix makes the old engine's
// own RunLoop goroutine close its channel right after RunLoop returns
// (which only happens once Stop() has actually taken effect), so that
// parked reader unblocks with ok=false instead of leaking.
func TestAutoScreenRestartClosesOldEventsChannel(t *testing.T) {
	app := setupAppForAutoScreen(t)
	as := newAutoScreen(app).(*autoScreen)
	as.Init()
	as.settings.IntervalSeconds = 0.001 // tick fast so RunLoop notices Stop promptly

	// Launch the first engine for real, exactly as Init()'s returned cmd
	// would if the bubbletea runtime dispatched it - this is what actually
	// starts oldEngine.RunLoop() in the background and arms a readEventCmd
	// reader on oldEvents. startEngine assigns s.events synchronously before
	// returning its cmd, so oldEvents must be captured only after calling it
	// (capturing it first would grab Init()'s earlier, never-launched
	// channel instead of this one).
	startCmd := as.startEngine(true)
	oldEvents := as.events
	runBatch(startCmd)

	// Wait for the old engine to actually complete its first tick before
	// restarting. RunLoop() recreates e.stopCh on entry (autoswitch_loop.go),
	// so calling Stop() before RunLoop has reached that line would close a
	// stop channel RunLoop then immediately discards and replaces - silently
	// losing the stop signal and hanging this test. That race is a property
	// of the underlying engine, not of the fix under test here, so this test
	// sidesteps it deliberately: every tick emits at least one PollEvent
	// (tickInner, unconditionally, even with zero managed accounts), so
	// draining one confirms RunLoop has already initialized its stop channel.
	select {
	case <-oldEvents:
	case <-time.After(2 * time.Second):
		t.Fatal("old engine never completed a first tick; test setup is broken")
	}

	// The live/dry-run toggle: Stop()s the old engine and swaps in a new
	// engine + channel, exactly as pressing 'l'/'esc' while live drives it.
	runBatch(as.restartEngine(true))

	// oldEvents must eventually be closed: drain past any events that were
	// already in flight before the restart (a fast interval can leave a
	// few queued), and require the terminal state to be a closed channel,
	// not just "empty for now".
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-oldEvents:
			if !ok {
				goto closed
			}
		case <-deadline:
			t.Fatal("old events channel was never closed after restartEngine; " +
				"the readEventCmd goroutine parked on it would leak forever")
		}
	}
closed:
	as.onUnmount()
}
