package cswap

import "testing"

func setupTwoSwitchableAccounts(t *testing.T, s *ClaudeAccountSwitcher) {
	t.Helper()
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Sequence = []int{1, 2}
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"}
	data.Accounts["2"] = AccountRecord{Email: "b@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"x":1}`)
	_ = s.WriteAccountConfig("1", "a@example.com", `{"y":1}`)
	_ = s.WriteAccountCredentials("2", "b@example.com", `{"x":2}`)
	_ = s.WriteAccountConfig("2", "b@example.com", `{"y":2}`)
}

func TestSelectBestSwitchableNoOtherAccounts(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	target, note := s.SelectBestSwitchable("1", nil, nil)
	if target != "" || note != "none" {
		t.Fatalf("got target=%q note=%q", target, note)
	}
}

func TestSelectBestSwitchableCurrentUnavailable(t *testing.T) {
	s := newTestSwitcher(t)
	setupTwoSwitchableAccounts(t, s)
	usage := map[string]any{"2": map[string]any{"five_hour": map[string]any{"pct": 10.0}}}
	target, note := s.SelectBestSwitchable("1", nil, usage)
	if target != "" || note != "current-unavailable" {
		t.Fatalf("got target=%q note=%q", target, note)
	}
}

func TestSelectBestSwitchablePicksBetterAccount(t *testing.T) {
	s := newTestSwitcher(t)
	setupTwoSwitchableAccounts(t, s)
	usage := map[string]any{
		"1": map[string]any{"five_hour": map[string]any{"pct": 80.0}},
		"2": map[string]any{"five_hour": map[string]any{"pct": 10.0}},
	}
	target, note := s.SelectBestSwitchable("1", nil, usage)
	if target != "2" || note != "" {
		t.Fatalf("got target=%q note=%q", target, note)
	}
}

func TestSelectBestSwitchableStaysWhenCurrentIsBest(t *testing.T) {
	s := newTestSwitcher(t)
	setupTwoSwitchableAccounts(t, s)
	usage := map[string]any{
		"1": map[string]any{"five_hour": map[string]any{"pct": 10.0}},
		"2": map[string]any{"five_hour": map[string]any{"pct": 80.0}},
	}
	target, note := s.SelectBestSwitchable("1", nil, usage)
	if target != "" || note != "stay" {
		t.Fatalf("got target=%q note=%q", target, note)
	}
}

func TestSelectBestSwitchableExhausted(t *testing.T) {
	s := newTestSwitcher(t)
	setupTwoSwitchableAccounts(t, s)
	usage := map[string]any{
		"1": map[string]any{"five_hour": map[string]any{"pct": 100.0}},
		"2": map[string]any{"five_hour": map[string]any{"pct": 100.0}},
	}
	target, note := s.SelectBestSwitchable("1", nil, usage)
	if target != "" || note != "exhausted" {
		t.Fatalf("got target=%q note=%q", target, note)
	}
}

func TestWarnInertModelsFlagsTypo(t *testing.T) {
	s := newTestSwitcher(t)
	usage := map[string]any{
		"1": map[string]any{"scoped": []map[string]any{{"name": "Fable", "pct": 5.0}}},
	}
	var warnings []string
	s.WarnInertModels(usage, []string{"Fabel"}, &warnings)
	if len(warnings) != 1 {
		t.Fatalf("expected a typo warning, got %#v", warnings)
	}
}

func TestWarnInertModelsNoWarningWhenMatched(t *testing.T) {
	s := newTestSwitcher(t)
	usage := map[string]any{
		"1": map[string]any{"scoped": []map[string]any{{"name": "Fable", "pct": 5.0}}},
	}
	var warnings []string
	s.WarnInertModels(usage, []string{"Fable"}, &warnings)
	if len(warnings) != 0 {
		t.Fatalf("expected no warning, got %#v", warnings)
	}
}
