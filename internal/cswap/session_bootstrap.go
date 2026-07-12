package cswap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// bootstrap mirrors SessionManager._bootstrap: seeds the session profile
// from backup storage. Caller holds the switcher's lock.
func bootstrap(s *ClaudeAccountSwitcher, sessionDir, accountNum, email, orgUUID string) error {
	// Claude reads the keychain before the plaintext file: a stale hashed
	// entry from an earlier profile at this path would shadow the seed.
	DeleteMacOSKeychainEntry(sessionDir)

	creds := s.ReadAccountCredentials(accountNum, email)
	if creds == "" {
		return fmt.Errorf("account-%s has no stored credentials, re-add with: cswap add --slot %s: %w", accountNum, accountNum, ErrSession)
	}

	// One refresh so the profile starts with a fresh access token; persist
	// a possibly-rotated refresh token back to backup so future switches
	// and runs see the latest. Failure is non-fatal: the stored token may
	// still be valid, and claude refreshes on its own at runtime.
	// Setup-token accounts have no refresh token by design, skip silently
	// instead of warning about a flow that can't happen.
	if hasRefreshToken(creds) {
		refreshed := RefreshOAuthCredentials(creds)
		if refreshed != "" {
			creds = refreshed
			if err := s.WriteAccountCredentials(accountNum, email, creds); err != nil {
				return err
			}
		} else {
			Warning(fmt.Sprintf("Could not refresh the token for Account-%s; continuing with the stored credentials.", accountNum))
		}
	}

	configText := s.ReadAccountConfig(accountNum, email)
	var configData map[string]any
	if configText != "" {
		_ = json.Unmarshal([]byte(configText), &configData)
	}
	if configData == nil {
		configData = map[string]any{}
	}
	oauthAccount, _ := configData["oauthAccount"].(map[string]any)
	if oauthAccount == nil {
		return fmt.Errorf("account-%s has no stored config backup, re-add with: cswap add --slot %s: %w", accountNum, accountNum, ErrSession)
	}

	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(sessionDir, 0o700); err != nil {
			return err
		}
	}

	credsPath := filepath.Join(sessionDir, ".credentials.json")
	if err := os.WriteFile(credsPath, []byte(creds), 0o600); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(credsPath, 0o600); err != nil {
			return err
		}
	}

	// Merge the identity seed into any existing .claude.json so a
	// re-bootstrap preserves the profile's own projects/history. The
	// theme key is load-bearing: claude shows onboarding when
	// !config.theme || !config.hasCompletedOnboarding.
	configPath := filepath.Join(sessionDir, ".claude.json")
	existing := map[string]any{}
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	if existing == nil {
		existing = map[string]any{}
	}
	existing["oauthAccount"] = oauthAccount
	existing["hasCompletedOnboarding"] = true
	if _, hasTheme := existing["theme"]; !hasTheme {
		theme, _ := configData["theme"].(string)
		if theme == "" {
			theme = "dark"
		}
		existing["theme"] = theme
	}
	encoded, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(configPath, 0o600); err != nil {
			return err
		}
	}

	s.Logger.Info("bootstrapped session profile", "account", accountNum, "sessionDir", sessionDir)
	return nil
}

// hasRefreshToken mirrors _has_refresh_token. Invalid JSON or a
// non-object top level is an unknown shape, deferring to the refresh
// attempt (true); a valid object simply missing claudeAiOauth or
// refreshToken is a known "no token" shape (false).
func hasRefreshToken(creds string) bool {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(creds), &parsed); err != nil || parsed == nil {
		return true
	}
	oauthAny, present := parsed["claudeAiOauth"]
	if !present {
		return false
	}
	oauth, ok := oauthAny.(map[string]any)
	if !ok {
		return true
	}
	token, _ := oauth["refreshToken"].(string)
	return token != ""
}

// cleanupFailedSession mirrors SessionManager._cleanup_failed_session.
// Keychain first: claude may have partially migrated the seed, and the
// hashed service name can't be recomputed once the dir is gone.
func cleanupFailedSession(sessionDir string) {
	DeleteMacOSKeychainEntry(sessionDir)
	_ = os.RemoveAll(sessionDir)
}
