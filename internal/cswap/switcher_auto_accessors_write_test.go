package cswap

import "testing"

func TestCurrentAccountNumberNoLiveLoginIsEmpty(t *testing.T) {
	s := newTestSwitcher(t)
	if got := s.CurrentAccountNumber(); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestCurrentAccountNumberManagedLogin(t *testing.T) {
	s := setupOneAccountForAutoAccessors(t)
	if got := s.CurrentAccountNumber(); got != "1" {
		t.Fatalf("got %q", got)
	}
}

func TestCurrentAccountNumberUnmanagedLoginIsEmptyNotGuessed(t *testing.T) {
	// A live login exists but was never added: CurrentAccountNumber must
	// return "" (not fall back to any recorded activeAccountNumber), the
	// exact safety invariant Python's docstring calls out by name.
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"unmanaged@example.com"}}`)
	if got := s.CurrentAccountNumber(); got != "" {
		t.Fatalf("expected empty (unmanaged), got %q", got)
	}
	if !s.HasLiveLogin() {
		t.Fatal("expected HasLiveLogin true, distinguishing this from the no-login case")
	}
}

func TestPersistBackupCredentialsWritesBackupNotActive(t *testing.T) {
	s := setupOneAccountForAutoAccessors(t)
	newCreds := `{"claudeAiOauth":{"accessToken":"rotated"}}`
	if err := s.PersistBackupCredentials("1", "a@example.com", newCreds); err != nil {
		t.Fatal(err)
	}
	if got := s.ReadAccountCredentials("1", "a@example.com"); got != newCreds {
		t.Fatalf("got %q", got)
	}
	// The active/live credential must be untouched.
	if got := s.ReadCredentials(); got == newCreds {
		t.Fatal("expected the active credential to be untouched by a backup-only write")
	}
}

func TestBackfillAccountUUIDFillsEmptyOnly(t *testing.T) {
	s := setupOneAccountForAutoAccessors(t)
	if err := s.BackfillAccountUUID("1", "resolved-uuid"); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	if data.Accounts["1"].UUID != "resolved-uuid" {
		t.Fatalf("got %#v", data.Accounts["1"])
	}
}

func TestBackfillAccountUUIDNeverOverwritesExisting(t *testing.T) {
	s := setupOneAccountForAutoAccessors(t)
	if err := s.BackfillAccountUUID("1", "first-uuid"); err != nil {
		t.Fatal(err)
	}
	if err := s.BackfillAccountUUID("1", "second-uuid"); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	if data.Accounts["1"].UUID != "first-uuid" {
		t.Fatalf("expected the first uuid to stick, got %#v", data.Accounts["1"])
	}
}

func TestBackfillAccountUUIDEmptyUUIDIsNoop(t *testing.T) {
	s := setupOneAccountForAutoAccessors(t)
	if err := s.BackfillAccountUUID("1", ""); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	if data.Accounts["1"].UUID != "" {
		t.Fatalf("got %#v", data.Accounts["1"])
	}
}
