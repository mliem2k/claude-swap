//go:build !windows

package cswap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceExecutableUnix(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "cswap")
	if err := os.WriteFile(currentPath, []byte("old content"), 0o755); err != nil {
		t.Fatal(err)
	}

	newBinDir := t.TempDir() // deliberately a different directory/filesystem-ish path
	newBinPath := filepath.Join(newBinDir, "cswap-new")
	if err := os.WriteFile(newBinPath, []byte("new content"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := replaceExecutable(newBinPath, currentPath); err != nil {
		t.Fatalf("replaceExecutable: %v", err)
	}

	got, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new content" {
		t.Errorf("currentPath content = %q, want %q", got, "new content")
	}
	info, err := os.Stat(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("replaced executable is not executable: mode %v", info.Mode())
	}
	if _, err := os.Stat(newBinPath); !os.IsNotExist(err) {
		t.Errorf("source temp file at %s should have been consumed by the replace, still exists", newBinPath)
	}
}

func TestReplaceExecutableUnixPermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, permission-denied case can't be exercised")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil { // read+execute, no write
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) // restore so t.TempDir() cleanup can remove it
	currentPath := filepath.Join(dir, "cswap")

	newBinDir := t.TempDir()
	newBinPath := filepath.Join(newBinDir, "cswap-new")
	if err := os.WriteFile(newBinPath, []byte("new content"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := replaceExecutable(newBinPath, currentPath)
	if err == nil {
		t.Fatal("expected a permission error, got nil")
	}
}
