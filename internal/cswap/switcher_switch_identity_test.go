package cswap

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveMatchesSlotBackupByteEqual(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())
	creds := `{"claudeAiOauth":{"accessToken":"x"}}`
	mustWrite(t, GetCredentialsPath(), creds)
	_ = s.WriteAccountCredentials("1", "a@example.com", creds)

	if !s.LiveMatchesSlotBackup("1", "a@example.com") {
		t.Fatal("expected byte-identical live and backup to match")
	}
}

func TestLiveMatchesSlotBackupDiverged(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"live","refreshToken":"live-r"}}`)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"backup","refreshToken":"backup-r"}}`)

	if s.LiveMatchesSlotBackup("1", "a@example.com") {
		t.Fatal("expected divergent refresh-token lineages to not match")
	}
}

func TestLiveMatchesSlotBackupEmptyLiveIsTrue(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())
	// No credentials file at all: an unreadable/empty live credential should
	// be treated as matching (the no-op is safe; forcing a switch on missing
	// evidence would fail later anyway).
	if !s.LiveMatchesSlotBackup("1", "a@example.com") {
		t.Fatal("expected empty live credential to read as matching")
	}
}

func TestSelfSwitchActionNoopWhenMatching(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())
	creds := `{"claudeAiOauth":{"accessToken":"x"}}`
	mustWrite(t, GetCredentialsPath(), creds)
	_ = s.WriteAccountCredentials("1", "a@example.com", creds)

	action, provenance := s.SelfSwitchAction("1", "a@example.com")
	if action != "noop" || provenance != nil {
		t.Fatalf("got action=%q provenance=%#v", action, provenance)
	}
}

func TestSelfSwitchActionReconcileWhenResolved(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"account":{"uuid":"u1","email":"a@example.com"},"organization":{"uuid":""}}`))
	}))
	defer srv.Close()
	withProfileURL(t, srv.URL)

	liveCreds := `{"claudeAiOauth":{"accessToken":"live-tok","refreshToken":"live-r"}}`
	mustWrite(t, GetCredentialsPath(), liveCreds)
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"backup-tok","refreshToken":"backup-r"}}`)
	data := s.GetSequenceData()
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"}
	_ = s.WriteJSON(s.SequenceFile, data)

	action, provenance := s.SelfSwitchAction("1", "a@example.com")
	if action != "reconcile" || provenance == nil {
		t.Fatalf("got action=%q provenance=%#v", action, provenance)
	}
}

func TestSelfSwitchActionNoopDivergedWhenUnresolvable(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())
	withProfileURL(t, "http://127.0.0.1:1") // nothing listens: profile fetch fails

	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"live-tok","refreshToken":"live-r"}}`)
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"backup-tok","refreshToken":"backup-r"}}`)
	data := s.GetSequenceData()
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"}
	_ = s.WriteJSON(s.SequenceFile, data)

	action, provenance := s.SelfSwitchAction("1", "a@example.com")
	if action != "noop-diverged" || provenance != nil {
		t.Fatalf("got action=%q provenance=%#v", action, provenance)
	}
}
