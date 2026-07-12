package cswap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SessionManager mirrors SessionManager: bootstraps per-account session
// profiles and launches Claude into them.
type SessionManager struct {
	Switcher    *ClaudeAccountSwitcher
	SessionsDir string
}

// NewSessionManager mirrors SessionManager.__init__.
func NewSessionManager(switcher *ClaudeAccountSwitcher) *SessionManager {
	return &SessionManager{
		Switcher:    switcher,
		SessionsDir: filepath.Join(switcher.BackupDir, "sessions"),
	}
}

// bootstrapLockTimeout mirrors _BOOTSTRAP_LOCK_TIMEOUT: bootstrap holds
// the backup-dir lock across one token refresh (10s network timeout) plus
// auth-status probes, so it needs more headroom than the default 10s
// acquire used by the switch paths.
const bootstrapLockTimeout = 30 * time.Second

// execClaudeFn is a test seam: real callers use execClaude (platform-
// specific, session_exec_unix.go/session_exec_windows.go); tests swap in
// a fake that never actually replaces or spawns a process.
var execClaudeFn = execClaude

// ensureNotAPIKey mirrors SessionManager._ensure_not_api_key: rejects
// API-key accounts in session mode (not supported yet). Session bootstrap
// is OAuth-shaped, it seeds .credentials.json and isSessionValid requires
// authMethod == "claude.ai", so an API-key account would otherwise fail
// validation opaquely.
func (m *SessionManager) ensureNotAPIKey(accountNum, email string) error {
	if m.Switcher.AccountKind(accountNum) == "api_key" {
		return fmt.Errorf("account-%s (%s) is an API-key account; 'cswap run' (session mode) does not support API-key accounts yet. Use 'cswap switch' to make it your default login instead: %w", accountNum, email, ErrSession)
	}
	return nil
}

// Run mirrors SessionManager.run: launches Claude Code as the given
// account in the current terminal. On POSIX this execs claude and never
// returns on success; on Windows it exits with claude's return code.
func (m *SessionManager) Run(identifier string, claudeArgs []string, share, shareHistory bool) error {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("'claude' was not found on PATH. Install Claude Code first: %w", ErrSession)
	}
	if shareHistory && m.Switcher.Platform == PlatformWindows {
		return fmt.Errorf("--share-history is not supported on Windows yet: sharing uses re-synced copies there, which would fork the history instead of sharing it: %w", ErrSession)
	}

	accountNum, email, orgUUID, err := m.Switcher.ResolveAccount(identifier)
	if err != nil {
		return err
	}
	// Guard before the same-account direct-launch fast path below (which
	// execs claude and never returns) and before SetupSession.
	if err := m.ensureNotAPIKey(accountNum, email); err != nil {
		return err
	}

	if configDirPreset := os.Getenv("CLAUDE_CONFIG_DIR"); configDirPreset != "" {
		// With CLAUDE_CONFIG_DIR set, "current default account" is
		// meaningless (we may already be inside a session terminal), so
		// the same-account fast path below must not trigger.
		Warning(fmt.Sprintf("CLAUDE_CONFIG_DIR is already set (%s); overriding it for this launch.", configDirPreset))
	} else {
		// Same-account fast path: never create a second credential copy
		// for the account that is already the active default login, two
		// copies of one account can drift if the server rotates the
		// refresh token.
		currentEmail, currentOrgUUID, hasCurrent := m.Switcher.GetCurrentAccount()
		if hasCurrent && currentEmail == email && currentOrgUUID == orgUUID {
			fmt.Println(Dimmed(fmt.Sprintf("Account-%s (%s) is already the active default login, launching claude directly.", accountNum, email)))
			return execClaudeFn(claudeBin, append([]string{claudeBin}, claudeArgs...), os.Environ())
		}
	}

	var scrubbed []string
	for _, v := range authOverrideEnvVars {
		if os.Getenv(v) != "" {
			scrubbed = append(scrubbed, v)
		}
	}
	if len(scrubbed) > 0 {
		Warning(fmt.Sprintf("Ignoring %s for this session, it would override the selected account inside Claude Code.", strings.Join(scrubbed, ", ")))
	}

	sessionDir, accountNum, email, err := m.SetupSession(identifier, share, shareHistory)
	if err != nil {
		return err
	}

	fmt.Printf("%s Account-%s (%s) %s\n", Accent("Launching"), accountNum, email, Muted("[session mode]"))
	// Drop any inherited CLAUDE_CONFIG_DIR before appending the session
	// dir's value, mirroring Python's dict assignment (a single
	// overwritten key). This is load-bearing on POSIX: syscall.Exec's
	// underlying execve passes the env array through with no dedup, so
	// the exec'd claude reads the FIRST matching key, unlike Go's
	// exec.Cmd.Env (last value wins), which would have masked this.
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if key == "CLAUDE_CONFIG_DIR" {
			continue
		}
		dropped := false
		for _, override := range authOverrideEnvVars {
			if key == override {
				dropped = true
				break
			}
		}
		if !dropped {
			env = append(env, kv)
		}
	}
	env = append(env, "CLAUDE_CONFIG_DIR="+sessionDir)
	return execClaudeFn(claudeBin, append([]string{claudeBin}, claudeArgs...), env)
}

// SetupSession mirrors SessionManager.setup_session: ensures a valid
// session profile exists, returns (dir, num, email).
func (m *SessionManager) SetupSession(identifier string, share, shareHistory bool) (sessionDir, accountNum, email string, err error) {
	accountNum, email, orgUUID, err := m.Switcher.ResolveAccount(identifier)
	if err != nil {
		return "", "", "", err
	}
	// Defense-in-depth: also guard here (Run guards before its fast path).
	if err := m.ensureNotAPIKey(accountNum, email); err != nil {
		return "", "", "", err
	}
	sessionDir = SessionDirFor(m.Switcher.BackupDir, accountNum, email)

	// Deferred invalidation: backup credentials changed while this
	// profile was live, so its credentials are presumed stale even if
	// they still pass the local reuse check. Honored only when no session
	// is live, a second `cswap run` joining a live session must not
	// invalidate under the running claude (the marker survives for
	// later).
	stalePath := filepath.Join(sessionDir, StaleMarker)
	_, staleStatErr := os.Stat(stalePath)
	stale := staleStatErr == nil && len(LiveSessionsFor(sessionDir)) == 0

	// Cheap reuse check without the lock: most launches hit this.
	if !stale && isSessionValid(sessionDir, email, orgUUID) {
		syncSharing(m.Switcher, sessionDir, share, shareHistory)
		return sessionDir, accountNum, email, nil
	}

	lock := NewFileLock(m.Switcher.LockFile, bootstrapLockTimeout)
	if lockErr := lock.Lock(bootstrapLockTimeout); lockErr != nil {
		return "", "", "", lockErr
	}
	var bootstrapErr error
	func() {
		defer lock.Release()

		// Re-evaluate the marker under the lock, then re-check validity:
		// another `cswap run` may have bootstrapped while we waited.
		if _, statErr := os.Stat(stalePath); statErr == nil && len(LiveSessionsFor(sessionDir)) == 0 {
			m.Switcher.InvalidateSessionCredentials(accountNum, email)
			os.Remove(stalePath)
		}
		if isSessionValid(sessionDir, email, orgUUID) {
			syncSharing(m.Switcher, sessionDir, share, shareHistory)
			return
		}

		if err := bootstrap(m.Switcher, sessionDir, accountNum, email, orgUUID); err != nil {
			bootstrapErr = err
			return
		}
		syncSharing(m.Switcher, sessionDir, share, shareHistory)

		if !isSessionValid(sessionDir, email, orgUUID) {
			cleanupFailedSession(sessionDir)
			bootstrapErr = fmt.Errorf("session profile for Account-%s (%s) failed validation. Log in with that account and re-add it: cswap add --slot %s: %w", accountNum, email, accountNum, ErrSession)
		}
	}()
	// Lock released here, before any exec.
	if bootstrapErr != nil {
		return "", "", "", bootstrapErr
	}

	return sessionDir, accountNum, email, nil
}
