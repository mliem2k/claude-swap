package cswap

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withExecClaudeFn(t *testing.T, fn func(claudeBin string, argv []string, env []string) error) {
	t.Helper()
	original := execClaudeFn
	execClaudeFn = fn
	t.Cleanup(func() { execClaudeFn = original })
}

// unsetEnv removes key from the environment for the rest of the test,
// restoring whatever value it had afterward. newTestSwitcher always sets
// CLAUDE_CONFIG_DIR (so the switcher's own backup paths resolve inside the
// test's temp HOME rather than a real one); Run's same-account fast path is
// specifically the "no CLAUDE_CONFIG_DIR preset" case, so a test exercising
// it needs to clear that back out. t.Setenv can't express "unset" (setting
// "" still leaves the key present with an empty value), hence the direct
// os.Unsetenv here.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	original, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(key, original)
		} else {
			os.Unsetenv(key)
		}
	})
}

func withAuthStatusRunnerAlwaysValid(t *testing.T, email, orgUUID string) {
	t.Helper()
	withAuthStatusRunner(t, func(claudeBin string, env []string, timeout time.Duration) (string, int, error) {
		status := map[string]any{"loggedIn": true, "authMethod": "claude.ai", "email": email}
		if orgUUID != "" {
			status["orgId"] = orgUUID
		}
		b, _ := json.Marshal(status)
		return string(b), 0, nil
	})
}

func setupAccountForRun(t *testing.T) (*ClaudeAccountSwitcher, string, string) {
	t.Helper()
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	creds := `{"claudeAiOauth":{"accessToken":"x","refreshToken":""}}`
	config := `{"oauthAccount":{"emailAddress":"a@example.com"}}`
	if err := s.WriteAccountCredentials("1", "a@example.com", creds); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccountConfig("1", "a@example.com", config); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Sequence = []int{1}
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	return s, "1", "a@example.com"
}

func TestSessionManagerRunNoClaudeOnPathIsSessionError(t *testing.T) {
	s, num, email := setupAccountForRun(t)
	t.Setenv("PATH", t.TempDir()) // guaranteed to not contain "claude"
	m := NewSessionManager(s)
	err := m.Run(num, nil, true, false)
	if !errors.Is(err, ErrSession) {
		t.Fatalf("got %v", err)
	}
	_ = email
}

func mustFakeClaudeOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestSessionManagerRunAPIKeyAccountIsSessionError(t *testing.T) {
	mustFakeClaudeOnPath(t)
	s, num, _ := setupAccountForRun(t)
	data := s.GetSequenceData()
	rec := data.Accounts[num]
	rec.Kind = "api_key"
	data.Accounts[num] = rec
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	m := NewSessionManager(s)
	err := m.Run(num, nil, true, false)
	if !errors.Is(err, ErrSession) {
		t.Fatalf("got %v", err)
	}
}

func TestSessionManagerRunShareHistoryOnWindowsIsSessionError(t *testing.T) {
	mustFakeClaudeOnPath(t)
	s, num, _ := setupAccountForRun(t)
	s.Platform = PlatformWindows
	m := NewSessionManager(s)
	err := m.Run(num, nil, true, true)
	if !errors.Is(err, ErrSession) {
		t.Fatalf("got %v", err)
	}
}

func TestSessionManagerRunSameAccountFastPathSkipsBootstrap(t *testing.T) {
	mustFakeClaudeOnPath(t)
	s, num, email := setupAccountForRun(t)
	// The fast path under test only triggers when CLAUDE_CONFIG_DIR is not
	// preset; newTestSwitcher sets it for its own path isolation, so clear
	// it here. The "preset disables the fast path" case is covered by the
	// other Run tests below, which all run with it left set.
	unsetEnv(t, "CLAUDE_CONFIG_DIR")
	// Make this account the current live login.
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"`+email+`"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"live"}}`)

	var capturedEnv []string
	withExecClaudeFn(t, func(claudeBin string, argv []string, env []string) error {
		capturedEnv = env
		return nil
	})

	m := NewSessionManager(s)
	if err := m.Run(num, nil, true, false); err != nil {
		t.Fatal(err)
	}

	// Fast path passes the environment through unmodified: CLAUDE_CONFIG_DIR
	// must NOT be forced to the session profile.
	for _, kv := range capturedEnv {
		if len(kv) >= len("CLAUDE_CONFIG_DIR=") && kv[:len("CLAUDE_CONFIG_DIR=")] == "CLAUDE_CONFIG_DIR=" {
			t.Fatalf("did not expect CLAUDE_CONFIG_DIR forced on the fast path, got %q", kv)
		}
	}
	if _, err := os.Stat(SessionDirFor(s.BackupDir, num, email)); !os.IsNotExist(err) {
		t.Fatal("expected no session profile bootstrapped on the same-account fast path")
	}
}

func TestSessionManagerRunBootstrapsAndExecsForDifferentAccount(t *testing.T) {
	mustFakeClaudeOnPath(t)
	s, num, email := setupAccountForRun(t)
	withAuthStatusRunnerAlwaysValid(t, email, "")

	var capturedArgv []string
	var capturedEnv []string
	withExecClaudeFn(t, func(claudeBin string, argv []string, env []string) error {
		capturedArgv = argv
		capturedEnv = env
		return nil
	})

	m := NewSessionManager(s)
	if err := m.Run(num, []string{"--resume"}, true, false); err != nil {
		t.Fatal(err)
	}

	if len(capturedArgv) < 2 || capturedArgv[len(capturedArgv)-1] != "--resume" {
		t.Fatalf("expected claude args forwarded, got %v", capturedArgv)
	}
	sessionDir := SessionDirFor(s.BackupDir, num, email)
	// newTestSwitcher already sets CLAUDE_CONFIG_DIR in the environment
	// (a different, stale value), so this also guards against the launch
	// env carrying two CLAUDE_CONFIG_DIR entries: on POSIX, syscall.Exec's
	// underlying execve has no dedup and honors the FIRST match, unlike
	// Go's exec.Cmd.Env, so a duplicate would silently launch claude
	// against the stale preset dir instead of the freshly bootstrapped
	// session profile.
	var matches []string
	for _, kv := range capturedEnv {
		if strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			matches = append(matches, kv)
		}
	}
	if len(matches) != 1 || matches[0] != "CLAUDE_CONFIG_DIR="+sessionDir {
		t.Fatalf("expected exactly one CLAUDE_CONFIG_DIR=%s in the launch env, got %v", sessionDir, matches)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, ".credentials.json")); err != nil {
		t.Fatal("expected the session profile to have been bootstrapped")
	}
}

func TestSessionManagerRunScrubsAuthOverrideEnvVars(t *testing.T) {
	mustFakeClaudeOnPath(t)
	s, num, email := setupAccountForRun(t)
	withAuthStatusRunnerAlwaysValid(t, email, "")
	t.Setenv("ANTHROPIC_API_KEY", "should-be-scrubbed")

	var capturedEnv []string
	withExecClaudeFn(t, func(claudeBin string, argv []string, env []string) error {
		capturedEnv = env
		return nil
	})

	m := NewSessionManager(s)
	if err := m.Run(num, nil, true, false); err != nil {
		t.Fatal(err)
	}
	for _, kv := range capturedEnv {
		if kv == "ANTHROPIC_API_KEY=should-be-scrubbed" {
			t.Fatal("expected ANTHROPIC_API_KEY scrubbed from the session launch env")
		}
	}
}

func TestSetupSessionReusesValidExistingProfile(t *testing.T) {
	s, num, email := setupAccountForRun(t)
	withAuthStatusRunnerAlwaysValid(t, email, "")
	sessionDir := SessionDirFor(s.BackupDir, num, email)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}

	m := NewSessionManager(s)
	gotDir, gotNum, gotEmail, err := m.SetupSession(num, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != sessionDir || gotNum != num || gotEmail != email {
		t.Fatalf("got %q %q %q", gotDir, gotNum, gotEmail)
	}
	// Reuse path: no credentials bootstrap file should have been written.
	if _, err := os.Stat(filepath.Join(sessionDir, ".credentials.json")); !os.IsNotExist(err) {
		t.Fatal("expected no bootstrap write on the reuse path")
	}
}

func TestSetupSessionBootstrapsWhenInvalid(t *testing.T) {
	s, num, email := setupAccountForRun(t)
	withAuthStatusRunner(t, func(claudeBin string, env []string, timeout time.Duration) (string, int, error) {
		return `{"loggedIn":false}`, 0, nil
	})

	m := NewSessionManager(s)
	sessionDir, _, _, err := m.SetupSession(num, true, false)
	// isSessionValid always reports false here (fake runner), so
	// SetupSession bootstraps, then re-checks validity and fails again,
	// surfacing the "failed validation" SessionError. That is the
	// expected outcome given a runner that never reports success.
	if err == nil {
		t.Fatal("expected an error: isSessionValid never succeeds with this fake runner")
	}
	if !errors.Is(err, ErrSession) {
		t.Fatalf("got %v", err)
	}
	// cleanupFailedSession removes the whole session directory on this path
	// (mirrors the Python suite's test_validation_failure_cleans_up), so
	// nothing bootstrap wrote survives the failure.
	if _, statErr := os.Stat(sessionDir); !os.IsNotExist(statErr) {
		t.Fatal("expected cleanupFailedSession to have removed the session directory")
	}
	_ = email
}

func TestSetupSessionAccountNotFoundPropagates(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	m := NewSessionManager(s)
	_, _, _, err := m.SetupSession("99", true, false)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("got %v", err)
	}
}
