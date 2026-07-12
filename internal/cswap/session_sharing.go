package cswap

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SharedItems mirrors SHARED_ITEMS: items mirrored from ~/.claude into
// session profiles when sharing is on.
var SharedItems = []string{"settings.json", "keybindings.json", "CLAUDE.md", "skills", "commands", "agents"}

// HistoryItems mirrors HISTORY_ITEMS: conversation-history items linked
// additionally under --share-history. POSIX symlinks only.
var HistoryItems = []string{"projects", "history.jsonl"}

// ShareManifest mirrors SHARE_MANIFEST.
const ShareManifest = ".cswap-shared.json"

// inStringSlice reports whether target appears in items.
func inStringSlice(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// mkdirPrivate mirrors _mkdir_private: mkdir -p with 0700 at every
// created level (Path.mkdir's mode= only applies to the leaf).
func mkdirPrivate(path string) error {
	var missing []string
	current := path
	for {
		if _, err := os.Stat(current); err == nil {
			break
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

// copyFile copies src to dest, preserving src's permission bits. Used for
// the Windows share mode, where copies stand in for symlinks.
func copyFile(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, info.Mode().Perm())
}

// copyDirRecursive recursively copies src into dest, preserving
// permission bits. Used for the Windows share mode.
func copyDirRecursive(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		destPath := filepath.Join(dest, entry.Name())
		if entry.IsDir() {
			if err := copyDirRecursive(srcPath, destPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, destPath); err != nil {
			return err
		}
	}
	return nil
}

// removeManaged mirrors _remove_managed: removes a cswap-created share
// entry (link or copy), never user data beyond it. Callers guarantee dest
// is manifest-listed or a symlink.
func removeManaged(dest string) {
	info, err := os.Lstat(dest)
	if err != nil {
		return
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().IsRegular() {
		_ = os.Remove(dest)
		return
	}
	if info.IsDir() {
		_ = os.RemoveAll(dest)
	}
}

// readManifest mirrors _read_manifest.
func readManifest(manifestPath string) []string {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil
	}
	var parsed struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	var result []string
	for _, item := range parsed.Items {
		if inStringSlice(SharedItems, item) || inStringSlice(HistoryItems, item) {
			result = append(result, item)
		}
	}
	return result
}

// writeManifest mirrors _write_manifest.
func writeManifest(s *ClaudeAccountSwitcher, manifestPath string, items []string) {
	mode := "symlink"
	if s.Platform == PlatformWindows {
		mode = "copy"
	}
	if items == nil {
		items = []string{}
	}
	payload, err := json.MarshalIndent(map[string]any{"items": items, "mode": mode}, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(manifestPath)
	f, err := os.CreateTemp(dir, ".cswap-shared-*.tmp")
	if err != nil {
		return
	}
	tmp := f.Name()
	if _, err := f.Write(payload); err != nil {
		f.Close()
		os.Remove(tmp)
		return
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, manifestPath); err != nil {
		os.Remove(tmp)
	}
}

// mergeHistoryIntoSource mirrors _merge_history_into_source: moves the
// profile's own history at dest into src. Directories merge file-by-file
// (transcript filenames are UUIDs, so collisions mean identical sessions,
// first writer wins and the duplicate is dropped). history.jsonl merges
// by appending lines not already present.
func mergeHistoryIntoSource(src, dest string) error {
	info, err := os.Stat(dest)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := mkdirPrivate(src); err != nil {
			return err
		}
		var paths []string
		if err := filepath.WalkDir(dest, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path != dest {
				paths = append(paths, path)
			}
			return nil
		}); err != nil {
			return err
		}
		sort.Sort(sort.Reverse(sort.StringSlice(paths)))
		for _, path := range paths {
			rel, err := filepath.Rel(dest, path)
			if err != nil {
				return err
			}
			target := filepath.Join(src, rel)
			fi, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if fi.IsDir() {
				if err := os.Remove(path); err != nil {
					return err
				}
				continue
			}
			if _, err := os.Stat(target); err == nil {
				if err := os.Remove(path); err != nil {
					return err
				}
				continue
			}
			if err := mkdirPrivate(filepath.Dir(target)); err != nil {
				return err
			}
			if err := os.Rename(path, target); err != nil {
				return err
			}
		}
		return os.Remove(dest)
	}

	existing := map[string]bool{}
	if data, err := os.ReadFile(src); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if line != "" {
				existing[line] = true
			}
		}
	}
	destData, err := os.ReadFile(dest)
	if err != nil {
		return err
	}
	var lines []string
	for _, line := range strings.Split(string(destData), "\n") {
		if line != "" && !existing[line] {
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 {
		if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
			return err
		}
		if _, err := os.Stat(src); err != nil {
			f, err := os.OpenFile(src, os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			f.Close()
		}
		f, err := os.OpenFile(src, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
			return err
		}
	}
	return os.Remove(dest)
}

// prepareHistoryShare mirrors _prepare_history_share: makes a history
// item linkable; returns false to skip it this launch.
func prepareHistoryShare(s *ClaudeAccountSwitcher, src, dest, sessionDir string) bool {
	destInfo, destErr := os.Lstat(dest)
	destExists := destErr == nil
	destIsSymlink := destExists && destInfo.Mode()&os.ModeSymlink != 0

	if destExists && !destIsSymlink {
		// Merging moves files out from under any claude still running in
		// this profile, so only migrate when the profile is quiescent.
		if len(LiveSessionsFor(sessionDir)) > 0 {
			fmt.Println(Dimmed(fmt.Sprintf("Not sharing %s yet: another session is using this profile, retrying on the next launch.", filepath.Base(dest))))
			return false
		}
		if err := mergeHistoryIntoSource(src, dest); err != nil {
			s.Logger.Warn("could not merge history into source", "dest", dest, "src", src, "error", err)
			fmt.Println(Dimmed(fmt.Sprintf("Not sharing %s: merging the profile's existing history failed (see log).", filepath.Base(dest))))
			return false
		}
		fmt.Println(Dimmed(fmt.Sprintf("Merged the profile's existing %s into %s, conversation history is now shared.", filepath.Base(dest), src)))
	}
	if _, err := os.Stat(src); err != nil {
		// Fresh ~/.claude (or first run): seed an empty share target so
		// the generic loop below has something to link.
		if strings.HasSuffix(dest, ".jsonl") {
			if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
				s.Logger.Warn("could not create history source", "src", src, "error", err)
				return false
			}
			f, err := os.OpenFile(src, os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				s.Logger.Warn("could not create history source", "src", src, "error", err)
				return false
			}
			f.Close()
		} else {
			if err := mkdirPrivate(src); err != nil {
				s.Logger.Warn("could not create history source", "src", src, "error", err)
				return false
			}
		}
	}
	return true
}

// syncSharing mirrors SessionManager._sync_sharing: mirrors shared items
// from ~/.claude into the profile (or undoes it). share governs
// SharedItems (customizations); shareHistory governs HistoryItems
// (conversation history), independent concerns. Idempotent; runs on every
// launch. Deliberately sources from the default ~/.claude (homeDir(), not
// GetClaudeConfigHome()): sharing always mirrors the default profile,
// even when CLAUDE_CONFIG_DIR is set in the invoking environment.
func syncSharing(s *ClaudeAccountSwitcher, sessionDir string, share, shareHistory bool) {
	info, err := os.Stat(sessionDir)
	if err != nil || !info.IsDir() {
		return
	}
	// History links are POSIX-only (Run rejects the flag on Windows; this
	// also drops any links left by a POSIX-to-Windows profile move).
	if s.Platform == PlatformWindows {
		shareHistory = false
	}
	var activeItems []string
	if share {
		activeItems = append(activeItems, SharedItems...)
	}
	if shareHistory {
		activeItems = append(activeItems, HistoryItems...)
	}
	sourceRoot := filepath.Join(homeDir(), ".claude")
	manifestPath := filepath.Join(sessionDir, ShareManifest)
	managed := readManifest(manifestPath)

	// A flag turned off since last launch: remove the links we created for
	// it (never plain files/dirs the user accumulated themselves). For
	// history items that holds even when the manifest claims them: a
	// stale manifest (lock-free launches race) must never delete real
	// conversation history, only ever unlink symlinks.
	for _, name := range managed {
		if !inStringSlice(activeItems, name) {
			dest := filepath.Join(sessionDir, name)
			if inStringSlice(HistoryItems, name) {
				if info, err := os.Lstat(dest); err == nil && info.Mode()&os.ModeSymlink == 0 {
					continue
				}
			}
			removeManaged(dest)
		}
	}
	if len(activeItems) == 0 {
		os.Remove(manifestPath)
		return
	}

	useSymlinks := s.Platform != PlatformWindows
	var newManaged []string

	for _, name := range activeItems {
		src := filepath.Join(sourceRoot, name)
		dest := filepath.Join(sessionDir, name)

		if inStringSlice(HistoryItems, name) {
			if !prepareHistoryShare(s, src, dest, sessionDir) {
				continue
			}
		}

		if _, err := os.Stat(src); err != nil {
			if inStringSlice(managed, name) {
				removeManaged(dest)
			}
			continue
		}

		destInfo, destErr := os.Lstat(dest)
		destExists := destErr == nil
		isSymlink := destExists && destInfo.Mode()&os.ModeSymlink != 0

		if isSymlink {
			if !inStringSlice(managed, name) {
				managed = append(managed, name)
			}
			if useSymlinks {
				target, readErr := os.Readlink(dest)
				if readErr != nil || target != src {
					if err := os.Remove(dest); err != nil {
						continue
					}
					if err := os.Symlink(src, dest); err != nil {
						continue
					}
				}
				newManaged = append(newManaged, name)
				continue
			}
			// Platform moved POSIX to Windows: replace link with a copy.
			os.Remove(dest)
		} else if destExists && !inStringSlice(managed, name) {
			// Pre-existing user data in the profile: never touch it.
			fmt.Println(Dimmed(fmt.Sprintf("Not sharing %s: the session profile already has its own copy.", name)))
			continue
		}

		var opErr error
		if useSymlinks {
			if destExists {
				removeManaged(dest)
			}
			opErr = os.Symlink(src, dest)
		} else {
			if destExists {
				removeManaged(dest)
			}
			srcInfo, statErr := os.Stat(src)
			if statErr != nil {
				opErr = statErr
			} else if srcInfo.IsDir() {
				opErr = copyDirRecursive(src, dest)
			} else {
				opErr = copyFile(src, dest)
			}
		}
		if opErr != nil {
			s.Logger.Warn("failed to share into session", "item", name, "error", opErr)
			continue
		}
		newManaged = append(newManaged, name)
	}

	writeManifest(s, manifestPath, newManaged)
}
