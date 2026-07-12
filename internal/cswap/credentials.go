package cswap

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Constants mirroring credentials.py.
const (
	// SecurityService is the macOS Keychain service for per-account backups.
	SecurityService = "claude-swap"
	// ClaudeCodeKeychainService is the active OAuth credential's Keychain service.
	ClaudeCodeKeychainService = "Claude Code-credentials"
	// ClaudeCodeManagedKeychainService is the active managed API key's service.
	ClaudeCodeManagedKeychainService = "Claude Code"

	activeReadAttempts       = 2
	activeReadRetryDelay     = 300 * time.Millisecond
	keychainRecheckCooldownS = 60.0 // float seconds, matching Python's time.monotonic() unit
)

// CredentialStore owns where credentials live and how they are read/written.
// It holds its own config fields (the Python store reads these through a host
// view owned by the switcher; here they are set at construction so the store
// is testable without the switcher, which lands in a later plan).
type CredentialStore struct {
	platform       Platform
	credentialsDir string
	logger         *slog.Logger

	// macOS Keychain usability, learned per-process. nil = not yet probed.
	keychainUsable        *bool
	keychainDisabledUntil float64 // monotonic seconds; 0 = no pending re-probe
	lastActiveBackend     string
}

// NewCredentialStore constructs a store for the given platform and backup dir.
func NewCredentialStore(platform Platform, credentialsDir string, logger *slog.Logger) *CredentialStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &CredentialStore{
		platform:       platform,
		credentialsDir: credentialsDir,
		logger:         logger,
	}
}

// kcCall mirrors _kc_call: runs a Keychain op, learning usability. A failure
// marks the Keychain unusable (sticky, with a re-probe cooldown) and returns
// the error. A success flips the cache nil -> true only (never false -> true).
func (s *CredentialStore) kcCall(fn func(string, string) (string, error), service, account string) (string, error) {
	v, err := fn(service, account)
	if err != nil {
		if IsUnavailable(err) {
			falsey := false
			s.keychainUsable = &falsey
			s.keychainDisabledUntil = monotonicNow() + keychainRecheckCooldownS
		}
		return "", err
	}
	if s.keychainUsable == nil {
		truthy := true
		s.keychainUsable = &truthy
	}
	return v, nil
}

// useKeychain mirrors _use_keychain. False off macOS. On macOS, true until a
// Keychain op fails, which drops to file mode for the cooldown window.
func (s *CredentialStore) useKeychain() bool {
	if s.platform != PlatformMacOS {
		return false
	}
	if s.keychainUsable != nil && !*s.keychainUsable &&
		s.keychainDisabledUntil != 0 &&
		monotonicNow() >= s.keychainDisabledUntil {
		s.keychainUsable = nil // cooldown elapsed; re-probe
		s.keychainDisabledUntil = 0
	}
	if s.keychainUsable == nil {
		return true // not yet probed on macOS assumes usable
	}
	return *s.keychainUsable
}

// pinFileMode mirrors _pin_file_mode: sticky file mode, no re-probe.
func (s *CredentialStore) pinFileMode() {
	falsey := false
	s.keychainUsable = &falsey
	s.keychainDisabledUntil = 0
}

// monotonicNow wraps a monotonic clock reading (matches Python's
// time.monotonic(), used only for the cooldown comparison, never wall clock).
var monotonicNow = func() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// atomicWriteJSON writes data as JSON with 0600 mode via temp-file rename.
// Shared helper for the stash manifest and other machine-local state files.
func atomicWriteJSON(path string, data any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// ActiveCredentials mirrors credentials.ActiveCredentials. Value is the
// credential string (OAuth JSON or a raw managed key), "" when none exists in
// any backend. KeychainUnavailable is true only when the macOS OAuth Keychain
// read failed (locked / denied / timeout) and nothing else covered it,
// letting callers distinguish a transiently unreadable Keychain from a
// genuinely empty slot.
//
// FileReadFailed is true only when the plaintext OAuth credentials file
// exists but could not be read (permission denied, disk error, a
// concurrent delete mid-read), as opposed to the file being absent or
// genuinely empty. Mirrors Python's _read_active_credentials returning
// None (vs "") for this one case. Callers that are about to overwrite a
// live credential without any other backup must treat this as "unknown,
// do not overwrite" rather than "empty, safe to overwrite."
type ActiveCredentials struct {
	Value               string
	KeychainUnavailable bool
	FileReadFailed      bool
}

// LooksLikeAPIKey mirrors looks_like_api_key. Strict: a managed key is a bare
// sk-ant-api... string, while every OAuth/setup-token credential is a JSON
// object. Requiring the sk-ant-api prefix (and that it is not JSON) keeps a
// raw/garbled sk-ant-oat... setup token from ever being misclassified as an
// API key.
func LooksLikeAPIKey(credentials string) bool {
	if credentials == "" {
		return false
	}
	text := strings.TrimSpace(credentials)
	return strings.HasPrefix(text, "sk-ant-api") && !strings.HasPrefix(text, "{")
}

// ApprovedForm mirrors approved_form: the value Claude Code stores in
// customApiKeyResponses.approved (the last 20 chars of the trimmed key).
func ApprovedForm(apiKey string) string {
	key := strings.TrimSpace(apiKey)
	if len(key) <= 20 {
		return key
	}
	return key[len(key)-20:]
}

// readActiveCredentials mirrors _read_active_credentials. Tries the OAuth
// credential first (Keychain when usable, with a bounded retry, then the
// plaintext file Claude Code also falls back to), and only then the
// managed-key locations (Keychain, then .claude.json primaryApiKey).
// Non-mutating.
func (s *CredentialStore) readActiveCredentials() ActiveCredentials {
	keychainFailed := false

	// 1. OAuth Keychain (macOS, when usable), with a bounded retry.
	if s.useKeychain() {
		val, failed := s.readActiveOAuthKeychain()
		keychainFailed = failed
		if val != "" {
			return ActiveCredentials{Value: val}
		}
	} else if s.platform == PlatformMacOS {
		// Keychain already known unusable this process: if nothing is found
		// below, that absence is "keychain unavailable", not a genuinely
		// empty slot.
		keychainFailed = true
	}

	// 2. OAuth plaintext file (Claude Code's own fallback; every platform).
	credFile := GetCredentialsPath()
	if fileExists(credFile) {
		text, err := os.ReadFile(credFile)
		if err != nil {
			s.logger.Error("failed to read credentials file", "error", err)
			return ActiveCredentials{FileReadFailed: true}
		}
		if strings.TrimSpace(string(text)) != "" {
			return ActiveCredentials{Value: string(text)}
		}
	}

	// 3. Managed API key (Keychain on macOS, then primaryApiKey).
	if key := s.readManagedKey(); key != "" {
		return ActiveCredentials{Value: key}
	}
	// Nothing anywhere. Flag a failed-and-uncovered OAuth Keychain read so the
	// UI distinguishes it from a real empty slot.
	return ActiveCredentials{KeychainUnavailable: keychainFailed}
}

// readActiveOAuthKeychain mirrors _read_active_oauth_keychain: reads the
// active OAuth Keychain item with a bounded retry. Returns (value, failed).
// value is "" when the item is absent or unreadable; failed is true only when
// every attempt hit a real Keychain failure (not a genuine absence, which is
// not retried).
func (s *CredentialStore) readActiveOAuthKeychain() (string, bool) {
	var lastErr error
	for attempt := range activeReadAttempts {
		val, err := s.kcCall(Security.GetPassword, ClaudeCodeKeychainService, KeychainAccountName())
		if err == nil {
			return val, false
		}
		if errors.Is(err, ErrKeychainNotFound) {
			return "", false // genuinely absent; not retried
		}
		lastErr = err
		if attempt+1 < activeReadAttempts {
			time.Sleep(activeReadRetryDelay)
		}
	}
	s.logger.Warn("keychain read failed, trying file",
		"attempts", activeReadAttempts, "error", lastErr)
	return "", true
}

// readManagedKey mirrors _read_managed_key: the active managed API key, or ""
// when absent. macOS Keychain first (when usable), then .claude.json
// primaryApiKey.
func (s *CredentialStore) readManagedKey() string {
	if s.useKeychain() {
		val, err := s.kcCall(Security.GetPassword, ClaudeCodeManagedKeychainService, KeychainAccountName())
		if err != nil && !errors.Is(err, ErrKeychainNotFound) {
			s.logger.Warn("managed-key keychain read failed", "error", err)
		}
		if val != "" {
			return val
		}
	}
	cfg := s.readGlobalConfig()
	if cfg != nil {
		if key, ok := cfg["primaryApiKey"].(string); ok && key != "" {
			return key
		}
	}
	return ""
}

// readGlobalConfig mirrors _read_global_config: reads and parses
// ~/.claude.json, or nil when absent/unreadable.
func (s *CredentialStore) readGlobalConfig() map[string]any {
	path := GetGlobalConfigPath()
	if !fileExists(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		s.logger.Warn("failed to read global config", "error", err)
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		s.logger.Warn("failed to read global config", "error", err)
		return nil
	}
	return out
}

// writeCredentials mirrors _write_credentials: writes Claude Code's active
// credential, enforcing a single auth axis. Detects the kind from the payload
// and mirrors Claude Code's own saveApiKey/removeApiKey: activating one axis
// clears the other so a stale credential can never shadow the switch.
func (s *CredentialStore) writeCredentials(credentials string) error {
	if LooksLikeAPIKey(credentials) {
		return s.writeManagedCredentials(strings.TrimSpace(credentials))
	}
	if err := s.writeOAuthCredentials(credentials); err != nil {
		return err
	}
	s.clearManagedKey()
	return nil
}

// writeManagedCredentials mirrors _write_managed_credentials: activates a
// managed API key, then clears OAuth (mutual exclusion). Always records
// key[-20:] in customApiKeyResponses.approved, even on Keychain success
// (Claude Code does this on every platform, or it re-prompts to approve the
// key). Stores the key in the macOS Keychain when usable, else
// primaryApiKey.
func (s *CredentialStore) writeManagedCredentials(apiKey string) error {
	wroteToKeychain := false
	if s.useKeychain() {
		if _, err := s.kcCall(func(service, account string) (string, error) {
			return "", Security.SetPassword(service, account, apiKey)
		}, ClaudeCodeManagedKeychainService, KeychainAccountName()); err != nil {
			s.logger.Warn("managed-key keychain write failed, falling back to config", "error", err)
		} else {
			wroteToKeychain = true
		}
	}

	approved := ApprovedForm(apiKey)
	err := s.updateGlobalConfig(func(cfg map[string]any) {
		responses, ok := cfg["customApiKeyResponses"].(map[string]any)
		if !ok {
			responses = map[string]any{}
		}
		approvedList, ok := responses["approved"].([]any)
		if !ok {
			approvedList = []any{}
		}
		found := false
		for _, v := range approvedList {
			if v == approved {
				found = true
				break
			}
		}
		if !found {
			approvedList = append(approvedList, approved)
		}
		responses["approved"] = approvedList
		if _, ok := responses["rejected"]; !ok {
			responses["rejected"] = []any{}
		}
		cfg["customApiKeyResponses"] = responses
		if wroteToKeychain {
			// Keychain holds the key; keep it out of plaintext config.
			delete(cfg, "primaryApiKey")
		} else {
			cfg["primaryApiKey"] = apiKey
		}
	})
	if err != nil {
		return fmt.Errorf("failed to write managed api key: %w: %v", ErrCredentialWrite, err)
	}

	// Mutual exclusion: drop the OAuth credential so it cannot shadow the key.
	s.clearOAuthCredential()
	if s.platform == PlatformMacOS && !wroteToKeychain {
		// Same stale-Keychain resurrection guard as the OAuth path: the key
		// fell back to plaintext primaryApiKey while a stale Keychain item may
		// remain, and managed-key reads check the Keychain first.
		s.pinFileMode()
	}
	if wroteToKeychain {
		s.lastActiveBackend = "keychain"
	} else {
		s.lastActiveBackend = "file"
	}
	return nil
}

// clearManagedKey mirrors _clear_managed_key (Claude Code removeApiKey
// semantics). Deletes the macOS Keychain item (best-effort) and drops
// primaryApiKey. Leaves customApiKeyResponses.approved untouched.
func (s *CredentialStore) clearManagedKey() {
	if s.platform == PlatformMacOS {
		if err := Security.DeletePassword(ClaudeCodeManagedKeychainService, KeychainAccountName()); err != nil {
			s.logger.Warn("failed to delete managed key from keychain", "error", err)
		}
	}
	cfg := s.readGlobalConfig()
	if cfg != nil && cfg["primaryApiKey"] != nil {
		err := s.updateGlobalConfig(func(c map[string]any) {
			delete(c, "primaryApiKey")
		})
		if err != nil {
			s.logger.Warn("failed to clear primaryApiKey", "error", err)
		}
	}
}

// clearOAuthCredential mirrors _clear_oauth_credential: clears the active
// OAuth credential, Keychain item and plaintext file. Best-effort.
func (s *CredentialStore) clearOAuthCredential() {
	s.deleteActiveKeychainEntry()
	credFile := GetCredentialsPath()
	if fileExists(credFile) {
		if err := os.Remove(credFile); err != nil {
			s.logger.Warn("failed to remove credentials file", "error", err)
		}
	}
}

// deleteActiveKeychainEntry mirrors _delete_active_keychain_entry:
// best-effort removal of the active-credential Keychain item (macOS only).
func (s *CredentialStore) deleteActiveKeychainEntry() {
	if s.platform != PlatformMacOS {
		return
	}
	if err := Security.DeletePassword(ClaudeCodeKeychainService, KeychainAccountName()); err != nil {
		// best-effort; a down keychain cannot be cleaned now
		_ = err
	}
}

// writeOAuthCredentials mirrors _write_oauth_credentials: writes Claude
// Code's active OAuth credentials. macOS writes the Keychain when usable, then
// rewrites an already-present .credentials.json with the same fresh creds
// (never creating one) so a running session's mtime-based cache invalidation
// fires. If the Keychain write fails, or is already known unusable, it writes
// the plaintext file and best-effort clears any stale Keychain entry.
func (s *CredentialStore) writeOAuthCredentials(credentials string) error {
	if s.useKeychain() {
		if _, err := s.kcCall(func(service, account string) (string, error) {
			return "", Security.SetPassword(service, account, credentials)
		}, ClaudeCodeKeychainService, KeychainAccountName()); err != nil {
			s.logger.Warn("keychain write failed, falling back to file", "error", err)
		} else {
			s.refreshStaleCredentialsFile(credentials)
			s.lastActiveBackend = "keychain"
			return nil
		}
	}

	if err := s.writeActiveCredentialsFile(credentials); err != nil {
		return fmt.Errorf("failed to write credentials: %w: %v", ErrCredentialWrite, err)
	}
	s.deleteActiveKeychainEntry()
	if s.platform == PlatformMacOS {
		// The delete above is best-effort; a stale Keychain item may remain.
		// Pin file mode so a later cooldown re-probe cannot resurrect it.
		s.pinFileMode()
	}
	s.lastActiveBackend = "file"
	return nil
}

// refreshStaleCredentialsFile mirrors _refresh_stale_credentials_file: bumps
// an already-present .credentials.json's mtime after a Keychain write.
// Rewrite-when-present, never-create. Best-effort: the Keychain write is
// authoritative on macOS and already succeeded.
func (s *CredentialStore) refreshStaleCredentialsFile(credentials string) {
	credFile := GetCredentialsPath()
	if !fileExists(credFile) {
		return
	}
	if err := s.writeActiveCredentialsFile(credentials); err != nil {
		s.logger.Warn("could not refresh credentials file after keychain write; "+
			"a running session may not hot-reload until restart", "error", err)
	}
}

// writeActiveCredentialsFile atomically writes Claude Code's plaintext
// active-credentials file (0600).
func (s *CredentialStore) writeActiveCredentialsFile(credentials string) error {
	credDir := GetClaudeConfigHome()
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		return err
	}
	credFile := filepath.Join(credDir, ".credentials.json")
	f, err := os.CreateTemp(credDir, ".tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.WriteString(credentials); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, credFile)
}

// updateGlobalConfig mirrors _update_global_config: atomically applies
// mutator to ~/.claude.json, key-scoped, preserving every other key.
func (s *CredentialStore) updateGlobalConfig(mutator func(map[string]any)) error {
	path := GetGlobalConfigPath()
	data := s.readGlobalConfig()
	if data == nil {
		data = map[string]any{}
	}
	mutator(data)
	return atomicWriteJSON(path, data)
}

// backupEncPath mirrors _backup_enc_path.
func (s *CredentialStore) backupEncPath(accountNum, email string) string {
	return filepath.Join(s.credentialsDir, fmt.Sprintf(".creds-%s-%s.enc", accountNum, email))
}

// backupUsername mirrors _backup_username.
func (s *CredentialStore) backupUsername(accountNum, email string) string {
	return fmt.Sprintf("account-%s-%s", accountNum, email)
}

// usesFileBackupBackend mirrors _uses_file_backup_backend.
func (s *CredentialStore) usesFileBackupBackend() bool {
	return !s.useKeychain()
}

// writeBackupEnc mirrors _write_backup_enc: atomically writes a per-account
// backup .enc (base64) file.
func (s *CredentialStore) writeBackupEnc(accountNum, email, credentials string) error {
	return s.atomicB64Write(s.backupEncPath(accountNum, email), credentials)
}

// atomicB64Write mirrors _atomic_b64_write: atomically writes a
// base64-encoded credential file (0600).
func (s *CredentialStore) atomicB64Write(target, credentials string) error {
	if err := os.MkdirAll(s.credentialsDir, 0o700); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
	f, err := os.CreateTemp(s.credentialsDir, ".tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.WriteString(encoded); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}

// reconcileEncAfterKeychainWrite mirrors _reconcile_enc_after_keychain_write:
// stops a leftover .enc from shadowing a just-written Keychain backup.
// .enc-wins reads make this correctness-critical: delete the .enc; if the
// delete fails, atomically rewrite it with the same fresh creds; if that also
// fails, return an error so the inconsistency surfaces rather than serving
// stale.
func (s *CredentialStore) reconcileEncAfterKeychainWrite(accountNum, email, credentials string) error {
	encFile := s.backupEncPath(accountNum, email)
	if !fileExists(encFile) {
		return nil
	}
	if err := os.Remove(encFile); err == nil {
		return nil
	}
	s.logger.Warn("could not delete .enc after keychain backup write; " +
		"rewriting it with the fresh credentials to keep both consistent")
	return s.writeBackupEnc(accountNum, email, credentials)
}

// readAccountCredentials mirrors _read_account_credentials: "" when missing.
// macOS is .enc-wins (a fallback file beats a possibly-stale Keychain copy);
// only an absent or corrupt .enc falls through to the Keychain. Linux/WSL/
// Windows read the .enc only.
func (s *CredentialStore) readAccountCredentials(accountNum, email string) string {
	encFile := s.backupEncPath(accountNum, email)
	if fileExists(encFile) {
		encoded, err := os.ReadFile(encFile)
		if err == nil {
			decoded, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
			if derr != nil {
				s.logger.Warn("failed to read credentials file", "error", derr)
			} else if len(decoded) > 0 {
				return string(decoded)
			}
			// Empty/whitespace .enc is not a real backup; try the Keychain.
		} else {
			s.logger.Warn("failed to read credentials file", "error", err)
		}
	}
	if s.platform == PlatformMacOS {
		val, err := s.kcReadBackup(accountNum, email)
		if err != nil && !errors.Is(err, ErrKeychainNotFound) {
			s.logger.Warn("failed to read credentials from keychain", "error", err)
		}
		return val
	}
	return ""
}

// kcReadBackup mirrors _kc_read_backup: reads a per-account backup from the
// Keychain only (no file fallback). Returns "" when absent.
func (s *CredentialStore) kcReadBackup(accountNum, email string) (string, error) {
	val, err := s.kcCall(Security.GetPassword, SecurityService, s.backupUsername(accountNum, email))
	if errors.Is(err, ErrKeychainNotFound) {
		return "", nil
	}
	return val, err
}

// writeAccountCredentials mirrors _write_account_credentials: writes account
// credentials to backup (pure I/O, no session invalidation). macOS writes the
// Keychain when usable, then reconciles the .enc away. When the Keychain is
// unusable it writes the .enc atomically, then best-effort deletes any stale
// Keychain copy. Before overwriting, the current generation is retained as a
// .prev file (best-effort, one generation).
func (s *CredentialStore) writeAccountCredentials(accountNum, email, credentials string) error {
	s.retainPreviousBackup(accountNum, email, credentials)

	if s.useKeychain() {
		if _, err := s.kcCall(func(service, account string) (string, error) {
			return "", Security.SetPassword(service, account, credentials)
		}, SecurityService, s.backupUsername(accountNum, email)); err != nil {
			s.logger.Warn("keychain backup write failed, falling back to file", "error", err)
		} else {
			return s.reconcileEncAfterKeychainWrite(accountNum, email, credentials)
		}
	}

	if err := s.writeBackupEnc(accountNum, email, credentials); err != nil {
		s.logger.Warn("failed to write credentials file", "error", err)
		return err
	}
	if s.platform == PlatformMacOS {
		s.deleteBackupKeychainQuiet(accountNum, email)
	}
	return nil
}

// deleteBackupKeychainQuiet mirrors _delete_backup_keychain_quiet:
// best-effort backup Keychain delete (never raises).
func (s *CredentialStore) deleteBackupKeychainQuiet(accountNum, email string) {
	if err := Security.DeletePassword(SecurityService, s.backupUsername(accountNum, email)); err != nil {
		s.logger.Warn("failed to delete credentials from keychain", "error", err)
	}
}

// deleteAccountCredentials mirrors _delete_account_credentials: removes the
// .enc file(s) and, on macOS, the Keychain item(s). Includes the legacy
// account-None-{email} alias.
func (s *CredentialStore) deleteAccountCredentials(accountNum, email string) {
	nums := []string{accountNum}
	if accountNum != "None" {
		nums = append(nums, "None")
	}
	for _, num := range nums {
		encFile := s.backupEncPath(num, email)
		if fileExists(encFile) {
			if err := os.Remove(encFile); err != nil {
				s.logger.Warn("failed to delete credentials file", "error", err)
			}
		}
		prevFile := s.prevBackupPath(num, email)
		if fileExists(prevFile) {
			if err := os.Remove(prevFile); err != nil {
				s.logger.Warn("failed to delete .prev file", "error", err)
			}
		}
		if s.platform == PlatformMacOS {
			s.deleteBackupKeychainQuiet(num, email)
			if err := Security.DeletePassword(SecurityService, s.prevBackupUsername(num, email)); err != nil {
				s.logger.Warn("failed to delete .prev from keychain", "error", err)
			}
		}
	}
}

// prevBackupPath mirrors _prev_backup_path.
func (s *CredentialStore) prevBackupPath(accountNum, email string) string {
	return filepath.Join(s.credentialsDir, fmt.Sprintf(".creds-%s-%s.enc.prev", accountNum, email))
}

// prevBackupUsername mirrors _prev_backup_username.
func (s *CredentialStore) prevBackupUsername(accountNum, email string) string {
	return s.backupUsername(accountNum, email) + ".prev"
}

// retainPreviousBackup mirrors _retain_previous_backup: retains the slot's
// current backup as .prev before it is replaced. Routed by the same rule as
// the backup itself (Keychain when in use, .enc.prev file otherwise), so
// retention never weakens the user's storage posture. No-op when there is no
// current backup or it is unchanged.
func (s *CredentialStore) retainPreviousBackup(accountNum, email, newCredentials string) {
	current := s.readAccountCredentials(accountNum, email)
	if current == "" || current == newCredentials {
		return
	}
	if s.useKeychain() {
		if _, err := s.kcCall(func(service, account string) (string, error) {
			return "", Security.SetPassword(service, account, current)
		}, SecurityService, s.prevBackupUsername(accountNum, email)); err != nil {
			s.logger.Warn("failed to retain previous credential generation", "account", accountNum, "error", err)
		}
		return
	}
	if err := s.atomicB64Write(s.prevBackupPath(accountNum, email), current); err != nil {
		s.logger.Warn("failed to retain previous credential generation", "account", accountNum, "error", err)
	}
}

// readPreviousBackup mirrors _read_previous_backup: reads the retained
// previous generation. "" when absent/corrupt. .enc.prev-wins like the main
// backup read.
func (s *CredentialStore) readPreviousBackup(accountNum, email string) string {
	prevFile := s.prevBackupPath(accountNum, email)
	if fileExists(prevFile) {
		encoded, err := os.ReadFile(prevFile)
		if err == nil {
			decoded, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
			if derr != nil {
				s.logger.Warn("failed to read .prev file", "error", derr)
			} else if len(decoded) > 0 {
				return string(decoded)
			}
		} else {
			s.logger.Warn("failed to read .prev file", "error", err)
		}
	}
	if s.platform == PlatformMacOS {
		val, err := s.kcCall(Security.GetPassword, SecurityService, s.prevBackupUsername(accountNum, email))
		if err != nil && !errors.Is(err, ErrKeychainNotFound) {
			s.logger.Warn("failed to read .prev from keychain", "error", err)
		}
		return val
	}
	return ""
}

// stashManifestPath mirrors _stash_manifest_path.
func (s *CredentialStore) stashManifestPath() string {
	return filepath.Join(s.credentialsDir, ".unclaimed-manifest.json")
}

// stashEntryPath mirrors _stash_entry_path.
func (s *CredentialStore) stashEntryPath(entryID string) string {
	return filepath.Join(s.credentialsDir, fmt.Sprintf(".unclaimed-%s.enc", entryID))
}

// stashManifest is the on-disk shape of the manifest file.
type stashManifest struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Entries       map[string]map[string]any `json:"entries"`
}

// readStashManifest mirrors _read_stash_manifest.
func (s *CredentialStore) readStashManifest() map[string]map[string]any {
	path := s.stashManifestPath()
	if !fileExists(path) {
		return map[string]map[string]any{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		s.logger.Warn("failed to read unclaimed manifest", "error", err)
		return map[string]map[string]any{}
	}
	var m stashManifest
	if err := json.Unmarshal(data, &m); err != nil {
		s.logger.Warn("failed to read unclaimed manifest", "error", err)
		return map[string]map[string]any{}
	}
	if m.Entries == nil {
		return map[string]map[string]any{}
	}
	return m.Entries
}

// writeStashManifest mirrors _write_stash_manifest. A corrupt manifest read
// is never silently clobbered (the rows are classification evidence): it is
// set aside as a .corrupt-<timestamp> file first. Failing closed instead
// would brick switching, since a stash-write failure must abort the switch
// by design, so this path degrades to starting fresh rather than erroring.
func (s *CredentialStore) writeStashManifest(entries map[string]map[string]any) error {
	path := s.stashManifestPath()
	if err := os.MkdirAll(s.credentialsDir, 0o700); err != nil {
		return err
	}
	if fileExists(path) {
		data, rerr := os.ReadFile(path)
		if rerr != nil || !json.Valid(data) {
			aside := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
			if err := os.Rename(path, aside); err != nil {
				s.logger.Warn("could not preserve corrupt unclaimed manifest", "error", err)
			} else {
				s.logger.Warn("unreadable unclaimed manifest preserved", "path", filepath.Base(aside))
			}
		}
	}
	return atomicWriteJSON(path, stashManifest{SchemaVersion: 1, Entries: entries})
}

// writeUnclaimedCredential mirrors _write_unclaimed_credential: stashes a
// credential of unknown provenance. Returns the entry id. The entry file is
// written before the manifest: an entry without manifest metadata is
// recoverable; a manifest row without bytes is not.
func (s *CredentialStore) writeUnclaimedCredential(credentials string, context map[string]any) (string, error) {
	ts := time.Now().UTC().Format("20060102T150405")
	digest := sha256.Sum256([]byte(credentials))
	nonce := make([]byte, 3)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	entryID := fmt.Sprintf("%s-%s-%s", ts, hex.EncodeToString(digest[:])[:12], hex.EncodeToString(nonce))

	if err := s.atomicB64Write(s.stashEntryPath(entryID), credentials); err != nil {
		return "", err
	}
	entries := s.readStashManifest()
	row := map[string]any{"createdAt": time.Now().UTC().Format("2006-01-02T15:04:05Z")}
	maps.Copy(row, context)
	entries[entryID] = row
	if err := s.writeStashManifest(entries); err != nil {
		return "", err
	}
	return entryID, nil
}

// listUnclaimedCredentials mirrors _list_unclaimed_credentials: manifest
// entries by id, including orphaned entry files (no metadata).
func (s *CredentialStore) listUnclaimedCredentials() map[string]map[string]any {
	entries := s.readStashManifest()
	out := make(map[string]map[string]any, len(entries))
	maps.Copy(out, entries)
	matches, err := filepath.Glob(filepath.Join(s.credentialsDir, ".unclaimed-*.enc"))
	if err == nil {
		for _, path := range matches {
			name := filepath.Base(path)
			entryID := strings.TrimSuffix(strings.TrimPrefix(name, ".unclaimed-"), ".enc")
			if _, ok := out[entryID]; !ok {
				out[entryID] = map[string]any{"createdAt": nil}
			}
		}
	}
	return out
}
