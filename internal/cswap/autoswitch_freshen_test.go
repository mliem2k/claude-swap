package cswap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFreshenTargetApiKeyIsAlwaysOk(t *testing.T) {
	e, s := setupOneAccountForAutoswitch(t)
	if err := s.WriteAccountCredentials("1", "a@example.com", `{"apiKey":"sk-ant-x"}`); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	acct := data.Accounts["1"]
	acct.Kind = "api_key"
	data.Accounts["1"] = acct
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	got, err := e.freshenTarget("1", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("got %q", got)
	}
}

func TestFreshenTargetNoCredentialsIsTransient(t *testing.T) {
	e, s := setupOneAccountForAutoswitch(t)
	if err := s.WriteAccountCredentials("1", "a@example.com", ""); err != nil {
		t.Fatal(err)
	}
	got, err := e.freshenTarget("1", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "transient" {
		t.Fatalf("got %q", got)
	}
}

func TestFreshenTargetNotNearExpiryIsOk(t *testing.T) {
	e, s := setupOneAccountForAutoswitch(t)
	farFuture := e.Clock().UnixMilli() + int64(time.Hour/time.Millisecond)
	creds := `{"claudeAiOauth":{"accessToken":"x","refreshToken":"r1","expiresAt":` +
		itoa64(farFuture) + `}}`
	if err := s.WriteAccountCredentials("1", "a@example.com", creds); err != nil {
		t.Fatal(err)
	}
	got, err := e.freshenTarget("1", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("got %q", got)
	}
}

func TestFreshenTargetMalformedCredentialsIsInvalidGrant(t *testing.T) {
	e, s := setupOneAccountForAutoswitch(t)
	if err := s.WriteAccountCredentials("1", "a@example.com", `{"notOauth":true}`); err != nil {
		t.Fatal(err)
	}
	got, err := e.freshenTarget("1", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "invalid_grant" {
		t.Fatalf("got %q", got)
	}
}

func TestFreshenTargetLiveSessionIsSkipped(t *testing.T) {
	e, s := setupOneAccountForAutoswitch(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	// SessionDirFor mirrors what LiveSessionPIDs itself builds
	// (BackupDir/sessions/<accountNum>-<SlugifyEmail(email)>): unlike
	// switcher_purge_test.go's TestPurgeRefusesWithLiveSession (whose
	// Purge scans every entry under sessions/ regardless of name),
	// freshenTarget's LiveSessionPIDs call needs the exact slugified
	// directory name, and SlugifyEmail turns "@" into "_", so a literal
	// "1-a@example.com" directory would silently not be found.
	profileDir := SessionDirFor(s.BackupDir, "1", "a@example.com")
	profileSessionsDir := filepath.Join(profileDir, "sessions")
	if err := os.MkdirAll(profileSessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// os.Getpid() is genuinely alive for the duration of this test process,
	// so IsPIDAlive(pid) is true without needing to spawn a real subprocess.
	// This exact pattern is already established at
	// internal/cswap/switcher_purge_test.go's TestPurgeRefusesWithLiveSession.
	if err := os.WriteFile(filepath.Join(profileSessionsDir, "session.json"),
		[]byte(fmt.Sprintf(`{"pid": %d, "startedAt": 0}`, os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := e.freshenTarget("1", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "skip-live-session" {
		t.Fatalf("got %q", got)
	}
}

func TestFreshenTargetPersistFailureSurfacesAsError(t *testing.T) {
	e, s := setupOneAccountForAutoswitch(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access", "expires_in": 3600, "refresh_token": "new-refresh",
		})
	}))
	defer srv.Close()
	withTokenURL(t, srv.URL)

	nearExpiry := e.Clock().UnixMilli() - 1000
	creds := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"x","refreshToken":"r1","expiresAt":%d}}`, nearExpiry)
	if err := s.WriteAccountCredentials("1", "a@example.com", creds); err != nil {
		t.Fatal(err)
	}

	// Force PersistBackupCredentials to fail fast: FileLock.Acquire calls
	// os.MkdirAll(filepath.Dir(lockPath), 0o700) before anything else, and
	// that fails immediately (no 10s timeout wait) if a path component is
	// an existing regular file rather than a directory.
	blockedDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.LockFile = filepath.Join(blockedDir, "sub", "lock")

	_, err := e.freshenTarget("1", "a@example.com")
	if err == nil {
		t.Fatal("expected a non-nil error from the persist failure")
	}
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

func TestNoteTokenIdentityNilTokenAccountIsFalse(t *testing.T) {
	e, _ := setupOneAccountForAutoswitch(t)
	if got := e.noteTokenIdentity("1", nil); got {
		t.Fatal("expected false for a nil token account")
	}
}

func TestNoteTokenIdentityEmptyUUIDIsFalse(t *testing.T) {
	e, _ := setupOneAccountForAutoswitch(t)
	if got := e.noteTokenIdentity("1", map[string]any{"uuid": ""}); got {
		t.Fatal("expected false for an empty uuid")
	}
}

func TestNoteTokenIdentityOrgConflictIsTrue(t *testing.T) {
	e, s := setupOneAccountForAutoswitch(t)
	data := s.GetSequenceData()
	acct := data.Accounts["1"]
	acct.OrganizationUUID = "org-slot"
	data.Accounts["1"] = acct
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	got := e.noteTokenIdentity("1", map[string]any{
		"uuid": "u-token", "organizationUuid": "org-token",
	})
	if !got {
		t.Fatal("expected true (org conflict)")
	}
}

func TestNoteTokenIdentityBackfillsEmptyUUID(t *testing.T) {
	e, s := setupOneAccountForAutoswitch(t)
	got := e.noteTokenIdentity("1", map[string]any{"uuid": "u-token"})
	if got {
		t.Fatal("expected false (no conflict, just a backfill)")
	}
	if s.AccountIdentity("1").UUID != "u-token" {
		t.Fatalf("got %#v", s.AccountIdentity("1"))
	}
}

func TestNoteTokenIdentityMismatchedExistingUUIDIsTrue(t *testing.T) {
	e, s := setupOneAccountForAutoswitch(t)
	data := s.GetSequenceData()
	acct := data.Accounts["1"]
	acct.UUID = "u-existing"
	data.Accounts["1"] = acct
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	got := e.noteTokenIdentity("1", map[string]any{"uuid": "u-different"})
	if !got {
		t.Fatal("expected true (uuid mismatch)")
	}
}
