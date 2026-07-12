package cswap

import "time"

// JSONSchemaVersion mirrors SCHEMA_VERSION. Bump only on a breaking change to
// any payload shape.
const JSONSchemaVersion = 1

// Sentinel strings mirroring the USAGE_* constants. Exact string values
// matter: they are compared against and also rendered in human mode.
const (
	UsageNoCredentials       = "no credentials"
	UsageTokenExpired        = "token expired"
	UsageAPIKey              = "api key"
	UsageKeychainUnavailable = "keychain unavailable"
	UsageReloginRequired     = "re-login needed"
)

// windowToJSON mirrors _window_to_json.
func windowToJSON(entry map[string]any) map[string]any {
	out := map[string]any{"pct": entry["pct"]}
	if resetsAt, ok := entry["resets_at"].(string); ok && resetsAt != "" {
		out["resetsAt"] = resetsAt
	}
	if countdown, clock, ok := FreshResetStrings(entry); ok {
		out["countdown"] = countdown
		out["clock"] = clock
	}
	return out
}

// scopedWindowToJSON mirrors _scoped_window_to_json.
func scopedWindowToJSON(entry map[string]any) map[string]any {
	out := windowToJSON(entry)
	out["name"] = entry["name"]
	return out
}

// UsageToJSON mirrors usage_to_json: the internal usage dict to its
// camelCase JSON projection. Sub-keys are emitted only when present.
func UsageToJSON(usage map[string]any) map[string]any {
	out := map[string]any{}
	if fh, ok := usage["five_hour"].(map[string]any); ok {
		out["fiveHour"] = windowToJSON(fh)
	}
	if sd, ok := usage["seven_day"].(map[string]any); ok {
		out["sevenDay"] = windowToJSON(sd)
	}
	if spend, ok := usage["spend"].(map[string]any); ok {
		spendOut := map[string]any{
			"used":     spend["used"],
			"limit":    spend["limit"],
			"pct":      spend["pct"],
			"currency": spend["currency"],
		}
		if resetsAt, ok := spend["resets_at"].(string); ok && resetsAt != "" {
			spendOut["resetsAt"] = resetsAt
		}
		if countdown, clock, ok := FreshResetStrings(spend); ok {
			spendOut["countdown"] = countdown
			spendOut["clock"] = clock
		}
		out["spend"] = spendOut
	}
	if scoped := ScopedWindows(usage); len(scoped) > 0 {
		projected := make([]map[string]any, len(scoped))
		for i, w := range scoped {
			projected[i] = scopedWindowToJSON(w)
		}
		out["scoped"] = projected
	}
	return out
}

// UsageFields mirrors usage_fields: maps a collected usage entry to
// (usageStatus, usage|nil). entry is one of: map[string]any (a usage dict),
// one of the Usage* sentinel strings, another string (treated as
// no_credentials, matching the Python isinstance(str) branch), or nil.
func UsageFields(entry any) (string, map[string]any) {
	switch v := entry.(type) {
	case map[string]any:
		return "ok", UsageToJSON(v)
	case string:
		switch v {
		case UsageTokenExpired:
			return "token_expired", nil
		case UsageAPIKey:
			return "api_key", nil
		case UsageKeychainUnavailable:
			return "keychain_unavailable", nil
		case UsageReloginRequired:
			return "relogin_required", nil
		default:
			return "no_credentials", nil
		}
	default:
		return "unavailable", nil
	}
}

// AccountRefDict mirrors account_ref: a minimal account reference for switch
// from/to fields, as the JSON-boundary dict shape. number nil mirrors
// Python's None. Named AccountRefDict (not AccountRef) to avoid colliding
// with the AccountRef struct in models.go, which is the internal plumbing
// type switch methods pass around before it is serialized via this function.
func AccountRefDict(number *int, email string) map[string]any {
	var n any
	if number != nil {
		n = *number
	}
	return map[string]any{"number": n, "email": email}
}

// UsageFreshnessFields mirrors usage_freshness_fields: additive fields
// describing how old a served usage measurement is. Emitted only alongside
// a non-nil usage (callers check that separately, matching the Python).
func UsageFreshnessFields(fetchedAt, ageS *float64) map[string]any {
	if fetchedAt == nil {
		return map[string]any{}
	}
	fields := map[string]any{
		"usageFetchedAt": unixToRFC3339(*fetchedAt),
	}
	if ageS != nil {
		fields["usageAgeSeconds"] = roundTo1Decimal(*ageS)
	}
	return fields
}

func roundTo1Decimal(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

func unixToRFC3339(epochSeconds float64) string {
	return time.Unix(int64(epochSeconds), 0).UTC().Format("2006-01-02T15:04:05Z")
}

// AccountRow mirrors account_row: a full account row for --list.
func AccountRow(
	number int, email, orgName, orgUUID string, active bool, usageEntry any,
	usageFetchedAt, usageAgeS *float64,
) map[string]any {
	status, usage := UsageFields(usageEntry)
	row := map[string]any{
		"number":           number,
		"email":            email,
		"organizationName": orgName,
		"organizationUuid": orgUUID,
		"isOrganization":   orgUUID != "",
		"active":           active,
		"usageStatus":      status,
		"usage":            usage,
	}
	if usage != nil {
		for k, v := range UsageFreshnessFields(usageFetchedAt, usageAgeS) {
			row[k] = v
		}
	}
	return row
}

// ErrorEnvelope mirrors error_envelope: the structured error payload emitted
// on a handled ClaudeSwitchError.
func ErrorEnvelope(err error) map[string]any {
	return map[string]any{
		"schemaVersion": JSONSchemaVersion,
		"error": map[string]any{
			"type":    errorClassName(err),
			"message": err.Error(),
		},
	}
}
