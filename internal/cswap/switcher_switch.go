package cswap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// SelectBestSwitchable mirrors _select_best_switchable: decides the "best"
// strategy target relative to the current account. Returns (target, note).
// target is "" (Python's None) when staying put; note explains why:
// "none", "current-unavailable", "no-comparison", "incomplete-comparison",
// "stay", "exhausted", or "" when target is non-empty.
func (s *ClaudeAccountSwitcher) SelectBestSwitchable(currentNum string, models []string, usage map[string]any) (string, string) {
	data := s.GetSequenceData()
	if data == nil {
		data = &SequenceData{}
	}
	var others []string
	for _, n := range data.Sequence {
		numStr := strconv.Itoa(n)
		if numStr != currentNum && s.AccountIsSwitchable(numStr) {
			others = append(others, numStr)
		}
	}
	if len(others) == 0 {
		return "", "none"
	}

	if usage == nil {
		usage = s.UsageByAccount()
	}
	currentUsage, _ := usage[currentNum].(map[string]any)
	currentHeadroom, currentOK := AccountHeadroom(currentUsage, models)
	if !currentOK {
		return "", "current-unavailable"
	}

	type scoredEntry struct {
		headroom float64
		ok       bool
		num      string
	}
	var scored []scoredEntry
	for _, num := range others {
		u, _ := usage[num].(map[string]any)
		h, ok := AccountHeadroom(u, models)
		scored = append(scored, scoredEntry{headroom: h, ok: ok, num: num})
	}

	var known []scoredEntry
	for _, e := range scored {
		if e.ok {
			known = append(known, e)
		}
	}
	if len(known) == 0 {
		return "", "no-comparison"
	}

	best := known[0]
	for _, e := range known[1:] {
		if e.headroom > best.headroom {
			best = e
		}
	}
	if best.headroom > currentHeadroom {
		return best.num, ""
	}

	for _, e := range scored {
		if !e.ok {
			return "", "incomplete-comparison"
		}
	}
	if currentHeadroom <= 0 {
		return "", "exhausted"
	}
	return "", "stay"
}

// WarnInertModels mirrors _warn_inert_models: a one-shot typo guard for
// --model on the manual strategies. Appends to warnings when a configured
// model name matches no account's usage windows. Only claimed when every
// account's usage is readable (an unreadable account could be the one
// carrying the window).
func (s *ClaudeAccountSwitcher) WarnInertModels(usage map[string]any, models []string, warnings *[]string) {
	wanted := map[string]string{}
	for _, m := range models {
		lower := strings.ToLower(m)
		if lower != "all" {
			wanted[lower] = m
		}
	}
	if len(wanted) == 0 || len(usage) == 0 {
		return
	}
	for _, v := range usage {
		if _, ok := v.(map[string]any); !ok {
			return
		}
	}
	seen := map[string]bool{}
	for _, v := range usage {
		u, _ := v.(map[string]any)
		for _, entry := range ScopedWindows(u) {
			name, _ := entry["name"].(string)
			if name != "" {
				seen[strings.ToLower(name)] = true
			}
		}
	}
	var missing []string
	for low, name := range wanted {
		if !seen[low] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return
	}
	*warnings = append(*warnings, "model(s) "+strings.Join(missing, ", ")+" match no account's usage windows (typo?)")
}

// DuplicateAccountWarnings mirrors _duplicate_account_warnings: slots that
// provably authenticate as the same account (identical credential
// fingerprint, or the same non-empty uuid+org recorded for two slots).
func (s *ClaudeAccountSwitcher) DuplicateAccountWarnings(accountsInfo []AccountUsageInfo) []string {
	data := s.GetSequenceData()
	if data == nil {
		data = &SequenceData{Accounts: map[string]AccountRecord{}}
	}
	byFP := map[string]string{}
	type identityKey struct{ uuid, org string }
	byIdentity := map[identityKey]string{}
	var out []string

	for _, info := range accountsInfo {
		numStr := strconv.Itoa(info.Number)
		if info.Credentials != "" {
			fp := CredentialFingerprint(info.Credentials)
			if fp != "" {
				if other, ok := byFP[fp]; ok {
					out = append(out, "Account-"+other+" and Account-"+numStr+" hold the same credential ("+
						info.Email+"): one slot's backup was overwritten. Log in with the missing account and re-add it: cswap add --slot N")
				} else {
					byFP[fp] = numStr
				}
			}
		}
		uuid := ""
		if rec, ok := data.Accounts[numStr]; ok {
			uuid = strings.TrimSpace(rec.UUID)
		}
		if uuid != "" {
			org := info.OrgUUID
			key := identityKey{uuid, org}
			if other, ok := byIdentity[key]; ok && other != numStr {
				out = append(out, "Account-"+other+" and Account-"+numStr+" both authenticate as "+
					info.Email+": remove or re-login one of them.")
			} else if !ok {
				byIdentity[key] = numStr
			}
		}
	}
	return out
}

// LockstepUsageWarnings mirrors _lockstep_usage_warnings: a heuristic
// flagging slots whose usage moves in perfect lockstep (both five_hour and
// seven_day pct and resets_at identical), which two different generations of
// the same account produce even though they carry different fingerprints.
// Only rows where both windows carry a non-nil resets_at are compared.
func (s *ClaudeAccountSwitcher) LockstepUsageWarnings(accountsInfo []AccountUsageInfo, entries map[string]UsageEntry) []string {
	type lockstepKey struct {
		h5Pct, h5Reset, d7Pct, d7Reset string
	}
	seen := map[lockstepKey]string{}
	var out []string

	for _, info := range accountsInfo {
		numStr := strconv.Itoa(info.Number)
		entry, ok := entries[numStr]
		if !ok {
			continue
		}
		usage, ok := entry.DecisionValue().(map[string]any)
		if !ok {
			continue
		}
		h5, ok1 := usage["five_hour"].(map[string]any)
		d7, ok2 := usage["seven_day"].(map[string]any)
		if !ok1 || !ok2 {
			continue
		}
		h5Reset, _ := h5["resets_at"].(string)
		d7Reset, _ := d7["resets_at"].(string)
		if h5Reset == "" || d7Reset == "" {
			continue
		}
		h5Pct, ok3 := h5["pct"].(float64)
		d7Pct, ok4 := d7["pct"].(float64)
		if !ok3 || !ok4 {
			continue
		}
		key := lockstepKey{
			strconv.FormatFloat(h5Pct, 'f', -1, 64), h5Reset,
			strconv.FormatFloat(d7Pct, 'f', -1, 64), d7Reset,
		}
		if other, ok := seen[key]; ok {
			out = append(out, "Account-"+other+" and Account-"+numStr+
				" report identical usage and reset times: they may be the same account (issue #117). "+
				"If it persists, log in with the missing account and re-add it: cswap add --slot N")
		} else {
			seen[key] = numStr
		}
	}
	return out
}

// LiveIdentityResolution mirrors the {"live", "resolved"} dict
// _prefetch_live_identity returns.
type LiveIdentityResolution struct {
	Live     string
	Resolved map[string]any
}

// PrefetchLiveIdentity mirrors _prefetch_live_identity: resolves the live
// credential's owner BEFORE any locks are taken (this method may hit the
// network). Only calls the profile endpoint when the live bytes diverge from
// the current slot's stored backup and carry an access token.
func (s *ClaudeAccountSwitcher) PrefetchLiveIdentity() LiveIdentityResolution {
	result := LiveIdentityResolution{}
	live := s.ReadCredentials()
	result.Live = live
	if live == "" {
		return result
	}
	email, orgUUID, ok := s.GetCurrentAccount()
	if !ok {
		return result
	}
	data := s.GetSequenceData()
	slot := FindAccountSlot(data, email, orgUUID)
	if slot == "" {
		return result
	}
	backup := s.ReadAccountCredentials(slot, email)
	if backup == live || CredentialFingerprint(backup) == CredentialFingerprint(live) {
		return result // provenance already established locally
	}
	accessToken := ExtractAccessToken(live)
	if accessToken == "" {
		return result // raw API key / garbled JSON: nothing to resolve
	}
	result.Resolved = FetchOAuthProfile(accessToken)
	return result
}

// LiveMatchesSlotBackup mirrors _live_matches_slot_backup: whether the live
// credential is provably the slot's stored lineage (byte or fingerprint
// equality). An unreadable/empty live credential returns true (keep the
// no-op: forcing a switch on missing evidence would fail later anyway).
func (s *ClaudeAccountSwitcher) LiveMatchesSlotBackup(slot, email string) bool {
	live := s.ReadCredentials()
	if live == "" {
		return true
	}
	backup := s.ReadAccountCredentials(slot, email)
	if backup == "" {
		return false
	}
	return live == backup || CredentialFingerprint(live) == CredentialFingerprint(backup)
}

// ClassifyOutgoingCredential mirrors _classify_outgoing_credential: decides
// what the switch-time backup may do with the live credential. See the
// Python docstring (switcher.py _classify_outgoing_credential) for the full
// rationale behind each of the seven outcomes; the check order here matches
// it exactly and must not be reordered. The priority order (from the brief's
// critical invariant) is: 1 own-bytes, 2 own-family, 3 unresolved
// (no-resolution or stale-resolution), 4 own-rotated, 5 alien, 6 foreign,
// 7 foreign-synced. uuid-match checks always run before the org-scoped
// fallback checks; getting that order wrong silently misclassifies a
// credential and can destroy a refresh token.
func (s *ClaudeAccountSwitcher) ClassifyOutgoingCredential(
	currentAccount, currentEmail, originalCreds string,
	provenance LiveIdentityResolution, data *SequenceData,
) (string, string) {
	backup := s.ReadAccountCredentials(currentAccount, currentEmail)

	// Priority 1: own-bytes. Byte-identical to the slot's stored backup:
	// nothing changed, nothing to capture.
	if backup != "" && backup == originalCreds {
		return "own-bytes", ""
	}
	// Priority 2: own-family. Same refresh-token lineage (fingerprint match)
	// even though the access token rotated: back up normally.
	if backup != "" && CredentialFingerprint(backup) == CredentialFingerprint(originalCreds) {
		return "own-family", ""
	}

	resolved := provenance.Resolved
	// Priority 3: unresolved (no-resolution or stale-resolution branch).
	// Either the identity oracle never ran/returned nothing, or the live
	// bytes moved since the pre-lock read (provenance.Live no longer
	// matches originalCreds), making the resolution stale and untrustworthy.
	if resolved == nil || provenance.Live != originalCreds {
		return "unresolved", ""
	}

	rEmail, _ := resolved["email"].(string)
	rOrg, _ := resolved["organizationUuid"].(string)
	rUUID := strings.TrimSpace(stringOrEmpty(resolved["uuid"]))

	// Priority 4: own-rotated (outgoing-slot uuid match, checked first).
	// Robust to partial responses (a drifted schema may drop email/
	// organization) and to an account whose email changed. Organization
	// must agree only when both sides record one: the codebase's usual
	// leniency for org matching.
	own := data.Accounts[currentAccount]
	ownUUID := strings.TrimSpace(own.UUID)
	ownOrg := own.OrganizationUUID
	if rUUID != "" && ownUUID != "" && rUUID == ownUUID && (rOrg == "" || ownOrg == "" || rOrg == ownOrg) {
		return "own-rotated", ""
	}

	// Org-scoped fallback lookup: only after the uuid-match-first check
	// above has had its chance. Resolve by (email, org) first.
	slot := ""
	if rEmail != "" {
		slot = FindAccountSlot(data, rEmail, rOrg)
	}
	if slot != "" && rUUID != "" {
		// When both sides carry a uuid it must agree: an email+org match
		// with a conflicting uuid is a different account wearing a recycled
		// email (e.g. deleted/recreated claude.ai account), and treating it
		// as the slot would poison the slot's backup.
		storedUUID := strings.TrimSpace(data.Accounts[slot].UUID)
		if storedUUID != "" && storedUUID != rUUID {
			slot = ""
		}
	}
	if slot == "" && rUUID != "" {
		// Fall back to the account uuid (org-scoped) in case the slot's
		// stored email is stale or synthesized (add-token placeholder).
		for num, acct := range data.Accounts {
			if acct.UUID != "" && acct.UUID == rUUID && acct.OrganizationUUID == rOrg {
				slot = num
				break
			}
		}
	}

	// Priority 4 (continued): own-rotated also covers the case where the
	// org-scoped fallback resolves back to the outgoing slot itself.
	if slot == currentAccount {
		return "own-rotated", ""
	}
	if slot == "" {
		// Priority 5: alien requires a structurally complete identity
		// (email plus organization present) matching nothing. A partial
		// one is indistinguishable from schema drift and must fail open
		// like any other oracle degradation, i.e. priority 3: unresolved.
		if rEmail != "" && resolved["organizationUuid"] != nil {
			return "alien", ""
		}
		return "unresolved", ""
	}

	// A cross-slot attribution must be uuid-positive: an email+org match
	// against a slot with no recorded uuid (add-token placeholder) is not
	// evidence enough to name that slot in user output. Priority 5: alien.
	storedUUID := strings.TrimSpace(data.Accounts[slot].UUID)
	if rUUID == "" || storedUUID != rUUID {
		return "alien", ""
	}

	foreignEmail := data.Accounts[slot].Email
	foreignBackup := s.ReadAccountCredentials(slot, foreignEmail)
	// Priority 7: foreign-synced. The other managed slot's stored backup
	// already holds this exact lineage: nothing needs preserving, nothing
	// may be written.
	if foreignBackup != "" && (foreignBackup == originalCreds || CredentialFingerprint(foreignBackup) == CredentialFingerprint(originalCreds)) {
		return "foreign-synced", slot
	}
	// Priority 6: foreign. uuid-positively resolved to another managed slot
	// holding a different lineage: backing it up here would destroy this
	// slot's only refresh token (issue #117's poisoning).
	return "foreign", slot
}

// stringOrEmpty type-asserts v to a string, returning "" for any other type
// (including nil), mirroring the Python `resolved.get("uuid") or ""` idiom.
func stringOrEmpty(v any) string {
	str, _ := v.(string)
	return str
}

// SelfSwitchAction mirrors _self_switch_action: how to treat a switch that
// targets the already-active slot. Returns (action, provenance):
//   - "noop", nil: live matches the slot's backup, nothing to do.
//   - "reconcile", &provenance: live diverged and its owner was resolved,
//     run the full switch so classification can decide what to do.
//   - "noop-diverged", nil: live diverged but ownership could not be
//     established (offline, endpoint failure, no profile access).
func (s *ClaudeAccountSwitcher) SelfSwitchAction(slot, email string) (string, *LiveIdentityResolution) {
	if s.LiveMatchesSlotBackup(slot, email) {
		return "noop", nil
	}
	provenance := s.PrefetchLiveIdentity()
	if provenance.Resolved == nil {
		s.Logger.Info("live credential diverges from stored backup and ownership could not be verified; self-switch left everything untouched",
			"slot", slot)
		return "noop-diverged", nil
	}
	return "reconcile", &provenance
}

// StashLiveCredential mirrors _stash_live_credential: preserves an unowned
// live credential before it is overwritten. Returns an error (rather than
// panicking) on failure: a successful stash is the caller's license to
// overwrite the live store, since the bytes may be the only live copy of
// some account's refresh token.
func (s *ClaudeAccountSwitcher) StashLiveCredential(
	originalCreds, reason, currentAccount string, resolved map[string]any,
) (string, error) {
	var credsMtime any
	if info, err := os.Stat(GetCredentialsPath()); err == nil {
		credsMtime = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
	}

	var liveOauthAccount any
	if config := s.ReadJSON(s.GetClaudeConfigPath()); config != nil {
		liveOauthAccount = config["oauthAccount"]
	}

	entryID, err := s.Store.writeUnclaimedCredential(originalCreds, map[string]any{
		"reason":           reason,
		"configSlot":       currentAccount,
		"fingerprint":      CredentialFingerprint(originalCreds),
		"liveOauthAccount": liveOauthAccount,
		"resolvedIdentity": resolved,
		"credentialsMtime": credsMtime,
	})
	if err != nil {
		return "", err
	}
	mtimeLabel := "unknown"
	if credsMtime != nil {
		mtimeLabel = credsMtime.(string)
	}
	s.Logger.Warn("live credential does not belong to this account, stashed",
		"account", currentAccount, "reason", reason, "entryId", entryID, "credentialsMtime", mtimeLabel)
	return entryID, nil
}

// PerformSwitchOp mirrors the {"from", "to", "warnings"} dict _perform_switch
// returns.
type PerformSwitchOp struct {
	From     *AccountRef
	To       *AccountRef
	Warnings []string
}

// PerformSwitch mirrors _perform_switch: performs the actual account switch
// with transaction support. emit_output's human-printing branches are
// omitted here (Plan 7 prints from the returned PerformSwitchOp); the
// warnings that Python conditionally prints vs. appends always land in
// Warnings here.
func (s *ClaudeAccountSwitcher) PerformSwitch(targetAccount string, forceActivate bool, provenance *LiveIdentityResolution) (*PerformSwitchOp, error) {
	var warningsOut []string

	preData := s.GetSequenceData()
	if preData == nil {
		preData = &SequenceData{Accounts: map[string]AccountRecord{}}
	}
	preEmail := preData.Accounts[targetAccount].Email
	if preEmail != "" {
		pids := s.LiveSessionPIDs(targetAccount, preEmail)
		if len(pids) > 0 {
			warningsOut = append(warningsOut, fmt.Sprintf(
				"Account-%s (%s) has a live session-mode Claude instance (PID %v). Running the same account as both "+
					"the default login and a session can make one copy's token go stale if the server rotates it. "+
					"If the session later fails to authenticate, exit it and re-run 'cswap run %s'.",
				targetAccount, preEmail, pids, targetAccount))
		}
	}

	if provenance == nil {
		if forceActivate {
			provenance = &LiveIdentityResolution{}
		} else {
			resolved := s.PrefetchLiveIdentity()
			provenance = &resolved
		}
	}

	lock := NewFileLock(s.LockFile, 10*time.Second)
	if err := lock.Lock(10 * time.Second); err != nil {
		return nil, err
	}
	defer lock.Release()

	var op *PerformSwitchOp
	var opErr error
	ctx := context.Background()
	lockErr := WithCredentialsLock(ctx, 0, func() error {
		return WithConfigLock(ctx, 0, func() error {
			op, opErr = s.performSwitchLocked(targetAccount, forceActivate, *provenance, warningsOut)
			return nil
		})
	})
	if lockErr != nil {
		return nil, lockErr
	}
	return op, opErr
}

// performSwitchLocked runs the mutation. Must only be called with cswap's own
// lock and Claude Code's two advisory locks all held.
func (s *ClaudeAccountSwitcher) performSwitchLocked(
	targetAccount string, forceActivate bool, provenance LiveIdentityResolution, warningsOut []string,
) (*PerformSwitchOp, error) {
	data := s.GetSequenceData()
	if data == nil {
		return nil, fmt.Errorf("no accounts are managed yet: %w", ErrConfig)
	}
	activeAccount := ""
	if data.ActiveAccountNumber != nil {
		activeAccount = strconv.Itoa(*data.ActiveAccountNumber)
	}
	currentAccount := activeAccount
	targetEmail := data.Accounts[targetAccount].Email
	targetNum := mustAtoi(targetAccount)
	toRef := &AccountRef{Number: &targetNum, Email: targetEmail}

	currentEmail, currentOrgUUID, hasCurrent := s.GetCurrentAccount()
	if hasCurrent {
		currentAccount = FindAccountSlot(data, currentEmail, currentOrgUUID)
	}

	configPath := s.GetClaudeConfigPath()

	if forceActivate || !hasCurrent || currentAccount == "" {
		var fromRef *AccountRef
		if !hasCurrent {
			fromRef = nil
		} else if currentAccount == "" {
			fromRef = &AccountRef{Email: currentEmail}
		} else {
			n := mustAtoi(currentAccount)
			fromRef = &AccountRef{Number: &n, Email: currentEmail}
		}

		targetCreds := s.ReadAccountCredentials(targetAccount, targetEmail)
		targetConfig := s.ReadAccountConfig(targetAccount, targetEmail)
		if targetCreds == "" {
			return nil, fmt.Errorf("account-%s has no stored credentials, re-add with: cswap add --slot %s: %w",
				targetAccount, targetAccount, ErrSwitch)
		}
		if targetConfig == "" {
			return nil, fmt.Errorf("account-%s has no stored config backup, re-add with: cswap add --slot %s: %w",
				targetAccount, targetAccount, ErrSwitch)
		}
		var targetConfigData map[string]any
		if err := json.Unmarshal([]byte(targetConfig), &targetConfigData); err != nil {
			return nil, fmt.Errorf("invalid backup config: %v: %w", err, ErrSwitch)
		}
		targetOAuth, _ := targetConfigData["oauthAccount"].(map[string]any)
		if targetOAuth == nil {
			return nil, fmt.Errorf("invalid oauthAccount in backup: %w", ErrSwitch)
		}

		var rollbackCreds string
		var rollbackConfigText string
		haveRollbackConfig := false
		if hasCurrent {
			ac := s.ReadActiveCredentials()
			if ac.FileReadFailed {
				return nil, fmt.Errorf("cannot snapshot live credentials before activation: %w", ErrCredentialRead)
			}
			rollbackCreds = ac.Value
			if fileExists(configPath) {
				text, err := os.ReadFile(configPath)
				if err != nil {
					return nil, fmt.Errorf("cannot snapshot live config before activation: %v: %w", err, ErrConfig)
				}
				rollbackConfigText = string(text)
				haveRollbackConfig = true
			}
		}

		if rollbackCreds != "" && rollbackCreds != targetCreds && hasCurrent {
			stashAccount := currentAccount
			if stashAccount == "" {
				stashAccount = "unmanaged"
			}
			if _, err := s.StashLiveCredential(rollbackCreds, "displaced-live-login", stashAccount, nil); err != nil {
				if !forceActivate {
					return nil, fmt.Errorf("could not preserve the live credential before activation (safety-copy write failed: %v); aborting rather than destroying it: %w",
						err, ErrSwitch)
				}
				warningsOut = append(warningsOut, fmt.Sprintf(
					"could not preserve the replaced live credential (safety-copy write failed: %v); proceeding because force explicitly rewrites the live login", err))
			}
		}

		credsWritten, configWritten := false, false
		writeErr := func() error {
			if err := s.WriteCredentials(targetCreds); err != nil {
				return err
			}
			credsWritten = true

			existingConfig := s.ReadJSON(configPath)
			if existingConfig != nil {
				existingConfig["oauthAccount"] = targetOAuth
				if err := s.WriteJSON(configPath, existingConfig); err != nil {
					return err
				}
			} else {
				if err := s.WriteJSON(configPath, targetConfigData); err != nil {
					return err
				}
			}
			configWritten = true

			data.ActiveAccountNumber = &targetNum
			data.LastUpdated = GetTimestamp()
			return s.WriteJSON(s.SequenceFile, data)
		}()
		if writeErr != nil {
			if configWritten && haveRollbackConfig {
				if err := os.WriteFile(configPath, []byte(rollbackConfigText), 0o600); err != nil {
					s.Logger.Error("failed to rollback config", "error", err)
				}
			}
			if credsWritten && rollbackCreds != "" {
				if err := s.WriteCredentials(rollbackCreds); err != nil {
					s.Logger.Error("failed to rollback credentials", "error", err)
				}
			}
			return nil, fmt.Errorf("could not write the new active account's credentials/config: %v: %w", writeErr, ErrSwitch)
		}

		if forceActivate && hasCurrent {
			s.Logger.Info("activated account (forced, backup of current login skipped)", "account", targetAccount)
		} else {
			s.Logger.Info("activated account (no prior live account)", "account", targetAccount)
		}
		s.ReplanNewActive(targetAccount, targetEmail, data.Accounts[targetAccount].OrganizationUUID)
		return &PerformSwitchOp{From: fromRef, To: toRef, Warnings: warningsOut}, nil
	}

	fromNum := mustAtoi(currentAccount)
	fromRef := &AccountRef{Number: &fromNum, Email: currentEmail}

	originalCreds := s.ReadCredentials()
	if originalCreds == "" {
		return nil, fmt.Errorf("current account credential is empty (keychain unreadable?), refusing to overwrite its backup: %w", ErrCredentialRead)
	}
	originalConfigBytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("claude config file not found: %w", ErrConfig)
	}
	originalConfig := string(originalConfigBytes)

	tx := &SwitchTransaction{
		OriginalCredentials: originalCreds,
		OriginalConfig:      originalConfig,
		OriginalAccountNum:  currentAccount,
		OriginalEmail:       currentEmail,
		ConfigPath:          configPath,
	}

	txErr := func() error {
		kind, foreignSlot := s.ClassifyOutgoingCredential(currentAccount, currentEmail, originalCreds, provenance, data)
		switch kind {
		case "foreign", "alien":
			if _, err := s.StashLiveCredential(originalCreds, kind, currentAccount, provenance.Resolved); err != nil {
				return err
			}
			if kind == "foreign" {
				warningsOut = append(warningsOut, fmt.Sprintf(
					"credential ownership mismatch detected: the live credential was preserved and was not written into account-%s; "+
						"if account-%s later cannot authenticate, log in as it and run: cswap add --slot %s",
					currentAccount, foreignSlot, foreignSlot))
			} else {
				warningsOut = append(warningsOut, fmt.Sprintf(
					"the live login does not match a managed account; it was preserved and not written into account-%s; "+
						"if you need that account, log in as it and run: cswap add", currentAccount))
			}
		case "foreign-synced":
			warningsOut = append(warningsOut, fmt.Sprintf(
				"credential ownership mismatch detected: the live credential already matches account-%s's stored backup, "+
					"so nothing was written into account-%s", foreignSlot, currentAccount))
		case "unresolved":
			if err := s.WriteAccountCredentials(currentAccount, currentEmail, originalCreds); err != nil {
				return err
			}
			if err := s.WriteAccountConfig(currentAccount, currentEmail, originalConfig); err != nil {
				return err
			}
			s.Logger.Info("backed up account (lineage differs, ownership unverified, pre-fix backup)", "account", currentAccount)
		case "own-bytes":
			if err := s.WriteAccountConfig(currentAccount, currentEmail, originalConfig); err != nil {
				return err
			}
			s.Logger.Info("backed up account (config only, credentials unchanged)", "account", currentAccount)
		default: // own-family, own-rotated
			if err := s.WriteAccountCredentials(currentAccount, currentEmail, originalCreds); err != nil {
				return err
			}
			if err := s.WriteAccountConfig(currentAccount, currentEmail, originalConfig); err != nil {
				return err
			}
			if kind == "own-rotated" {
				resolved := provenance.Resolved
				acct := data.Accounts[currentAccount]
				if acct.UUID == "" && resolved != nil {
					if uuid, _ := resolved["uuid"].(string); uuid != "" {
						acct.UUID = uuid
						data.Accounts[currentAccount] = acct
					}
				}
			}
			s.Logger.Info("backed up account", "account", currentAccount)
		}

		targetCreds := s.ReadAccountCredentials(targetAccount, targetEmail)
		targetConfig := s.ReadAccountConfig(targetAccount, targetEmail)
		if targetCreds == "" {
			return fmt.Errorf("account-%s has no stored credentials, re-add with: cswap add --slot %s: %w",
				targetAccount, targetAccount, ErrSwitch)
		}
		if targetConfig == "" {
			return fmt.Errorf("account-%s has no stored config backup, re-add with: cswap add --slot %s: %w",
				targetAccount, targetAccount, ErrSwitch)
		}

		if err := s.WriteCredentials(targetCreds); err != nil {
			return err
		}
		tx.RecordStep("credentials_written")

		var targetConfigData map[string]any
		if err := json.Unmarshal([]byte(targetConfig), &targetConfigData); err != nil {
			return fmt.Errorf("invalid backup config: %v: %w", err, ErrSwitch)
		}
		oauthSection, _ := targetConfigData["oauthAccount"].(map[string]any)
		if oauthSection == nil {
			return fmt.Errorf("invalid oauthAccount in backup: %w", ErrSwitch)
		}
		currentConfigData := s.ReadJSON(configPath)
		if currentConfigData == nil {
			currentConfigData = map[string]any{}
		}
		currentConfigData["oauthAccount"] = oauthSection
		if err := s.WriteJSON(configPath, currentConfigData); err != nil {
			return err
		}
		tx.RecordStep("config_written")

		data.ActiveAccountNumber = &targetNum
		data.LastUpdated = GetTimestamp()
		if err := s.WriteJSON(s.SequenceFile, data); err != nil {
			return err
		}
		tx.RecordStep("sequence_updated")

		s.Logger.Info("switched account", "from", currentAccount, "to", targetAccount)
		return nil
	}()

	if txErr != nil {
		s.Logger.Error("switch failed, attempting rollback", "error", txErr)
		if len(tx.CompletedSteps) > 0 {
			if tx.Rollback(s) {
				s.Logger.Info("rollback successful")
				return nil, fmt.Errorf("switch failed and was rolled back: %v: %w", txErr, ErrSwitch)
			}
			s.Logger.Error("rollback failed")
			return nil, fmt.Errorf("switch failed and rollback also failed: %v; manual recovery may be needed: %w", txErr, ErrSwitch)
		}
		return nil, fmt.Errorf("switch failed before any step completed: %v: %w", txErr, ErrSwitch)
	}

	s.ReplanNewActive(targetAccount, targetEmail, data.Accounts[targetAccount].OrganizationUUID)
	return &PerformSwitchOp{From: fromRef, To: toRef, Warnings: warningsOut}, nil
}

// SwitchResult mirrors the dict switch()/switch_to() build. Switched is
// derived from whether the live identity actually changed (from != to).
type SwitchResult struct {
	Switched bool
	From     *AccountRef
	To       *AccountRef
	Strategy string
	Reason   string
	Message  string
	Warnings []string
}

func switchResultFromOp(op *PerformSwitchOp, strategy string, extraWarnings []string) *SwitchResult {
	switched := !accountRefEqual(op.From, op.To)
	reason, message := "already-active", fmt.Sprintf("Already on Account-%d (%s)", *op.To.Number, op.To.Email)
	if switched {
		reason, message = "switched", fmt.Sprintf("Switched to Account-%d (%s)", *op.To.Number, op.To.Email)
	}
	return &SwitchResult{
		Switched: switched, From: op.From, To: op.To, Strategy: strategy,
		Reason: reason, Message: message,
		Warnings: append(append([]string{}, extraWarnings...), op.Warnings...),
	}
}

func switchNoop(strategy, reason, message string, fromRef, toRef *AccountRef, warnings []string) *SwitchResult {
	if fromRef == nil {
		fromRef = toRef
	}
	return &SwitchResult{
		Switched: false, From: fromRef, To: toRef, Strategy: strategy,
		Reason: reason, Message: message, Warnings: warnings,
	}
}

func accountRefEqual(a, b *AccountRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	if (a.Number == nil) != (b.Number == nil) {
		return false
	}
	if a.Number != nil && *a.Number != *b.Number {
		return false
	}
	return a.Email == b.Email
}

// Switch mirrors switch: rotates to the next account, or jumps by usage-aware
// strategy. strategy is "", "best", or "next-available".
func (s *ClaudeAccountSwitcher) Switch(strategy string, models []string) (*SwitchResult, error) {
	strategyLabel := "rotation"
	if strategy == "best" || strategy == "next-available" {
		strategyLabel = strategy
	} else {
		models = nil
	}
	var warnings []string

	if !fileExists(s.SequenceFile) {
		return nil, fmt.Errorf("no accounts are managed yet: %w", ErrConfig)
	}

	email, orgUUID, hasIdentity := s.GetCurrentAccount()
	s.GetSequenceDataMigrated()

	if !hasIdentity {
		data := s.GetSequenceData()
		if data == nil {
			data = &SequenceData{}
		}
		var preferred int
		if data.ActiveAccountNumber != nil {
			preferred = *data.ActiveAccountNumber
		} else if len(data.Sequence) > 0 {
			preferred = data.Sequence[0]
		}
		if preferred == 0 {
			return nil, fmt.Errorf("no accounts are managed yet: %w", ErrConfig)
		}
		target := strconv.Itoa(preferred)
		if !s.AccountIsSwitchable(target) {
			warnings = append(warnings, fmt.Sprintf("Skipped Account-%s (no stored credentials/config)", target))
			fallback := ""
			for _, num := range data.Sequence {
				numStr := strconv.Itoa(num)
				if numStr != target && s.AccountIsSwitchable(numStr) {
					fallback = numStr
					break
				}
			}
			if fallback == "" {
				return nil, fmt.Errorf("no managed accounts have valid stored credentials/config, re-add a slot with: cswap add --slot <number>: %w", ErrConfig)
			}
			target = fallback
		}
		op, err := s.PerformSwitch(target, false, nil)
		if err != nil {
			return nil, err
		}
		return switchResultFromOp(op, strategyLabel, warnings), nil
	}

	if !s.AccountExists(email, orgUUID) {
		if _, err := s.AddAccount(nil, nil); err != nil {
			return nil, err
		}
		data := s.GetSequenceData()
		num := 0
		if data.ActiveAccountNumber != nil {
			num = *data.ActiveAccountNumber
		}
		return switchNoop(strategyLabel, "auto-added",
			fmt.Sprintf("Active account was not managed; it has been automatically added as Account-%d. Run switch again to switch to the next account.", num),
			nil, &AccountRef{Number: &num, Email: email}, warnings), nil
	}

	data := s.GetSequenceData()
	if len(data.Sequence) < 2 {
		num := FindAccountSlot(data, email, orgUUID)
		var toRef *AccountRef
		if num != "" {
			n := mustAtoi(num)
			toRef = &AccountRef{Number: &n, Email: email}
		}
		return switchNoop(strategyLabel, "only-one-account",
			"Only one account is managed. Add more accounts to switch between.", nil, toRef, warnings), nil
	}

	activeAccount := 0
	if data.ActiveAccountNumber != nil {
		activeAccount = *data.ActiveAccountNumber
	}
	currentNum := FindAccountSlot(data, email, orgUUID)
	if currentNum == "" && activeAccount != 0 {
		currentNum = strconv.Itoa(activeAccount)
	}
	var currentRef *AccountRef
	if currentNum != "" {
		n := mustAtoi(currentNum)
		currentRef = &AccountRef{Number: &n, Email: email}
	}

	if strategy == "best" {
		bestUsage := s.UsageByAccount()
		s.WarnInertModels(bestUsage, models, &warnings)
		target, note := s.SelectBestSwitchable(currentNum, models, bestUsage)
		if target != "" {
			op, err := s.PerformSwitch(target, false, nil)
			if err != nil {
				return nil, err
			}
			return switchResultFromOp(op, strategyLabel, warnings), nil
		}
		switch note {
		case "current-unavailable":
			return switchNoop(strategyLabel, "usage-unavailable",
				fmt.Sprintf("Current account usage is unavailable, staying on Account-%s.", currentNum),
				nil, currentRef, warnings), nil
		case "no-comparison":
			return switchNoop(strategyLabel, "usage-unavailable",
				fmt.Sprintf("No other account has usage data to compare, staying on Account-%s.", currentNum),
				nil, currentRef, warnings), nil
		case "incomplete-comparison":
			return switchNoop(strategyLabel, "usage-unavailable",
				fmt.Sprintf("No account with known usage has more remaining quota; some usage is unavailable, staying on Account-%s.", currentNum),
				nil, currentRef, warnings), nil
		case "stay":
			return switchNoop(strategyLabel, "already-best",
				fmt.Sprintf("Already on the account with the most remaining quota (Account-%s).", currentNum),
				nil, currentRef, warnings), nil
		case "exhausted":
			limitsLabel := "5h/7d limit"
			if len(models) > 0 {
				limitsLabel = "usage limits"
			}
			return switchNoop(strategyLabel, "candidates-exhausted",
				fmt.Sprintf("All accounts are at their %s, staying on Account-%s.", limitsLabel, currentNum),
				nil, currentRef, warnings), nil
		}
	}

	anchor := activeAccount
	if strategy == "next-available" && currentNum != "" {
		anchor = mustAtoi(currentNum)
	}
	currentIndex := indexOfInt(data.Sequence, anchor)
	if currentIndex < 0 {
		currentIndex = indexOfInt(data.Sequence, activeAccount)
	}
	if currentIndex < 0 {
		currentIndex = 0
	}

	var usage map[string]any
	if strategy == "next-available" {
		usage = s.UsageByAccount()
		s.WarnInertModels(usage, models, &warnings)
	}

	nextAccount := ""
	var skippedExhausted []string
	for offset := 1; offset < len(data.Sequence); offset++ {
		candidate := strconv.Itoa(data.Sequence[(currentIndex+offset)%len(data.Sequence)])
		if !s.AccountIsSwitchable(candidate) {
			warnings = append(warnings, fmt.Sprintf("Skipped Account-%s (no stored credentials/config)", candidate))
			continue
		}
		if strategy == "next-available" {
			headroom, ok := AccountHeadroom(mapAny(usage[candidate]), models)
			if ok && headroom <= 0 {
				skippedExhausted = append(skippedExhausted, candidate)
				label := "5h/7d"
				if len(models) > 0 {
					var at []string
					for _, w := range RelevantWindows(mapAny(usage[candidate]), models) {
						if w.Pct >= 100.0 {
							at = append(at, w.Label)
						}
					}
					if len(at) > 0 {
						label = strings.Join(at, "/")
					}
				}
				warnings = append(warnings, fmt.Sprintf("Skipped Account-%s (at %s limit)", candidate, label))
				continue
			}
		}
		nextAccount = candidate
		break
	}

	if nextAccount == "" && len(skippedExhausted) > 0 {
		limitsLabel := "5h/7d limit"
		if len(models) > 0 {
			limitsLabel = "usage limits"
		}
		return switchNoop(strategyLabel, "candidates-exhausted",
			fmt.Sprintf("All other accounts are at their %s, staying on Account-%s.", limitsLabel, currentNum),
			nil, currentRef, warnings), nil
	}
	if nextAccount == "" {
		return switchNoop(strategyLabel, "no-valid-target",
			"No other accounts have valid stored credentials/config.", nil, currentRef, warnings), nil
	}

	var provenance *LiveIdentityResolution
	if nextAccount == currentNum {
		action, prov := s.SelfSwitchAction(nextAccount, email)
		if action != "reconcile" {
			return switchNoop(strategyLabel, "already-active",
				fmt.Sprintf("Already on Account-%s (%s)", nextAccount, email),
				currentRef, currentRef, warnings), nil
		}
		provenance = prov
	}

	op, err := s.PerformSwitch(nextAccount, false, provenance)
	if err != nil {
		return nil, err
	}
	return switchResultFromOp(op, strategyLabel, warnings), nil
}

func indexOfInt(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func mapAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// SwitchTo mirrors switch_to: switches to a specific account. force activates
// the target's stored credentials directly, skipping both the already-active
// no-op guard and the backup-current step. disambiguate resolves an ambiguous
// email match interactively; a "" return cancels (mirrors Python's
// "Cancelled" path). disambiguate is never called when identifier is
// numeric or unambiguous. A nil disambiguate mirrors Python's
// `if not json_output:` guard: the ambiguous match falls through to
// ResolveAccountIdentifier's own informative ConfigError instead of ever
// prompting, the shape a JSON-mode (or otherwise non-interactive) caller
// should pass.
func (s *ClaudeAccountSwitcher) SwitchTo(identifier string, force bool, disambiguate func(candidates []string) string) (*SwitchResult, error) {
	if !fileExists(s.SequenceFile) {
		return nil, fmt.Errorf("no accounts are managed yet: %w", ErrConfig)
	}
	s.GetSequenceDataMigrated()

	if !isAllDigits(identifier) {
		if !s.ValidateEmail(identifier) {
			return nil, fmt.Errorf("invalid email format %q: %w", identifier, ErrValidation)
		}
		data := s.GetSequenceData()
		var matches []string
		if data != nil {
			for num, acc := range data.Accounts {
				if acc.Email == identifier {
					matches = append(matches, num)
				}
			}
		}
		// A nil disambiguate (JSON mode, or a caller with no interactive
		// capability) mirrors Python's `if not json_output:` guard: skip
		// straight to ResolveAccountIdentifier below with identifier
		// untouched, which raises its own informative ambiguous-match
		// ConfigError (listing every candidate and its org tag) instead of
		// a bare, uninformative "cancelled".
		if len(matches) > 1 && disambiguate != nil {
			choice := disambiguate(matches)
			if choice == "" {
				return nil, fmt.Errorf("cancelled: %w", ErrConfig)
			}
			identifier = choice
		}
	}

	targetAccount, err := s.ResolveAccountIdentifier(identifier)
	if err != nil {
		return nil, err
	}
	if targetAccount == "" {
		return nil, fmt.Errorf("no account found with identifier %q: %w", identifier, ErrAccountNotFound)
	}

	data := s.GetSequenceData()
	if _, ok := data.Accounts[targetAccount]; !ok {
		return nil, fmt.Errorf("account-%s does not exist: %w", targetAccount, ErrAccountNotFound)
	}

	var provenance *LiveIdentityResolution
	if !force && data != nil {
		email, orgUUID, hasIdentity := s.GetCurrentAccount()
		if hasIdentity {
			curSlot := FindAccountSlot(data, email, orgUUID)
			if curSlot == targetAccount {
				action, prov := s.SelfSwitchAction(targetAccount, email)
				if action != "reconcile" {
					targetEmail := data.Accounts[targetAccount].Email
					n := mustAtoi(targetAccount)
					ref := &AccountRef{Number: &n, Email: targetEmail}
					return switchNoop("direct", "already-active",
						fmt.Sprintf("Already on Account-%s (%s)", targetAccount, targetEmail),
						ref, ref, nil), nil
				}
				provenance = prov
			}
		}
	}

	op, err := s.PerformSwitch(targetAccount, force, provenance)
	if err != nil {
		return nil, err
	}
	result := switchResultFromOp(op, "direct", nil)
	if force && !result.Switched {
		result.Reason = "activated"
		result.Message = fmt.Sprintf("Activated Account-%d (%s) from stored backup", *result.To.Number, result.To.Email)
	}
	return result, nil
}

// SwitchFollowupMessage mirrors _print_switch_followup: the note after a
// successful switch, keyed to where the active credential write landed.
func (s *ClaudeAccountSwitcher) SwitchFollowupMessage() string {
	backend := s.Store.lastActiveBackend
	if backend == "" {
		if s.Store.useKeychain() {
			backend = "keychain"
		} else {
			backend = "file"
		}
	}
	if backend == "keychain" {
		return "Restart Claude Code to apply immediately, otherwise the session can take up to ~30 seconds to pick up the new account."
	}
	return "New account is active on your next message, no restart needed."
}
