package cswap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// StaleMarker mirrors STALE_MARKER.
const StaleMarker = ".cswap-stale-credentials"

// MarkSessionStale mirrors mark_session_stale: flags a live session profile
// for re-bootstrap once it exits. Best-effort.
func MarkSessionStale(sessionDir string) {
	_ = os.WriteFile(filepath.Join(sessionDir, StaleMarker), nil, 0o600)
}

// SlugifyEmail mirrors slugify_email: a filesystem-safe slug. Uniqueness
// comes from the slot-number prefix on the session dir, so this only needs
// to be safe (incl. Windows-forbidden chars), not injective. NFC-normalizes
// first (matching Python's unicodedata.normalize("NFC", email)): an
// unnormalized NFD sequence would otherwise slug its base character and
// each combining mark separately (extra "_"s), producing a different
// session directory name for the same email than Python would.
func SlugifyEmail(email string) string {
	normalized := norm.NFC.String(email)
	var b strings.Builder
	for _, ch := range normalized {
		if isASCIIAlnum(ch) || ch == '.' || ch == '_' || ch == '-' {
			b.WriteRune(ch)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func isASCIIAlnum(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

// SessionDirFor mirrors session_dir_for: the session profile directory for
// an account.
func SessionDirFor(backupDir, accountNum, email string) string {
	return filepath.Join(backupDir, "sessions", accountNum+"-"+SlugifyEmail(email))
}

// KeychainServiceName mirrors keychain_service_name: the Keychain service
// name Claude Code derives for this config dir (sha256 of the exact,
// unresolved path string, NFC-normalized, first 8 hex chars). Claude Code
// itself NFC-normalizes the raw CLAUDE_CONFIG_DIR value before hashing it
// (claude src envUtils.ts/macOsKeychainHelpers.ts); skipping that step here
// would compute a different hash (and so read/delete the wrong Keychain
// entry, or none at all) whenever the path surfaces in NFD-decomposed
// form, a real possibility on macOS for certain locales/usernames.
func KeychainServiceName(sessionDir string) string {
	normalized := norm.NFC.String(sessionDir)
	sum := sha256.Sum256([]byte(normalized))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
}

// DeleteMacOSKeychainEntry mirrors delete_macos_keychain_entry: best-effort
// delete of a session profile's hashed keychain entry. No-op off macOS.
func DeleteMacOSKeychainEntry(sessionDir string) {
	if DetectPlatform() != PlatformMacOS {
		return
	}
	_ = Security.DeletePassword(KeychainServiceName(sessionDir), KeychainAccountName())
}

// ReadSessionCredentials mirrors read_session_credentials: best-effort read
// of a session profile's current credential JSON. "" when unreadable.
func ReadSessionCredentials(sessionDir string) string {
	info, err := os.Stat(sessionDir)
	if err != nil || !info.IsDir() {
		return ""
	}
	if DetectPlatform() == PlatformMacOS {
		creds, err := Security.GetPassword(KeychainServiceName(sessionDir), KeychainAccountName())
		if err == nil && creds != "" {
			return creds
		}
	}
	data, err := os.ReadFile(filepath.Join(sessionDir, ".credentials.json"))
	if err != nil {
		return ""
	}
	return string(data)
}

// ReadSessionIdentity mirrors read_session_identity: best-effort read of the
// account identity a session profile is logged in as.
func ReadSessionIdentity(sessionDir string) (email, orgUUID string, ok bool) {
	data, err := os.ReadFile(filepath.Join(sessionDir, ".claude.json"))
	if err != nil {
		return "", "", false
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return "", "", false
	}
	oauthAccount, _ := config["oauthAccount"].(map[string]any)
	if oauthAccount == nil {
		return "", "", false
	}
	email, _ = oauthAccount["emailAddress"].(string)
	if email == "" {
		return "", "", false
	}
	orgUUID, _ = oauthAccount["organizationUuid"].(string)
	return email, orgUUID, true
}

// SessionIdentityDrifted mirrors session_identity_drifted: whether the
// profile is logged in as a different account than its slot. An unreadable
// identity is NOT drift.
func SessionIdentityDrifted(sessionDir, email, orgUUID string) bool {
	profileEmail, profileOrg, ok := ReadSessionIdentity(sessionDir)
	if !ok {
		return false
	}
	if profileEmail != email {
		return true
	}
	return profileOrg != "" && orgUUID != "" && profileOrg != orgUUID
}

// LiveSessionsFor mirrors live_sessions_for: live Claude instances running
// against a session profile.
func LiveSessionsFor(sessionDir string) []ClaudeSession {
	if info, err := os.Stat(sessionDir); err != nil || !info.IsDir() {
		return nil
	}
	return ListSessions(sessionDir)
}
