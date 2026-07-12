package cswap

import "testing"

func classifyTestData() *SequenceData {
	return &SequenceData{Accounts: map[string]AccountRecord{
		"1": {Email: "a@example.com", UUID: "u1"},
		"2": {Email: "b@example.com", UUID: "u2"},
	}}
}

func TestClassifyOutgoingCredentialOwnBytes(t *testing.T) {
	s := newTestSwitcher(t)
	creds := `{"claudeAiOauth":{"accessToken":"x"}}`
	_ = s.WriteAccountCredentials("1", "a@example.com", creds)

	kind, slot := s.ClassifyOutgoingCredential("1", "a@example.com", creds, LiveIdentityResolution{}, classifyTestData())
	if kind != "own-bytes" || slot != "" {
		t.Fatalf("got kind=%q slot=%q", kind, slot)
	}
}

func TestClassifyOutgoingCredentialOwnFamily(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"old","refreshToken":"same-r"}}`)
	live := `{"claudeAiOauth":{"accessToken":"new","refreshToken":"same-r"}}`

	kind, _ := s.ClassifyOutgoingCredential("1", "a@example.com", live, LiveIdentityResolution{}, classifyTestData())
	if kind != "own-family" {
		t.Fatalf("got kind=%q", kind)
	}
}

func TestClassifyOutgoingCredentialUnresolvedWhenNoResolution(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-r"}}`)
	live := `{"claudeAiOauth":{"accessToken":"new","refreshToken":"new-r"}}`

	kind, _ := s.ClassifyOutgoingCredential("1", "a@example.com", live,
		LiveIdentityResolution{Live: live, Resolved: nil}, classifyTestData())
	if kind != "unresolved" {
		t.Fatalf("got kind=%q", kind)
	}
}

func TestClassifyOutgoingCredentialUnresolvedWhenStaleProvenance(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-r"}}`)
	live := `{"claudeAiOauth":{"accessToken":"new","refreshToken":"new-r"}}`
	// provenance.Live does not match original_creds: the bytes moved since
	// the pre-lock read, so the resolution is stale and must not be trusted.
	kind, _ := s.ClassifyOutgoingCredential("1", "a@example.com", live,
		LiveIdentityResolution{Live: "different-bytes", Resolved: map[string]any{"uuid": "u1"}}, classifyTestData())
	if kind != "unresolved" {
		t.Fatalf("got kind=%q", kind)
	}
}

func TestClassifyOutgoingCredentialOwnRotated(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-r"}}`)
	live := `{"claudeAiOauth":{"accessToken":"new","refreshToken":"new-r"}}`

	kind, slot := s.ClassifyOutgoingCredential("1", "a@example.com", live,
		LiveIdentityResolution{Live: live, Resolved: map[string]any{"uuid": "u1", "email": "a@example.com"}},
		classifyTestData())
	if kind != "own-rotated" || slot != "" {
		t.Fatalf("got kind=%q slot=%q", kind, slot)
	}
}

func TestClassifyOutgoingCredentialAlienNoMatchingSlot(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-r"}}`)
	live := `{"claudeAiOauth":{"accessToken":"new","refreshToken":"new-r"}}`

	kind, slot := s.ClassifyOutgoingCredential("1", "a@example.com", live,
		LiveIdentityResolution{Live: live, Resolved: map[string]any{
			"uuid": "u-unknown", "email": "stranger@example.com", "organizationUuid": "",
		}},
		classifyTestData())
	if kind != "alien" || slot != "" {
		t.Fatalf("got kind=%q slot=%q", kind, slot)
	}
}

func TestClassifyOutgoingCredentialForeign(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-r"}}`)
	_ = s.WriteAccountCredentials("2", "b@example.com", `{"claudeAiOauth":{"accessToken":"b-tok","refreshToken":"b-r"}}`)
	live := `{"claudeAiOauth":{"accessToken":"new","refreshToken":"new-r"}}`

	kind, slot := s.ClassifyOutgoingCredential("1", "a@example.com", live,
		LiveIdentityResolution{Live: live, Resolved: map[string]any{"uuid": "u2", "email": "b@example.com"}},
		classifyTestData())
	if kind != "foreign" || slot != "2" {
		t.Fatalf("got kind=%q slot=%q", kind, slot)
	}
}

// TestClassifyOutgoingCredentialUUIDConflictVetoesEmailMatch covers the
// uuid-conflict veto (lines 339-348): the email+org lookup finds a slot, but
// that slot's own stored uuid is non-empty and disagrees with the resolved
// uuid. The veto must reset slot back to "", refusing to attribute the
// credential to a slot whose identity provably conflicts. No account in the
// fixture carries the resolved uuid, and organizationUuid is omitted from
// the resolved map entirely (so resolved["organizationUuid"] is the nil
// interface value, not a present-but-empty string), which lands the outcome
// on "unresolved" rather than "alien" at the fork at the end of the
// function.
func TestClassifyOutgoingCredentialUUIDConflictVetoesEmailMatch(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-r"}}`)
	live := `{"claudeAiOauth":{"accessToken":"new","refreshToken":"new-r"}}`

	data := &SequenceData{Accounts: map[string]AccountRecord{
		"1": {Email: "a@example.com", UUID: "u1"},
		// "2" is the wrong-match slot: its email equals rEmail below, but its
		// own stored uuid conflicts with the resolved uuid.
		"2": {Email: "wrong@example.com", UUID: "u-wrong"},
	}}

	kind, slot := s.ClassifyOutgoingCredential("1", "a@example.com", live,
		LiveIdentityResolution{Live: live, Resolved: map[string]any{
			"uuid": "u-resolved", "email": "wrong@example.com",
		}}, data)
	if kind != "unresolved" || slot != "" {
		t.Fatalf("got kind=%q slot=%q, want unresolved/'' (the uuid conflict must veto the email match to slot 2, "+
			"and no other slot carries u-resolved for the fallback scan to find)", kind, slot)
	}
}

// TestClassifyOutgoingCredentialUUIDOnlyFallbackFindsOtherSlot covers the
// uuid-only fallback scan (lines 349-358): rEmail is empty, so the
// email-based lookup at line 337 is skipped entirely, but rUUID is set and
// some managed slot other than the outgoing one has a stored
// (uuid, organizationUuid) pair that matches it exactly. The outgoing
// slot's own uuid does not equal rUUID, so the priority-4 own-rotated check
// at line 329 does not fire first and this genuinely reaches the fallback
// loop.
func TestClassifyOutgoingCredentialUUIDOnlyFallbackFindsOtherSlot(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-r"}}`)
	live := `{"claudeAiOauth":{"accessToken":"new","refreshToken":"new-r"}}`

	data := &SequenceData{Accounts: map[string]AccountRecord{
		"1": {Email: "a@example.com", UUID: "u1"},
		"2": {Email: "b@example.com", UUID: "u2", OrganizationUUID: "org-x"},
	}}

	// "foreign" (not "foreign-synced") is asserted: slot 2 has no stored
	// backup at all here, the simplest setup that still proves the result
	// came from the uuid-only fallback scan naming slot 2, not from the
	// (skipped) email lookup.
	kind, slot := s.ClassifyOutgoingCredential("1", "a@example.com", live,
		LiveIdentityResolution{Live: live, Resolved: map[string]any{
			"uuid": "u2", "organizationUuid": "org-x",
		}}, data)
	if kind != "foreign" || slot != "2" {
		t.Fatalf("got kind=%q slot=%q, want foreign/2 via the uuid-only fallback scan", kind, slot)
	}
}

// TestClassifyOutgoingCredentialEmailLookupFoldsBackToOwnSlot covers the
// slot==currentAccount fold-back at line 362, reached via the email+org
// lookup at line 337 rather than via the priority-4 own-rotated check at
// line 329 already having matched. The outgoing slot's own stored uuid is
// "" (e.g. added via an add-token path that never recorded one), so
// priority-4 cannot fire (it requires ownUUID != ""). rEmail/rOrg are set to
// match the outgoing slot's own stored email/org exactly, so
// FindAccountSlot resolves slot back to currentAccount itself; since the
// outgoing slot's own stored uuid is "", the uuid-conflict veto at lines
// 339-348 does not fire either (it only resets slot when
// storedUUID != "" && storedUUID != rUUID).
func TestClassifyOutgoingCredentialEmailLookupFoldsBackToOwnSlot(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-r"}}`)
	live := `{"claudeAiOauth":{"accessToken":"new","refreshToken":"new-r"}}`

	data := &SequenceData{Accounts: map[string]AccountRecord{
		"1": {Email: "a@example.com", UUID: ""},
	}}

	kind, slot := s.ClassifyOutgoingCredential("1", "a@example.com", live,
		LiveIdentityResolution{Live: live, Resolved: map[string]any{
			"uuid": "u-new", "email": "a@example.com", "organizationUuid": "",
		}}, data)
	if kind != "own-rotated" || slot != "" {
		t.Fatalf("got kind=%q slot=%q, want own-rotated/'' via the email-lookup fold-back", kind, slot)
	}
}

func TestClassifyOutgoingCredentialForeignSynced(t *testing.T) {
	s := newTestSwitcher(t)
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-r"}}`)
	live := `{"claudeAiOauth":{"accessToken":"new","refreshToken":"live-r"}}`
	_ = s.WriteAccountCredentials("2", "b@example.com", live) // slot 2 already holds this exact lineage

	kind, slot := s.ClassifyOutgoingCredential("1", "a@example.com", live,
		LiveIdentityResolution{Live: live, Resolved: map[string]any{"uuid": "u2", "email": "b@example.com"}},
		classifyTestData())
	if kind != "foreign-synced" || slot != "2" {
		t.Fatalf("got kind=%q slot=%q", kind, slot)
	}
}
