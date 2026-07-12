package cswap

import (
	"strings"
	"testing"
)

func TestBuildStatusPayloadNoActiveAccount(t *testing.T) {
	s := newTestSwitcher(t)
	payload := s.BuildStatusPayload()
	if payload["active"] != nil {
		t.Fatalf("got %#v", payload["active"])
	}
}

func TestBuildStatusPayloadUnmanagedAccount(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)

	payload := s.BuildStatusPayload()
	active, ok := payload["active"].(map[string]any)
	if !ok {
		t.Fatalf("got %#v", payload["active"])
	}
	if active["email"] != "a@example.com" || active["managed"] != false {
		t.Fatalf("got %#v", active)
	}
}

func TestBuildStatusPayloadManagedAccount(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	payload := s.BuildStatusPayload()
	active, ok := payload["active"].(map[string]any)
	if !ok {
		t.Fatalf("got %#v", payload["active"])
	}
	if active["number"] != 1 || active["managed"] != true {
		t.Fatalf("got %#v", active)
	}
	if payload["totalManagedAccounts"] != 1 {
		t.Fatalf("got %#v", payload["totalManagedAccounts"])
	}
}

func TestStatusHumanNoActiveAccount(t *testing.T) {
	s := newTestSwitcher(t)
	res, err := s.Status(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) != 1 || !strings.Contains(res.Lines[0], "No active Claude account") {
		t.Fatalf("got %#v", res.Lines)
	}
}

func TestStatusHumanManagedAccount(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	res, err := s.Status(false)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(res.Lines, "\n")
	if !strings.Contains(joined, "Account-1") || !strings.Contains(joined, "a@example.com") {
		t.Fatalf("got %q", joined)
	}
	if !strings.Contains(joined, "Total managed accounts: 1") {
		t.Fatalf("got %q", joined)
	}
}

func TestStatusJSONReturnsPayload(t *testing.T) {
	s := newTestSwitcher(t)
	res, err := s.Status(true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Payload == nil || len(res.Lines) != 0 {
		t.Fatalf("got payload=%#v lines=%#v", res.Payload, res.Lines)
	}
}
