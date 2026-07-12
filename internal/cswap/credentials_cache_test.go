package cswap

import (
	"log/slog"
	"testing"
)

func TestUseKeychainOffMacOS(t *testing.T) {
	s := NewCredentialStore(PlatformLinux, t.TempDir(), slog.Default())
	if s.useKeychain() {
		t.Fatal("useKeychain must be false off macOS")
	}
}

func TestUseKeychainStickyAfterFailure(t *testing.T) {
	fake := newFakeSecurity()
	fake.getErr = ErrKeychainUnavailable // a real failure
	restore := swapSecurity(fake)
	defer restore()

	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())
	// A failing kcCall must drop the store to file mode.
	_, _ = s.kcCall(fake.GetPassword, "svc", "acct")
	if s.useKeychain() {
		t.Fatal("useKeychain must be false after a Keychain failure (sticky)")
	}
}

func TestPinFileModeSticks(t *testing.T) {
	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())
	s.pinFileMode()
	if s.useKeychain() {
		t.Fatal("useKeychain must be false after pinFileMode")
	}
}

func TestUseKeychainDefaultsTrueOnMacOSUnprobed(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())
	if !s.useKeychain() {
		t.Fatal("useKeychain should default true on macOS before any probe")
	}
}
