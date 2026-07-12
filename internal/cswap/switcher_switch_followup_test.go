package cswap

import (
	"strings"
	"testing"
)

func TestSwitchFollowupMessageFileBackend(t *testing.T) {
	// Override platform to simulate Linux environment for file backend testing
	old := runtimeGOOS
	runtimeGOOS = func() string { return "linux" }
	defer func() { runtimeGOOS = old }()

	s := newTestSwitcher(t) // Linux platform in this fixture: file backend
	msg := s.SwitchFollowupMessage()
	if !strings.Contains(msg, "no restart needed") {
		t.Fatalf("got %q", msg)
	}
}

func TestSwitchFollowupMessageKeychainBackend(t *testing.T) {
	old := runtimeGOOS
	runtimeGOOS = func() string { return "darwin" }
	defer func() { runtimeGOOS = old }()

	// A fresh CredentialStore has never probed the keychain (lastActiveBackend
	// == "" and keychainUsable == nil), so useKeychain()'s macOS default
	// ("not yet probed assumes usable") is exactly what SwitchFollowupMessage
	// falls back on before any real switch has happened.
	s := newTestSwitcher(t)
	msg := s.SwitchFollowupMessage()
	if !strings.Contains(msg, "Restart Claude Code") {
		t.Fatalf("got %q", msg)
	}
}
