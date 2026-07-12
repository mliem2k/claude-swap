package cswap

import "strings"

// FreshenBufferMs mirrors FRESHEN_BUFFER_MS: freshen targets whose access
// token expires within this window, twice Claude Code's own 5-minute
// refresh buffer, so its post-lock "abort refresh if not expired" re-read
// holds with margin after our swap.
const FreshenBufferMs = 10 * 60 * 1000

// freshenTarget mirrors _freshen_target: ensures a candidate's stored
// token outlives Claude Code's 5-min refresh buffer before it gets
// activated.
//
// Returns "ok", "invalid_grant" (dead lineage, quarantine),
// "identity-conflict" (alive but authenticates as a different account,
// quarantine, do not activate), "transient" (network trouble, try again
// next tick), or "skip-live-session". Only ever touches the slot's
// backup store; the active credential belongs to Claude Code.
//
// Returns a non-nil error ONLY when a successful refresh's credentials
// fail to persist: Python lets that specific failure propagate as an
// unhandled exception up to tick()'s outer handler (a whole-tick
// ErrorEvent), rather than reporting it through this function's own
// "transient" outcome, which would wrongly invite a retry after the
// refresh token's generation was already consumed. See this task's
// Interfaces note for the full rationale. Every other path returns
// (result, nil).
func (e *AutoSwitchEngine) freshenTarget(number, email string) (string, error) {
	if e.Switcher.AccountKind(number) == "api_key" {
		return "ok", nil // API keys don't expire/refresh
	}
	if len(e.Switcher.LiveSessionPIDs(number, email)) > 0 {
		// A live `cswap run` session owns this account's token in its own
		// profile. Auto-activating it as the default login too would put
		// one rotating refresh token in two config dirs (the stale-copy
		// failure class) with nobody reading the warning, and its quota
		// is already being consumed by that session anyway. Manual
		// switch_to keeps its warn-and-proceed behavior; auto skips.
		return "skip-live-session", nil
	}
	creds := e.Switcher.ReadAccountCredentials(number, email)
	if creds == "" {
		return "transient", nil
	}
	data := ExtractOAuthData(creds)
	if data == nil {
		return "invalid_grant", nil
	}
	nowMs := float64(e.Clock().UnixMilli())
	nearExpiry := false
	if expiresAt, ok := data["expiresAt"].(float64); ok {
		nearExpiry = nowMs+float64(FreshenBufferMs) >= expiresAt
	}
	if !nearExpiry {
		return "ok", nil
	}
	outcome := TryRefreshOAuthCredentials(creds)
	if outcome.Error == "" && outcome.Credentials != "" {
		// Persist first, unconditionally: the grant consumed a
		// generation, and not writing the successor would kill the
		// lineage regardless of whose it turns out to be.
		if err := e.Switcher.PersistBackupCredentials(number, email, outcome.Credentials); err != nil {
			return "", err
		}
		if e.noteTokenIdentity(number, outcome.TokenAccount) {
			// The slot's stored credential authenticates as a different
			// account, activating it would put the user on the wrong
			// account with every gauge reading normal. Not a viable
			// target; the caller quarantines it (released automatically
			// once the credential is replaced by a re-add).
			return "identity-conflict", nil
		}
		return "ok", nil
	}
	if outcome.Error == "invalid_grant" || outcome.Error == "no_refresh_token" {
		return "invalid_grant", nil
	}
	return "transient", nil
}

// noteTokenIdentity mirrors _note_token_identity: uses the token
// endpoint's free identity to verify/backfill a slot.
//
// The refresh grant just ran against the slot's own stored credential,
// so tokenAccount (when the server includes it) names who that
// credential really is. Returns true on a conflict: the credential
// authenticates under a different organization than the slot records
// (org compared first, whenever both sides record one), or as a
// different account uuid. An empty slot uuid (blank-uuid records from
// older versions, add-token placeholders) is backfilled, but only when
// no org conflict exists: a wrong-org credential is evidence the slot
// holds the wrong account, and backfilling its uuid would poison the
// slot's identity record (BackfillAccountUUID never overwrites a
// non-empty uuid, so that corruption would be sticky).
func (e *AutoSwitchEngine) noteTokenIdentity(number string, tokenAccount map[string]any) bool {
	if tokenAccount == nil {
		return false
	}
	taUUIDRaw, _ := tokenAccount["uuid"].(string)
	taUUID := strings.TrimSpace(taUUIDRaw)
	if taUUID == "" {
		return false
	}
	slotIdentity := e.Switcher.AccountIdentity(number)
	taOrg, _ := tokenAccount["organizationUuid"].(string)
	slotOrg := slotIdentity.OrganizationUUID
	if taOrg != "" && slotOrg != "" && taOrg != slotOrg {
		return true
	}
	if slotIdentity.UUID == "" {
		if err := e.Switcher.BackfillAccountUUID(number, taUUID); err != nil {
			// Never let bookkeeping break a freshen: log and continue,
			// matching Python's own try/except around this call.
			e.Switcher.Logger.Debug("uuid backfill failed", "account", number, "error", err)
		}
		return false
	}
	return slotIdentity.UUID != taUUID
}
