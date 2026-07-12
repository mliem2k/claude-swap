package cswap

import (
	"log/slog"
	"os"
	"testing"
)

func TestWriteOAuthCredentialsToFileOffMacOS(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)
	t.Setenv("HOME", tmp)
	restore := swapSecurity(newFakeSecurity())
	defer restore()

	s := NewCredentialStore(PlatformLinux, t.TempDir(), slog.Default())
	creds := `{"claudeAiOauth":{"accessToken":"abc"}}`
	if err := s.writeCredentials(creds); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(GetCredentialsPath())
	if string(got) != creds {
		t.Fatalf("file should hold oauth creds, got %s", got)
	}
	if s.lastActiveBackend != "file" {
		t.Fatalf("expected file backend, got %q", s.lastActiveBackend)
	}
}

func TestWriteManagedKeyClearsOAuth(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)
	t.Setenv("HOME", tmp)
	restore := swapSecurity(newFakeSecurity())
	defer restore()

	// Seed an OAuth file so the managed-key write must clear it.
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"old"}}`)

	s := NewCredentialStore(PlatformLinux, t.TempDir(), slog.Default())
	if err := s.writeCredentials("sk-ant-api03deadbeefxxxxxxxxxx"); err != nil {
		t.Fatal(err)
	}
	// OAuth file must be gone (mutual exclusion).
	if _, err := os.Stat(GetCredentialsPath()); !os.IsNotExist(err) {
		t.Fatalf("oauth file should be removed, err=%v", err)
	}
	// Managed key recorded in .claude.json primaryApiKey + approved list.
	cfg := s.readGlobalConfig()
	if cfg["primaryApiKey"] != "sk-ant-api03deadbeefxxxxxxxxxx" {
		t.Fatalf("primaryApiKey not set: %v", cfg["primaryApiKey"])
	}
	responses, ok := cfg["customApiKeyResponses"].(map[string]any)
	if !ok {
		t.Fatalf("customApiKeyResponses missing: %v", cfg)
	}
	approved, ok := responses["approved"].([]any)
	if !ok || len(approved) == 0 {
		t.Fatalf("approved list not populated: %v", responses)
	}
}

func TestWriteOAuthClearsManagedKey(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)
	t.Setenv("HOME", tmp)
	restore := swapSecurity(newFakeSecurity())
	defer restore()

	// Seed a managed key in config.
	mustWrite(t, GetGlobalConfigPath(), `{"primaryApiKey":"sk-ant-api03keep","customApiKeyResponses":{"approved":["x"]}}`)

	s := NewCredentialStore(PlatformLinux, t.TempDir(), slog.Default())
	creds := `{"claudeAiOauth":{"accessToken":"new"}}`
	if err := s.writeCredentials(creds); err != nil {
		t.Fatal(err)
	}
	cfg := s.readGlobalConfig()
	if _, present := cfg["primaryApiKey"]; present {
		t.Fatal("primaryApiKey should be cleared after OAuth write")
	}
}

func TestWriteOAuthKeychainBumpsExistingFileMtime(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)
	t.Setenv("HOME", tmp)
	restore := swapSecurity(newFakeSecurity())
	defer restore()

	// A pre-existing shadow file that a Keychain write should refresh (bump
	// mtime, keep content in sync) but never delete or newly create elsewhere.
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"stale"}}`)

	s := NewCredentialStore(PlatformMacOS, t.TempDir(), slog.Default())
	fresh := `{"claudeAiOauth":{"accessToken":"fresh"}}`
	if err := s.writeCredentials(fresh); err != nil {
		t.Fatal(err)
	}
	if s.lastActiveBackend != "keychain" {
		t.Fatalf("expected keychain backend on macOS, got %q", s.lastActiveBackend)
	}
	got, _ := os.ReadFile(GetCredentialsPath())
	if string(got) != fresh {
		t.Fatalf("shadow file should be refreshed with fresh creds, got %s", got)
	}
}
