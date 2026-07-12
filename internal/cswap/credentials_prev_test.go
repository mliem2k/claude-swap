package cswap

import (
	"log/slog"
	"testing"
)

func TestRetainPreviousHoldsPriorGeneration(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	s := NewCredentialStore(PlatformLinux, t.TempDir(), slog.Default())

	old := `{"claudeAiOauth":{"accessToken":"old"}}`
	next := `{"claudeAiOauth":{"accessToken":"new"}}`
	_ = s.writeAccountCredentials("1", "a@example.com", old)
	// writeAccountCredentials retains the prior generation before overwriting.
	if err := s.writeAccountCredentials("1", "a@example.com", next); err != nil {
		t.Fatal(err)
	}
	if got := s.readAccountCredentials("1", "a@example.com"); got != next {
		t.Fatalf("current should be new, got %q", got)
	}
	if got := s.readPreviousBackup("1", "a@example.com"); got != old {
		t.Fatalf("prev should hold old generation, got %q", got)
	}
}

func TestRetainNoopWhenUnchanged(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	s := NewCredentialStore(PlatformLinux, t.TempDir(), slog.Default())
	creds := `{"x":1}`
	_ = s.writeAccountCredentials("1", "a@example.com", creds)
	// Re-writing identical creds should not create a .prev.
	s.retainPreviousBackup("1", "a@example.com", creds)
	if got := s.readPreviousBackup("1", "a@example.com"); got != "" {
		t.Fatalf("expected no prev for unchanged creds, got %q", got)
	}
}

func TestRetainNoopWhenNoCurrentBackup(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	s := NewCredentialStore(PlatformLinux, t.TempDir(), slog.Default())
	// No prior backup exists yet; retaining must not fabricate a .prev.
	s.retainPreviousBackup("1", "a@example.com", `{"x":1}`)
	if got := s.readPreviousBackup("1", "a@example.com"); got != "" {
		t.Fatalf("expected no prev with no prior backup, got %q", got)
	}
}

func TestRetainOnMacOSDoesNotWeakenKeychainPosture(t *testing.T) {
	// A Keychain-using Mac must not grow a plaintext .prev file.
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())

	_ = s.writeAccountCredentials("1", "a@example.com", `{"x":"old"}`)
	_ = s.writeAccountCredentials("1", "a@example.com", `{"x":"new"}`)

	if fileExists(s.prevBackupPath("1", "a@example.com")) {
		t.Fatal(".prev should not be a plaintext file when the keychain is in use")
	}
	if got := s.readPreviousBackup("1", "a@example.com"); got != `{"x":"old"}` {
		t.Fatalf("prev should still be readable via keychain, got %q", got)
	}
}
