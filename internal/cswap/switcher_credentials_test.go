package cswap

import (
	"errors"
	"os"
	"testing"
)

func TestWriteReadAccountCredentialsRoundTrip(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	creds := `{"claudeAiOauth":{"accessToken":"x"}}`
	if err := s.WriteAccountCredentials("1", "a@example.com", creds); err != nil {
		t.Fatal(err)
	}
	if got := s.ReadAccountCredentials("1", "a@example.com"); got != creds {
		t.Fatalf("got %q", got)
	}
}

func TestWriteAccountConfigRoundTrip(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccountConfig("1", "a@example.com", `{"x":1}`); err != nil {
		t.Fatal(err)
	}
	if got := s.ReadAccountConfig("1", "a@example.com"); got != `{"x":1}` {
		t.Fatalf("got %q", got)
	}
}

func TestAccountIsSwitchableRequiresBothCredsAndConfig(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}

	if s.AccountIsSwitchable("1") {
		t.Fatal("expected not switchable with no creds/config yet")
	}
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"x":1}`)
	if s.AccountIsSwitchable("1") {
		t.Fatal("expected not switchable with creds but no config")
	}
	_ = s.WriteAccountConfig("1", "a@example.com", `{"y":1}`)
	if !s.AccountIsSwitchable("1") {
		t.Fatal("expected switchable once both creds and config exist")
	}
}

func TestDeleteAccountFilesRefusesLiveSession(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	sessionDir := s.SessionDir("1", "a@example.com")
	if err := os.MkdirAll(sessionDir+"/sessions", 0o700); err != nil {
		t.Fatal(err)
	}
	writeSessionFile(t, sessionDir+"/sessions", "1.json", map[string]any{
		"pid": os.Getpid(), "sessionId": "live",
	})

	err := s.DeleteAccountFiles("1", "a@example.com")
	if !errors.Is(err, ErrSession) {
		t.Fatalf("expected ErrSession, got %v", err)
	}
}

func TestDeleteAccountFilesRemovesConfigAndCreds(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"x":1}`)
	_ = s.WriteAccountConfig("1", "a@example.com", `{"y":1}`)

	if err := s.DeleteAccountFiles("1", "a@example.com"); err != nil {
		t.Fatal(err)
	}
	if s.ReadAccountCredentials("1", "a@example.com") != "" {
		t.Fatal("expected credentials gone")
	}
	if s.ReadAccountConfig("1", "a@example.com") != "" {
		t.Fatal("expected config gone")
	}
}

func TestEnsureNoLiveSessionPassesWhenNoneLive(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.EnsureNoLiveSession("1", "a@example.com", "test"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
