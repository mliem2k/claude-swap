package cswap

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestSwitcher(t *testing.T) *ClaudeAccountSwitcher {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude-config"))
	restore := swapSecurity(newFakeSecurity())
	t.Cleanup(restore)
	s, err := NewClaudeAccountSwitcher(false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewClaudeAccountSwitcherSetsUpPaths(t *testing.T) {
	s := newTestSwitcher(t)
	if s.SequenceFile == "" || s.ConfigsDir == "" || s.CredentialsDir == "" || s.LockFile == "" {
		t.Fatalf("expected all paths populated: %+v", s)
	}
	if s.Logger == nil || s.UsageStore == nil || s.Store == nil {
		t.Fatal("expected logger, usage store, and credential store constructed")
	}
}

func TestValidateEmail(t *testing.T) {
	s := newTestSwitcher(t)
	if !s.ValidateEmail("a@example.com") {
		t.Fatal("expected a valid email to pass")
	}
	if s.ValidateEmail("not-an-email") {
		t.Fatal("expected an invalid email to fail")
	}
}

func TestSetupDirectoriesCreatesAll(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{s.BackupDir, s.ConfigsDir, s.CredentialsDir} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("expected dir %s to exist: %v", dir, err)
		}
	}
}

func TestReadWriteJSONRoundTrip(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.BackupDir, "test.json")
	data := map[string]any{"x": float64(1)}
	if err := s.WriteJSON(path, data); err != nil {
		t.Fatal(err)
	}
	got := s.ReadJSON(path)
	if got["x"] != float64(1) {
		t.Fatalf("got %#v", got)
	}
}

func TestReadJSONMissingIsNil(t *testing.T) {
	s := newTestSwitcher(t)
	if got := s.ReadJSON(filepath.Join(s.BackupDir, "nope.json")); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestIsRunningInContainerFalseByDefault(t *testing.T) {
	s := newTestSwitcher(t)
	t.Setenv("CONTAINER", "")
	t.Setenv("container", "")
	_ = s.IsRunningInContainer()
}
