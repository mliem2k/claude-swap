package cswap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// UpdateCheckCacheTTL mirrors CACHE_TTL: 24 hours.
const UpdateCheckCacheTTL = 24 * time.Hour

// githubReleasesURL is GitHub's own "latest release" redirect endpoint for
// this port's fork (mliem2k/claude-swap), where Go binary releases are
// published, NOT the REST API (api.github.com/repos/.../releases/latest).
// The REST API is capped at 60 unauthenticated requests/hour per IP,
// easy to exhaust with normal use (this session hit it during heavy
// release testing). This plain github.com URL redirects (302, with the
// release tag in the Location header) and carries none of that limit,
// since it's the same request a browser or `curl -I` makes, not an API
// call. Deliberately NOT PyPI/uv/pipx: those are Python packaging
// concepts that do not apply to a Go binary. This whole file is an
// adaptation of update_check.py, not a literal port, per explicit user
// direction. A package-level var (not a const) so tests can redirect it,
// matching the oauthTokenURL/usageAPIURL test-seam pattern already
// established in oauth.go.
var githubReleasesURL = "https://github.com/mliem2k/claude-swap/releases/latest"

// releaseAssetURLFunc and currentExecutablePathFunc are test seams,
// matching the githubReleasesURL package-var-for-test-override pattern
// already established above. Production code always uses
// releaseAssetURL and os.Executable (resolved through symlinks) directly.
var (
	releaseAssetURLFunc       = releaseAssetURL
	currentExecutablePathFunc = func() (string, error) {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(exe)
	}
)

// GitHubRelease is the release tag and its release page, resolved from the
// Location header of githubReleasesURL's redirect, not from a JSON API
// response.
type GitHubRelease struct {
	TagName string
	HTMLURL string
}

// FetchLatestRelease resolves the latest GitHub release for this port's
// fork by reading the Location header off githubReleasesURL's redirect
// (one hop, not followed) instead of calling GitHub's REST API; see
// githubReleasesURL's doc comment for why. Adapted from Python's PyPI
// JSON API fetch in check_for_update, same shape (one request, short
// timeout), different mechanism.
func FetchLatestRelease() (*GitHubRelease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "claude-swap/1.0")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || location == "" {
		return nil, fmt.Errorf("github releases redirect: http %d", resp.StatusCode)
	}
	u, err := url.Parse(location)
	if err != nil {
		return nil, fmt.Errorf("github releases redirect: unparseable location %q: %w", location, err)
	}
	tag := path.Base(u.Path)
	if tag == "" || tag == "." || tag == "/" {
		return nil, fmt.Errorf("github releases redirect: no tag in location %q", location)
	}
	return &GitHubRelease{TagName: tag, HTMLURL: location}, nil
}

// parseVersion mirrors _parse_version, additionally tolerating a leading
// "v" (GitHub's tag convention, e.g. "v1.2.3"), which Python's PyPI
// versions never carried.
func parseVersion(v string) ([]int, error) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, nil
}

// versionGreater reports whether a > b, comparing element-wise (a missing
// trailing element on either side is treated as 0, so "1.3" > "1.2.9" and
// "1.3.0" == "1.3").
func versionGreater(a, b []int) bool {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}

func updateCheckCachePath() string {
	return filepath.Join(CacheDir(), "update_check.json")
}

// CheckForUpdate mirrors check_for_update's shape (a passive, 24h-cached
// check, silent on any failure), adapted to this port's own GitHub
// releases instead of PyPI. Returns a notification string if a newer
// release exists, or "" otherwise.
func CheckForUpdate(currentVersion string) string {
	var latestTag string
	if cached, ok := ReadCache(updateCheckCachePath(), UpdateCheckCacheTTL); ok {
		latestTag, _ = cached.(string)
	} else {
		release, err := FetchLatestRelease()
		if err == nil && release != nil {
			latestTag = release.TagName
		}
		_ = WriteCache(updateCheckCachePath(), latestTag)
	}
	if latestTag == "" {
		return ""
	}
	latest, err := parseVersion(latestTag)
	if err != nil {
		return ""
	}
	current, err := parseVersion(currentVersion)
	if err != nil {
		return ""
	}
	if !versionGreater(latest, current) {
		return ""
	}
	return fmt.Sprintf(
		"A newer version of claude-swap is available (%s). You are using %s. Run `cswap upgrade` for details.",
		latestTag, currentVersion,
	)
}

// RunSelfUpgrade mirrors run_self_upgrade's role (the cswap upgrade
// command's action), adapted for a Go binary: checks this port's own
// GitHub releases (always fresh, no cache, since this is an explicit
// user request for current state), and, if a newer release exists,
// downloads the release asset for the running platform, extracts the
// binary from it, and replaces the currently-running executable in
// place (see replaceExecutable in self_upgrade_unix.go/
// self_upgrade_windows.go for the platform-specific mechanics). Unlike
// Python's own upgrade, which only executes automatically on POSIX and
// merely prints instructions on Windows (a running executable can't
// safely replace itself there without care), this port performs the
// in-place replacement on every platform, since Go binaries have no
// package-manager intermediary (uv/pipx) to hand a safe replacement off
// to, and Windows does support replacing a running .exe via a
// rename-aside-then-replace trick. Returns 0 on a successful check
// (whether or not a newer release exists) and successful install (if
// one was needed), 1 if the check itself failed (network/parse error)
// or if a newer release was found but downloading/extracting/installing
// it failed; every failure path before the final replaceExecutable call
// leaves the currently-running binary completely untouched.
func RunSelfUpgrade(currentVersion string, out io.Writer) int {
	release, err := FetchLatestRelease()
	if err != nil {
		Error(fmt.Sprintf("Could not check for updates: %s", err))
		return 1
	}
	latest, err := parseVersion(release.TagName)
	if err != nil {
		// A malformed or prerelease tag shouldn't block the user from
		// seeing where to download; degrade the same way an unparseable
		// current version does below, just skip the comparison instead.
		fmt.Fprintf(out, "Latest release: %s\n", Accent(release.TagName))
		fmt.Fprintf(out, "Download it from: %s\n", release.HTMLURL)
		fmt.Fprintln(out, Dimmed(fmt.Sprintf("(release version %q is not a parseable version, skipping comparison)", release.TagName)))
		return 0
	}
	current, currentErr := parseVersion(currentVersion)
	if currentErr != nil {
		fmt.Fprintf(out, "Latest release: %s\n", Accent(release.TagName))
		fmt.Fprintf(out, "Download it from: %s\n", release.HTMLURL)
		fmt.Fprintln(out, Dimmed(fmt.Sprintf("(current version %q is not a parseable release version, skipping comparison)", currentVersion)))
		return 0
	}
	if !versionGreater(latest, current) {
		fmt.Fprintln(out, Dimmed(fmt.Sprintf("You are already on the latest version (%s).", currentVersion)))
		return 0
	}
	fmt.Fprintf(out, "A newer version is available: %s\n", Accent(release.TagName))
	fmt.Fprintln(out, "Downloading and installing…")

	assetName := releaseAssetName(runtimeGOOS(), runtime.GOARCH)
	archivePath, err := downloadReleaseAsset(releaseAssetURLFunc(assetName))
	if err != nil {
		Error(fmt.Sprintf("Could not download the new release: %s", err))
		fmt.Fprintf(out, "Download it manually from: %s\n", release.HTMLURL)
		return 1
	}
	defer os.Remove(archivePath)

	newBinPath, err := extractBinaryFromArchive(archivePath, assetName)
	if err != nil {
		Error(fmt.Sprintf("Could not extract the new binary: %s", err))
		fmt.Fprintf(out, "Download it manually from: %s\n", release.HTMLURL)
		return 1
	}

	currentExePath, err := currentExecutablePathFunc()
	if err != nil {
		os.Remove(newBinPath)
		Error(fmt.Sprintf("Could not determine the running executable's path: %s", err))
		fmt.Fprintf(out, "Download it manually from: %s\n", release.HTMLURL)
		return 1
	}

	if err := replaceExecutable(newBinPath, currentExePath); err != nil {
		Error(fmt.Sprintf("Could not install the new binary: %s", err))
		fmt.Fprintf(out, "Download it manually from: %s\n", release.HTMLURL)
		return 1
	}

	fmt.Fprintf(out, "Upgraded to %s. This is already the version that will run next time.\n", Accent(release.TagName))
	return 0
}
