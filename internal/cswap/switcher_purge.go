package cswap

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PurgeResult is what Purge returns. Cancelled distinguishes "the user
// declined" from "it ran and found nothing" (both would otherwise leave
// RemovedItems empty), and from a real error, which Purge returns via its
// second return value instead.
type PurgeResult struct {
	Cancelled    bool
	RemovedItems []string
}

// Purge mirrors purge: removes all traces of claude-swap from the system.
// This removes all stored account credentials (.enc files on Linux/WSL/
// Windows; on macOS both the Keychain items and any fallback .enc files),
// plus a best-effort sweep of pre-migration entries; the active backup
// directory; and any stale legacy backup directory left from before the
// XDG migration. Never prints or reads stdin directly: confirm receives the
// full descriptive preamble Python prints before its input() call, joined
// with the final question, matching this port's AddAccount/RemoveAccount
// confirm-callback convention. The legacy keyring sweep
// (_sweep_legacy_keyring in Python) has no Go equivalent: this port never
// depended on the third-party keyring library it cleans up after (see the
// design doc's documented divergence). Individual deletion failures below
// (a single .enc file, a single Keychain item) are intentionally swallowed
// rather than aborting the whole purge: this mirrors Python's own "except
// Exception: pass # Ignore errors during purge" around each delete, on the
// theory that a best-effort teardown should remove everything it can rather
// than stopping at the first already-gone or permission-denied item. This
// is a deliberate, Python-mirrored choice, not an oversight.
func (s *ClaudeAccountSwitcher) Purge(confirm func(string) bool) (*PurgeResult, error) {
	legacy := GetLegacyBackupRoot()
	legacyDistinct := legacy != s.BackupDir

	// Refuse while any session-mode claude is running: purging would pull
	// its profile (and keychain entry) out from under a live process.
	sessionsRoot := filepath.Join(s.BackupDir, "sessions")
	var sessionDirs []string
	if entries, err := os.ReadDir(sessionsRoot); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				sessionDirs = append(sessionDirs, filepath.Join(sessionsRoot, e.Name()))
			}
		}
	}
	live := map[string][]int{}
	for _, dir := range sessionDirs {
		var pids []int
		for _, sess := range LiveSessionsFor(dir) {
			pids = append(pids, sess.PID)
		}
		if len(pids) > 0 {
			live[filepath.Base(dir)] = pids
		}
	}
	if len(live) > 0 {
		details := ""
		first := true
		for name, pids := range live {
			if !first {
				details += "; "
			}
			first = false
			pidStrs := make([]string, len(pids))
			for i, p := range pids {
				pidStrs[i] = strconv.Itoa(p)
			}
			details += fmt.Sprintf("%s (PID %s)", name, strings.Join(pidStrs, ", "))
		}
		return nil, fmt.Errorf("live session-mode Claude instance(s) found: %s. Exit them first, then retry purge: %w", details, ErrSession)
	}

	var preamble []string
	preamble = append(preamble, "This will remove ALL claude-swap data from your system:")
	preamble = append(preamble, fmt.Sprintf("  - Backup directory: %s", s.BackupDir))
	if legacyDistinct && fileExists(legacy) {
		preamble = append(preamble, fmt.Sprintf("  - Legacy backup directory: %s", legacy))
	}
	if s.Platform == PlatformMacOS {
		preamble = append(preamble, "  - All stored account credentials (macOS Keychain and/or files)")
	} else {
		preamble = append(preamble, "  - All stored account credential files")
	}
	if len(sessionDirs) > 0 {
		preamble = append(preamble, "  - All session profiles and their Keychain entries")
	}
	preamble = append(preamble, "", "Note: This does NOT affect your current Claude Code login.", "",
		"Are you sure you want to purge all data?")
	prompt := ""
	for i, line := range preamble {
		if i > 0 {
			prompt += "\n"
		}
		prompt += line
	}

	if confirm != nil && !confirm(prompt) {
		return &PurgeResult{Cancelled: true}, nil
	}

	var removedItems []string

	// Remove credentials. On macOS backups may be in the Keychain and/or
	// .enc files (auto-fallback), so clean both; Linux/WSL/Windows are
	// file-only.
	if data := s.GetSequenceData(); data != nil {
		for accountNum, info := range data.Accounts {
			nums := []string{accountNum}
			if accountNum != "None" {
				nums = append(nums, "None")
			}

			for _, num := range nums {
				credFile := s.Store.backupEncPath(num, info.Email)
				if fileExists(credFile) {
					if err := os.Remove(credFile); err == nil {
						removedItems = append(removedItems, fmt.Sprintf("Credential file: %s", filepath.Base(credFile)))
					}
				}
			}

			if s.Platform == PlatformMacOS {
				for _, num := range nums {
					username := s.Store.backupUsername(num, info.Email)
					if err := DeletePassword(SecurityService, username); err == nil {
						removedItems = append(removedItems, fmt.Sprintf("Credential: %s", username))
					}
				}
			}
			// A pre-migration keyring / Windows Credential Manager sweep
			// would run here in Python; this port has no keyring dependency
			// to sweep (see the design doc's documented divergence).
		}
	}

	// Session-profile keychain entries must go before the backup dir: the
	// hashed service names are derived from the dir paths and can't be
	// recomputed once the directories are deleted.
	if len(sessionDirs) > 0 {
		names := make([]string, 0, len(sessionDirs))
		for _, dir := range sessionDirs {
			DeleteMacOSKeychainEntry(dir)
			names = append(names, filepath.Base(dir))
		}
		joined := ""
		for i, n := range names {
			if i > 0 {
				joined += ", "
			}
			joined += n
		}
		removedItems = append(removedItems, fmt.Sprintf("Session profiles: %s", joined))
	}

	// Remove backup directory. Close the log file first (required on
	// Windows: an open handle blocks directory removal).
	if fileExists(s.BackupDir) {
		if s.logFile != nil {
			_ = s.logFile.Close()
		}
		if err := os.RemoveAll(s.BackupDir); err == nil {
			removedItems = append(removedItems, fmt.Sprintf("Directory: %s", s.BackupDir))
		}
	}

	// Also clean a stale legacy directory if it somehow still exists (e.g.
	// a partial pre-migration state, or files re-created after init).
	if legacyDistinct && fileExists(legacy) {
		if err := os.RemoveAll(legacy); err == nil {
			removedItems = append(removedItems, fmt.Sprintf("Legacy directory: %s", legacy))
		}
	}

	return &PurgeResult{RemovedItems: removedItems}, nil
}
