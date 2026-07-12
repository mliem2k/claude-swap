package cswap

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTryFetchUsageForAccountNoAccessToken(t *testing.T) {
	out := TryFetchUsageForAccount("1", "a@example.com", `{}`, false, nil, slog.Default())
	if out.Error != "no-access-token" {
		t.Fatalf("expected no-access-token, got %q", out.Error)
	}
}

func TestTryFetchUsageForAccountActiveNeverRefreshed(t *testing.T) {
	var refreshCalled, usageCalled bool
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalled = true
	}))
	defer tokenSrv.Close()
	withTokenURL(t, tokenSrv.URL)

	usageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usageCalled = true
		_ = json.NewEncoder(w).Encode(map[string]any{"five_hour": map[string]any{"utilization": 1.0}})
	}))
	defer usageSrv.Close()
	withUsageURL(t, usageSrv.URL)

	expired := float64(time.Now().Add(-time.Hour).UnixMilli())
	creds := mustJSON(t, map[string]any{"claudeAiOauth": map[string]any{
		"accessToken": "tok", "refreshToken": "r", "expiresAt": expired,
	}})
	out := TryFetchUsageForAccount("1", "a@example.com", creds, true, nil, slog.Default())
	if refreshCalled {
		t.Fatal("an active account must never be refreshed")
	}
	if !usageCalled {
		t.Fatal("usage endpoint should still be called with the expired token")
	}
	if out.Error != "" {
		t.Fatalf("expected success, got %q", out.Error)
	}
}

func TestTryFetchUsageForAccountInactiveExpiredRefreshesFirst(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh", "expires_in": 3600,
		})
	}))
	defer tokenSrv.Close()
	withTokenURL(t, tokenSrv.URL)

	var gotAuth string
	usageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"five_hour": map[string]any{"utilization": 1.0}})
	}))
	defer usageSrv.Close()
	withUsageURL(t, usageSrv.URL)

	expired := float64(time.Now().Add(-time.Hour).UnixMilli())
	creds := mustJSON(t, map[string]any{"claudeAiOauth": map[string]any{
		"accessToken": "old", "refreshToken": "r", "expiresAt": expired,
	}})

	var persisted string
	persist := func(num, email, c string) error { persisted = c; return nil }

	out := TryFetchUsageForAccount("1", "a@example.com", creds, false, persist, slog.Default())
	if out.Error != "" {
		t.Fatalf("expected success, got %q", out.Error)
	}
	if gotAuth != "Bearer fresh" {
		t.Fatalf("expected the refreshed token to be used, got %q", gotAuth)
	}
	if persisted == "" {
		t.Fatal("expected the refreshed credentials to be persisted")
	}
}

func TestTryFetchUsageForAccountRefreshInvalidGrantIsPermanent(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenSrv.Close()
	withTokenURL(t, tokenSrv.URL)

	expired := float64(time.Now().Add(-time.Hour).UnixMilli())
	creds := mustJSON(t, map[string]any{"claudeAiOauth": map[string]any{
		"accessToken": "old", "refreshToken": "dead", "expiresAt": expired,
	}})
	out := TryFetchUsageForAccount("1", "a@example.com", creds, false, nil, slog.Default())
	if out.Error != "invalid_grant" {
		t.Fatalf("expected invalid_grant, got %q", out.Error)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
