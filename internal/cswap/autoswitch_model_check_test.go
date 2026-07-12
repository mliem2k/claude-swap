package cswap

import "testing"

func setupTwoOAuthAccountsForModelCheck(t *testing.T) (*AutoSwitchEngine, *ClaudeAccountSwitcher) {
	t.Helper()
	s := newTestSwitcher(t)
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
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"x"}}`)
	_ = s.WriteAccountConfig("1", "a@example.com", `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	_ = s.WriteAccountCredentials("2", "b@example.com", `{"claudeAiOauth":{"accessToken":"x"}}`)
	_ = s.WriteAccountConfig("2", "b@example.com", `{"oauthAccount":{"emailAddress":"b@example.com"}}`)
	model := "Opus"
	settings := DefaultAutoSwitchSettings()
	settings.Model = &model
	var events []AutoSwitchEvent
	e := NewAutoSwitchEngine(s, settings, func(ev AutoSwitchEvent) { events = append(events, ev) }, false, "", nil)
	return e, s
}

func TestCheckModelNamesNoModelFilterMarksDoneImmediately(t *testing.T) {
	s := newTestSwitcher(t)
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(AutoSwitchEvent) {}, false, "", nil)
	e.checkModelNames(map[string]bool{}, map[string]any{})
	if !e.modelCheckDone {
		t.Fatal("expected modelCheckDone true with no model filter configured")
	}
}

func TestCheckModelNamesBareAllMarksDoneNoWarning(t *testing.T) {
	s := newTestSwitcher(t)
	model := "all"
	settings := DefaultAutoSwitchSettings()
	settings.Model = &model
	var events []AutoSwitchEvent
	e := NewAutoSwitchEngine(s, settings, func(ev AutoSwitchEvent) { events = append(events, ev) }, false, "", nil)
	e.checkModelNames(map[string]bool{}, map[string]any{})
	if !e.modelCheckDone {
		t.Fatal("expected modelCheckDone true for bare all")
	}
	if len(events) != 0 {
		t.Fatalf("expected no events, got %#v", events)
	}
}

func TestCheckModelNamesIncompleteUsageDoesNotMarkDone(t *testing.T) {
	e, _ := setupTwoOAuthAccountsForModelCheck(t)
	usage := map[string]any{"1": map[string]any{"scoped": []any{}}}
	e.checkModelNames(map[string]bool{}, usage)
	if e.modelCheckDone {
		t.Fatal("expected modelCheckDone still false, account 2's usage is missing")
	}
}

func TestCheckModelNamesMatchedModelNoWarning(t *testing.T) {
	e, _ := setupTwoOAuthAccountsForModelCheck(t)
	usage := map[string]any{
		"1": map[string]any{"scoped": []any{map[string]any{"name": "Opus"}}},
		"2": map[string]any{"scoped": []any{}},
	}
	var events []AutoSwitchEvent
	e.OnEvent = func(ev AutoSwitchEvent) { events = append(events, ev) }
	e.checkModelNames(map[string]bool{}, usage)
	if !e.modelCheckDone {
		t.Fatal("expected modelCheckDone true")
	}
	if len(events) != 0 {
		t.Fatalf("expected no warning, got %#v", events)
	}
}

func TestCheckModelNamesUnmatchedModelWarns(t *testing.T) {
	e, _ := setupTwoOAuthAccountsForModelCheck(t)
	usage := map[string]any{
		"1": map[string]any{"scoped": []any{}},
		"2": map[string]any{"scoped": []any{}},
	}
	var events []AutoSwitchEvent
	e.OnEvent = func(ev AutoSwitchEvent) { events = append(events, ev) }
	e.checkModelNames(map[string]bool{}, usage)
	if !e.modelCheckDone {
		t.Fatal("expected modelCheckDone true")
	}
	if len(events) != 1 {
		t.Fatalf("expected one warning event, got %#v", events)
	}
	cw, ok := events[0].(*ConfigWarningEvent)
	if !ok {
		t.Fatalf("got %#v", events[0])
	}
	want := "autoswitch.model: Opus matches no account's usage windows, only the 5h/7d limits are being watched for it (typo?)"
	if cw.ToJSON()["message"] != want {
		t.Fatalf("got %#v", cw.ToJSON()["message"])
	}
}

func TestCheckModelNamesQuarantinedAccountExcludedFromRelevance(t *testing.T) {
	e, _ := setupTwoOAuthAccountsForModelCheck(t)
	usage := map[string]any{"1": map[string]any{"scoped": []any{map[string]any{"name": "Opus"}}}}
	var events []AutoSwitchEvent
	e.OnEvent = func(ev AutoSwitchEvent) { events = append(events, ev) }
	e.checkModelNames(map[string]bool{"2": true}, usage)
	if !e.modelCheckDone {
		t.Fatal("expected modelCheckDone true, account 2 is quarantined so only account 1 is relevant")
	}
	if len(events) != 0 {
		t.Fatalf("expected no warning, got %#v", events)
	}
}
