package cswap

import (
	"errors"
	"fmt"
	"testing"
)

func TestUsageToJSONFiveHourSevenDay(t *testing.T) {
	usage := map[string]any{
		"five_hour": map[string]any{"pct": 42.0},
		"seven_day": map[string]any{"pct": 10.0},
	}
	got := UsageToJSON(usage)
	fh := got["fiveHour"].(map[string]any)
	if fh["pct"] != 42.0 {
		t.Fatalf("got %#v", got)
	}
	sd := got["sevenDay"].(map[string]any)
	if sd["pct"] != 10.0 {
		t.Fatalf("got %#v", got)
	}
}

func TestUsageToJSONSpendAndScoped(t *testing.T) {
	usage := map[string]any{
		"spend":  map[string]any{"used": 1.5, "limit": 20.0, "pct": 7.5, "currency": "USD"},
		"scoped": []map[string]any{{"name": "Fable", "pct": 90.0}},
	}
	got := UsageToJSON(usage)
	spend := got["spend"].(map[string]any)
	if spend["currency"] != "USD" || spend["used"] != 1.5 {
		t.Fatalf("got %#v", got)
	}
	scoped := got["scoped"].([]map[string]any)
	if len(scoped) != 1 || scoped[0]["name"] != "Fable" {
		t.Fatalf("got %#v", got)
	}
}

func TestUsageFieldsMapsDictToOK(t *testing.T) {
	status, usage := UsageFields(map[string]any{"five_hour": map[string]any{"pct": 1.0}})
	if status != "ok" || usage == nil {
		t.Fatalf("got status=%q usage=%#v", status, usage)
	}
}

func TestUsageFieldsMapsSentinels(t *testing.T) {
	cases := map[string]string{
		UsageTokenExpired:        "token_expired",
		UsageAPIKey:              "api_key",
		UsageKeychainUnavailable: "keychain_unavailable",
		UsageReloginRequired:     "relogin_required",
	}
	for sentinel, want := range cases {
		status, usage := UsageFields(sentinel)
		if status != want || usage != nil {
			t.Fatalf("sentinel %q: got status=%q usage=%#v", sentinel, status, usage)
		}
	}
}

func TestUsageFieldsUnknownStringIsNoCredentials(t *testing.T) {
	status, _ := UsageFields("no credentials")
	if status != "no_credentials" {
		t.Fatalf("got %q", status)
	}
}

func TestUsageFieldsNilIsUnavailable(t *testing.T) {
	status, usage := UsageFields(nil)
	if status != "unavailable" || usage != nil {
		t.Fatalf("got status=%q usage=%#v", status, usage)
	}
}

func TestAccountRefWithNumber(t *testing.T) {
	n := 3
	got := AccountRefDict(&n, "a@example.com")
	if got["number"] != 3 || got["email"] != "a@example.com" {
		t.Fatalf("got %#v", got)
	}
}

func TestAccountRefNilNumber(t *testing.T) {
	got := AccountRefDict(nil, "a@example.com")
	if got["number"] != nil {
		t.Fatalf("expected nil number, got %#v", got["number"])
	}
}

func TestUsageFreshnessFieldsEmptyWhenNoFetchedAt(t *testing.T) {
	got := UsageFreshnessFields(nil, nil)
	if len(got) != 0 {
		t.Fatalf("expected no fields, got %#v", got)
	}
}

func TestUsageFreshnessFieldsPopulated(t *testing.T) {
	fetchedAt := 1700000000.0
	ageS := 12.34
	got := UsageFreshnessFields(&fetchedAt, &ageS)
	if got["usageFetchedAt"] == nil || got["usageAgeSeconds"] != 12.3 {
		t.Fatalf("got %#v", got)
	}
}

func TestAccountRowIncludesFreshnessOnlyWithUsage(t *testing.T) {
	row := AccountRow(1, "a@example.com", "Acme", "org1", true, "no credentials", nil, nil)
	if _, present := row["usageFetchedAt"]; present {
		t.Fatalf("no-usage row should not carry freshness fields: %#v", row)
	}
	if row["usageStatus"] != "no_credentials" {
		t.Fatalf("got %#v", row)
	}
}

func TestErrorEnvelope(t *testing.T) {
	got := ErrorEnvelope(errors.New("boom"))
	errMap := got["error"].(map[string]any)
	if errMap["message"] != "boom" || got["schemaVersion"] != JSONSchemaVersion {
		t.Fatalf("got %#v", got)
	}
	if errMap["type"] != "ClaudeSwitchError" {
		t.Fatalf("expected the base type name for an error matching no known sentinel, got %#v", errMap)
	}
}

// TestErrorEnvelopeReportsSpecificErrorType mirrors Python's
// type(exc).__name__: the JSON error envelope's "type" field must name the
// specific domain error, not always the generic base "ClaudeSwitchError",
// so a script branching on error.type can tell these apart the way it
// could against the real Python exception hierarchy.
func TestErrorEnvelopeReportsSpecificErrorType(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrCredentialRead, "CredentialReadError"},
		{ErrCredentialWrite, "CredentialWriteError"},
		{ErrCredential, "CredentialError"},
		{ErrConfig, "ConfigError"},
		{ErrSwitch, "SwitchError"},
		{ErrSession, "SessionError"},
		{ErrClaudeCodeLockTimeout, "ClaudeCodeLockTimeout"},
		{ErrLock, "LockError"},
		{ErrAccountNotFound, "AccountNotFoundError"},
		{ErrValidation, "ValidationError"},
		{ErrTransfer, "TransferError"},
		{ErrMigrationIncomplete, "MigrationIncomplete"},
		{ErrMigration, "MigrationError"},
		{ErrClaudeSwitch, "ClaudeSwitchError"},
	}
	for _, c := range cases {
		got := ErrorEnvelope(c.err)
		errMap := got["error"].(map[string]any)
		if errMap["type"] != c.want {
			t.Errorf("ErrorEnvelope(%v)[\"type\"] = %v, want %q", c.err, errMap["type"], c.want)
		}
	}
}

// TestErrorEnvelopeWrappedErrorStillMatchesMostSpecificType confirms a
// real, wrapped error (as every actual call site produces, e.g. via
// fmt.Errorf("...: %w", ErrCredentialRead)) still resolves to the leaf
// sentinel's name, not a more generic ancestor's, exercising the same
// errors.Is chain-walking a bare sentinel value doesn't.
func TestErrorEnvelopeWrappedErrorStillMatchesMostSpecificType(t *testing.T) {
	wrapped := fmt.Errorf("could not read the stored credential: %w", ErrCredentialRead)
	got := ErrorEnvelope(wrapped)
	errMap := got["error"].(map[string]any)
	if errMap["type"] != "CredentialReadError" {
		t.Fatalf("got %#v", errMap)
	}
}
