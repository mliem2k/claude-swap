package cswap

import (
	"os"
	"testing"
	"time"
)

func withAuthStatusRunner(t *testing.T, fn func(claudeBin string, env []string, timeout time.Duration) (string, int, error)) {
	t.Helper()
	original := authStatusRunner
	authStatusRunner = fn
	t.Cleanup(func() { authStatusRunner = original })
}

func TestProbeEnvSetsConfigDirAndDropsOverrides(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "should-be-dropped")
	t.Setenv("SOME_OTHER_VAR", "should-survive")
	env := probeEnv("/tmp/session-dir")
	var sawConfigDir, sawOther, sawOverride bool
	for _, kv := range env {
		switch kv {
		case "CLAUDE_CONFIG_DIR=/tmp/session-dir":
			sawConfigDir = true
		case "SOME_OTHER_VAR=should-survive":
			sawOther = true
		}
		if kv == "ANTHROPIC_API_KEY=should-be-dropped" {
			sawOverride = true
		}
	}
	if !sawConfigDir || !sawOther || sawOverride {
		t.Fatalf("sawConfigDir=%v sawOther=%v sawOverride=%v env=%v", sawConfigDir, sawOther, sawOverride, env)
	}
}

func TestIsSessionValidMissingDirIsFalse(t *testing.T) {
	if isSessionValid(os.TempDir()+"/does-not-exist-cswap-test", "a@example.com", "") {
		t.Fatal("expected false for a missing directory")
	}
}

func TestIsSessionValidLoggedInMatchingIdentity(t *testing.T) {
	dir := t.TempDir()
	withAuthStatusRunner(t, func(claudeBin string, env []string, timeout time.Duration) (string, int, error) {
		return `{"loggedIn":true,"authMethod":"claude.ai","email":"a@example.com","orgId":"org1"}`, 0, nil
	})
	if !isSessionValid(dir, "a@example.com", "org1") {
		t.Fatal("expected valid")
	}
}

func TestIsSessionValidWrongEmailIsFalse(t *testing.T) {
	dir := t.TempDir()
	withAuthStatusRunner(t, func(claudeBin string, env []string, timeout time.Duration) (string, int, error) {
		return `{"loggedIn":true,"authMethod":"claude.ai","email":"other@example.com"}`, 0, nil
	})
	if isSessionValid(dir, "a@example.com", "") {
		t.Fatal("expected false for a mismatched email")
	}
}

func TestIsSessionValidNonZeroExitIsFalse(t *testing.T) {
	dir := t.TempDir()
	withAuthStatusRunner(t, func(claudeBin string, env []string, timeout time.Duration) (string, int, error) {
		return "", 1, nil
	})
	if isSessionValid(dir, "a@example.com", "") {
		t.Fatal("expected false for a non-zero exit code")
	}
}

func TestIsSessionValidNotLoggedInIsFalse(t *testing.T) {
	dir := t.TempDir()
	withAuthStatusRunner(t, func(claudeBin string, env []string, timeout time.Duration) (string, int, error) {
		return `{"loggedIn":false}`, 0, nil
	})
	if isSessionValid(dir, "a@example.com", "") {
		t.Fatal("expected false when not logged in")
	}
}

func TestIsSessionValidWrongAuthMethodIsFalse(t *testing.T) {
	dir := t.TempDir()
	withAuthStatusRunner(t, func(claudeBin string, env []string, timeout time.Duration) (string, int, error) {
		return `{"loggedIn":true,"authMethod":"api-key","email":"a@example.com"}`, 0, nil
	})
	if isSessionValid(dir, "a@example.com", "") {
		t.Fatal("expected false for a non-claude.ai auth method")
	}
}

func TestIsSessionValidRunnerErrorIsFalse(t *testing.T) {
	dir := t.TempDir()
	withAuthStatusRunner(t, func(claudeBin string, env []string, timeout time.Duration) (string, int, error) {
		return "", -1, os.ErrDeadlineExceeded
	})
	if isSessionValid(dir, "a@example.com", "") {
		t.Fatal("expected false on a runner error")
	}
}

func TestIsSessionValidLenientOrgCheck(t *testing.T) {
	dir := t.TempDir()
	withAuthStatusRunner(t, func(claudeBin string, env []string, timeout time.Duration) (string, int, error) {
		// No orgId in the response at all: schema drift degrades to
		// email-only validation, not a false negative.
		return `{"loggedIn":true,"authMethod":"claude.ai","email":"a@example.com"}`, 0, nil
	})
	if !isSessionValid(dir, "a@example.com", "org1") {
		t.Fatal("expected valid: a missing orgId in the response must not fail validation")
	}
}
