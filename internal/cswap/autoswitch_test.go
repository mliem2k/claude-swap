package cswap

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestAutoSwitchEngine(t *testing.T) (*AutoSwitchEngine, *ClaudeAccountSwitcher) {
	t.Helper()
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	var events []AutoSwitchEvent
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(ev AutoSwitchEvent) {
		events = append(events, ev)
	}, false, "", nil)
	return e, s
}

func TestNewAutoSwitchEngineDefaultsStatePathUnderBackupDir(t *testing.T) {
	e, s := newTestAutoSwitchEngine(t)
	want := filepath.Join(s.BackupDir, StateFilename)
	if e.StatePath != want {
		t.Fatalf("got %q want %q", e.StatePath, want)
	}
}

func TestNewAutoSwitchEngineHonorsExplicitStatePath(t *testing.T) {
	s := newTestSwitcher(t)
	custom := filepath.Join(t.TempDir(), "custom_state.json")
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(AutoSwitchEvent) {}, false, custom, nil)
	if e.StatePath != custom {
		t.Fatalf("got %q want %q", e.StatePath, custom)
	}
}

func TestNewAutoSwitchEngineDefaultsClockToTimeNow(t *testing.T) {
	e, _ := newTestAutoSwitchEngine(t)
	before := time.Now()
	got := e.Clock()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("clock() = %v, expected between %v and %v", got, before, after)
	}
}

func TestNewAutoSwitchEngineParsesModelNames(t *testing.T) {
	s := newTestSwitcher(t)
	model := "Opus,Sonnet"
	settings := DefaultAutoSwitchSettings()
	settings.Model = &model
	e := NewAutoSwitchEngine(s, settings, func(AutoSwitchEvent) {}, false, "", nil)
	if len(e.models) != 2 || e.models[0] != "Opus" || e.models[1] != "Sonnet" {
		t.Fatalf("got %#v", e.models)
	}
}

func TestEmitCallsOnEvent(t *testing.T) {
	s := newTestSwitcher(t)
	var got AutoSwitchEvent
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(ev AutoSwitchEvent) {
		got = ev
	}, false, "", nil)
	want := NewErrorEvent("boom", true)
	e.Emit(want)
	if got != want {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestReadStateMissingFileIsEmpty(t *testing.T) {
	e, _ := newTestAutoSwitchEngine(t)
	got := e.readState()
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestReadStateInvalidJSONIsEmpty(t *testing.T) {
	e, _ := newTestAutoSwitchEngine(t)
	if err := os.MkdirAll(filepath.Dir(e.StatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.StatePath, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := e.readState()
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestReadStateNonObjectJSONIsEmpty(t *testing.T) {
	e, _ := newTestAutoSwitchEngine(t)
	if err := os.MkdirAll(filepath.Dir(e.StatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.StatePath, []byte("[1,2,3]"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := e.readState()
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestMutateStateRoundTripsAndStampsSchemaVersion(t *testing.T) {
	e, _ := newTestAutoSwitchEngine(t)
	got, err := e.mutateState(func(state map[string]any) {
		state["quarantine"] = map[string]any{"1": "x"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["schemaVersion"] != float64(StateSchemaVersion) && got["schemaVersion"] != StateSchemaVersion {
		t.Fatalf("got %#v", got["schemaVersion"])
	}
	reread := e.readState()
	if reread["schemaVersion"] != float64(StateSchemaVersion) {
		t.Fatalf("got %#v", reread["schemaVersion"])
	}
	q, ok := reread["quarantine"].(map[string]any)
	if !ok || q["1"] != "x" {
		t.Fatalf("got %#v", reread["quarantine"])
	}
}

func TestMutateStatePreservesExistingKeys(t *testing.T) {
	e, _ := newTestAutoSwitchEngine(t)
	if _, err := e.mutateState(func(state map[string]any) {
		state["a"] = 1.0
	}); err != nil {
		t.Fatal(err)
	}
	got, err := e.mutateState(func(state map[string]any) {
		state["b"] = 2.0
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != 1.0 || got["b"] != 2.0 {
		t.Fatalf("got %#v", got)
	}
}
