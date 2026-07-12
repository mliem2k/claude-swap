package cswap

import (
	"encoding/json"
	"testing"
)

func TestTickOutcomeValuesMatchExitCodes(t *testing.T) {
	cases := map[TickOutcome]int{
		TickSwitched: 0,
		TickError:    1,
		TickNoAction: 2,
		TickBlocked:  3,
	}
	for outcome, want := range cases {
		if int(outcome) != want {
			t.Fatalf("got %d want %d", int(outcome), want)
		}
	}
}

func TestPollEventNoActiveHuman(t *testing.T) {
	e := NewPollEvent(nil, nil, nil, 90.0, nil, nil)
	if got := e.Human(); got != "poll: no active account" {
		t.Fatalf("got %q", got)
	}
}

func TestPollEventHumanWithHeadroomAndOthers(t *testing.T) {
	num := 1
	active := &AccountRef{Number: &num, Email: "a@example.com"}
	h1, h2 := 20.0, 50.0
	e := NewPollEvent(
		[]string{"1", "2"},
		active,
		map[string]*float64{"1": &h1, "2": &h2},
		90.0,
		nil,
		nil,
	)
	want := "Account-1 (a@example.com): 80% used (switch at 90%) | others: #2: 50%"
	if got := e.Human(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPollEventHumanUnknownUsageWithFetchError(t *testing.T) {
	num := 1
	active := &AccountRef{Number: &num, Email: "a@example.com"}
	e := NewPollEvent(
		[]string{"1"},
		active,
		map[string]*float64{"1": nil},
		90.0,
		map[string]string{"1": "http-429"},
		nil,
	)
	want := "Account-1 (a@example.com): usage unknown (http-429) (switch at 90%)"
	if got := e.Human(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPollEventHumanOthersUseWindowsWhenPresent(t *testing.T) {
	num := 1
	active := &AccountRef{Number: &num, Email: "a@example.com"}
	h1, h2 := 20.0, 30.0
	e := NewPollEvent(
		[]string{"1", "2"},
		active,
		map[string]*float64{"1": &h1, "2": &h2},
		90.0,
		nil,
		map[string][]WindowPct{"2": {{Label: "5h", Pct: 89.0}, {Label: "7d", Pct: 40.0}}},
	)
	want := "Account-1 (a@example.com): 80% used (switch at 90%) | others: #2: 5h 89% · 7d 40%"
	if got := e.Human(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPollEventToJSONOmitsEmptyAdditiveFields(t *testing.T) {
	num := 1
	active := &AccountRef{Number: &num, Email: "a@example.com"}
	h1 := 20.0
	e := NewPollEvent([]string{"1"}, active, map[string]*float64{"1": &h1}, 90.0, nil, nil)
	got := e.ToJSON()
	if _, ok := got["fetchErrors"]; ok {
		t.Fatalf("did not expect fetchErrors key, got %#v", got)
	}
	if _, ok := got["windowsPct"]; ok {
		t.Fatalf("did not expect windowsPct key, got %#v", got)
	}
	if got["event"] != "poll" || got["schemaVersion"] != JSONSchemaVersion {
		t.Fatalf("got %#v", got)
	}
	if got["threshold"] != 90.0 {
		t.Fatalf("got %#v", got["threshold"])
	}
}

func TestPollEventToJSONIncludesNonEmptyAdditiveFields(t *testing.T) {
	num := 1
	active := &AccountRef{Number: &num, Email: "a@example.com"}
	h1 := 20.0
	e := NewPollEvent(
		[]string{"1"}, active, map[string]*float64{"1": &h1}, 90.0,
		map[string]string{"1": "timeout"},
		map[string][]WindowPct{"1": {{Label: "5h", Pct: 80.0}}},
	)
	got := e.ToJSON()
	if fe, ok := got["fetchErrors"].(map[string]string); !ok || fe["1"] != "timeout" {
		t.Fatalf("got %#v", got["fetchErrors"])
	}
	if _, ok := got["windowsPct"]; !ok {
		t.Fatalf("expected windowsPct key, got %#v", got)
	}
}

func TestPollEventToJSONWindowsPctMarshalsAsLabelKeyedObject(t *testing.T) {
	num := 1
	active := &AccountRef{Number: &num, Email: "a@example.com"}
	h1 := 20.0
	e := NewPollEvent(
		[]string{"1"}, active, map[string]*float64{"1": &h1}, 90.0,
		nil,
		map[string][]WindowPct{"1": {{Label: "5h", Pct: 89.0}, {Label: "7d", Pct: 40.0}}},
	)
	data, err := json.Marshal(e.ToJSON())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	windowsPct, ok := decoded["windowsPct"].(map[string]any)
	if !ok {
		t.Fatalf("got %#v", decoded["windowsPct"])
	}
	inner, ok := windowsPct["1"].(map[string]any)
	if !ok {
		t.Fatalf("got %#v", windowsPct["1"])
	}
	if inner["5h"] != 89.0 || inner["7d"] != 40.0 {
		t.Fatalf("got %#v", inner)
	}
}

func TestSwitchEventHumanAndJSON(t *testing.T) {
	fromNum, toNum := 1, 2
	from := &AccountRef{Number: &fromNum, Email: "a@example.com"}
	to := &AccountRef{Number: &toNum, Email: "b@example.com"}
	e := NewSwitchEvent("proactive", from, to, nil, false)
	if got := e.Human(); got != "Switched Account-1 -> Account-2 (b@example.com) (proactive)" {
		t.Fatalf("got %q", got)
	}
	got := e.ToJSON()
	if got["trigger"] != "proactive" || got["dryRun"] != false {
		t.Fatalf("got %#v", got)
	}
	warnings, ok := got["warnings"].([]string)
	if !ok || len(warnings) != 0 {
		t.Fatalf("expected an empty (not nil-omitted) warnings slice, got %#v", got["warnings"])
	}
}

func TestSwitchEventDryRunHuman(t *testing.T) {
	toNum := 2
	to := &AccountRef{Number: &toNum, Email: "b@example.com"}
	e := NewSwitchEvent("failover", nil, to, nil, true)
	if got := e.Human(); got != "[dry-run] would switch (none) -> Account-2 (b@example.com) (failover)" {
		t.Fatalf("got %q", got)
	}
}

func TestSwitchEventNilToRefHuman(t *testing.T) {
	e := NewSwitchEvent("failover", nil, nil, nil, false)
	if got := e.Human(); got != "Switched (none) -> ? (failover)" {
		t.Fatalf("got %q", got)
	}
}

func TestNoSwitchEventHuman(t *testing.T) {
	e := NewNoSwitchEvent("below-threshold", "")
	if got := e.Human(); got != "no switch: below-threshold" {
		t.Fatalf("got %q", got)
	}
	e2 := NewNoSwitchEvent("cooldown", "30s remaining")
	if got := e2.Human(); got != "no switch: cooldown (30s remaining)" {
		t.Fatalf("got %q", got)
	}
}

func TestQuarantineEventHumanAndJSON(t *testing.T) {
	e := NewQuarantineEvent("2", "b@example.com", "invalid_grant")
	want := "Account-2 (b@example.com) quarantined: invalid_grant. Log in with it and run 'cswap --add-account --slot 2' to recover."
	if got := e.Human(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got := e.ToJSON()
	if got["number"] != "2" || got["email"] != "b@example.com" || got["reason"] != "invalid_grant" {
		t.Fatalf("got %#v", got)
	}
}

func TestUnquarantineEventDefaultReason(t *testing.T) {
	e := NewUnquarantineEvent("2", "b@example.com", "")
	if e.ToJSON()["reason"] != "credentials-replaced" {
		t.Fatalf("got %#v", e.ToJSON())
	}
	if got := e.Human(); got != "Account-2 (b@example.com) back in rotation (credentials-replaced)" {
		t.Fatalf("got %q", got)
	}
}

func TestAllExhaustedEventHuman(t *testing.T) {
	ts := "2026-07-11T00:00:00Z"
	e := NewAllExhaustedEvent(&ts)
	if got := e.Human(); got != "all accounts exhausted; earliest reset 2026-07-11T00:00:00Z" {
		t.Fatalf("got %q", got)
	}
	e2 := NewAllExhaustedEvent(nil)
	if got := e2.Human(); got != "all accounts exhausted; no reset time known" {
		t.Fatalf("got %q", got)
	}
}

func TestSleepEventHumanAndJSONRounding(t *testing.T) {
	e := NewSleepEvent(125.449, "2026-07-11T00:02:05Z")
	if got := e.Human(); got != "sleeping 2m (until 2026-07-11T00:02:05Z)" {
		t.Fatalf("got %q", got)
	}
	if got := e.ToJSON()["seconds"]; got != 125.4 {
		t.Fatalf("got %#v", got)
	}
}

func TestErrorEventHuman(t *testing.T) {
	e := NewErrorEvent("boom", true)
	if got := e.Human(); got != "error: boom (will retry)" {
		t.Fatalf("got %q", got)
	}
	e2 := NewErrorEvent("fatal", false)
	if got := e2.Human(); got != "error: fatal" {
		t.Fatalf("got %q", got)
	}
}

func TestConfigWarningEventHuman(t *testing.T) {
	e := NewConfigWarningEvent("autoswitch.model \"fable\" matches no account")
	if got := e.Human(); got != "warning: autoswitch.model \"fable\" matches no account" {
		t.Fatalf("got %q", got)
	}
}

func TestEveryEventKindAndTimestamp(t *testing.T) {
	events := []AutoSwitchEvent{
		NewPollEvent(nil, nil, nil, 0, nil, nil),
		NewSwitchEvent("", nil, nil, nil, false),
		NewNoSwitchEvent("", ""),
		NewQuarantineEvent("", "", ""),
		NewUnquarantineEvent("", "", ""),
		NewAllExhaustedEvent(nil),
		NewSleepEvent(0, ""),
		NewErrorEvent("", false),
		NewConfigWarningEvent(""),
	}
	wantKinds := []string{
		"poll", "switch", "no-switch", "account-quarantined", "account-unquarantined",
		"all-exhausted", "sleep", "error", "config-warning",
	}
	for i, e := range events {
		if e.Kind() != wantKinds[i] {
			t.Fatalf("event %d: got kind %q want %q", i, e.Kind(), wantKinds[i])
		}
		if e.Timestamp() == "" {
			t.Fatalf("event %d: expected a non-empty default timestamp", i)
		}
		if e.ToJSON()["event"] != wantKinds[i] {
			t.Fatalf("event %d: ToJSON event key mismatch: %#v", i, e.ToJSON())
		}
	}
}
