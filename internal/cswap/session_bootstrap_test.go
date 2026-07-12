package cswap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHasRefreshTokenTrue(t *testing.T) {
	if !hasRefreshToken(`{"claudeAiOauth":{"refreshToken":"r"}}`) {
		t.Fatal("expected true")
	}
}

func TestHasRefreshTokenMissingKeyIsFalse(t *testing.T) {
	if hasRefreshToken(`{"claudeAiOauth":{}}`) {
		t.Fatal("expected false: refreshToken key absent")
	}
}

func TestHasRefreshTokenMissingClaudeAiOauthIsFalse(t *testing.T) {
	if hasRefreshToken(`{}`) {
		t.Fatal("expected false: claudeAiOauth key absent from a valid object")
	}
}

func TestHasRefreshTokenInvalidJSONIsTrue(t *testing.T) {
	if !hasRefreshToken("not json") {
		t.Fatal("expected true: unknown shape defers to the refresh attempt")
	}
}

func TestHasRefreshTokenLiteralNullIsTrue(t *testing.T) {
	// Go's json.Unmarshal into a map[string]any target uniquely succeeds
	// on a literal "null" (nil map, no error), unlike every other
	// non-object JSON shape; the `|| parsed == nil` check in
	// hasRefreshToken exists specifically to still classify this as an
	// unknown shape, matching Python's json.loads("null") -> None ->
	// AttributeError -> True.
	if !hasRefreshToken("null") {
		t.Fatal("expected true: literal JSON null is an unknown shape")
	}
}

func TestHasRefreshTokenClaudeAiOauthNotObjectIsTrue(t *testing.T) {
	if !hasRefreshToken(`{"claudeAiOauth":"not-an-object"}`) {
		t.Fatal("expected true: claudeAiOauth present but not itself an object")
	}
}

func TestHasRefreshTokenEmptyStringTokenIsFalse(t *testing.T) {
	if hasRefreshToken(`{"claudeAiOauth":{"refreshToken":""}}`) {
		t.Fatal("expected false for an empty refreshToken")
	}
}

func TestCleanupFailedSessionRemovesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cleanupFailedSession(dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("expected the directory to be removed")
	}
}

func setupAccountForBootstrap(t *testing.T) (*ClaudeAccountSwitcher, string, string) {
	t.Helper()
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	creds := `{"claudeAiOauth":{"accessToken":"x","refreshToken":""}}`
	config := `{"oauthAccount":{"emailAddress":"a@example.com"},"theme":"light"}`
	if err := s.WriteAccountCredentials("1", "a@example.com", creds); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccountConfig("1", "a@example.com", config); err != nil {
		t.Fatal(err)
	}
	return s, "1", "a@example.com"
}

func TestBootstrapSeedsCredentialsAndConfig(t *testing.T) {
	s, num, email := setupAccountForBootstrap(t)
	sessionDir := filepath.Join(t.TempDir(), "session")

	if err := bootstrap(s, sessionDir, num, email, ""); err != nil {
		t.Fatal(err)
	}

	credsData, err := os.ReadFile(filepath.Join(sessionDir, ".credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(credsData) != `{"claudeAiOauth":{"accessToken":"x","refreshToken":""}}` {
		t.Fatalf("got %s", credsData)
	}

	configData, err := os.ReadFile(filepath.Join(sessionDir, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(configData, &config); err != nil {
		t.Fatal(err)
	}
	oauth, _ := config["oauthAccount"].(map[string]any)
	if oauth["emailAddress"] != "a@example.com" {
		t.Fatalf("got %#v", config)
	}
	if config["hasCompletedOnboarding"] != true {
		t.Fatalf("got %#v", config)
	}
	if config["theme"] != "light" {
		t.Fatalf("got %#v", config)
	}
}

func TestBootstrapPreservesExistingProfileConfig(t *testing.T) {
	s, num, email := setupAccountForBootstrap(t)
	sessionDir := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := `{"someProfileSetting":"keepme","theme":"dracula"}`
	if err := os.WriteFile(filepath.Join(sessionDir, ".claude.json"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := bootstrap(s, sessionDir, num, email, ""); err != nil {
		t.Fatal(err)
	}

	configData, err := os.ReadFile(filepath.Join(sessionDir, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(configData, &config); err != nil {
		t.Fatal(err)
	}
	if config["someProfileSetting"] != "keepme" {
		t.Fatalf("expected pre-existing profile settings preserved, got %#v", config)
	}
	if config["theme"] != "dracula" {
		t.Fatalf("expected pre-existing theme preserved (not overwritten from config_data), got %#v", config)
	}
}

func TestBootstrapNoCredentialsIsSessionError(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(t.TempDir(), "session")
	err := bootstrap(s, sessionDir, "1", "a@example.com", "")
	if err == nil {
		t.Fatal("expected an error for missing credentials")
	}
}

func TestBootstrapNoConfigBackupIsSessionError(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"x"}}`); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(t.TempDir(), "session")
	err := bootstrap(s, sessionDir, "1", "a@example.com", "")
	if err == nil {
		t.Fatal("expected an error for missing config backup (no oauthAccount)")
	}
}
