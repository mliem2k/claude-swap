package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mliem2k/claude-swap/internal/cswap"
)

// stubScreen is a minimal screen for exercising the stack in isolation.
type stubScreen struct{ label string }

func (s *stubScreen) Init() tea.Cmd                        { return nil }
func (s *stubScreen) Update(msg tea.Msg) (screen, tea.Cmd) { return s, nil }
func (s *stubScreen) View() string                         { return s.label }

// fakeSnapshot is a package-level fixture; cswap.AccountsSnapshotResult has
// exported fields and can be built directly.
var fakeSnapshot = cswap.AccountsSnapshotResult{TakenAt: 1}

func TestPushAndPopScreen(t *testing.T) {
	a := &App{}
	a.PushScreen(&stubScreen{label: "one"})
	a.PushScreen(&stubScreen{label: "two"})
	if got := a.View(); got != "two" {
		t.Fatalf("View() = %q, want the top of the stack", got)
	}
	a.PopScreen()
	if got := a.View(); got != "one" {
		t.Fatalf("View() after pop = %q, want the previous screen", got)
	}
}

func TestApplySnapshotSetsField(t *testing.T) {
	a := &App{}
	snap := &fakeSnapshot
	a.applySnapshot(snapshotMsg{snap: snap})
	if a.Snapshot != snap {
		t.Fatal("applySnapshot did not set a.Snapshot")
	}
	if a.refreshing {
		t.Fatal("applySnapshot did not clear the refreshing flag")
	}
}

func TestBusyGateBlocksSecondAction(t *testing.T) {
	a := &App{}
	a.busy = true
	started := a.startAction()
	if started {
		t.Fatal("startAction should refuse to start while busy")
	}
}

// TestContentWidthDefaultsBeforeAnyResize covers the fallback every
// render path relied on exclusively before terminal-resize
// responsiveness existed (a hardcoded 80/78 at each call site).
func TestContentWidthDefaultsBeforeAnyResize(t *testing.T) {
	a := &App{}
	if got := a.ContentWidth(); got != defaultContentWidth {
		t.Fatalf("ContentWidth() before any resize = %d, want the default %d", got, defaultContentWidth)
	}
}

// TestWindowSizeMsgUpdatesContentWidth is the regression test for a real
// gap: App.Update had no tea.WindowSizeMsg case at all, and every render
// path hardcoded its width, so bars/cards never adapted to a narrow or
// wide terminal, unlike Python's account_card_text/AccountsPanel, which
// read self.size.width live from Textual's layout on every render.
func TestWindowSizeMsgUpdatesContentWidth(t *testing.T) {
	a := &App{}
	a.PushScreen(&stubScreen{label: "x"})
	if _, cmd := a.Update(tea.WindowSizeMsg{Width: 120, Height: 40}); cmd != nil {
		t.Fatalf("expected no command from a resize, got %v", cmd)
	}
	if got := a.ContentWidth(); got != 120 {
		t.Fatalf("ContentWidth() after a 120-wide resize = %d, want 120", got)
	}
}

// TestDashboardPanelViewRespectsResize confirms the resize actually
// reaches rendered output through a real screen, not just the App field:
// a narrow terminal must shrink AccountsPanelText's rendered width
// (visible via a shorter track/bar line) compared to the default.
func TestDashboardPanelViewRespectsResize(t *testing.T) {
	acc := cswap.AccountSnapshot{
		Number: "1", Email: "a@example.com", IsActive: true, Switchable: true,
		Usage: cswap.UsageEntry{LastGood: map[string]any{"five_hour": map[string]any{"pct": 10.0}}},
	}
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{Accounts: []cswap.AccountSnapshot{acc}}}
	s := newDashboardScreen(app).(*dashboardScreen)

	wide := s.panelView()
	app.width = 40
	narrow := s.panelView()
	if len(narrow) >= len(wide) {
		t.Fatalf("expected a narrower render at width=40 than the default width=%d, got narrow len=%d wide len=%d", defaultContentWidth, len(narrow), len(wide))
	}
}

func intPtr(n int) *int { return &n }

func TestSlotOccupant(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{Accounts: []cswap.AccountSnapshot{
		{Number: "2", Email: "occupant@example.com"},
	}}}
	if got := app.SlotOccupant(intPtr(2)); got != "occupant@example.com" {
		t.Fatalf("SlotOccupant(2) = %q, want the occupying account's email", got)
	}
	if got := app.SlotOccupant(intPtr(3)); got != "" {
		t.Fatalf("SlotOccupant(3) (empty slot) = %q, want empty", got)
	}
	if got := app.SlotOccupant(nil); got != "" {
		t.Fatalf("SlotOccupant(nil) = %q, want empty", got)
	}
}

// addTokenModalFromCmd runs a pushScreenMsg-producing tea.Cmd and unwraps
// the concrete *AddTokenModal it carries, matching how App.Update's own
// pushScreenMsg case would receive it.
func addTokenModalFromCmd(t *testing.T, cmd tea.Cmd) *AddTokenModal {
	t.Helper()
	msg, ok := cmd().(pushScreenMsg)
	if !ok {
		t.Fatalf("expected a pushScreenMsg, got %T", cmd())
	}
	adapter, ok := msg.s.(screenAdapter)
	if !ok {
		t.Fatalf("expected a screenAdapter, got %T", msg.s)
	}
	modal, ok := adapter.Model.(*AddTokenModal)
	if !ok {
		t.Fatalf("expected *AddTokenModal, got %T", adapter.Model)
	}
	return modal
}

// TestActionAddTokenConfirmsBeforeOverwritingOccupiedSlot is the
// regression test for a real gap: Python's _on_token_form shows a
// ConfirmModal ("Slot X is occupied by email. Overwrite?") before running
// add_account_from_token when the target slot already holds another
// account; an earlier Go draft ran AddAccountFromToken unconditionally.
func TestActionAddTokenConfirmsBeforeOverwritingOccupiedSlot(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{Accounts: []cswap.AccountSnapshot{
		{Number: "2", Email: "occupant@example.com"},
	}}}
	modal := addTokenModalFromCmd(t, app.ActionAddToken())

	modal.tokenInput.SetValue("sk-ant-oat-example")
	modal.slotInput.SetValue("2")
	modal.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if app.pendingCmd == nil {
		t.Fatal("expected the AddTokenModal's onDismiss to have set a.pendingCmd")
	}
	next := app.pendingCmd()
	pushMsg, ok := next.(pushScreenMsg)
	if !ok {
		t.Fatalf("expected the occupied-slot case to push a confirm modal, got %T", next)
	}
	adapter, ok := pushMsg.s.(screenAdapter)
	if !ok {
		t.Fatalf("expected a screenAdapter, got %T", pushMsg.s)
	}
	if _, ok := adapter.Model.(*ConfirmModal); !ok {
		t.Fatalf("expected *ConfirmModal, got %T", adapter.Model)
	}
}

// TestActionAddTokenRunsDirectlyWhenSlotIsFree confirms the happy path
// (an empty or unspecified slot) is unaffected: no confirm modal, the add
// action's own command is set directly.
func TestActionAddTokenRunsDirectlyWhenSlotIsFree(t *testing.T) {
	app := &App{Snapshot: &cswap.AccountsSnapshotResult{}}
	modal := addTokenModalFromCmd(t, app.ActionAddToken())

	modal.tokenInput.SetValue("sk-ant-oat-example")
	modal.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if app.pendingCmd == nil {
		t.Fatal("expected the AddTokenModal's onDismiss to have set a.pendingCmd")
	}
	if !app.busy {
		t.Fatal("expected runActionCmd to have set the busy flag for the direct add action")
	}
}

// TestNotifySetsNoticeAndClearsOnTimeout checks Notify's own bookkeeping
// (the notice it set, and the generation the returned command's message
// carries) without actually invoking the returned tea.Cmd: Bubble Tea's
// real tea.Tick genuinely blocks on a timer for the full duration before
// returning (commands.go), so calling it here would make the test sleep
// for real; a Notify caller only cares that the message it eventually
// produces carries the right generation, not that this test waits for it.
func TestNotifySetsNoticeAndClearsOnTimeout(t *testing.T) {
	a := &App{}
	a.Notify("hello", "Title", "warning", time.Hour)
	if a.notice == nil || a.notice.message != "hello" || a.notice.title != "Title" || a.notice.severity != "warning" {
		t.Fatalf("got notice=%+v", a.notice)
	}
	if _, cmd2 := a.Update(clearNoticeMsg{gen: a.noticeGen}); cmd2 != nil {
		t.Fatalf("expected no follow-up command from clearing a notice, got %v", cmd2)
	}
	if a.notice != nil {
		t.Fatal("expected the notice to be cleared")
	}
}

// TestClearNoticeMsgIgnoresStaleGeneration confirms a still-pending timer
// for a replaced notice can never clear the newer one. Constructs the
// clearNoticeMsg directly (the stale notice's own generation) rather than
// invoking Notify's real, genuinely-blocking tea.Tick-based command.
func TestClearNoticeMsgIgnoresStaleGeneration(t *testing.T) {
	a := &App{}
	a.Notify("first", "", "", time.Hour)
	staleGen := a.noticeGen
	a.Notify("second", "", "", time.Hour) // replaces the notice and bumps noticeGen
	a.Update(clearNoticeMsg{gen: staleGen})
	if a.notice == nil || a.notice.message != "second" {
		t.Fatalf("a stale clear must not remove a newer notice, got %+v", a.notice)
	}
}

// TestRunActionCmdNotifiesWhenBusy is the regression test for a real
// gap: Python's _start_action notifies "Another action is still
// running" (severity warning) when busy; an earlier Go draft silently
// returned nil.
func TestRunActionCmdNotifiesWhenBusy(t *testing.T) {
	a := &App{busy: true}
	cmd := a.runActionCmd("label", func() ActionResult { return ActionResult{OK: true} }, false)
	if cmd == nil {
		t.Fatal("expected a notify command when busy, got nil")
	}
	if a.notice == nil || a.notice.severity != "warning" {
		t.Fatalf("got notice=%+v", a.notice)
	}
}

// TestActionDoneSwitchSuccessNotifiesInsteadOfModal is the regression
// test for a real gap: Python's _action_done detects a switch result via
// `"switched" in payload` and always shows a toast ("Switched to X"),
// never the generic OutputModal every other action gets.
func TestActionDoneSwitchSuccessNotifiesInsteadOfModal(t *testing.T) {
	a := &App{}
	n := 2
	msg := actionDoneMsg{label: "Switch", result: ActionResult{
		OK: true, Switch: &cswap.SwitchResult{Switched: true, To: &cswap.AccountRef{Number: &n, Email: "b@example.com"}},
	}}
	// a.notice is set synchronously by Notify before it returns the
	// tea.Tick-based clear command, so there's no need to invoke that
	// command here (doing so would genuinely block for its real timeout,
	// tea.Tick's actual behavior, not a mock).
	cmd := a.actionDone(msg)
	if cmd == nil {
		t.Fatal("expected a notify command")
	}
	if a.notice == nil || a.notice.message != "Switched to b@example.com" || a.notice.title != "Switch" {
		t.Fatalf("got notice=%+v", a.notice)
	}
}

// TestActionDoneSwitchNoopNotifiesWarningWithReason covers the
// switched=false branch: a warning-severity toast naming the reason
// (e.g. "already-active"), not a modal.
func TestActionDoneSwitchNoopNotifiesWarningWithReason(t *testing.T) {
	a := &App{}
	msg := actionDoneMsg{label: "Switch", result: ActionResult{
		OK: true, Switch: &cswap.SwitchResult{Switched: false, Reason: "already-active"},
	}}
	cmd := a.actionDone(msg)
	if cmd == nil {
		t.Fatal("expected a notify command")
	}
	if a.notice == nil || a.notice.message != "already-active" || a.notice.title != "No switch" || a.notice.severity != "warning" {
		t.Fatalf("got notice=%+v", a.notice)
	}
}

// TestActionDoneNonSwitchNotifiesFirstLine is the regression test for
// remove/add-account success feedback: Python's _action_done falls
// through to `elif result.first_line: self.notify(result.first_line)`
// for a non-switch action with showOutput=false; an earlier Go draft
// returned nil unconditionally, silently dropping this feedback.
func TestActionDoneNonSwitchNotifiesFirstLine(t *testing.T) {
	a := &App{}
	msg := actionDoneMsg{label: "Remove account 1", result: ActionResult{OK: true, Message: "Removed Account-1 (a@example.com)"}, showOutput: false}
	cmd := a.actionDone(msg)
	if cmd == nil {
		t.Fatal("expected a notify command")
	}
	if a.notice == nil || a.notice.message != "Removed Account-1 (a@example.com)" {
		t.Fatalf("got notice=%+v", a.notice)
	}
}

// TestActionDoneShowOutputEmptyMessageFallsThroughToNotify mirrors
// Python's if/elif shape exactly: showOutput=true with an empty message
// must NOT push a blank OutputModal, it must fall through to the
// first-line notify branch like showOutput=false would.
func TestActionDoneShowOutputEmptyMessageFallsThroughToNotify(t *testing.T) {
	a := &App{}
	msg := actionDoneMsg{label: "Add account", result: ActionResult{OK: true, Message: ""}, showOutput: true}
	if cmd := a.actionDone(msg); cmd != nil {
		t.Fatalf("expected no command (no modal, no notify) for a genuinely empty message, got %v", cmd)
	}
	if a.notice != nil {
		t.Fatalf("expected no notice for a genuinely empty message, got %+v", a.notice)
	}
}
