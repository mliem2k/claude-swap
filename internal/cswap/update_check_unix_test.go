//go:build !windows

package cswap

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunSelfUpgradeReplaceFailureReturnsOne exercises RunSelfUpgrade's
// replace-failure branch end-to-end: the release-info and asset servers
// both succeed, and the served archive contains a real cswap/cswap.exe
// entry, so extraction succeeds too — the only thing that fails is
// replaceExecutable itself, because the destination directory has been
// made unwritable, the same permission-denied setup
// TestReplaceExecutableUnixPermissionDenied already uses at the helper
// level. Unix-only (root bypasses permission checks, and Windows has no
// equivalent chmod semantics), matching why TestReplaceExecutableUnixPermissionDenied
// itself lives in a //go:build !windows file.
func TestRunSelfUpgradeReplaceFailureReturnsOne(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, permission-denied case can't be exercised")
	}

	newContent := []byte("new fake cswap binary")
	assetName := releaseAssetName(runtimeGOOS(), runtime.GOARCH)
	var archiveBody []byte
	if strings.HasSuffix(assetName, ".zip") {
		archiveBody = buildTestZipArchive(t, "cswap.exe", newContent)
	} else {
		archiveBody = buildTestTarGzArchive(t, "cswap", newContent)
	}

	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archiveBody)
	}))
	defer assetSrv.Close()
	restoreAssetURL := overrideReleaseAssetURLForTest(t, func(name string) string { return assetSrv.URL })
	defer restoreAssetURL()

	releaseSrv := httptest.NewServer(serveGithubReleaseRedirect("https://example.invalid/releases/tag/v99.0.0"))
	defer releaseSrv.Close()
	withGithubReleasesURL(t, releaseSrv.URL)

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil { // read+execute, no write
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) // restore so t.TempDir() cleanup can remove it
	currentExePath := filepath.Join(dir, "cswap")
	restoreExe := overrideCurrentExecutablePathForTest(t, currentExePath)
	defer restoreExe()

	var out bytes.Buffer
	var code int
	stderr := captureStderr(t, func() { code = RunSelfUpgrade("1.0.0", &out) })
	if code != 1 {
		t.Fatalf("RunSelfUpgrade returned %d, want 1 (replace should have failed); output: %s; stderr: %s", code, out.String(), stderr)
	}
	if !strings.Contains(stderr, "Could not install the new binary") {
		t.Errorf("stderr = %q, want a clear replace-failure error message", stderr)
	}
	if !strings.Contains(out.String(), "Download it manually from: https://example.invalid/releases/tag/v99.0.0") {
		t.Errorf("output = %q, want the fallback manual-download URL", out.String())
	}
}
