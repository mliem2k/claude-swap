package cswap

import (
	"log/slog"
	"os"
	"testing"
)

func TestBackupEncRoundTripLinux(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	s := NewCredentialStore(PlatformLinux, t.TempDir(), slog.Default())

	creds := `{"claudeAiOauth":{"accessToken":"x"}}`
	if err := s.writeAccountCredentials("1", "a@example.com", creds); err != nil {
		t.Fatal(err)
	}
	if got := s.readAccountCredentials("1", "a@example.com"); got != creds {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

func TestBackupEncWinsOverKeychain(t *testing.T) {
	// macOS: a fallback .enc must beat a (stale) Keychain copy.
	fake := newFakeSecurity()
	restore := swapSecurity(fake)
	defer restore()
	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())

	enc := `{"claudeAiOauth":{"accessToken":"file"}}`
	kc := `{"claudeAiOauth":{"accessToken":"keychain"}}`
	// Write the .enc directly (simulating a prior fallback).
	if err := s.writeBackupEnc("1", "a@example.com", enc); err != nil {
		t.Fatal(err)
	}
	// And a Keychain copy directly (simulating a stale Keychain entry).
	if err := fake.SetPassword(SecurityService, s.backupUsername("1", "a@example.com"), kc); err != nil {
		t.Fatal(err)
	}
	if got := s.readAccountCredentials("1", "a@example.com"); got != enc {
		t.Fatalf(".enc should win, got %q", got)
	}
}

func TestWriteAccountCredentialsReconcilesEncAfterKeychainWrite(t *testing.T) {
	// On macOS with a usable Keychain, a fresh write must land in the
	// Keychain and delete any leftover .enc so it cannot shadow it.
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())

	creds := `{"claudeAiOauth":{"accessToken":"fresh"}}`
	if err := s.writeAccountCredentials("1", "a@example.com", creds); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.backupEncPath("1", "a@example.com")); !os.IsNotExist(err) {
		t.Fatalf(".enc should be reconciled away after a keychain write, err=%v", err)
	}
	if got := s.readAccountCredentials("1", "a@example.com"); got != creds {
		t.Fatalf("read should return the fresh keychain value, got %q", got)
	}
}

func TestDeleteAccountCredentialsRemovesEnc(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	s := NewCredentialStore(PlatformLinux, t.TempDir(), slog.Default())
	_ = s.writeAccountCredentials("1", "a@example.com", `{"x":1}`)
	s.deleteAccountCredentials("1", "a@example.com")
	if got := s.readAccountCredentials("1", "a@example.com"); got != "" {
		t.Fatalf("expected empty after delete, got %q", got)
	}
	if _, err := os.Stat(s.backupEncPath("1", "a@example.com")); !os.IsNotExist(err) {
		t.Fatalf("enc file should be gone, err=%v", err)
	}
}

func TestReadAccountCredentialsCorruptEncFallsThroughOnMacOS(t *testing.T) {
	fake := newFakeSecurity()
	restore := swapSecurity(fake)
	defer restore()
	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())

	kc := `{"claudeAiOauth":{"accessToken":"keychain-good"}}`
	if err := fake.SetPassword(SecurityService, s.backupUsername("1", "a@example.com"), kc); err != nil {
		t.Fatal(err)
	}
	// Corrupt .enc: not valid base64.
	mustWrite(t, s.backupEncPath("1", "a@example.com"), "!!!not-base64!!!")

	if got := s.readAccountCredentials("1", "a@example.com"); got != kc {
		t.Fatalf("corrupt .enc should fall through to keychain, got %q", got)
	}
}
