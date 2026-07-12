package cswap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugifyEmailReplacesUnsafeChars(t *testing.T) {
	got := SlugifyEmail("a+b@example.com")
	if got != "a_b_example.com" {
		t.Fatalf("got %q", got)
	}
}

// TestSlugifyEmailNormalizesNFDToNFC mirrors slugify_email's
// unicodedata.normalize("NFC", email) call: an NFD-decomposed email
// ("jose" + a combining acute accent, U+0301, the same email a macOS
// filesystem can hand back) must slug identically to its NFC-composed
// form ("josé" as one codepoint, U+00E9), not as an extra, separately
// underscored character.
func TestSlugifyEmailNormalizesNFDToNFC(t *testing.T) {
	nfc := "josé@example.com"  // "é" as one composed codepoint
	nfd := "josé@example.com" // "e" + combining acute accent
	// "@" is also outside the safe-char set (like "+" in
	// TestSlugifyEmailReplacesUnsafeChars above), so it becomes its own
	// "_" too: one underscore for "é", one for "@".
	if got, want := SlugifyEmail(nfc), "jos__example.com"; got != want {
		t.Fatalf("NFC input: got %q want %q", got, want)
	}
	if got, want := SlugifyEmail(nfd), SlugifyEmail(nfc); got != want {
		t.Fatalf("NFD input should slug identically to its NFC form, got %q want %q", got, want)
	}
}

func TestSessionDirForShape(t *testing.T) {
	got := SessionDirFor("/backup", "2", "user@x.com")
	want := filepath.Join("/backup", "sessions", "2-user_x.com")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestKeychainServiceNameIsStableForSamePath(t *testing.T) {
	a := KeychainServiceName("/backup/sessions/2-user_x.com")
	b := KeychainServiceName("/backup/sessions/2-user_x.com")
	if a != b {
		t.Fatalf("expected stable hash, got %q vs %q", a, b)
	}
	if len(a) < len("Claude Code-credentials-")+8 {
		t.Fatalf("expected an 8-hex-char suffix, got %q", a)
	}
}

// TestKeychainServiceNameNormalizesNFDToNFC mirrors keychain_service_name's
// unicodedata.normalize("NFC", str(session_dir)) call: an NFD-decomposed
// path (as macOS can hand back) must hash to the exact same service name
// as its NFC-composed equivalent, matching the hash Claude Code itself
// computes over the NFC-normalized CLAUDE_CONFIG_DIR value. Without this,
// cswap would read/delete the wrong (or no) Keychain entry for affected
// paths.
func TestKeychainServiceNameNormalizesNFDToNFC(t *testing.T) {
	nfc := "/backup/sessions/2-josé"  // "é" as one composed codepoint
	nfd := "/backup/sessions/2-josé" // "e" + combining acute accent
	if got, want := KeychainServiceName(nfd), KeychainServiceName(nfc); got != want {
		t.Fatalf("NFD path should hash identically to its NFC form, got %q want %q", got, want)
	}
}

func TestReadSessionCredentialsFallsBackToFile(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "session")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(sessionDir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"x"}}`)

	got := ReadSessionCredentials(sessionDir)
	if got != `{"claudeAiOauth":{"accessToken":"x"}}` {
		t.Fatalf("got %q", got)
	}
}

func TestReadSessionCredentialsMissingDirIsEmpty(t *testing.T) {
	if got := ReadSessionCredentials(filepath.Join(t.TempDir(), "nope")); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestReadSessionIdentity(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".claude.json"), `{"oauthAccount":{"emailAddress":"a@example.com","organizationUuid":"org1"}}`)

	email, org, ok := ReadSessionIdentity(dir)
	if !ok || email != "a@example.com" || org != "org1" {
		t.Fatalf("got email=%q org=%q ok=%v", email, org, ok)
	}
}

func TestReadSessionIdentityMissingIsNotOK(t *testing.T) {
	_, _, ok := ReadSessionIdentity(filepath.Join(t.TempDir(), "nope"))
	if ok {
		t.Fatal("expected ok=false for a missing profile")
	}
}

func TestSessionIdentityDriftedEmailMismatch(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".claude.json"), `{"oauthAccount":{"emailAddress":"other@example.com"}}`)
	if !SessionIdentityDrifted(dir, "a@example.com", "") {
		t.Fatal("expected drift on email mismatch")
	}
}

func TestSessionIdentityDriftedUnreadableIsNotDrift(t *testing.T) {
	if SessionIdentityDrifted(filepath.Join(t.TempDir(), "nope"), "a@example.com", "") {
		t.Fatal("an unreadable identity must not count as drift")
	}
}

func TestMarkSessionStaleCreatesMarker(t *testing.T) {
	dir := t.TempDir()
	MarkSessionStale(dir)
	if _, err := os.Stat(filepath.Join(dir, StaleMarker)); err != nil {
		t.Fatalf("expected stale marker file: %v", err)
	}
}

func TestLiveSessionsForMissingDirIsEmpty(t *testing.T) {
	if got := LiveSessionsFor(filepath.Join(t.TempDir(), "nope")); len(got) != 0 {
		t.Fatalf("expected empty, got %#v", got)
	}
}
