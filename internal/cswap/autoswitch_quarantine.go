package cswap

import (
	"fmt"
	"log/slog"
)

// quarantine mirrors _quarantine: takes an account out of rotation,
// recording its refresh-token fingerprint so a later re-login can be
// detected and auto-released.
func (e *AutoSwitchEngine) quarantine(number, email, reason string) error {
	creds := e.Switcher.ReadAccountCredentials(number, email)
	var fingerprint any
	if creds != "" {
		fingerprint = CredentialFingerprint(creds)
	}

	_, err := e.mutateState(func(state map[string]any) {
		quarantine, ok := state["quarantine"].(map[string]any)
		if !ok {
			quarantine = map[string]any{}
			state["quarantine"] = quarantine
		}
		quarantine[number] = map[string]any{
			"email":                   email,
			"reason":                  reason,
			"at":                      GetTimestamp(),
			"refreshTokenFingerprint": fingerprint,
		}
	})
	if err != nil {
		return err
	}
	e.Emit(NewQuarantineEvent(number, email, reason))
	return nil
}

// releaseRecoveredQuarantines mirrors _release_recovered_quarantines:
// drops quarantine entries whose credential was replaced since. A
// changed refresh-token fingerprint (or a removed/re-added slot) means
// the user re-logged in and re-captured the account, so the dead lineage
// is gone and it re-enters rotation.
func (e *AutoSwitchEngine) releaseRecoveredQuarantines(state map[string]any) (map[string]any, error) {
	quarantine, ok := state["quarantine"].(map[string]any)
	if !ok || len(quarantine) == 0 {
		return state, nil
	}

	type release struct {
		number, email, reason string
	}
	var toRelease []release
	for number, rawEntry := range quarantine {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			// Python's entry.get("email") raises AttributeError here,
			// visibly failing the whole tick (an ErrorEvent, repeating
			// every tick until the state file is fixed). Go's defensive
			// style skips this one malformed entry instead so the
			// engine keeps running for every other account, but that
			// would otherwise be a silent, indefinitely-stuck-in-
			// quarantine failure with no operator-visible signal at
			// all; log it so it is at least discoverable.
			slog.Default().Warn(fmt.Sprintf("autoswitch: quarantine entry for account-%s is malformed (not an object), skipping", number))
			continue
		}
		wantEmail, _ := entry["email"].(string)
		emailNow := e.Switcher.AccountEmail(number)
		if emailNow == "" || emailNow != wantEmail {
			toRelease = append(toRelease, release{number, wantEmail, "account-replaced"})
			continue
		}
		creds := e.Switcher.ReadAccountCredentials(number, emailNow)
		var fingerprint any
		if creds != "" {
			fingerprint = CredentialFingerprint(creds)
		}
		if fingerprint != entry["refreshTokenFingerprint"] {
			toRelease = append(toRelease, release{number, emailNow, "credentials-replaced"})
		}
	}
	if len(toRelease) == 0 {
		return state, nil
	}

	newState, err := e.mutateState(func(s map[string]any) {
		q, ok := s["quarantine"].(map[string]any)
		if !ok {
			return
		}
		for _, r := range toRelease {
			delete(q, r.number)
		}
	})
	if err != nil {
		return nil, err
	}
	for _, r := range toRelease {
		e.Emit(NewUnquarantineEvent(r.number, r.email, r.reason))
	}
	return newState, nil
}
