package cswap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPurgeNoConfirmationCancels(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	res, err := s.Purge(func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if !res.Cancelled {
		t.Fatal("expected Cancelled")
	}
	if !fileExists(s.BackupDir) {
		t.Fatal("expected backup dir to survive a cancelled purge")
	}
}

func TestPurgeRemovesBackupDirAndCredentialFiles(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	res, err := s.Purge(func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if res.Cancelled {
		t.Fatal("expected the purge to proceed")
	}
	if len(res.RemovedItems) == 0 {
		t.Fatal("expected at least the backup directory to be reported removed")
	}
	if fileExists(s.BackupDir) {
		t.Fatal("expected backup dir to be removed")
	}
}

func TestPurgeRefusesWithLiveSession(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	// LiveSessionsFor(profileDir) mirrors live_sessions_for: it calls
	// ListSessions(profileDir), which reads profileDir/sessions/*.json (the
	// same shape as the primary ~/.claude/sessions/{pid}.json this port
	// already reads for GetRunningInstances). A session profile directory
	// (what SessionDirFor builds for `cswap run`) is itself a full
	// CLAUDE_CONFIG_DIR, so Claude Code running against it writes its own
	// nested sessions/*.json, distinct from claude-swap's own
	// BackupDir/sessions/<profile>/ layer that Purge iterates below. Both
	// "sessions" segments are real and intentional, just two different
	// directory trees for two different purposes.
	profileDir := filepath.Join(s.BackupDir, "sessions", "1-a@example.com")
	profileSessionsDir := filepath.Join(profileDir, "sessions")
	if err := os.MkdirAll(profileSessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// os.Getpid() is genuinely alive for the duration of this test process,
	// so IsPIDAlive(pid) is true without needing to spawn a real subprocess.
	mustWrite(t, filepath.Join(profileSessionsDir, "session.json"),
		fmt.Sprintf(`{"pid": %d, "startedAt": 0}`, os.Getpid()))

	_, err := s.Purge(func(string) bool { return true })
	if err == nil || !errors.Is(err, ErrSession) {
		t.Fatalf("expected ErrSession, got %v", err)
	}
	if !fileExists(s.BackupDir) {
		t.Fatal("expected backup dir to survive a refused purge")
	}
}

func TestPurgeLiveSessionErrorFormatsPidsWithCommas(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	profileDir := filepath.Join(s.BackupDir, "sessions", "1-a@example.com")
	profileSessionsDir := filepath.Join(profileDir, "sessions")
	if err := os.MkdirAll(profileSessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Two distinct session files for the same profile, both reporting the
	// (genuinely alive) test process PID, so ListSessions returns a
	// two-element pids slice for this one profile and the comma-join path
	// is actually exercised.
	pid := os.Getpid()
	mustWrite(t, filepath.Join(profileSessionsDir, "a.json"), fmt.Sprintf(`{"pid": %d, "startedAt": 0}`, pid))
	mustWrite(t, filepath.Join(profileSessionsDir, "b.json"), fmt.Sprintf(`{"pid": %d, "startedAt": 0}`, pid))

	_, err := s.Purge(func(string) bool { return true })
	if err == nil || !errors.Is(err, ErrSession) {
		t.Fatalf("expected ErrSession, got %v", err)
	}
	want := fmt.Sprintf("PID %d, %d", pid, pid)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error to contain %q (comma-joined, not a Go slice literal), got: %v", want, err)
	}
	if strings.Contains(err.Error(), "[") {
		t.Fatalf("error should not contain a raw Go slice literal, got: %v", err)
	}
}
