package cswap

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func withGithubReleasesURL(t *testing.T, url string) {
	t.Helper()
	old := githubReleasesURL
	githubReleasesURL = url
	t.Cleanup(func() { githubReleasesURL = old })
}

// serveGithubReleaseRedirect mimics githubReleasesURL's real shape: a 302
// with the release tag in the Location header, no body, no
// api.github.com rate limit involved. htmlURL must end in the release
// tag as its final path segment (e.g. ".../releases/tag/v1.2.3"), since
// FetchLatestRelease reads the tag from that segment, not from a separate
// field.
func serveGithubReleaseRedirect(htmlURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", htmlURL)
		w.WriteHeader(http.StatusFound)
	}
}

// captureStderr redirects os.Stderr for the duration of fn, returning
// whatever was written to it. RunSelfUpgrade's failure messages go through
// Error(), which always prints to os.Stderr (see printer.go), not the
// io.Writer passed to RunSelfUpgrade itself, so tests asserting on those
// messages need this instead of just inspecting the out buffer.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()

	_ = w.Close()
	captured, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(captured)
}

func TestFetchLatestReleaseSuccess(t *testing.T) {
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://github.com/mliem2k/claude-swap/releases/tag/v1.2.3"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	release, err := FetchLatestRelease()
	if err != nil {
		t.Fatal(err)
	}
	if release.TagName != "v1.2.3" || release.HTMLURL != "https://github.com/mliem2k/claude-swap/releases/tag/v1.2.3" {
		t.Fatalf("got %#v", release)
	}
}

func TestFetchLatestReleaseNon200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	if _, err := FetchLatestRelease(); err == nil {
		t.Fatal("expected an error for a non-redirect response")
	}
}

func TestFetchLatestReleaseRedirectWithoutLocationIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound) // 302 but no Location header
	}))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	if _, err := FetchLatestRelease(); err == nil {
		t.Fatal("expected an error for a redirect with no Location header")
	}
}

func TestParseVersionStripsLeadingV(t *testing.T) {
	got, err := parseVersion("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestParseVersionWithoutLeadingV(t *testing.T) {
	got, err := parseVersion("2.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 2 || got[1] != 0 || got[2] != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestParseVersionInvalidIsError(t *testing.T) {
	if _, err := parseVersion("dev"); err == nil {
		t.Fatal("expected an error for a non-numeric version like the default dev build")
	}
}

func TestVersionGreaterBasic(t *testing.T) {
	if !versionGreater([]int{1, 3, 0}, []int{1, 2, 9}) {
		t.Fatal("expected 1.3.0 > 1.2.9")
	}
	if versionGreater([]int{1, 2, 0}, []int{1, 2, 0}) {
		t.Fatal("expected 1.2.0 not > 1.2.0")
	}
	if versionGreater([]int{1, 2}, []int{1, 2, 1}) {
		t.Fatal("expected 1.2 not > 1.2.1")
	}
}

func TestVersionGreaterDifferentLengths(t *testing.T) {
	// 1.3 vs 1.2.9: missing trailing elements treated as 0.
	if !versionGreater([]int{1, 3}, []int{1, 2, 9}) {
		t.Fatal("expected 1.3 > 1.2.9")
	}
	if versionGreater([]int{1, 3, 0}, []int{1, 3}) {
		t.Fatal("expected 1.3.0 not > 1.3 (equal, missing treated as 0)")
	}
}

func TestCheckForUpdateNoUpdateWhenCurrentIsLatest(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/tag/v1.0.0"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	if got := CheckForUpdate("1.0.0"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestCheckForUpdateNotifiesOnNewerRelease(t *testing.T) {
	newTestSwitcher(t)
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/tag/v2.0.0"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	got := CheckForUpdate("1.0.0")
	if got == "" {
		t.Fatal("expected a notification for a newer release")
	}
	if !contains(got, "2.0.0") || !contains(got, "1.0.0") {
		t.Fatalf("expected both versions mentioned, got %q", got)
	}
}

func TestCheckForUpdateNetworkFailureIsSilent(t *testing.T) {
	newTestSwitcher(t)
	withGithubReleasesURL(t, "http://127.0.0.1:1") // nothing listens here
	if got := CheckForUpdate("1.0.0"); got != "" {
		t.Fatalf("expected a silent empty string on network failure, got %q", got)
	}
}

func TestCheckForUpdateUnparseableCurrentVersionIsSilent(t *testing.T) {
	newTestSwitcher(t)
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/tag/v2.0.0"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	if got := CheckForUpdate("dev"); got != "" {
		t.Fatalf("expected silent empty string for an unparseable current version (the default dev build), got %q", got)
	}
}

func TestCheckForUpdateUsesCacheOnSecondCall(t *testing.T) {
	newTestSwitcher(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "https://example.com/releases/tag/v2.0.0")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	CheckForUpdate("1.0.0")
	CheckForUpdate("1.0.0")
	if calls != 1 {
		t.Fatalf("expected exactly one fetch (second call served from cache), got %d", calls)
	}
}

func TestRunSelfUpgradeAlreadyLatest(t *testing.T) {
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/tag/v1.0.0"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	var out bytes.Buffer
	if code := RunSelfUpgrade("1.0.0", &out); code != 0 {
		t.Fatalf("got %d", code)
	}
	if !strings.Contains(out.String(), "already on the latest version") {
		t.Fatalf("got %q", out.String())
	}
}

func TestRunSelfUpgradeNewerAvailable(t *testing.T) {
	// Now that RunSelfUpgrade actually downloads and installs, this
	// scenario is a "newer version found but the download step itself
	// fails" case (deterministic, no real network dependency: nothing
	// listens on 127.0.0.1:1, matching the existing pattern used above
	// in TestRunSelfUpgradeNetworkFailureReturnsOne). The happy-path
	// "newer version found and successfully installed" scenario is
	// covered by TestRunSelfUpgradeActuallyReplacesBinary below, which
	// mocks the asset download to succeed.
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/v2.0.0"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	restoreAssetURL := overrideReleaseAssetURLForTest(t, func(name string) string { return "http://127.0.0.1:1" })
	defer restoreAssetURL()

	var out bytes.Buffer
	if code := RunSelfUpgrade("1.0.0", &out); code != 1 {
		t.Fatalf("got %d, want 1 (download should have failed); output: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "v2.0.0") || !strings.Contains(out.String(), "https://example.com/releases/v2.0.0") {
		t.Fatalf("got %q", out.String())
	}
}

func TestRunSelfUpgradeActuallyReplacesBinary(t *testing.T) {
	// Build a tiny fake "new" binary archive and serve it locally, then
	// point both the release-info fetch and the asset download at test
	// servers, and confirm RunSelfUpgrade replaces a fake "current
	// executable" file with the downloaded content.
	newContent := []byte("new fake cswap binary")
	assetName := releaseAssetName(runtimeGOOS(), runtime.GOARCH)

	var archiveBody []byte
	if strings.HasSuffix(assetName, ".zip") {
		archiveBody = buildTestZipArchive(t, "cswap.exe", newContent)
	} else {
		archiveBody = buildTestTarGzArchive(t, "cswap", newContent)
	}

	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archiveBody)
	}))
	defer assetSrv.Close()

	restoreAssetURL := overrideReleaseAssetURLForTest(t, func(name string) string { return assetSrv.URL })
	defer restoreAssetURL()

	releaseSrv := httptest.NewServer(serveGithubReleaseRedirect("https://example.invalid/releases/tag/v99.0.0"))
	defer releaseSrv.Close()
	oldURL := githubReleasesURL
	githubReleasesURL = releaseSrv.URL
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	currentExePath := filepath.Join(dir, "cswap")
	if err := os.WriteFile(currentExePath, []byte("old fake cswap binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreExe := overrideCurrentExecutablePathForTest(t, currentExePath)
	defer restoreExe()

	var out bytes.Buffer
	code := RunSelfUpgrade("1.0.0", &out)
	if code != 0 {
		t.Fatalf("RunSelfUpgrade returned %d, want 0; output: %s", code, out.String())
	}

	got, err := os.ReadFile(currentExePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newContent) {
		t.Errorf("current executable content = %q, want %q", got, newContent)
	}
	if !strings.Contains(out.String(), "v99.0.0") {
		t.Errorf("output = %q, want it to mention the new version", out.String())
	}
}

func TestRunSelfUpgradeAlreadyLatestDoesNotDownload(t *testing.T) {
	releaseSrv := httptest.NewServer(serveGithubReleaseRedirect("https://example.invalid/releases/tag/v1.0.0"))
	defer releaseSrv.Close()
	oldURL := githubReleasesURL
	githubReleasesURL = releaseSrv.URL
	defer func() { githubReleasesURL = oldURL }()

	downloadCalled := false
	restoreAssetURL := overrideReleaseAssetURLForTest(t, func(name string) string {
		downloadCalled = true
		return "http://should-not-be-fetched.invalid"
	})
	defer restoreAssetURL()

	var out bytes.Buffer
	code := RunSelfUpgrade("1.0.0", &out)
	if code != 0 {
		t.Fatalf("RunSelfUpgrade returned %d, want 0", code)
	}
	if downloadCalled {
		t.Error("already on the latest version, should not have attempted a download")
	}
}

func overrideReleaseAssetURLForTest(t *testing.T, fn func(assetName string) string) (restore func()) {
	t.Helper()
	old := releaseAssetURLFunc
	releaseAssetURLFunc = fn
	return func() { releaseAssetURLFunc = old }
}

func overrideCurrentExecutablePathForTest(t *testing.T, path string) (restore func()) {
	t.Helper()
	old := currentExecutablePathFunc
	currentExecutablePathFunc = func() (string, error) { return path, nil }
	return func() { currentExecutablePathFunc = old }
}

func buildTestTarGzArchive(t *testing.T, entryName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: entryName, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func buildTestZipArchive(t *testing.T, entryName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	return buf.Bytes()
}

func TestRunSelfUpgradeUnparseableCurrentVersionStillReportsLatest(t *testing.T) {
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/tag/v2.0.0"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	var out bytes.Buffer
	if code := RunSelfUpgrade("dev", &out); code != 0 {
		t.Fatalf("got %d", code)
	}
	if !strings.Contains(out.String(), "v2.0.0") {
		t.Fatalf("got %q", out.String())
	}
}

func TestRunSelfUpgradeUnparseableLatestTagStillReportsURL(t *testing.T) {
	// The URL's final path segment ("weird") becomes the release tag; it
	// isn't a parseable X.Y.Z version, exercising the same
	// unparseable-tag branch the old JSON-fixture test name described.
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/weird"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	var out bytes.Buffer
	if code := RunSelfUpgrade("1.0.0", &out); code != 0 {
		t.Fatalf("got %d", code)
	}
	if !strings.Contains(out.String(), "https://example.com/releases/weird") {
		t.Fatalf("expected the download URL reported even for an unparseable tag, got %q", out.String())
	}
}

func TestRunSelfUpgradeNetworkFailureReturnsOne(t *testing.T) {
	withGithubReleasesURL(t, "http://127.0.0.1:1")
	var out bytes.Buffer
	if code := RunSelfUpgrade("1.0.0", &out); code != 1 {
		t.Fatalf("got %d", code)
	}
}

func TestRunSelfUpgradeExtractionFailureReturnsOne(t *testing.T) {
	// The asset server returns garbage bytes that are neither a valid
	// .tar.gz nor a valid .zip, so downloadReleaseAsset succeeds (the HTTP
	// round trip itself is fine) but extractBinaryFromArchive fails,
	// exercising RunSelfUpgrade's extraction-failure branch end-to-end
	// (matching the "missing entry"/invalid-archive scenario
	// TestExtractBinaryFromArchiveMissingEntry already exercises at the
	// helper level, just now driven through the full RunSelfUpgrade call).
	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not a valid archive, just garbage bytes"))
	}))
	defer assetSrv.Close()
	restoreAssetURL := overrideReleaseAssetURLForTest(t, func(name string) string { return assetSrv.URL })
	defer restoreAssetURL()

	releaseSrv := httptest.NewServer(serveGithubReleaseRedirect("https://example.invalid/releases/tag/v99.0.0"))
	defer releaseSrv.Close()
	withGithubReleasesURL(t, releaseSrv.URL)

	var out bytes.Buffer
	var code int
	stderr := captureStderr(t, func() { code = RunSelfUpgrade("1.0.0", &out) })
	if code != 1 {
		t.Fatalf("RunSelfUpgrade returned %d, want 1 (extraction should have failed); output: %s; stderr: %s", code, out.String(), stderr)
	}
	if !strings.Contains(stderr, "Could not extract the new binary") {
		t.Errorf("stderr = %q, want a clear extraction-failure error message", stderr)
	}
	if !strings.Contains(out.String(), "Download it manually from: https://example.invalid/releases/tag/v99.0.0") {
		t.Errorf("output = %q, want the fallback manual-download URL", out.String())
	}
}
