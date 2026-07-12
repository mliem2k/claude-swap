package cswap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func withTokenURL(t *testing.T, url string) {
	t.Helper()
	old := oauthTokenURL
	oauthTokenURL = url
	t.Cleanup(func() { oauthTokenURL = old })
}

func withProfileURL(t *testing.T, url string) {
	t.Helper()
	old := oauthProfileURL
	oauthProfileURL = url
	t.Cleanup(func() { oauthProfileURL = old })
}

func TestTryRefreshOAuthCredentialsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access", "expires_in": 3600, "refresh_token": "new-refresh",
			"account":      map[string]any{"uuid": "u1", "email_address": "a@example.com"},
			"organization": map[string]any{"uuid": "org1"},
		})
	}))
	defer srv.Close()
	withTokenURL(t, srv.URL)

	in := `{"claudeAiOauth":{"refreshToken":"old-refresh","accessToken":"old-access"}}`
	out := TryRefreshOAuthCredentials(in)
	if out.Error != "" {
		t.Fatalf("expected success, got error %q", out.Error)
	}
	oauth := ExtractOAuthData(out.Credentials)
	if oauth["accessToken"] != "new-access" || oauth["refreshToken"] != "new-refresh" {
		t.Fatalf("credentials not rotated: %#v", oauth)
	}
	if out.TokenAccount == nil || out.TokenAccount["uuid"] != "u1" {
		t.Fatalf("expected token account identity, got %#v", out.TokenAccount)
	}
}

func TestTryRefreshOAuthCredentialsNoRefreshToken(t *testing.T) {
	out := TryRefreshOAuthCredentials(`{"claudeAiOauth":{}}`)
	if out.Error != "no_refresh_token" {
		t.Fatalf("expected no_refresh_token, got %q", out.Error)
	}
}

func TestTryRefreshOAuthCredentialsInvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	withTokenURL(t, srv.URL)

	in := `{"claudeAiOauth":{"refreshToken":"dead"}}`
	out := TryRefreshOAuthCredentials(in)
	if out.Error != "invalid_grant" {
		t.Fatalf("expected invalid_grant, got %q", out.Error)
	}
}

func TestTryRefreshOAuthCredentialsTransientOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	withTokenURL(t, srv.URL)

	in := `{"claudeAiOauth":{"refreshToken":"x"}}`
	out := TryRefreshOAuthCredentials(in)
	if out.Error != "transient" {
		t.Fatalf("expected transient, got %q", out.Error)
	}
}

func TestFetchOAuthProfileSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account":      map[string]any{"uuid": "u2", "email": "b@example.com"},
			"organization": map[string]any{"uuid": "org2"},
		})
	}))
	defer srv.Close()
	withProfileURL(t, srv.URL)

	got := FetchOAuthProfile("some-token")
	if got == nil || got["uuid"] != "u2" || got["organizationUuid"] != "org2" {
		t.Fatalf("got %#v", got)
	}
}

func TestFetchOAuthProfile401ReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	withProfileURL(t, srv.URL)

	if got := FetchOAuthProfile("bad-token"); got != nil {
		t.Fatalf("expected nil on 401, got %#v", got)
	}
}
