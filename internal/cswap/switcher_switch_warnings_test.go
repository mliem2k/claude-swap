package cswap

import "testing"

func TestDuplicateAccountWarningsSameFingerprint(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	creds := `{"claudeAiOauth":{"refreshToken":"same"}}`
	infos := []AccountUsageInfo{
		{Number: 1, Email: "a@example.com", Credentials: creds},
		{Number: 2, Email: "b@example.com", Credentials: creds},
	}
	got := s.DuplicateAccountWarnings(infos)
	if len(got) != 1 {
		t.Fatalf("expected one duplicate warning, got %#v", got)
	}
}

func TestDuplicateAccountWarningsDifferentCredsNoWarning(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	infos := []AccountUsageInfo{
		{Number: 1, Email: "a@example.com", Credentials: `{"claudeAiOauth":{"refreshToken":"r1"}}`},
		{Number: 2, Email: "b@example.com", Credentials: `{"claudeAiOauth":{"refreshToken":"r2"}}`},
	}
	got := s.DuplicateAccountWarnings(infos)
	if len(got) != 0 {
		t.Fatalf("expected no warnings, got %#v", got)
	}
}

func TestDuplicateAccountWarningsSameUUID(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Accounts["1"] = AccountRecord{Email: "a@example.com", UUID: "u1"}
	data.Accounts["2"] = AccountRecord{Email: "b@example.com", UUID: "u1"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	infos := []AccountUsageInfo{
		{Number: 1, Email: "a@example.com", Credentials: `{"claudeAiOauth":{"refreshToken":"r1"}}`},
		{Number: 2, Email: "b@example.com", Credentials: `{"claudeAiOauth":{"refreshToken":"r2"}}`},
	}
	got := s.DuplicateAccountWarnings(infos)
	if len(got) != 1 {
		t.Fatalf("expected one uuid-collision warning, got %#v", got)
	}
}

func TestLockstepUsageWarningsIdenticalWindows(t *testing.T) {
	s := newTestSwitcher(t)
	resetsAt := "2099-01-01T00:00:00Z"
	usage := map[string]any{
		"five_hour": map[string]any{"pct": 50.0, "resets_at": resetsAt},
		"seven_day": map[string]any{"pct": 20.0, "resets_at": resetsAt},
	}
	entries := map[string]UsageEntry{
		"1": {LastGood: usage, AgeS: floatPtr(0)},
		"2": {LastGood: usage, AgeS: floatPtr(0)},
	}
	infos := []AccountUsageInfo{{Number: 1}, {Number: 2}}
	got := s.LockstepUsageWarnings(infos, entries)
	if len(got) != 1 {
		t.Fatalf("expected one lockstep warning, got %#v", got)
	}
}

func TestLockstepUsageWarningsMissingResetsAtNotFlagged(t *testing.T) {
	s := newTestSwitcher(t)
	usage := map[string]any{
		"five_hour": map[string]any{"pct": 0.0},
		"seven_day": map[string]any{"pct": 0.0},
	}
	entries := map[string]UsageEntry{
		"1": {LastGood: usage, AgeS: floatPtr(0)},
		"2": {LastGood: usage, AgeS: floatPtr(0)},
	}
	infos := []AccountUsageInfo{{Number: 1}, {Number: 2}}
	got := s.LockstepUsageWarnings(infos, entries)
	if len(got) != 0 {
		t.Fatalf("two idle accounts with no resets_at should never be flagged, got %#v", got)
	}
}
