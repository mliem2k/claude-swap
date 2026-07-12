package cswap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBuildAccountsInfoMarksActiveSlot(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	infos := s.BuildAccountsInfo()
	if len(infos) != 1 || !infos[0].IsActive {
		t.Fatalf("expected one active account, got %#v", infos)
	}
}

func TestStaticUsageSentinelAPIKey(t *testing.T) {
	s := newTestSwitcher(t)
	info := AccountUsageInfo{Number: 1, Email: "a@token.local", Credentials: "sk-ant-api03-xxxx"}
	if got := s.StaticUsageSentinel(info); got != UsageAPIKey {
		t.Fatalf("got %q", got)
	}
}

func TestStaticUsageSentinelNoCredentials(t *testing.T) {
	s := newTestSwitcher(t)
	info := AccountUsageInfo{Number: 1, Email: "a@example.com", Credentials: ""}
	if got := s.StaticUsageSentinel(info); got != UsageNoCredentials {
		t.Fatalf("got %q", got)
	}
}

func TestFetchActiveUsageNoCredentials(t *testing.T) {
	s := newTestSwitcher(t)
	rec := s.FetchActiveUsage("1", "a@example.com", "")
	if rec.Sentinel != UsageNoCredentials {
		t.Fatalf("expected no-credentials sentinel, got %#v", rec)
	}
}

func TestFetchActiveUsageSuccess(t *testing.T) {
	s := newTestSwitcher(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"five_hour": map[string]any{"utilization": 5.0}})
	}))
	defer srv.Close()
	withUsageURL(t, srv.URL)

	expiresAt := float64(time.Now().Add(time.Hour).UnixMilli())
	creds := mustJSON(t, map[string]any{"claudeAiOauth": map[string]any{
		"accessToken": "tok", "expiresAt": expiresAt,
	}})
	rec := s.FetchActiveUsage("1", "a@example.com", creds)
	if rec.Error != "" {
		t.Fatalf("expected success, got %#v", rec)
	}
}

func TestCollectUsageEntriesSentinelSkipsFetch(t *testing.T) {
	s := newTestSwitcher(t)
	fetchCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCalled = true
	}))
	defer srv.Close()
	withUsageURL(t, srv.URL)

	infos := []AccountUsageInfo{{Number: 1, Email: "a@token.local", Credentials: "sk-ant-api03-xxxx"}}
	entries := s.CollectUsageEntries(infos, nil)
	if entries["1"].Sentinel == nil || *entries["1"].Sentinel != UsageAPIKey {
		t.Fatalf("expected api-key sentinel, got %#v", entries["1"])
	}
	if fetchCalled {
		t.Fatal("an api-key account must never hit the network")
	}
}

func TestUsageByAccountMapsSentinels(t *testing.T) {
	// UsageByAccount fetches every account eligible, which would
	// otherwise reach the real usage API; redirect it locally so the
	// test stays offline and deterministic (oauth_usage_test.go's
	// established with UsageURL pattern).
	withUsageURL(t, "http://127.0.0.1:1")
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	got := s.UsageByAccount()
	if _, ok := got["1"]; !ok {
		t.Fatalf("expected slot 1 present, got %#v", got)
	}
}
