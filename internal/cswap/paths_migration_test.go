package cswap

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// legacyAt sets HOME to a fresh temp root and returns the legacy backup root
// path under it (root/.claude-swap-backup), matching GetLegacyBackupRoot().
func legacyAt(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	return GetLegacyBackupRoot()
}

func TestMigrateLegacyMovesIntoTarget(t *testing.T) {
	legacy := legacyAt(t)
	target := filepath.Join(filepath.Dir(legacy), "new")
	mustMkdir(t, legacy)
	mustWrite(t, filepath.Join(legacy, "sequence.json"), "{}")

	ran, err := MigrateLegacyBackupDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("expected migration to run")
	}
	got, rerr := os.ReadFile(filepath.Join(target, "sequence.json"))
	if rerr != nil {
		t.Fatalf("target missing migrated file: %v", rerr)
	}
	if string(got) != "{}" {
		t.Fatalf("unexpected content: %s", got)
	}
	if fileExists(legacy) {
		t.Fatal("legacy dir should be gone after move")
	}
}

func TestMigrateLegacyRefusesCollisionWithData(t *testing.T) {
	legacy := legacyAt(t)
	target := filepath.Join(filepath.Dir(legacy), "new")
	mustMkdir(t, legacy)
	mustMkdir(t, target)
	mustWrite(t, filepath.Join(legacy, "sequence.json"), "{}")
	mustWrite(t, filepath.Join(target, "sequence.json"), "{\"x\":1}")

	_, err := MigrateLegacyBackupDir(target)
	if !errors.Is(err, ErrMigration) {
		t.Fatalf("expected ErrMigration on collision, got %v", err)
	}
}

func TestMigrateLegacyWipesThrowawayArtifacts(t *testing.T) {
	legacy := legacyAt(t)
	target := filepath.Join(filepath.Dir(legacy), "new")
	mustMkdir(t, legacy)
	mustMkdir(t, target)
	mustMkdir(t, filepath.Join(target, "cache")) // throwaway
	mustWrite(t, filepath.Join(target, "claude-swap.log.1"), "x")
	mustWrite(t, filepath.Join(legacy, "sequence.json"), "{}")

	ran, err := MigrateLegacyBackupDir(target)
	if err != nil {
		t.Fatalf("expected ok with throwaway-only target, got %v", err)
	}
	if !ran {
		t.Fatal("expected migration to run")
	}
	if !fileExists(filepath.Join(target, "sequence.json")) {
		t.Fatal("migrated file should be present")
	}
}

func TestMigrateLegacyNoopWhenSamePath(t *testing.T) {
	legacy := legacyAt(t)
	mustMkdir(t, legacy)
	ran, err := MigrateLegacyBackupDir(legacy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ran {
		t.Fatal("migrating legacy onto itself should be a no-op")
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
