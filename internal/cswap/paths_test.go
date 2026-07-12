package cswap

import (
	"path/filepath"
	"testing"
)

func TestGetClaudeConfigHomeEnvOverride(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/custom-claude")
	got := GetClaudeConfigHome()
	if got != "/tmp/custom-claude" {
		t.Fatalf("expected env override, got %s", got)
	}
}

func TestGetCredentialsPathUnderConfigHome(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/custom-claude")
	got := GetCredentialsPath()
	want := filepath.Join("/tmp/custom-claude", ".credentials.json")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestGetGlobalConfigPathPrefersLegacyIfExists(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmp)
	// Legacy .config.json absent: with CLAUDE_CONFIG_DIR set, base is the env dir.
	got := GetGlobalConfigPath()
	want := filepath.Join(tmp, ".claude.json")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestGetBackupRootXDG(t *testing.T) {
	// Force the Linux branch and an absolute XDG_DATA_HOME.
	old := runtimeGOOS
	runtimeGOOS = func() string { return "linux" }
	defer func() { runtimeGOOS = old }()
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdgdata")
	got := GetBackupRoot()
	want := filepath.Join("/tmp/xdgdata", "claude-swap")
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestGetBackupRootLinuxDefault(t *testing.T) {
	old := runtimeGOOS
	runtimeGOOS = func() string { return "linux" }
	defer func() { runtimeGOOS = old }()
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("XDG_DATA_HOME", "")
	got := GetBackupRoot()
	if !filepath.IsAbs(got) {
		t.Fatalf("backup root should be absolute: %s", got)
	}
	// Default Linux path is <home>/.local/share/claude-swap.
	if filepath.Base(got) != "claude-swap" {
		t.Fatalf("unexpected base: %s", got)
	}
}
