package cswap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupTwoOAuthAccountsForTick(t *testing.T) (*AutoSwitchEngine, *ClaudeAccountSwitcher) {
	t.Helper()
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x","refreshToken":"r1"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccountCredentials("2", "b@example.com", `{"claudeAiOauth":{"accessToken":"y","refreshToken":"r2","expiresAt":9999999999999}}`); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAccountConfig("2", "b@example.com", `{"oauthAccount":{"emailAddress":"b@example.com"}}`); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Sequence = append(data.Sequence, 2)
	data.Accounts["2"] = AccountRecord{Email: "b@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	var events []AutoSwitchEvent
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(ev AutoSwitchEvent) { events = append(events, ev) }, false, "", nil)
	return e, s
}

func usageServer(t *testing.T, activePct, otherPct float64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		pct := activePct
		if token == "Bearer y" {
			pct = otherPct
		}
		future := "2099-01-01T00:00:00Z"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"five_hour": map[string]any{"utilization": pct, "resets_at": future},
			"seven_day": map[string]any{"utilization": 0.0, "resets_at": future},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTickNoActiveAccountIsNoAction(t *testing.T) {
	s := newTestSwitcher(t)
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(AutoSwitchEvent) {}, false, "", nil)
	if got := e.Tick(); got != TickNoAction {
		t.Fatalf("got %v", got)
	}
}

func TestTickBelowThresholdIsNoAction(t *testing.T) {
	e, s := setupTwoOAuthAccountsForTick(t)
	withUsageURL(t, usageServer(t, 10.0, 10.0).URL)
	var events []AutoSwitchEvent
	e.OnEvent = func(ev AutoSwitchEvent) { events = append(events, ev) }

	got := e.Tick()
	if got != TickNoAction {
		t.Fatalf("got %v", got)
	}
	if s.CurrentAccountNumber() != "1" {
		t.Fatal("expected no switch below threshold")
	}
	found := false
	for _, ev := range events {
		if ne, ok := ev.(*NoSwitchEvent); ok && ne.reason == "below-threshold" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a below-threshold NoSwitchEvent, got %#v", events)
	}
}

func TestTickAtLimitSwitchesEvenInCooldown(t *testing.T) {
	e, s := setupTwoOAuthAccountsForTick(t)
	withUsageURL(t, usageServer(t, 100.0, 10.0).URL)
	if _, err := e.mutateState(func(state map[string]any) {
		state["lastSwitchAt"] = float64(e.Clock().Unix())
	}); err != nil {
		t.Fatal(err)
	}

	got := e.Tick()
	if got != TickSwitched {
		t.Fatalf("got %v", got)
	}
	if s.CurrentAccountNumber() != "2" {
		t.Fatalf("expected switch to account 2, got %q", s.CurrentAccountNumber())
	}
}

func TestTickProactiveSwitchesAboveThresholdWithHeadroom(t *testing.T) {
	e, s := setupTwoOAuthAccountsForTick(t)
	withUsageURL(t, usageServer(t, 95.0, 10.0).URL)

	got := e.Tick()
	if got != TickSwitched {
		t.Fatalf("got %v", got)
	}
	if s.CurrentAccountNumber() != "2" {
		t.Fatalf("expected switch to account 2, got %q", s.CurrentAccountNumber())
	}
}

func TestTickProactiveRespectsCooldown(t *testing.T) {
	e, s := setupTwoOAuthAccountsForTick(t)
	withUsageURL(t, usageServer(t, 95.0, 10.0).URL)
	if _, err := e.mutateState(func(state map[string]any) {
		state["lastSwitchAt"] = float64(e.Clock().Unix())
	}); err != nil {
		t.Fatal(err)
	}

	got := e.Tick()
	if got != TickNoAction {
		t.Fatalf("got %v", got)
	}
	if s.CurrentAccountNumber() != "1" {
		t.Fatal("expected the cooldown to block the proactive switch")
	}
}

func TestTickHysteresisBlocksNearLineCandidate(t *testing.T) {
	e, s := setupTwoOAuthAccountsForTick(t)
	// Active at 91% (headroom 9), candidate at 85% (headroom 15): only 6
	// points better, below the default 10-point hysteresis margin.
	withUsageURL(t, usageServer(t, 91.0, 85.0).URL)

	got := e.Tick()
	if got != TickBlocked {
		t.Fatalf("got %v", got)
	}
	if s.CurrentAccountNumber() != "1" {
		t.Fatal("expected the hysteresis gate to block the switch")
	}
}

func TestTickNoCandidatesIsBlocked(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	withUsageURL(t, usageServer(t, 95.0, 0.0).URL)
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(AutoSwitchEvent) {}, false, "", nil)

	if got := e.Tick(); got != TickBlocked {
		t.Fatalf("got %v", got)
	}
}

func TestTickActiveApiKeyIsNoAction(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"apiKey":"sk-ant-x"}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	acct := data.Accounts["1"]
	acct.Kind = "api_key"
	data.Accounts["1"] = acct
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(AutoSwitchEvent) {}, false, "", nil)

	if got := e.Tick(); got != TickNoAction {
		t.Fatalf("got %v", got)
	}
}

func TestTickQuarantinesInvalidGrantCandidateAndBlocks(t *testing.T) {
	e, s := setupTwoOAuthAccountsForTick(t)
	// Account 2's stored credential expires in 7 minutes: inside
	// freshenTarget's 10-minute FreshenBufferMs (so freshenTarget treats it
	// as near-expiry and tries to refresh it before activation), but
	// outside the usage collector's own tighter 5-minute OAuthExpiryBufferMs
	// (so collectScheduledUsage's pre-fetch expiry check does NOT treat it
	// as already expired, and its usage poll succeeds against the mock
	// server using the still-valid access token instead of racing
	// freshenTarget for the same invalid_grant via its own independent
	// auth-dead-strike tracking). That keeps headroom for account 2 known
	// this tick, so it reaches candidate selection; only then does
	// freshenTarget's own refresh attempt fail with invalid_grant, and
	// freshenTarget quarantines it. With only one candidate, no viable
	// target remains.
	nearExpiry := e.Clock().UnixMilli() + 7*60*1000
	if err := s.WriteAccountCredentials("2", "b@example.com",
		`{"claudeAiOauth":{"accessToken":"y","refreshToken":"dead","expiresAt":`+itoa64(nearExpiry)+`}}`); err != nil {
		t.Fatal(err)
	}
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenSrv.Close()
	withTokenURL(t, tokenSrv.URL)
	withUsageURL(t, usageServer(t, 95.0, 10.0).URL)

	got := e.Tick()
	if got != TickBlocked {
		t.Fatalf("got %v", got)
	}
	state := e.readState()
	q, ok := state["quarantine"].(map[string]any)
	if !ok || q["2"] == nil {
		t.Fatalf("expected account 2 quarantined, got %#v", state)
	}
}
