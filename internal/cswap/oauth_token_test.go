package cswap

import (
	"strings"
	"testing"
	"time"
)

func TestExtractAccessToken(t *testing.T) {
	if got := ExtractAccessToken(`{"claudeAiOauth":{"accessToken":"abc"}}`); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := ExtractAccessToken(`not json`); got != "" {
		t.Fatalf("expected empty for bad json, got %q", got)
	}
	if got := ExtractAccessToken(`{}`); got != "" {
		t.Fatalf("expected empty when oauth missing, got %q", got)
	}
}

func TestExtractOAuthData(t *testing.T) {
	data := ExtractOAuthData(`{"claudeAiOauth":{"refreshToken":"r"}}`)
	if data == nil || data["refreshToken"] != "r" {
		t.Fatalf("got %#v", data)
	}
	if ExtractOAuthData(`{}`) != nil {
		t.Fatal("expected nil when claudeAiOauth missing")
	}
}

func TestCredentialFingerprintPrefersRefreshToken(t *testing.T) {
	a := CredentialFingerprint(`{"claudeAiOauth":{"refreshToken":"same","accessToken":"old"}}`)
	b := CredentialFingerprint(`{"claudeAiOauth":{"refreshToken":"same","accessToken":"new"}}`)
	if a != b {
		t.Fatalf("fingerprint should be stable across access-token rotation: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "sha256:") {
		t.Fatalf("expected sha256: prefix for refresh-token fingerprint, got %q", a)
	}
}

func TestCredentialFingerprintFullContentWithoutRefreshToken(t *testing.T) {
	got := CredentialFingerprint("sk-ant-api03-plain-key")
	if !strings.HasPrefix(got, "sha256-full:") {
		t.Fatalf("expected sha256-full: prefix, got %q", got)
	}
}

func TestCredentialFingerprintEmptyInput(t *testing.T) {
	if got := CredentialFingerprint(""); got != "" {
		t.Fatalf("expected empty for empty input, got %q", got)
	}
}

func TestIsOAuthTokenExpired(t *testing.T) {
	future := float64(time.Now().Add(time.Hour).UnixMilli())
	past := float64(time.Now().Add(-time.Hour).UnixMilli())
	if IsOAuthTokenExpired(future) {
		t.Fatal("a token expiring in an hour should not be expired")
	}
	if !IsOAuthTokenExpired(past) {
		t.Fatal("a token that expired an hour ago should be expired")
	}
	if IsOAuthTokenExpired("not-a-number") {
		t.Fatal("a non-numeric expiresAt should not be treated as expired")
	}
}

func TestFormatReset(t *testing.T) {
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	countdown, clock, err := FormatReset(future)
	if err != nil {
		t.Fatal(err)
	}
	if countdown == "" || clock == "" {
		t.Fatalf("expected non-empty countdown/clock, got %q %q", countdown, clock)
	}
}

func TestResetClockStringSameDayIsTimeOnly(t *testing.T) {
	// Anchored at local noon, not real wall-clock "now": a +1h offset off
	// the real current time can cross local midnight (caught live by a
	// run at 23:30 local, where "now+1h" landed on the next calendar
	// day), making this test flaky depending on when it happens to run.
	local := time.Now().Local()
	noon := time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, local.Location())
	now := noon.UTC()
	reset := noon.Add(time.Hour).UTC()
	got := ResetClockString(reset, now)
	want := reset.Local().Format("15:04")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResetClockStringDifferentDayIncludesMonthAndDay(t *testing.T) {
	now := time.Now().UTC()
	// +30h always crosses at least one local midnight, regardless of the
	// test machine's timezone offset, unlike a same-day fixed hour would.
	reset := now.Add(30 * time.Hour)
	got := ResetClockString(reset, now)
	want := reset.Local().Format("Jan 2 15:04")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildTokenStatusNoOAuth(t *testing.T) {
	if got := BuildTokenStatus(`{"primaryApiKey":"x"}`); got != "" {
		t.Fatalf("expected empty for a non-oauth credential, got %q", got)
	}
}
