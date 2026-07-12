package cswap

import (
	"encoding/base64"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteUnclaimedCredentialCreatesEntryAndManifest(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	dir := t.TempDir()
	s := NewCredentialStore(PlatformLinux, dir, slog.Default())

	creds := `{"claudeAiOauth":{"accessToken":"stray"}}`
	id, err := s.writeUnclaimedCredential(creds, map[string]any{"reason": "classified outgoing"})
	if err != nil {
		t.Fatal(err)
	}
	// Entry file exists and is base64 (not plaintext).
	entry := s.stashEntryPath(id)
	data, rerr := os.ReadFile(entry)
	if rerr != nil {
		t.Fatalf("entry file missing: %v", rerr)
	}
	if strings.Contains(string(data), "stray") {
		t.Fatal("entry must be base64-encoded, not plaintext")
	}
	decoded, derr := base64.StdEncoding.DecodeString(string(data))
	if derr != nil || string(decoded) != creds {
		t.Fatalf("entry should decode back to original creds: %v %q", derr, decoded)
	}
	// Manifest lists the entry with its context.
	listed := s.listUnclaimedCredentials()
	row, ok := listed[id]
	if !ok {
		t.Fatalf("manifest should list %s, got %v", id, listed)
	}
	if row["reason"] != "classified outgoing" {
		t.Fatalf("context not preserved: %v", row)
	}
	if _, ok := row["createdAt"]; !ok {
		t.Fatalf("createdAt should be recorded: %v", row)
	}
}

func TestListUnclaimedIncludesOrphanedEntries(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	dir := t.TempDir()
	s := NewCredentialStore(PlatformLinux, dir, slog.Default())
	// An orphan entry file with no manifest row.
	orphan := filepath.Join(dir, ".unclaimed-orphan.enc")
	mustWrite(t, orphan, base64.StdEncoding.EncodeToString([]byte("bytes")))
	listed := s.listUnclaimedCredentials()
	if _, ok := listed["orphan"]; !ok {
		t.Fatalf("orphan should be listed, got %v", listed)
	}
}

func TestWriteUnclaimedCredentialUniqueIdsForSameBytes(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	s := NewCredentialStore(PlatformLinux, t.TempDir(), slog.Default())

	creds := `{"same":"bytes"}`
	id1, err := s.writeUnclaimedCredential(creds, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.writeUnclaimedCredential(creds, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Fatal("identical bytes preserved twice must still get distinct ids")
	}
	listed := s.listUnclaimedCredentials()
	if len(listed) != 2 {
		t.Fatalf("expected 2 manifest rows, got %d: %v", len(listed), listed)
	}
}

func TestCorruptManifestIsSetAsideNotClobbered(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	dir := t.TempDir()
	s := NewCredentialStore(PlatformLinux, dir, slog.Default())

	manifestPath := s.stashManifestPath()
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, manifestPath, "{not valid json")

	// Writing a new unclaimed credential must not silently destroy the
	// corrupt manifest; it should set it aside and start a fresh one.
	if _, err := s.writeUnclaimedCredential(`{"x":1}`, map[string]any{}); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	foundAside := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt-") {
			foundAside = true
		}
	}
	if !foundAside {
		t.Fatalf("expected a preserved corrupt-manifest file, got entries: %v", entries)
	}
}
