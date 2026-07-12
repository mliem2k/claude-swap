package cswap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func setupSharingTest(t *testing.T) (*ClaudeAccountSwitcher, string, string) {
	t.Helper()
	s := newTestSwitcher(t)
	sourceRoot := filepath.Join(homeDir(), ".claude")
	if err := os.MkdirAll(sourceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(sourceRoot, "settings.json"), `{"a":1}`)
	sessionDir := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return s, sourceRoot, sessionDir
}

func TestSyncSharingCreatesSymlinkOnPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink path")
	}
	s, sourceRoot, sessionDir := setupSharingTest(t)
	syncSharing(s, sessionDir, true, false)

	dest := filepath.Join(sessionDir, "settings.json")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected settings.json to be a symlink")
	}
	target, err := os.Readlink(dest)
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(sourceRoot, "settings.json") {
		t.Fatalf("got %q", target)
	}
}

func TestSyncSharingNoShareRemovesManagedLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink path")
	}
	s, _, sessionDir := setupSharingTest(t)
	syncSharing(s, sessionDir, true, false)
	syncSharing(s, sessionDir, false, false)

	if _, err := os.Lstat(filepath.Join(sessionDir, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("expected the managed symlink to be removed")
	}
	if _, err := os.Stat(filepath.Join(sessionDir, ShareManifest)); !os.IsNotExist(err) {
		t.Fatal("expected the manifest to be removed when nothing is shared")
	}
}

func TestSyncSharingNeverTouchesPreexistingUserData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink path")
	}
	s, _, sessionDir := setupSharingTest(t)
	mustWrite(t, filepath.Join(sessionDir, "settings.json"), `{"userOwned":true}`)

	syncSharing(s, sessionDir, true, false)

	data, err := os.ReadFile(filepath.Join(sessionDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"userOwned":true}` {
		t.Fatalf("expected pre-existing user data untouched, got %s", data)
	}
}

func TestSyncSharingMissingSourcePrunesOwnEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink path")
	}
	s, sourceRoot, sessionDir := setupSharingTest(t)
	syncSharing(s, sessionDir, true, false)
	if err := os.Remove(filepath.Join(sourceRoot, "settings.json")); err != nil {
		t.Fatal(err)
	}

	syncSharing(s, sessionDir, true, false)

	if _, err := os.Lstat(filepath.Join(sessionDir, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("expected the dangling managed link pruned")
	}
}

func TestSyncSharingHistorySeedsEmptySourceWhenMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink path")
	}
	s, sourceRoot, sessionDir := setupSharingTest(t)
	syncSharing(s, sessionDir, false, true)

	if _, err := os.Stat(filepath.Join(sourceRoot, "history.jsonl")); err != nil {
		t.Fatalf("expected an empty history.jsonl seeded at the source, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, "projects")); err != nil {
		t.Fatalf("expected an empty projects/ dir seeded at the source, got err=%v", err)
	}
	dest, err := os.Lstat(filepath.Join(sessionDir, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if dest.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected history.jsonl symlinked into the profile")
	}
}

func TestSyncSharingHistoryMergesExistingProfileHistoryIntoSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink path")
	}
	s, sourceRoot, sessionDir := setupSharingTest(t)
	mustWrite(t, filepath.Join(sessionDir, "history.jsonl"), "line-one\nline-two\n")

	syncSharing(s, sessionDir, false, true)

	data, err := os.ReadFile(filepath.Join(sourceRoot, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(data), "line-one", "line-two") {
		t.Fatalf("expected merged lines in source, got %q", data)
	}
	dest, err := os.Lstat(filepath.Join(sessionDir, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if dest.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected the profile's history.jsonl replaced with a symlink after merge")
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		if !contains(haystack, n) {
			return false
		}
	}
	return true
}

func TestSyncSharingHistoryMergesExistingProfileDirIntoSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink path")
	}
	s, sourceRoot, sessionDir := setupSharingTest(t)
	if err := os.MkdirAll(filepath.Join(sessionDir, "projects", "proj-a"), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(sessionDir, "projects", "proj-a", "transcript.jsonl"), "hello")

	syncSharing(s, sessionDir, false, true)

	data, err := os.ReadFile(filepath.Join(sourceRoot, "projects", "proj-a", "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("got %q", data)
	}
	dest, err := os.Lstat(filepath.Join(sessionDir, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	if dest.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected projects/ replaced with a symlink after merge")
	}
}

func TestSyncSharingHistorySkippedWhileLive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink path")
	}
	s, _, sessionDir := setupSharingTest(t)
	mustWrite(t, filepath.Join(sessionDir, "history.jsonl"), "line-one\n")
	sessionsSubdir := filepath.Join(sessionDir, "sessions")
	if err := os.MkdirAll(sessionsSubdir, 0o700); err != nil {
		t.Fatal(err)
	}
	pid := os.Getpid()
	mustWrite(t, filepath.Join(sessionsSubdir, "livepid.json"), `{"pid":`+itoaForTest(pid)+`,"sessionId":"s1","cwd":"/tmp","startedAt":0,"kind":"claude","entrypoint":"cli"}`)

	syncSharing(s, sessionDir, false, true)

	dest, err := os.Lstat(filepath.Join(sessionDir, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if dest.Mode()&os.ModeSymlink != 0 {
		t.Fatal("expected the real history.jsonl left untouched (not replaced with a symlink) while a session is live")
	}
}

func itoaForTest(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestSyncSharingWindowsUsesCopyNotSymlink(t *testing.T) {
	s, sourceRoot, sessionDir := setupSharingTest(t)
	s.Platform = PlatformWindows

	syncSharing(s, sessionDir, true, false)

	dest := filepath.Join(sessionDir, "settings.json")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("expected a real copy on Windows, not a symlink")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"a":1}` {
		t.Fatalf("got %s", data)
	}
	_ = sourceRoot
}

func TestSyncSharingWindowsRejectsShareHistory(t *testing.T) {
	s, _, sessionDir := setupSharingTest(t)
	s.Platform = PlatformWindows

	// syncSharing itself silently downgrades share_history to false on
	// Windows (the hard error lives in SessionManager.Run, Task 6); this
	// only confirms no history items get linked/copied.
	syncSharing(s, sessionDir, false, true)

	if _, err := os.Lstat(filepath.Join(sessionDir, "history.jsonl")); !os.IsNotExist(err) {
		t.Fatal("expected no history.jsonl on Windows even when share_history is requested")
	}
}

func TestSyncSharingWritesManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink path")
	}
	s, _, sessionDir := setupSharingTest(t)
	syncSharing(s, sessionDir, true, false)

	data, err := os.ReadFile(filepath.Join(sessionDir, ShareManifest))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Items []string `json:"items"`
		Mode  string   `json:"mode"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Mode != "symlink" {
		t.Fatalf("got mode %q", manifest.Mode)
	}
	found := false
	for _, item := range manifest.Items {
		if item == "settings.json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected settings.json in the manifest, got %v", manifest.Items)
	}
}
