package cswap

import "testing"

func TestAccountEmailKnownAndUnknown(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := s.AccountEmail("1"); got != "a@example.com" {
		t.Fatalf("got %q", got)
	}
	if got := s.AccountEmail("999"); got != "" {
		t.Fatalf("expected empty for unknown slot, got %q", got)
	}
}

func TestAccountEmailNoSequenceFileIsEmpty(t *testing.T) {
	s := newTestSwitcher(t)
	if got := s.AccountEmail("1"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestHasLiveLoginTrueAndFalse(t *testing.T) {
	s := newTestSwitcher(t)
	if s.HasLiveLogin() {
		t.Fatal("expected false with no live identity")
	}
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	if !s.HasLiveLogin() {
		t.Fatal("expected true once a live identity exists")
	}
}

func TestAccountIdentityFieldsAndTrim(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Accounts["1"] = AccountRecord{Email: "a@example.com", OrganizationUUID: "org1", UUID: "  u1  "}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	got := s.AccountIdentity("1")
	want := AccountIdentityInfo{Email: "a@example.com", OrganizationUUID: "org1", UUID: "u1"}
	if got != want {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestAccountIdentityUnknownSlotIsZeroValue(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	got := s.AccountIdentity("999")
	if got != (AccountIdentityInfo{}) {
		t.Fatalf("got %#v", got)
	}
}

func TestSwitchableAccountNumbersFollowsSequenceOrder(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Sequence = []int{2, 1}
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"}
	data.Accounts["2"] = AccountRecord{Email: "b@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"x"}}`)
	_ = s.WriteAccountConfig("1", "a@example.com", `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	// Slot 2 has no stored backup, so it is not switchable.
	got := s.SwitchableAccountNumbers()
	want := []string{"1"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %#v want %#v", got, want)
	}
}
