package cswap

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// usageAgeNoteS mirrors _USAGE_AGE_NOTE_S: an age note is shown on displayed
// usage older than this.
const usageAgeNoteS = 90.0

// UsageAgeNoteS re-exports usageAgeNoteS for callers outside this package
// (the tui package's data.go FormatAge), so the TUI shows the same
// measurement-age note at the same staleness threshold as `cswap list`'s
// UsageEntryLines, rather than drifting to an unrelated constant.
const UsageAgeNoteS = usageAgeNoteS

// sentinelNotes mirrors SENTINEL_NOTES: human notes for sentinel usage
// states (fallback: the raw sentinel string).
var sentinelNotes = map[string]string{
	UsageTokenExpired:        "token expired, Claude Code refreshes the active account",
	UsageAPIKey:              "API key (no quota)",
	UsageKeychainUnavailable: "keychain unavailable, locked or in use; try again",
	UsageReloginRequired:     "re-login needed, refresh token dead; log in with Claude Code, then run: cswap add",
}

// SentinelLabel mirrors sentinel_label: the same wording `cswap list`
// prints for this sentinel state (the raw sentinel string as a fallback).
func SentinelLabel(sentinel string) string {
	if label, ok := sentinelNotes[sentinel]; ok {
		return label
	}
	return sentinel
}

// formatMoney mirrors Python's f"{value:,.2f}": two decimals, thousands
// separated with commas. Rounding the fractional part can carry into the
// whole part (e.g. 999.999 -> "1,000.00", not the malformed "999.100"), so
// the carry is folded back into whole before grouping.
func formatMoney(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int64(math.Trunc(v))
	frac := int64(math.Round((v - math.Trunc(v)) * 100))
	if frac == 100 {
		frac = 0
		whole++
	}
	digits := fmt.Sprintf("%d", whole)
	var grouped strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(d)
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%s.%02d", sign, grouped.String(), frac)
}

// FormatUsageLines mirrors _format_usage_lines: styled body lines (label
// padded to the widest one so per-model names like "Fable" don't shift the
// other lines' columns) for one account's usage dict.
func FormatUsageLines(usage map[string]any) []string {
	type row struct{ label, body string }
	var rows []row

	if spend, ok := usage["spend"].(map[string]any); ok && spend != nil {
		used, _ := spend["used"].(float64)
		limit, _ := spend["limit"].(float64)
		pct, _ := spend["pct"].(float64)
		if _, clock, ok := FreshResetStrings(spend); ok {
			rows = append(rows, row{"$$", fmt.Sprintf("%3.0f%%   resets %-12s  $%s / $%s",
				pct, clock, formatMoney(used), formatMoney(limit))})
		} else {
			rows = append(rows, row{"$$", fmt.Sprintf("%3.0f%%   $%s / $%s", pct, formatMoney(used), formatMoney(limit))})
		}
	}
	for _, lw := range []struct {
		label string
		key   string
	}{{"5h", "five_hour"}, {"7d", "seven_day"}} {
		w, ok := usage[lw.key].(map[string]any)
		if !ok || w == nil {
			continue
		}
		pct, _ := w["pct"].(float64)
		if countdown, clock, ok := FreshResetStrings(w); ok {
			rows = append(rows, row{lw.label, fmt.Sprintf("%3.0f%%   resets %-12s  in %s", pct, clock, countdown)})
		} else {
			rows = append(rows, row{lw.label, fmt.Sprintf("%3.0f%%", pct)})
		}
	}
	for _, w := range ScopedWindows(usage) {
		name, _ := w["name"].(string)
		pct, _ := w["pct"].(float64)
		marker := ""
		if pct >= 100 {
			marker = "  (!)"
		}
		if countdown, clock, ok := FreshResetStrings(w); ok {
			rows = append(rows, row{name, fmt.Sprintf("%3.0f%%   resets %-12s  in %s%s", pct, clock, countdown, marker)})
		} else {
			rows = append(rows, row{name, fmt.Sprintf("%3.0f%%%s", pct, marker)})
		}
	}

	width := 0
	for _, r := range rows {
		if l := len(r.label) + 1; l > width {
			width = l
		}
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = fmt.Sprintf("%-*s %s", width, r.label+":", r.body)
	}
	return lines
}

// LastSeenNote mirrors last_seen_note: "last seen 53% used - 12m ago" from
// an entry's last-good measurement, or "" (Python's None) when there is
// nothing to report. Public: a future TUI can render the same note under
// sentinel states, so both surfaces stay word-for-word identical.
func LastSeenNote(entry UsageEntry) string {
	if entry.LastGood == nil || entry.FetchedAt == nil {
		return ""
	}
	headroom, ok := AccountHeadroom(entry.LastGood, nil)
	if !ok {
		return ""
	}
	return fmt.Sprintf("last seen %.0f%% used · %s", 100-headroom, FormatAge(int64(*entry.FetchedAt*1000)))
}

// UsageEntryLines mirrors _usage_entry_lines: styled usage lines (sans
// indent) for one account's entry. Sentinel states render their note first,
// with a supplementary "last seen" line when an older measurement exists.
// Measurements render as usual, age-annotated once older than
// usageAgeNoteS (stale-served); an account with no measurement at all shows
// "usage unavailable" plus the last fetch error, so a failing endpoint is
// visible instead of a silent blank.
func UsageEntryLines(entry UsageEntry) []string {
	if entry.Sentinel != nil {
		note, ok := sentinelNotes[*entry.Sentinel]
		if !ok {
			note = *entry.Sentinel
		}
		out := []string{Dimmed(note)}
		if lastSeen := LastSeenNote(entry); lastSeen != "" && *entry.Sentinel != UsageAPIKey {
			out = append(out, fmt.Sprintf("%s %s", Dimmed("└"), Muted(lastSeen)))
		}
		return out
	}
	if entry.LastGood != nil {
		lines := FormatUsageLines(entry.LastGood)
		if len(lines) > 0 && entry.AgeS != nil && *entry.AgeS > usageAgeNoteS && entry.FetchedAt != nil {
			lines[len(lines)-1] += fmt.Sprintf(" · %s", FormatAge(int64(*entry.FetchedAt*1000)))
		}
		out := make([]string, len(lines))
		for j, line := range lines {
			connector := "├"
			if j == len(lines)-1 {
				connector = "└"
			}
			out[j] = fmt.Sprintf("%s %s", Dimmed(connector), Muted(line))
		}
		return out
	}
	detail := "usage unavailable"
	if entry.LastError != "" {
		detail += fmt.Sprintf(" (%s)", entry.LastError)
	}
	return []string{Dimmed(detail)}
}

// BuildListPayload mirrors _build_list_payload: the --list --json payload
// from gathered account + usage data.
func (s *ClaudeAccountSwitcher) BuildListPayload(accountsInfo []AccountUsageInfo, entries map[string]UsageEntry) map[string]any {
	var activeNum any
	accounts := make([]map[string]any, 0, len(accountsInfo))
	for _, info := range accountsInfo {
		if info.IsActive {
			activeNum = info.Number
		}
		entry := entries[strconv.Itoa(info.Number)]
		// JSON carries the decision-grade value: last-good only while it is
		// recent enough to act on. Showing older measurements is a human-
		// display affordance only; scripts keying on usageStatus=="ok" must
		// not act on arbitrarily old data.
		accounts = append(accounts, AccountRow(
			info.Number, info.Email, info.OrgName, info.OrgUUID, info.IsActive,
			entry.DecisionValue(), entry.FetchedAt, entry.AgeS,
		))
	}
	payload := map[string]any{
		"schemaVersion":       JSONSchemaVersion,
		"activeAccountNumber": activeNum,
		"accounts":            accounts,
	}
	// Additive fields (absent when clean), never printed warnings: the JSON
	// contract keeps stdout a single machine-readable object.
	if dup := s.DuplicateAccountWarnings(accountsInfo); len(dup) > 0 {
		payload["duplicateAccountWarnings"] = dup
	}
	if lock := s.LockstepUsageWarnings(accountsInfo, entries); len(lock) > 0 {
		payload["lockstepUsageWarnings"] = lock
	}
	if unclaimed := s.Store.listUnclaimedCredentials(); len(unclaimed) > 0 {
		names := make([]string, 0, len(unclaimed))
		for name := range unclaimed {
			names = append(names, name)
		}
		sort.Strings(names)
		payload["unclaimedCredentials"] = names
	}
	return payload
}

// FirstRunSetup mirrors _first_run_setup: the first-run setup workflow.
// confirm is asked whether to add the current live account to the managed
// list; on acceptance it is also passed through to AddAccount, so any of
// AddAccount's own prompts go through the same callback (matching Python,
// where both prompts are real synchronous input() calls against the same
// stdin).
func (s *ClaudeAccountSwitcher) FirstRunSetup(confirm func(string) bool) (string, error) {
	email, _, ok := s.GetCurrentAccount()
	if !ok {
		return "No active Claude account found. Please log in first.", nil
	}

	if confirm != nil && !confirm(fmt.Sprintf("No managed accounts found. Add current account (%s) to managed list?", email)) {
		return "Setup cancelled. You can run 'cswap add' later.", nil
	}

	return s.AddAccount(nil, confirm)
}

// ActiveAccountUsage mirrors _active_account_usage: a store-backed usage
// entry for just the active account. Builds a single-account info row
// instead of the full accounts list (Status touches one slot) and runs it
// through the shared collector, so freshness/backoff/claim gating and the
// shared usage cache behave exactly as in ListAccounts.
func (s *ClaudeAccountSwitcher) ActiveAccountUsage(accountNum, currentEmail, orgUUID string) UsageEntry {
	active := s.ReadActiveCredentials()
	s.ActiveKeychainUnavailable = active.KeychainUnavailable
	info := AccountUsageInfo{
		Number: mustAtoi(accountNum), Email: currentEmail, OrgUUID: orgUUID,
		IsActive: true, Credentials: active.Value,
	}
	entries := s.CollectUsageEntries([]AccountUsageInfo{info}, nil)
	return entries[accountNum]
}

// BuildStatusPayload mirrors _build_status_payload: the --status --json
// payload (no active / unmanaged / managed).
func (s *ClaudeAccountSwitcher) BuildStatusPayload() map[string]any {
	email, orgUUID, ok := s.GetCurrentAccount()
	if !ok {
		return map[string]any{"schemaVersion": JSONSchemaVersion, "active": nil}
	}

	data := s.GetSequenceDataMigrated()
	if data == nil {
		return map[string]any{
			"schemaVersion": JSONSchemaVersion,
			"active":        map[string]any{"email": email, "managed": false},
		}
	}

	accountNum := FindAccountSlot(data, email, orgUUID)
	if accountNum == "" {
		return map[string]any{
			"schemaVersion": JSONSchemaVersion,
			"active":        map[string]any{"email": email, "managed": false},
		}
	}

	acct := data.Accounts[accountNum]
	entry := s.ActiveAccountUsage(accountNum, email, acct.OrganizationUUID)
	// Decision-grade projection, same rule as the list payload: stale
	// beyond the trust window reports unavailable, not "ok" with old numbers.
	status, usage := UsageFields(entry.DecisionValue())
	active := map[string]any{
		"number":           mustAtoi(accountNum),
		"email":            email,
		"organizationName": acct.OrganizationName,
		"organizationUuid": acct.OrganizationUUID,
		"isOrganization":   acct.OrganizationUUID != "",
		"managed":          true,
		"usageStatus":      status,
		"usage":            usage,
	}
	if usage != nil {
		for k, v := range UsageFreshnessFields(entry.FetchedAt, entry.AgeS) {
			active[k] = v
		}
	}
	return map[string]any{
		"schemaVersion":        JSONSchemaVersion,
		"active":               active,
		"totalManagedAccounts": len(data.Accounts),
	}
}

// StatusResult is what Status returns. In JSON mode only Payload is
// populated; in human mode only Lines is, mirroring ListAccountsResult's
// split for the same reason.
type StatusResult struct {
	Payload map[string]any
	Lines   []string
}

// Status mirrors status: displays current account status (or returns the
// schema-v1 payload).
func (s *ClaudeAccountSwitcher) Status(jsonOutput bool) (*StatusResult, error) {
	if jsonOutput {
		return &StatusResult{Payload: s.BuildStatusPayload()}, nil
	}

	email, orgUUID, ok := s.GetCurrentAccount()
	if !ok {
		return &StatusResult{Lines: []string{
			fmt.Sprintf("%s %s", Bolded("Status:"), Dimmed("No active Claude account")),
		}}, nil
	}

	data := s.GetSequenceDataMigrated()
	if data == nil {
		return &StatusResult{Lines: []string{
			fmt.Sprintf("%s %s %s", Bolded("Status:"), email, Dimmed("(not managed)")),
		}}, nil
	}

	accountNum := FindAccountSlot(data, email, orgUUID)
	orgName := ""
	if accountNum != "" {
		orgName = data.Accounts[accountNum].OrganizationName
	}

	if accountNum == "" {
		return &StatusResult{Lines: []string{
			fmt.Sprintf("%s %s %s", Bolded("Status:"), email, Dimmed("(not managed)")),
		}}, nil
	}

	tag := GetDisplayTag(email, orgName, orgUUID)
	total := len(data.Accounts)
	lines := []string{
		fmt.Sprintf("%s %s (%s %s)", Bolded("Status:"), Accent(fmt.Sprintf("Account-%s", accountNum)), email, Muted(fmt.Sprintf("[%s]", tag))),
		fmt.Sprintf("  %s", Dimmed(fmt.Sprintf("Total managed accounts: %d", total))),
	}
	entry := s.ActiveAccountUsage(accountNum, email, orgUUID)
	for _, line := range UsageEntryLines(entry) {
		lines = append(lines, fmt.Sprintf("  %s", line))
	}
	return &StatusResult{Lines: lines}, nil
}

// ListAccountsResult is what ListAccounts returns for a given call. In JSON
// mode (jsonOutput=true) only Payload is populated; in human mode only
// Lines and NeedsFirstRunSetup are, mirroring Python's json_output branch
// (which either returns the payload dict or prints and returns None).
type ListAccountsResult struct {
	Payload            map[string]any
	Lines              []string
	NeedsFirstRunSetup bool
}

// ListAccounts mirrors list_accounts: lists all managed accounts. In JSON
// mode returns the schema-v1 payload; otherwise returns the human-readable
// display as pre-styled lines instead of printing them directly (real
// terminal output is the CLI layer's job, matching this port's established
// convention for Switch/SwitchTo). fetch restricts which accounts may be
// fetched this pass; nil leaves every stale account eligible.
func (s *ClaudeAccountSwitcher) ListAccounts(showTokenStatus, jsonOutput bool, fetch map[string]bool) (*ListAccountsResult, error) {
	if !fileExists(s.SequenceFile) {
		if jsonOutput {
			return &ListAccountsResult{Payload: map[string]any{
				"schemaVersion":       JSONSchemaVersion,
				"activeAccountNumber": nil,
				"accounts":            []map[string]any{},
			}}, nil
		}
		return &ListAccountsResult{
			Lines:              []string{Dimmed("No accounts are managed yet.")},
			NeedsFirstRunSetup: true,
		}, nil
	}

	accountsInfo := s.BuildAccountsInfo()
	entries := s.CollectUsageEntries(accountsInfo, fetch)

	if jsonOutput {
		return &ListAccountsResult{Payload: s.BuildListPayload(accountsInfo, entries)}, nil
	}

	var lines []string
	lines = append(lines, Bolded("Accounts:"))
	for i, info := range accountsInfo {
		tag := GetDisplayTag(info.Email, info.OrgName, info.OrgUUID)
		// NOTE: a future TUI watch view may parse this output to map rows to
		// accounts for quick-switch: it would rely on the uncolored
		// "  {num}: " prefix and the "(active)" marker below. Keep them
		// intact when tweaking this, matching Python's own comment.
		if info.IsActive {
			marker := fmt.Sprintf(" %s", BoldAccent("(active)"))
			lines = append(lines, fmt.Sprintf("  %d: %s %s%s", info.Number, info.Email, Muted(fmt.Sprintf("[%s]", tag)), marker))
		} else {
			lines = append(lines, fmt.Sprintf("  %d: %s %s", info.Number, info.Email, Muted(fmt.Sprintf("[%s]", tag))))
		}
		for _, line := range UsageEntryLines(entries[strconv.Itoa(info.Number)]) {
			lines = append(lines, fmt.Sprintf("     %s", line))
		}
		if showTokenStatus {
			if status := BuildTokenStatus(info.Credentials); status != "" {
				lines = append(lines, fmt.Sprintf("     %s %s", Dimmed("•"), Muted(status)))
			}
		}
		if i < len(accountsInfo)-1 {
			lines = append(lines, "")
		}
	}

	// Safety copies (unclaimed credentials) are deliberately not surfaced
	// here: users can't act on them directly (recovery is always
	// re-login + cswap add), and with no GC a one-time event would nag
	// forever. They stay in the JSON payload and logs for diagnostics.
	dupWarnings := s.DuplicateAccountWarnings(accountsInfo)
	lockstepWarnings := s.LockstepUsageWarnings(accountsInfo, entries)
	if len(dupWarnings) > 0 || len(lockstepWarnings) > 0 {
		lines = append(lines, "")
		for _, msg := range dupWarnings {
			lines = append(lines, Yellowed(msg))
		}
		for _, msg := range lockstepWarnings {
			lines = append(lines, Yellowed(msg))
		}
	}

	if runningLines := runningInstancesLines(); len(runningLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, Bolded("Running instances:"))
		lines = append(lines, runningLines...)
	}

	return &ListAccountsResult{Lines: lines}, nil
}

// runningInstanceGroup mirrors one (label, cwd) group's tallies.
type runningInstanceGroup struct {
	label, cwd    string
	sessions, ide int
}

// runningInstancesLines mirrors the "Running instances" block inside
// list_accounts: groups sessions and IDE instances by (label, folder) to
// avoid repetitive lines, preserving first-seen order (Python relies on
// dict insertion order; Go maps don't preserve order, so an explicit key
// slice tracks it here).
func runningInstancesLines() []string {
	sessions, ideInstances := GetRunningInstances(GetClaudeConfigHome())
	if len(sessions) == 0 && len(ideInstances) == 0 {
		return nil
	}

	index := map[[2]string]int{}
	var groups []*runningInstanceGroup
	get := func(label, cwd string) *runningInstanceGroup {
		key := [2]string{label, cwd}
		if i, ok := index[key]; ok {
			return groups[i]
		}
		g := &runningInstanceGroup{label: label, cwd: cwd}
		index[key] = len(groups)
		groups = append(groups, g)
		return g
	}
	for _, sess := range sessions {
		g := get(EntrypointLabel(sess.Entrypoint), AbbreviatePath(sess.Cwd))
		g.sessions++
	}
	for _, ide := range ideInstances {
		name := IdeShortName(ide.IdeName)
		for _, folder := range ide.WorkspaceFolders {
			g := get(name, AbbreviatePath(folder))
			g.ide++
		}
	}

	lines := make([]string, 0, len(groups))
	for _, g := range groups {
		var parts []string
		if g.sessions > 0 {
			plural := ""
			if g.sessions > 1 {
				plural = "s"
			}
			parts = append(parts, fmt.Sprintf("%d session%s", g.sessions, plural))
		}
		if g.ide > 0 {
			parts = append(parts, "IDE")
		}
		lines = append(lines, fmt.Sprintf("  %s %s   %s  %s",
			Dimmed("●"), Muted(g.label), Muted(g.cwd), Dimmed(fmt.Sprintf("(%s)", strings.Join(parts, ", ")))))
	}
	return lines
}
