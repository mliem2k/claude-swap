package cswap

import "testing"

func TestStashLiveCredentialReturnsEntryID(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	creds := `{"claudeAiOauth":{"accessToken":"stray"}}`
	id, err := s.StashLiveCredential(creds, "alien", "1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected a non-empty entry id")
	}
	listed := s.Store.listUnclaimedCredentials()
	row, ok := listed[id]
	if !ok {
		t.Fatalf("expected the entry listed, got %#v", listed)
	}
	if row["reason"] != "alien" || row["configSlot"] != "1" {
		t.Fatalf("got %#v", row)
	}
}

func TestStashLiveCredentialIncludesResolvedIdentity(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	creds := `{"claudeAiOauth":{"accessToken":"stray"}}`
	resolved := map[string]any{"uuid": "u9", "email": "x@example.com"}
	id, err := s.StashLiveCredential(creds, "foreign", "1", resolved)
	if err != nil {
		t.Fatal(err)
	}
	listed := s.Store.listUnclaimedCredentials()
	row := listed[id]
	got, ok := row["resolvedIdentity"].(map[string]any)
	if !ok || got["uuid"] != "u9" {
		t.Fatalf("got %#v", row)
	}
}
