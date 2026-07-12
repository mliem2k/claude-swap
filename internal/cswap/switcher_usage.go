package cswap

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// FetchStaggerS mirrors _FETCH_STAGGER_S: delay between successive usage-
// request launches in one collect pass.
const FetchStaggerS = 250 * time.Millisecond

// AccountUsageInfo mirrors the (num, email, org_name, org_uuid, is_active,
// creds) tuple _build_accounts_info returns. Named distinctly from models.go's
// AccountInfo (the sequence-registry serialization type) since the two are
// unrelated shapes despite the similar name in the Python source's comments.
type AccountUsageInfo struct {
	Number      int
	Email       string
	OrgName     string
	OrgUUID     string
	IsActive    bool
	Credentials string
}

// BuildAccountsInfo mirrors _build_accounts_info: per-account info, with the
// active slot's credentials read from the live store and every other slot's
// from its backup.
func (s *ClaudeAccountSwitcher) BuildAccountsInfo() []AccountUsageInfo {
	data := s.GetSequenceDataMigrated()
	if data == nil {
		data = &SequenceData{Accounts: map[string]AccountRecord{}}
	}
	currentEmail, currentOrgUUID, hasCurrent := s.GetCurrentAccount()

	activeNum := ""
	if hasCurrent {
		activeNum = FindAccountSlot(data, currentEmail, currentOrgUUID)
	}

	s.ActiveKeychainUnavailable = false
	var infos []AccountUsageInfo
	for _, num := range data.Sequence {
		numStr := strconv.Itoa(num)
		account := data.Accounts[numStr]
		isActive := numStr == activeNum

		var creds string
		if isActive {
			active := s.ReadActiveCredentials()
			creds = active.Value
			s.ActiveKeychainUnavailable = active.KeychainUnavailable
		} else {
			creds = s.ReadAccountCredentials(numStr, account.Email)
		}

		infos = append(infos, AccountUsageInfo{
			Number: num, Email: account.Email, OrgName: account.OrganizationName,
			OrgUUID: account.OrganizationUUID, IsActive: isActive, Credentials: creds,
		})
	}
	return infos
}

// ActiveClaudeCodeRunning mirrors _active_cc_running: fails closed (assumes
// an owner exists) if instance detection errors, so an active token is never
// refreshed out from under a process we failed to see. (The Go port's
// GetRunningInstances does not itself error, matching the fail-closed intent
// by simply never treating "detection ran" as absence of an owner.)
func (s *ClaudeAccountSwitcher) ActiveClaudeCodeRunning() bool {
	sessions, ides := GetRunningInstances(GetClaudeConfigHome())
	return len(sessions) > 0 || len(ides) > 0
}

// FetchActiveUsage mirrors _fetch_active_usage: usage fetch for the active
// account, refreshing its token only when no owner (default-profile claude
// or a live cswap run session) is detected. Does NOT hold any lock across
// the network refresh: FileLock is non-reentrant, and holding it across
// persistActive's re-acquire would deadlock.
func (s *ClaudeAccountSwitcher) FetchActiveUsage(accountNum, email, creds string) FetchRecord {
	oauthData := ExtractOAuthData(creds)
	accessToken, _ := oauthData["accessToken"].(string)
	if oauthData == nil || accessToken == "" {
		return FetchRecord{Sentinel: UsageNoCredentials}
	}

	owned := s.ActiveClaudeCodeRunning() || len(s.LiveSessionPIDs(accountNum, email)) > 0

	unattributed := false
	if !owned {
		backup := s.ReadAccountCredentials(accountNum, email)
		unattributed = creds != backup && CredentialFingerprint(creds) != CredentialFingerprint(backup)
		if unattributed {
			s.Logger.Warn("active credential does not match stored backup; skipping refresh", "account", accountNum)
		}
	}

	if owned || unattributed {
		if IsOAuthTokenExpired(oauthData["expiresAt"]) {
			return FetchRecord{Sentinel: UsageTokenExpired}
		}
		outcome := TryFetchUsageForAccount(accountNum, email, creds, true, nil, s.Logger)
		if outcome.Usage == nil && IsOAuthTokenExpired(oauthData["expiresAt"]) {
			return FetchRecord{Sentinel: UsageTokenExpired}
		}
		return FetchRecord{Usage: outcome.Usage, Error: outcome.Error, RetryAfterS: outcome.RetryAfterS}
	}

	originalRefresh, _ := oauthData["refreshToken"].(string)
	persistSkipped := false

	persistActive := func(num, acctEmail, newCreds string) error {
		lock := NewFileLock(s.LockFile, 10*time.Second)
		if err := lock.Lock(10 * time.Second); err != nil {
			persistSkipped = true
			return err
		}
		defer lock.Release()

		ctx := context.Background()
		return WithCredentialsLock(ctx, 0, func() error {
			return WithConfigLock(ctx, 0, func() error {
				live := s.ReadCredentials()
				liveOAuth := ExtractOAuthData(live)
				liveRefresh, _ := liveOAuth["refreshToken"].(string)

				if s.ActiveClaudeCodeRunning() || len(s.LiveSessionPIDs(num, acctEmail)) > 0 || liveRefresh != originalRefresh {
					persistSkipped = true
					s.Logger.Warn("owner appeared or refresh token changed mid-refresh; discarding rotated credential",
						"account", num)
					return nil
				}
				if err := s.WriteCredentials(newCreds); err != nil {
					persistSkipped = true
					return err
				}
				return s.WriteAccountCredentials(num, acctEmail, newCreds)
			})
		})
	}

	outcome := TryFetchUsageForAccount(accountNum, email, creds, false, persistActive, s.Logger)
	if persistSkipped {
		return FetchRecord{Sentinel: UsageTokenExpired}
	}
	if outcome.Usage == nil && IsOAuthTokenExpired(oauthData["expiresAt"]) {
		return FetchRecord{Sentinel: UsageTokenExpired}
	}
	return FetchRecord{Usage: outcome.Usage, Error: outcome.Error, RetryAfterS: outcome.RetryAfterS}
}

// StaticUsageSentinel mirrors _static_usage_sentinel: a sentinel derivable
// without any network call, or "" (Python's None). Re-derived every pass.
func (s *ClaudeAccountSwitcher) StaticUsageSentinel(info AccountUsageInfo) string {
	if LooksLikeAPIKey(info.Credentials) {
		return UsageAPIKey
	}
	if info.Credentials == "" || ExtractAccessToken(info.Credentials) == "" {
		if info.IsActive && s.ActiveKeychainUnavailable {
			return UsageKeychainUnavailable
		}
		return UsageNoCredentials
	}
	if info.IsActive {
		oauthData := ExtractOAuthData(info.Credentials)
		if oauthData != nil && IsOAuthTokenExpired(oauthData["expiresAt"]) {
			numStr := strconv.Itoa(info.Number)
			if s.ActiveClaudeCodeRunning() || len(s.LiveSessionPIDs(numStr, info.Email)) > 0 {
				return UsageTokenExpired
			}
		}
	}
	return ""
}

// FetchAccountUsage mirrors _fetch_account_usage: one network fetch for one
// account. Never panics; failures land in FetchRecord.Error.
func (s *ClaudeAccountSwitcher) FetchAccountUsage(info AccountUsageInfo) FetchRecord {
	numStr := strconv.Itoa(info.Number)
	if info.IsActive {
		return s.FetchActiveUsage(numStr, info.Email, info.Credentials)
	}

	persist := func(acctNum, acctEmail, newCreds string) error {
		lock := NewFileLock(s.LockFile, 10*time.Second)
		if err := lock.Lock(10 * time.Second); err != nil {
			return err
		}
		defer lock.Release()
		return s.WriteAccountCredentials(acctNum, acctEmail, newCreds)
	}

	hasLiveSession := len(s.LiveSessionPIDs(numStr, info.Email)) > 0

	sessionDir := s.SessionDir(numStr, info.Email)
	sessionCreds := ReadSessionCredentials(sessionDir)
	if sessionCreds != "" && SessionIdentityDrifted(sessionDir, info.Email, info.OrgUUID) {
		s.Logger.Debug("session profile logged in as a different account; using backup credential", "account", numStr)
		sessionCreds = ""
		hasLiveSession = false
	}
	if sessionCreds != "" {
		sessionOAuth := ExtractOAuthData(sessionCreds)
		if accessToken, _ := sessionOAuth["accessToken"].(string); accessToken != "" {
			if !IsOAuthTokenExpired(sessionOAuth["expiresAt"]) {
				outcome := TryFetchUsageForAccount(numStr, info.Email, sessionCreds, true, nil, s.Logger)
				return FetchRecord{Usage: outcome.Usage, Error: outcome.Error, RetryAfterS: outcome.RetryAfterS}
			}
			if hasLiveSession {
				return FetchRecord{Sentinel: UsageTokenExpired}
			}
			// Expired profile credential, no live session: fall through to backup.
		}
	}

	outcome := TryFetchUsageForAccount(numStr, info.Email, info.Credentials, hasLiveSession, persist, s.Logger)
	return FetchRecord{Usage: outcome.Usage, Error: outcome.Error, RetryAfterS: outcome.RetryAfterS}
}

// RunUsageFetches mirrors _run_usage_fetches: fetches the given accounts
// concurrently, staggering start times so N accounts never hit the endpoint
// in the same instant.
func (s *ClaudeAccountSwitcher) RunUsageFetches(infos []AccountUsageInfo) map[string]FetchRecord {
	results := make(map[string]FetchRecord, len(infos))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for idx, info := range infos {
		wg.Add(1)
		go func(idx int, info AccountUsageInfo) {
			defer wg.Done()
			if idx > 0 {
				time.Sleep(time.Duration(idx) * FetchStaggerS)
			}
			rec := s.FetchAccountUsage(info)
			mu.Lock()
			results[strconv.Itoa(info.Number)] = rec
			mu.Unlock()
		}(idx, info)
	}
	wg.Wait()
	return results
}

// persistPollPlans mirrors _persist_poll_plans: adapts and persists the
// cadence of every slot just fetched successfully, so the next
// collector, whichever surface it runs in, inherits the plan. Failures
// are paced by the store's backoff instead and keep their (now past-due)
// plan for when the backoff lifts.
func (s *ClaudeAccountSwitcher) persistPollPlans(
	records map[string]FetchRecord,
	pre, post map[string]UsageEntry,
	infoByNum map[string]AccountUsageInfo,
	identities map[string]Identity,
) {
	now := float64(s.UsageStore.clock().Unix())
	threshold, models := s.pollPolicyInputs()
	plans := map[string][2]*float64{}
	for num, rec := range records {
		if rec.Sentinel != "" || rec.Error != "" {
			continue
		}
		after, hasAfter := post[num]
		if !hasAfter || after.FetchedAt == nil {
			continue
		}
		before, hasBefore := pre[num]
		var prevIntervalS *float64
		var prevUsage map[string]any
		var recent429 bool
		if hasBefore {
			prevIntervalS = before.PollIntervalS
			prevUsage = before.LastGood
			recent429 = before.Last429At != nil && now-*before.Last429At < Recent429WindowS
		}
		next, interval := PlanAfterFetch(PlanAfterFetchInput{
			PrevIntervalS: prevIntervalS,
			PrevUsage:     prevUsage,
			NewUsage:      after.LastGood,
			IsActive:      infoByNum[num].IsActive,
			Threshold:     threshold,
			Models:        models,
			Recent429:     recent429,
			Now:           now,
		})
		n, iv := next, interval
		plans[num] = [2]*float64{&n, &iv}
	}
	if len(plans) > 0 {
		s.UsageStore.SetPollPlan(plans, identities)
	}
}

// CollectUsageEntries mirrors _collect_usage_entries: store-backed usage
// collection. fetch nil makes every account eligible (on-demand callers);
// a non-nil set restricts which accounts may be fetched this pass.
//
// Eligibility and the win-the-fetch claim used to be two separate steps
// (a lock-free Entries read to decide, then Claim to mark it): two
// collectors racing this function could both pass the check and both
// fetch the same account, the very sustained-429 cause this port fixes.
// Reserve makes that one atomic, lock-held step instead.
func (s *ClaudeAccountSwitcher) CollectUsageEntries(accountsInfo []AccountUsageInfo, fetch map[string]bool) map[string]UsageEntry {
	store := s.UsageStore
	identities := make(map[string]Identity, len(accountsInfo))
	infoByNum := make(map[string]AccountUsageInfo, len(accountsInfo))
	for _, info := range accountsInfo {
		numStr := strconv.Itoa(info.Number)
		identities[numStr] = Identity{Email: info.Email, OrganizationUUID: info.OrgUUID}
		infoByNum[numStr] = info
	}

	sentinels := map[string]string{}
	for num, info := range infoByNum {
		if static := s.StaticUsageSentinel(info); static != "" {
			sentinels[num] = static
		}
	}

	entries := store.Entries(identities)
	for num := range infoByNum {
		if _, has := sentinels[num]; !has && entries[num].TokenDead(AuthDeadStrikes) {
			sentinels[num] = UsageReloginRequired
		}
	}

	var requested []string
	for num := range infoByNum {
		if _, has := sentinels[num]; has {
			continue
		}
		if fetch != nil && !fetch[num] {
			continue
		}
		requested = append(requested, num)
	}
	// respectPlans is fetch == nil: an on-demand caller (list/status/switch
	// strategies) passes fetch == nil, mirroring Python's "fetch is None"
	// meaning every account is a candidate but plans are still respected;
	// the auto engine supplies an explicit fetch set, mirroring Python's
	// respect_plans=fetch is None evaluating to False for that caller.
	toFetch := store.Reserve(requested, identities, fetch == nil)

	if len(toFetch) > 0 {
		pre := entries
		var infosToFetch []AccountUsageInfo
		for _, num := range toFetch {
			infosToFetch = append(infosToFetch, infoByNum[num])
		}
		records := s.RunUsageFetches(infosToFetch)
		store.Record(records, identities)
		for num, rec := range records {
			if rec.Sentinel != "" {
				sentinels[num] = rec.Sentinel
			}
		}
		entries = store.Entries(identities)
		s.persistPollPlans(records, pre, entries, infoByNum, identities)
		for _, num := range toFetch {
			if entries[num].TokenDead(AuthDeadStrikes) {
				sentinels[num] = UsageReloginRequired
			}
		}
	}

	out := make(map[string]UsageEntry, len(infoByNum))
	for num := range infoByNum {
		if sentinel, has := sentinels[num]; has {
			out[num] = WithSentinel(entries[num], sentinel)
		} else {
			out[num] = entries[num]
		}
	}
	return out
}

// UsageByAccount mirrors _usage_by_account: account number to decision-grade
// usage value.
func (s *ClaudeAccountSwitcher) UsageByAccount() map[string]any {
	accountsInfo := s.BuildAccountsInfo()
	entries := s.CollectUsageEntries(accountsInfo, nil)
	out := make(map[string]any, len(entries))
	for num, entry := range entries {
		out[num] = entry.DecisionValue()
	}
	return out
}
