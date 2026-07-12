package cswap

import (
	"fmt"
	"os"
	"path/filepath"
)

// LegacyBackupDirname mirrors LEGACY_BACKUP_DIRNAME.
const LegacyBackupDirname = ".claude-swap-backup"

// GetClaudeConfigHome mirrors get_claude_config_home.
func GetClaudeConfigHome() string {
	if env := os.Getenv("CLAUDE_CONFIG_DIR"); env != "" {
		return env
	}
	return filepath.Join(homeDir(), ".claude")
}

// GetGlobalConfigPath mirrors get_global_config_path. Legacy
// <config_home>/.config.json wins if it exists; otherwise the env-or-home
// .claude.json. Note the asymmetry: .claude.json sits at the env/home base,
// not inside the config home.
func GetGlobalConfigPath() string {
	legacy := filepath.Join(GetClaudeConfigHome(), ".config.json")
	if fileExists(legacy) {
		return legacy
	}
	base := homeDir()
	if env := os.Getenv("CLAUDE_CONFIG_DIR"); env != "" {
		base = env
	}
	return filepath.Join(base, ".claude.json")
}

// GetCredentialsPath mirrors get_credentials_path.
func GetCredentialsPath() string {
	return filepath.Join(GetClaudeConfigHome(), ".credentials.json")
}

// GetLegacyBackupRoot mirrors get_legacy_backup_root.
func GetLegacyBackupRoot() string {
	return filepath.Join(homeDir(), LegacyBackupDirname)
}

// GetBackupRoot mirrors get_backup_root. Linux/WSL follow XDG; macOS, Windows,
// and unknown use the legacy layout. Per the XDG spec, XDG_DATA_HOME is
// ignored when unset, empty, or non-absolute. A leading ~ is expanded.
func GetBackupRoot() string {
	if DetectPlatform() == PlatformLinux || DetectPlatform() == PlatformWSL {
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg != "" {
			expanded := expandHome(xdg)
			if filepath.IsAbs(expanded) {
				return filepath.Join(expanded, "claude-swap")
			}
		}
		return filepath.Join(homeDir(), ".local", "share", "claude-swap")
	}
	return GetLegacyBackupRoot()
}

// fileExists is true if path exists (any type).
func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// homeDir returns $HOME, or the best OS fallback. Tests override via HOME.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.Getenv("HOME")
}

// expandHome replaces a leading ~ with $HOME (for XDG values set without
// shell expansion in systemd units or Dockerfiles).
func expandHome(path string) string {
	if path == "~" {
		return homeDir()
	}
	if len(path) >= 2 && path[0] == '~' && (path[1] == '/' || path[1] == '\\') {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}

var (
	throwawayNames    = map[string]bool{"cache": true}
	throwawayPrefixes = []string{"claude-swap.log"}
)

// MigrateLegacyBackupDir mirrors migrate_legacy_backup_dir. Moves the legacy
// backup directory to target if needed, guarded by a <target>.migrating flag
// file so an interrupted run is resumable and a foreign collision is refused.
// Returns true if the move ran in this call, false if it was a no-op.
// Returns an error wrapping ErrMigration on a genuine collision or a move
// failure.
func MigrateLegacyBackupDir(target string) (bool, error) {
	legacy := GetLegacyBackupRoot()
	if samePath(legacy, target) {
		return false, nil
	}
	flag := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".migrating")

	if !fileExists(legacy) {
		// Successful prior run that died before unlinking the flag.
		_ = os.Remove(flag)
		return false, nil
	}

	if fileExists(flag) {
		// Prior run was interrupted. Discard partial target and retry.
		if fileExists(target) {
			_ = os.RemoveAll(target)
		}
	} else if fileExists(target) {
		if targetHasMeaningfulData(target) {
			return false, fmt.Errorf("both legacy (%s) and new (%s) backup paths exist: %w",
				legacy, target, ErrMigration)
		}
		wipeThrowawayArtifacts(target)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return false, fmt.Errorf("mkdir parent: %w: %v", ErrMigration, err)
	}
	if err := os.WriteFile(flag, nil, 0o600); err != nil {
		return false, fmt.Errorf("write flag: %w: %v", ErrMigration, err)
	}
	if err := moveDir(legacy, target); err != nil {
		return false, fmt.Errorf("move %s -> %s: %w: %v", legacy, target, ErrMigration, err)
	}
	_ = os.Remove(flag)
	return true, nil
}

func targetHasMeaningfulData(target string) bool {
	entries, err := os.ReadDir(target)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if throwawayNames[name] {
			continue
		}
		matched := false
		for _, p := range throwawayPrefixes {
			if len(name) >= len(p) && name[:len(p)] == p {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		return true
	}
	return false
}

func wipeThrowawayArtifacts(target string) {
	entries, err := os.ReadDir(target)
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(target, e.Name()))
	}
	_ = os.Remove(target)
}

// moveDir mirrors shutil.move: atomic rename on the same filesystem, copy
// plus unlink across filesystems (EXDEV falls back to a recursive copy).
func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return copyTree(src, dst)
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return aa == bb
}
