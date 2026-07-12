package cswap

import (
	"testing"
	"time"
)

func setupTwoAccountsForPerform(t *testing.T) (*AutoSwitchEngine, *ClaudeAccountSwitcher) {
	t.Helper()
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccountCredentials("2", "b@example.com", `{"claudeAiOauth":{"accessToken":"y"}}`); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccountConfig("2", "b@example.com", `{"oauthAccount":{"emailAddress":"b@example.com"}}`); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Sequence = append(data.Sequence, 2)
	data.Accounts["2"] = AccountRecord{Email: "b@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	var events []AutoSwitchEvent
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(ev AutoSwitchEvent) { events = append(events, ev) }, false, "", nil)
	return e, s
}

func TestAccountRefFromNumStringBuildsRef(t *testing.T) {
	ref := accountRefFromNumString("2", "b@example.com")
	if ref == nil || ref.Number == nil || *ref.Number != 2 || ref.Email != "b@example.com" {
		t.Fatalf("got %#v", ref)
	}
}

func TestAccountRefFromNumStringInvalidNumberIsNil(t *testing.T) {
	if got := accountRefFromNumString("not-a-number", "x@example.com"); got != nil {
		t.Fatalf("got %#v", got)
	}
}

func TestPerformDryRunEmitsSimulatedSwitchAndDoesNotMutate(t *testing.T) {
	e, s := setupTwoAccountsForPerform(t)
	e.DryRun = true
	var events []AutoSwitchEvent
	e.OnEvent = func(ev AutoSwitchEvent) { events = append(events, ev) }

	outcome, err := e.perform("2", "b@example.com", "proactive")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != TickSwitched {
		t.Fatalf("got %v", outcome)
	}
	if len(events) != 1 {
		t.Fatalf("got %#v", events)
	}
	se, ok := events[0].(*SwitchEvent)
	if !ok || !se.dryRun {
		t.Fatalf("got %#v", events[0])
	}
	// Dry-run must not actually switch: account 1 stays active.
	current := s.CurrentAccountNumber()
	if current != "1" {
		t.Fatalf("expected account 1 still active, got %q", current)
	}
}

func TestPerformRealSwitchUpdatesStateAndEmits(t *testing.T) {
	e, s := setupTwoAccountsForPerform(t)
	var events []AutoSwitchEvent
	e.OnEvent = func(ev AutoSwitchEvent) { events = append(events, ev) }

	outcome, err := e.perform("2", "b@example.com", "proactive")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != TickSwitched {
		t.Fatalf("got %v", outcome)
	}
	if s.CurrentAccountNumber() != "2" {
		t.Fatalf("expected account 2 now active, got %q", s.CurrentAccountNumber())
	}
	state := e.readState()
	if state["lastSwitchTo"] != "2" {
		t.Fatalf("got %#v", state["lastSwitchTo"])
	}
	if _, ok := state["lastSwitchAt"].(float64); !ok {
		t.Fatalf("expected a numeric lastSwitchAt, got %#v", state["lastSwitchAt"])
	}
	found := false
	for _, ev := range events {
		if se, ok := ev.(*SwitchEvent); ok && !se.dryRun {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a real SwitchEvent, got %#v", events)
	}
}

func TestPerformSuccessEmitsAfterLockReleased(t *testing.T) {
	e, _ := setupTwoAccountsForPerform(t)
	var lockedDuringEmit bool
	e.OnEvent = func(ev AutoSwitchEvent) {
		if _, ok := ev.(*SwitchEvent); !ok {
			return
		}
		// If the lock is still held while this callback runs, acquiring
		// it again here would fail fast (a fresh FileLock on the same
		// path, well under the real 10s timeout via a short one) since
		// this port's flock-based lock is not reentrant within the same
		// process either. A successful acquire here proves the real
		// lock.Release() already ran before this event fired.
		lock := NewFileLock(e.stateLockPath(), 50*time.Millisecond)
		if err := lock.Lock(50 * time.Millisecond); err != nil {
			lockedDuringEmit = true
			return
		}
		lock.Release()
	}

	if _, err := e.perform("2", "b@example.com", "proactive"); err != nil {
		t.Fatal(err)
	}
	if lockedDuringEmit {
		t.Fatal("expected the state lock to already be released by the time the success SwitchEvent fires")
	}
}

func TestPerformRespectsCooldownForProactiveTrigger(t *testing.T) {
	e, _ := setupTwoAccountsForPerform(t)
	if _, err := e.mutateState(func(state map[string]any) {
		state["lastSwitchAt"] = float64(e.Clock().Unix())
	}); err != nil {
		t.Fatal(err)
	}
	var events []AutoSwitchEvent
	e.OnEvent = func(ev AutoSwitchEvent) { events = append(events, ev) }

	outcome, err := e.perform("2", "b@example.com", "proactive")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != TickNoAction {
		t.Fatalf("got %v", outcome)
	}
	ne, ok := events[0].(*NoSwitchEvent)
	if !ok || ne.reason != "cooldown" {
		t.Fatalf("got %#v", events)
	}
}

func TestPerformCooldownDoesNotApplyToAtLimitTrigger(t *testing.T) {
	e, _ := setupTwoAccountsForPerform(t)
	if _, err := e.mutateState(func(state map[string]any) {
		state["lastSwitchAt"] = float64(e.Clock().Unix())
	}); err != nil {
		t.Fatal(err)
	}
	outcome, err := e.perform("2", "b@example.com", "at-limit")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != TickSwitched {
		t.Fatalf("expected at-limit to bypass cooldown, got %v", outcome)
	}
}
