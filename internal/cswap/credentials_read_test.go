package cswap

import (
	"log/slog"
	"testing"
)

func TestLooksLikeAPIKey(t *testing.T) {
	cases := map[string]bool{
		"sk-ant-api03xxxx":     true,
		`{"claudeAiOauth":{}}`: false,
		"sk-ant-oat01setup":    false, // setup token, not an api key
		"":                     false,
		"  sk-ant-api03xx  ":   true, // trimmed prefix
	}
	for in, want := range cases {
		if got := LooksLikeAPIKey(in); got != want {
			t.Errorf("LooksLikeAPIKey(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestApprovedFormIsLast20(t *testing.T) {
	key := "sk-ant-api03-0123456789abcdef0123"
	want := key[len(key)-20:]
	if got := ApprovedForm(key); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestReadActiveCredentialsFromFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)
	fake := newFakeSecurity()
	restore := swapSecurity(fake)
	defer restore()

	// No Keychain item; write the plaintext OAuth file.
	creds := `{"claudeAiOauth":{"accessToken":"abc"}}`
	mustWrite(t, GetCredentialsPath(), creds)

	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())
	got := s.readActiveCredentials()
	if got.Value != creds {
		t.Fatalf("expected file creds, got %q", got.Value)
	}
	if got.KeychainUnavailable {
		t.Fatal("file read should not report keychain unavailable")
	}
}

func TestReadActiveCredentialsManagedKeyFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)
	t.Setenv("HOME", tmp)
	restore := swapSecurity(newFakeSecurity())
	defer restore()

	// No OAuth anywhere; managed key in .claude.json primaryApiKey.
	mustWrite(t, GetGlobalConfigPath(), `{"primaryApiKey":"sk-ant-api03zz"}`)

	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())
	got := s.readActiveCredentials()
	if got.Value != "sk-ant-api03zz" {
		t.Fatalf("expected managed key, got %q", got.Value)
	}
}

func TestReadActiveCredentialsEmptyWhenNothing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)
	t.Setenv("HOME", tmp)
	restore := swapSecurity(newFakeSecurity())
	defer restore()

	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())
	got := s.readActiveCredentials()
	if got.Value != "" {
		t.Fatalf("expected empty, got %q", got.Value)
	}
	if got.KeychainUnavailable {
		t.Fatal("an absent Keychain item is not an unavailability")
	}
}

func TestReadActiveCredentialsOAuthKeychainWins(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)
	t.Setenv("HOME", tmp)
	fake := newFakeSecurity()
	restore := swapSecurity(fake)
	defer restore()

	oauth := `{"claudeAiOauth":{"accessToken":"from-keychain"}}`
	_ = fake.SetPassword(ClaudeCodeKeychainService, KeychainAccountName(), oauth)

	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())
	got := s.readActiveCredentials()
	if got.Value != oauth {
		t.Fatalf("expected keychain oauth to win, got %q", got.Value)
	}
}

func TestReadActiveCredentialsKeychainUnavailableFlag(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)
	t.Setenv("HOME", tmp)
	fake := newFakeSecurity()
	fake.getErr = ErrKeychainUnavailable
	restore := swapSecurity(fake)
	defer restore()

	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())
	got := s.readActiveCredentials()
	if got.Value != "" {
		t.Fatalf("expected empty value, got %q", got.Value)
	}
	if !got.KeychainUnavailable {
		t.Fatal("a failed oauth keychain read with nothing else found should flag unavailable")
	}
}
