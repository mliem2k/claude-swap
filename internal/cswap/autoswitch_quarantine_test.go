package cswap

import "testing"

func setupOneAccountForAutoswitch(t *testing.T) (*AutoSwitchEngine, *ClaudeAccountSwitcher) {
	t.Helper()
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x","refreshToken":"r1"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	var events []AutoSwitchEvent
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(ev AutoSwitchEvent) {
		events = append(events, ev)
	}, false, "", nil)
	return e, s
}

func TestQuarantinePersistsStateAndEmits(t *testing.T) {
	e, _ := setupOneAccountForAutoswitch(t)
	var got []AutoSwitchEvent
	e.OnEvent = func(ev AutoSwitchEvent) { got = append(got, ev) }

	if err := e.quarantine("1", "a@example.com", "invalid_grant"); err != nil {
		t.Fatal(err)
	}

	state := e.readState()
	q, ok := state["quarantine"].(map[string]any)
	if !ok {
		t.Fatalf("got %#v", state)
	}
	entry, ok := q["1"].(map[string]any)
	if !ok || entry["email"] != "a@example.com" || entry["reason"] != "invalid_grant" {
		t.Fatalf("got %#v", q["1"])
	}
	if entry["refreshTokenFingerprint"] == nil || entry["refreshTokenFingerprint"] == "" {
		t.Fatalf("expected a non-empty fingerprint, got %#v", entry["refreshTokenFingerprint"])
	}

	if len(got) != 1 {
		t.Fatalf("got %d events", len(got))
	}
	qe, ok := got[0].(*QuarantineEvent)
	if !ok || qe.number != "1" || qe.reason != "invalid_grant" {
		t.Fatalf("got %#v", got[0])
	}
}

func TestReleaseRecoveredQuarantinesNoQuarantineIsNoop(t *testing.T) {
	e, _ := setupOneAccountForAutoswitch(t)
	state := map[string]any{}
	got, err := e.releaseRecoveredQuarantines(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestReleaseRecoveredQuarantinesReleasesOnFingerprintChange(t *testing.T) {
	e, _ := setupOneAccountForAutoswitch(t)
	if err := e.quarantine("1", "a@example.com", "invalid_grant"); err != nil {
		t.Fatal(err)
	}

	// Simulate a re-login: the account's stored backup credential changes,
	// so its refresh-token fingerprint no longer matches the quarantine
	// record.
	if err := e.Switcher.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"x","refreshToken":"r2"}}`); err != nil {
		t.Fatal(err)
	}

	var got []AutoSwitchEvent
	e.OnEvent = func(ev AutoSwitchEvent) { got = append(got, ev) }

	state := e.readState()
	newState, err := e.releaseRecoveredQuarantines(state)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := newState["quarantine"].(map[string]any)
	if _, stillThere := q["1"]; stillThere {
		t.Fatalf("expected slot 1 released, got %#v", q)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events", len(got))
	}
	ue, ok := got[0].(*UnquarantineEvent)
	if !ok || ue.number != "1" || ue.reason != "credentials-replaced" {
		t.Fatalf("got %#v", got[0])
	}
}

func TestReleaseRecoveredQuarantinesKeepsUnchangedEntries(t *testing.T) {
	e, _ := setupOneAccountForAutoswitch(t)
	if err := e.quarantine("1", "a@example.com", "invalid_grant"); err != nil {
		t.Fatal(err)
	}
	var got []AutoSwitchEvent
	e.OnEvent = func(ev AutoSwitchEvent) { got = append(got, ev) }

	state := e.readState()
	newState, err := e.releaseRecoveredQuarantines(state)
	if err != nil {
		t.Fatal(err)
	}
	q, ok := newState["quarantine"].(map[string]any)
	if !ok {
		t.Fatalf("got %#v", newState)
	}
	if _, stillThere := q["1"]; !stillThere {
		t.Fatal("expected slot 1 to remain quarantined (credential unchanged)")
	}
	if len(got) != 0 {
		t.Fatalf("expected no unquarantine events, got %#v", got)
	}
}
