package cswap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsPIDAliveSelf(t *testing.T) {
	if !IsPIDAlive(os.Getpid()) {
		t.Fatal("the current process should report as alive")
	}
}

func TestIsPIDAliveRejectsNonPositive(t *testing.T) {
	if IsPIDAlive(0) || IsPIDAlive(1) {
		t.Fatal("pid <= 1 should never report alive")
	}
}

func TestIsPIDAliveFalseForImpossiblePID(t *testing.T) {
	if IsPIDAlive(999999999) {
		t.Fatal("an implausible pid should not be alive")
	}
}

func TestListSessionsSkipsDeadPIDs(t *testing.T) {
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSessionFile(t, sessionsDir, "1.json", map[string]any{
		"pid": 999999999, "sessionId": "dead", "kind": "interactive",
	})
	writeSessionFile(t, sessionsDir, "2.json", map[string]any{
		"pid": os.Getpid(), "sessionId": "alive", "kind": "interactive",
	})

	sessions := ListSessions(dir)
	if len(sessions) != 1 || sessions[0].SessionID != "alive" {
		t.Fatalf("expected only the alive session, got %#v", sessions)
	}
}

func TestListSessionsEmptyWhenDirMissing(t *testing.T) {
	if got := ListSessions(filepath.Join(t.TempDir(), "nope")); len(got) != 0 {
		t.Fatalf("expected empty, got %#v", got)
	}
}

func TestListIdeInstancesSkipsDeadPIDs(t *testing.T) {
	dir := t.TempDir()
	ideDir := filepath.Join(dir, "ide")
	if err := os.MkdirAll(ideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSessionFile(t, ideDir, "5555.lock", map[string]any{
		"pid": 999999999, "ideName": "Dead Editor",
	})
	writeSessionFile(t, ideDir, "6666.lock", map[string]any{
		"pid": os.Getpid(), "ideName": "Live Editor",
		"workspaceFolders": []string{"/tmp/proj"},
	})

	got := ListIdeInstances(dir)
	if len(got) != 1 || got[0].Port != 6666 || got[0].IdeName != "Live Editor" {
		t.Fatalf("expected only the live instance, got %#v", got)
	}
}

func TestGetRunningInstances(t *testing.T) {
	dir := t.TempDir()
	sessions, ides := GetRunningInstances(dir)
	if len(sessions) != 0 || len(ides) != 0 {
		t.Fatalf("expected empty results for a fresh dir, got %#v %#v", sessions, ides)
	}
}

func writeSessionFile(t *testing.T, dir, name string, data map[string]any) {
	t.Helper()
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
		t.Fatal(err)
	}
}
