package cswap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageStoreEntriesEmptyWhenNoRow(t *testing.T) {
	store := NewUsageStore(t.TempDir(), time.Now)
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	entries := store.Entries(identities)
	e, ok := entries["1"]
	if !ok || e.LastGood != nil {
		t.Fatalf("expected an empty entry, got %#v", e)
	}
}

func TestUsageStoreRecordThenEntries(t *testing.T) {
	store := NewUsageStore(t.TempDir(), time.Now)
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	usage := map[string]any{"five_hour": map[string]any{"pct": 5.0}}
	store.Record(map[string]FetchRecord{"1": {Usage: usage}}, identities)

	entries := store.Entries(identities)
	e := entries["1"]
	if e.LastGood == nil {
		t.Fatal("expected lastGood to be recorded")
	}
	if e.ConsecutiveFailures != 0 {
		t.Fatalf("expected zero failures after success, got %d", e.ConsecutiveFailures)
	}
}

func TestUsageStoreRecordFailureSetsBackoffWithoutTouchingLastGood(t *testing.T) {
	store := NewUsageStore(t.TempDir(), time.Now)
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	usage := map[string]any{"five_hour": map[string]any{"pct": 5.0}}
	store.Record(map[string]FetchRecord{"1": {Usage: usage}}, identities)
	store.Record(map[string]FetchRecord{"1": {Error: "timeout"}}, identities)

	entries := store.Entries(identities)
	e := entries["1"]
	if e.LastGood == nil {
		t.Fatal("a failure must not erase the prior last-good measurement")
	}
	if e.ConsecutiveFailures != 1 {
		t.Fatalf("expected 1 consecutive failure, got %d", e.ConsecutiveFailures)
	}
	if e.BackoffUntil == nil {
		t.Fatal("expected a backoff window to be set")
	}
}

func TestUsageStoreIdentityMismatchInvisible(t *testing.T) {
	store := NewUsageStore(t.TempDir(), time.Now)
	original := map[string]Identity{"1": {Email: "a@example.com"}}
	store.Record(map[string]FetchRecord{"1": {Usage: map[string]any{"x": 1.0}}}, original)

	reused := map[string]Identity{"1": {Email: "different@example.com"}}
	entries := store.Entries(reused)
	if entries["1"].LastGood != nil {
		t.Fatal("a slot reused by a different identity must not see the old row")
	}
}

func TestUsageStoreClaimStampsLastAttemptAt(t *testing.T) {
	store := NewUsageStore(t.TempDir(), time.Now)
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	store.Claim([]string{"1"}, identities)
	entries := store.Entries(identities)
	if entries["1"].LastAttemptAt == nil {
		t.Fatal("expected lastAttemptAt to be stamped")
	}
}

func TestUsageStoreClearDeadTokenResetsStrikes(t *testing.T) {
	store := NewUsageStore(t.TempDir(), time.Now)
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	store.Record(map[string]FetchRecord{"1": {Error: "invalid_grant"}}, identities)
	if !store.Entries(identities)["1"].TokenDead(AuthDeadStrikes) {
		t.Fatal("expected token dead after invalid_grant")
	}
	store.ClearDeadToken([]string{"1"}, identities)
	if store.Entries(identities)["1"].TokenDead(AuthDeadStrikes) {
		t.Fatal("expected quarantine lifted after ClearDeadToken")
	}
}

// TestUsageStoreMalformedFieldOnOneAccountDoesNotBlankOthers is the
// regression test for the malformed-field resilience the module's own
// docstring calls a hard invariant: "one failed round trip no longer
// blanks every account." Account "1"'s consecutiveFailures is a string
// (wrong JSON type); account "2" is entirely well-formed. Only "1"'s bad
// field should degrade to its zero value, "2" must read normally.
func TestUsageStoreMalformedFieldOnOneAccountDoesNotBlankOthers(t *testing.T) {
	dir := t.TempDir()
	table := map[string]any{
		"schemaVersion": UsageSchemaVersion,
		"accounts": map[string]any{
			"1": map[string]any{
				"email":               "a@example.com",
				"lastGood":            map[string]any{"x": 1.0},
				"consecutiveFailures": "not-a-number", // wrong type
			},
			"2": map[string]any{
				"email":               "b@example.com",
				"lastGood":            map[string]any{"y": 2.0},
				"consecutiveFailures": 3.0,
			},
		},
	}
	body, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "usage.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewUsageStore(dir, time.Now)
	identities := map[string]Identity{
		"1": {Email: "a@example.com"},
		"2": {Email: "b@example.com"},
	}
	entries := store.Entries(identities)

	e1 := entries["1"]
	if e1.LastGood == nil {
		t.Fatal("account 1's lastGood should still be readable despite its malformed consecutiveFailures field")
	}
	if e1.ConsecutiveFailures != 0 {
		t.Fatalf("account 1's malformed consecutiveFailures should degrade to 0, got %d", e1.ConsecutiveFailures)
	}

	e2 := entries["2"]
	if e2.LastGood == nil || e2.ConsecutiveFailures != 3 {
		t.Fatalf("account 2 should be entirely unaffected by account 1's malformed field, got %#v", e2)
	}
}

// TestUsageStoreMalformedRowShapeOnOneAccountDoesNotBlankOthers covers the
// more severe case: account "1"'s entire row is not even a JSON object
// (a bare string), which previously made the whole table's
// json.Unmarshal fail and wipe every account, not just "1".
func TestUsageStoreMalformedRowShapeOnOneAccountDoesNotBlankOthers(t *testing.T) {
	dir := t.TempDir()
	table := map[string]any{
		"schemaVersion": UsageSchemaVersion,
		"accounts": map[string]any{
			"1": "this is not an object at all",
			"2": map[string]any{
				"email":    "b@example.com",
				"lastGood": map[string]any{"y": 2.0},
			},
		},
	}
	body, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "usage.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewUsageStore(dir, time.Now)
	identities := map[string]Identity{
		"1": {Email: "a@example.com"},
		"2": {Email: "b@example.com"},
	}
	entries := store.Entries(identities)

	if entries["1"].LastGood != nil {
		t.Fatalf("account 1's malformed row should read as empty, got %#v", entries["1"])
	}
	e2 := entries["2"]
	if e2.LastGood == nil {
		t.Fatal("account 2 should be entirely unaffected by account 1's malformed row shape")
	}
}

func TestUsageStorePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	identities := map[string]Identity{"1": {Email: "a@example.com"}}
	store1 := NewUsageStore(dir, time.Now)
	store1.Record(map[string]FetchRecord{"1": {Usage: map[string]any{"x": 1.0}}}, identities)

	store2 := NewUsageStore(dir, time.Now)
	if store2.Entries(identities)["1"].LastGood == nil {
		t.Fatal("expected data to persist to disk and be read by a fresh store")
	}
	if _, err := filepath.Abs(dir); err != nil {
		t.Fatal(err)
	}
}
