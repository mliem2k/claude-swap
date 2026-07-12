package cswap

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

// withVersion temporarily overrides the package-level version var (normally
// "dev", or ldflags-injected at release build time) with a parseable
// version, mirroring withGithubReleasesURL's swap-and-restore shape. The
// default "dev" is deliberately unparseable (see
// TestCheckForUpdateUnparseableCurrentVersionIsSilent), so any test that
// wants CheckForUpdate/RunSelfUpgrade to reach the version-compare branch
// (rather than the silent/unparseable one) needs this.
func withVersion(t *testing.T, v string) {
	t.Helper()
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

func TestCliUpgradeCommandRuns(t *testing.T) {
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/tag/v1.0.0"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "upgrade"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

// TestCliUpdateAliasRunsUpgrade mirrors _SUBCOMMAND_FLAGS's "update" ->
// "--upgrade" mapping (a bare verb alias, not a legacy --flag translation,
// so it goes through cobra's own Aliases rather than translateLegacyArgs).
func TestCliUpdateAliasRunsUpgrade(t *testing.T) {
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/tag/v1.0.0"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "update"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestPersistentPostRunSkipsUpgradeAndPurge(t *testing.T) {
	newTestSwitcher(t)
	withVersion(t, "1.0.0")
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/tag/v99.0.0"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	var stdout, stderr bytes.Buffer
	// `list` with no accounts still succeeds (an empty list), so its
	// PersistentPostRunE should fire and print an update notice.
	code := RunCLI([]string{"cswap", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !contains(stdout.String()+stderr.String(), "99.0.0") {
		t.Fatalf("expected an update notice for a non-purge, non-upgrade, non-json command, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestPersistentPostRunSkipsJSONMode(t *testing.T) {
	newTestSwitcher(t)
	withVersion(t, "1.0.0")
	srv := httptest.NewServer(serveGithubReleaseRedirect("https://example.com/releases/tag/v99.0.0"))
	defer srv.Close()
	withGithubReleasesURL(t, srv.URL)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "list", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if contains(stdout.String(), "99.0.0") {
		t.Fatalf("expected no update notice mixed into --json stdout, got %q", stdout.String())
	}
}
