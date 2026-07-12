package cswap

import "testing"

func TestAccountsSnapshotOnePassCoherentView(t *testing.T) {
	s := setupOneAccountForAutoAccessors(t)
	got := s.AccountsSnapshot(nil)
	if got.ActiveNumber == nil || *got.ActiveNumber != "1" {
		t.Fatalf("got %#v", got.ActiveNumber)
	}
	if len(got.Accounts) != 1 {
		t.Fatalf("got %#v", got.Accounts)
	}
	row := got.Accounts[0]
	if row.Number != "1" || row.Email != "a@example.com" || !row.IsActive {
		t.Fatalf("got %#v", row)
	}
	if row.Kind != "oauth" {
		t.Fatalf("got kind %q", row.Kind)
	}
	if !row.Switchable {
		t.Fatal("expected switchable true (has a stored backup)")
	}
	if got.TakenAt == 0 {
		t.Fatal("expected a non-zero taken-at timestamp")
	}
}

func TestAccountsSnapshotNoAccountsHasNilActive(t *testing.T) {
	s := newTestSwitcher(t)
	got := s.AccountsSnapshot(nil)
	if got.ActiveNumber != nil {
		t.Fatalf("got %#v", got.ActiveNumber)
	}
	if len(got.Accounts) != 0 {
		t.Fatalf("got %#v", got.Accounts)
	}
}

func TestAccountSnapshotDisplayTag(t *testing.T) {
	withOrg := AccountSnapshot{OrgName: "Acme"}
	if got := withOrg.DisplayTag(); got != "Acme" {
		t.Fatalf("got %q", got)
	}
	personal := AccountSnapshot{OrgName: ""}
	if got := personal.DisplayTag(); got != "personal" {
		t.Fatalf("got %q", got)
	}
}

func TestListUnclaimedCredentialsEmptyByDefault(t *testing.T) {
	s := newTestSwitcher(t)
	got := s.ListUnclaimedCredentials()
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}
