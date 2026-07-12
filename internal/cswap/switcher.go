package cswap

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// ClaudeAccountSwitcher mirrors ClaudeAccountSwitcher: the multi-account
// switcher for Claude Code.
type ClaudeAccountSwitcher struct {
	Platform       Platform
	BackupDir      string
	SequenceFile   string
	ConfigsDir     string
	CredentialsDir string
	LockFile       string
	Logger         *slog.Logger
	UsageStore     *UsageStore
	Store          *CredentialStore

	// logFile is the raw lumberjack.Logger backing Logger, kept so Purge can
	// close the OS file handle before removing BackupDir (Windows: an open
	// file blocks directory removal).
	logFile *lumberjack.Logger

	// ActiveKeychainUnavailable is set by BuildAccountsInfo: true when the
	// active account's OAuth credential could not be read because the macOS
	// Keychain was unavailable, with no fallback.
	ActiveKeychainUnavailable bool

	// pollInputsCacheMtime/pollInputsCache back pollPolicyInputs' settings-
	// file cache (reloaded only when the file's mtime changes, one stat
	// per pass). pollInputsOverride is the hosted engine's pin, set by
	// SetPollPolicyInputs; nil means "no pin, use the settings file".
	pollInputsCacheMtime *int64
	pollInputsCache      *pollPolicyInputsPair
	pollInputsOverride   *pollPolicyInputsPair

	// pollInputsMu guards pollInputsCacheMtime/pollInputsCache/
	// pollInputsOverride: ApplyThreshold (a TUI-hosted engine's session
	// override) can now write these from a different goroutine than
	// RunLoop's own fetch path reads them from, which the pre-TUI code
	// never had to guard against.
	pollInputsMu sync.Mutex
}

// pollPolicyInputsPair mirrors the (threshold, models) tuple
// set_poll_policy_inputs/_poll_policy_inputs pass around.
type pollPolicyInputsPair struct {
	threshold float64
	models    []string
}

var emailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// NewClaudeAccountSwitcher mirrors ClaudeAccountSwitcher.__init__. Migrates
// legacy backup data before any logger or directory setup writes to the new
// location. migrations.py has no Go equivalent: it exists solely to migrate
// off the third-party keyring library, which this port never depended on.
func NewClaudeAccountSwitcher(debug bool) (*ClaudeAccountSwitcher, error) {
	platform := DetectPlatform()
	backupDir := GetBackupRoot()

	ran, err := MigrateLegacyBackupDir(backupDir)
	if err != nil {
		return nil, err
	}
	if ran {
		fmt.Fprintf(os.Stderr, "claude-swap: migrated data from %s to %s\n",
			GetLegacyBackupRoot(), backupDir)
	}

	logger, logFile := SetupLogging(backupDir, debug)
	s := &ClaudeAccountSwitcher{
		Platform:       platform,
		BackupDir:      backupDir,
		SequenceFile:   filepath.Join(backupDir, "sequence.json"),
		ConfigsDir:     filepath.Join(backupDir, "configs"),
		CredentialsDir: filepath.Join(backupDir, "credentials"),
		LockFile:       filepath.Join(backupDir, ".lock"),
		Logger:         logger,
		logFile:        logFile,
		UsageStore:     NewUsageStore(filepath.Join(backupDir, "cache"), nil),
	}
	s.Store = NewCredentialStore(platform, s.CredentialsDir, logger)
	return s, nil
}

// IsRunningInContainer mirrors _is_running_in_container.
func (s *ClaudeAccountSwitcher) IsRunningInContainer() bool {
	if os.Getenv("CONTAINER") != "" || os.Getenv("container") != "" {
		return true
	}
	if s.Platform == PlatformWindows {
		return false
	}
	if fileExists("/.dockerenv") {
		return true
	}
	if content, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		text := string(content)
		for _, marker := range []string{"docker", "lxc", "containerd", "kubepods"} {
			if strings.Contains(text, marker) {
				return true
			}
		}
	}
	if content, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		text := string(content)
		for _, marker := range []string{"docker", "overlay"} {
			if strings.Contains(text, marker) {
				return true
			}
		}
	}
	return false
}

// GetClaudeConfigPath mirrors _get_claude_config_path.
func (s *ClaudeAccountSwitcher) GetClaudeConfigPath() string {
	return GetGlobalConfigPath()
}

// ValidateEmail mirrors _validate_email.
func (s *ClaudeAccountSwitcher) ValidateEmail(email string) bool {
	return emailPattern.MatchString(email)
}

// SetupDirectories mirrors _setup_directories: creates backup directories
// with 0700 permissions.
func (s *ClaudeAccountSwitcher) SetupDirectories() error {
	for _, dir := range []string{s.BackupDir, s.ConfigsDir, s.CredentialsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(dir, 0o700); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReadJSON mirrors _read_json: nil when missing or invalid, logs a warning
// on invalid JSON.
func (s *ClaudeAccountSwitcher) ReadJSON(path string) map[string]any {
	if !fileExists(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		s.Logger.Warn("invalid json", "path", path)
		return nil
	}
	return out
}

// WriteJSON mirrors _write_json: atomic write with validation.
func (s *ClaudeAccountSwitcher) WriteJSON(path string, data any) error {
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("generate json: %w: %v", ErrConfig, err)
	}
	var scratch any
	if err := json.Unmarshal(content, &scratch); err != nil {
		return fmt.Errorf("generated invalid json: %w", ErrConfig)
	}
	tempPath := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tempPath, content, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// AccountRecord mirrors one entry in sequence.json's "accounts" map.
type AccountRecord struct {
	Email            string `json:"email"`
	UUID             string `json:"uuid"`
	OrganizationUUID string `json:"organizationUuid"`
	OrganizationName string `json:"organizationName"`
	Added            string `json:"added"`
	Kind             string `json:"kind,omitempty"` // "api_key" or "" (oauth default)
}

// SequenceData mirrors sequence.json's shape.
type SequenceData struct {
	ActiveAccountNumber *int                     `json:"activeAccountNumber"`
	LastUpdated         string                   `json:"lastUpdated"`
	Sequence            []int                    `json:"sequence"`
	Accounts            map[string]AccountRecord `json:"accounts"`
}

// InitSequenceFile mirrors _init_sequence_file: creates sequence.json if
// absent.
func (s *ClaudeAccountSwitcher) InitSequenceFile() error {
	if fileExists(s.SequenceFile) {
		return nil
	}
	return s.WriteJSON(s.SequenceFile, SequenceData{
		LastUpdated: GetTimestamp(),
		Sequence:    []int{},
		Accounts:    map[string]AccountRecord{},
	})
}

// GetSequenceData mirrors _get_sequence_data.
func (s *ClaudeAccountSwitcher) GetSequenceData() *SequenceData {
	if !fileExists(s.SequenceFile) {
		return nil
	}
	data, err := os.ReadFile(s.SequenceFile)
	if err != nil {
		return nil
	}
	var out SequenceData
	if err := json.Unmarshal(data, &out); err != nil {
		s.Logger.Warn("invalid json", "path", s.SequenceFile)
		return nil
	}
	if out.Accounts == nil {
		out.Accounts = map[string]AccountRecord{}
	}
	return &out
}

// GetNextAccountNumber mirrors _get_next_account_number.
func (s *ClaudeAccountSwitcher) GetNextAccountNumber() int {
	data := s.GetSequenceData()
	if data == nil || len(data.Accounts) == 0 {
		return 1
	}
	maxNum := 0
	for numStr := range data.Accounts {
		if n, err := strconvAtoi(numStr); err == nil && n > maxNum {
			maxNum = n
		}
	}
	return maxNum + 1
}

// GetCurrentAccount mirrors _get_current_account: the live identity from
// .claude.json.
func (s *ClaudeAccountSwitcher) GetCurrentAccount() (email, orgUUID string, ok bool) {
	configPath := s.GetClaudeConfigPath()
	if !fileExists(configPath) {
		return "", "", false
	}
	data := s.ReadJSON(configPath)
	if data == nil {
		return "", "", false
	}
	oauthAccount, _ := data["oauthAccount"].(map[string]any)
	email, _ = oauthAccount["emailAddress"].(string)
	if email == "" {
		return "", "", false
	}
	orgUUID, _ = oauthAccount["organizationUuid"].(string)
	return email, orgUUID, true
}

// FindAccountSlot mirrors the @staticmethod _find_account_slot: the slot key
// matching (email, organizationUuid), or "" if none.
func FindAccountSlot(data *SequenceData, email, organizationUUID string) string {
	if data == nil {
		return ""
	}
	for num, account := range data.Accounts {
		if account.Email == email && account.OrganizationUUID == organizationUUID {
			return num
		}
	}
	return ""
}

// AccountExists mirrors _account_exists.
func (s *ClaudeAccountSwitcher) AccountExists(email, organizationUUID string) bool {
	return FindAccountSlot(s.GetSequenceData(), email, organizationUUID) != ""
}

// AccountKind mirrors _account_kind: "api_key" or "oauth" (back-compat
// default for slots added before the field existed).
func (s *ClaudeAccountSwitcher) AccountKind(accountNum string) string {
	if accountNum == "" {
		return "oauth"
	}
	data := s.GetSequenceData()
	if data == nil {
		return "oauth"
	}
	record, ok := data.Accounts[accountNum]
	if ok && record.Kind == "api_key" {
		return "api_key"
	}
	return "oauth"
}

// GetDisplayTag mirrors the @staticmethod _get_display_tag.
func GetDisplayTag(email, orgName, orgUUID string) string {
	if orgName != "" {
		return orgName
	}
	return "personal"
}

// ResolveAccountIdentifier mirrors _resolve_account_identifier: NUM or email
// to account number. Returns an error wrapping ErrConfig on an ambiguous
// email match; "" with a nil error when nothing matches.
func (s *ClaudeAccountSwitcher) ResolveAccountIdentifier(identifier string) (string, error) {
	if isAllDigits(identifier) {
		return identifier, nil
	}
	data := s.GetSequenceData()
	if data == nil {
		return "", nil
	}
	var matches []string
	for num, account := range data.Accounts {
		if account.Email == identifier {
			matches = append(matches, num)
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	var details []string
	for _, num := range matches {
		tag := data.Accounts[num].OrganizationName
		if tag == "" {
			tag = "personal"
		}
		details = append(details, fmt.Sprintf("%s [%s]", num, tag))
	}
	return "", fmt.Errorf("email %q is ambiguous, matches accounts: %s; use account number instead: %w",
		identifier, strings.Join(details, ", "), ErrConfig)
}

// ResolveAccount mirrors resolve_account: NUM|EMAIL to (accountNum, email,
// organizationUuid). Unlike ResolveAccountIdentifier (used by switch/remove,
// where the caller degrades an ambiguous match to an interactive prompt),
// ambiguity here is a hard error: session mode ends in an exec, so callers
// need a deterministic resolution before ever launching a subprocess.
func (s *ClaudeAccountSwitcher) ResolveAccount(identifier string) (accountNum, email, orgUUID string, err error) {
	s.GetSequenceDataMigrated()
	accountNum, err = s.ResolveAccountIdentifier(identifier)
	if err != nil {
		return "", "", "", err
	}
	if accountNum == "" {
		return "", "", "", fmt.Errorf("no account found with identifier: %s: %w", identifier, ErrAccountNotFound)
	}
	data := s.GetSequenceData()
	if data == nil {
		return "", "", "", fmt.Errorf("account-%s does not exist: %w", accountNum, ErrAccountNotFound)
	}
	record, ok := data.Accounts[accountNum]
	if !ok {
		return "", "", "", fmt.Errorf("account-%s does not exist: %w", accountNum, ErrAccountNotFound)
	}
	return accountNum, record.Email, record.OrganizationUUID, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func strconvAtoi(s string) (int, error) {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("not a number: %s", s)
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

// GetSequenceDataMigrated mirrors _get_sequence_data_migrated: sequence data
// with org-field migration ensured first.
func (s *ClaudeAccountSwitcher) GetSequenceDataMigrated() *SequenceData {
	data := s.GetSequenceData()
	if data == nil {
		return nil
	}
	if s.sequenceNeedsOrgMigration() {
		_ = s.MigrateOrgFields()
		data = s.GetSequenceData()
	}
	return data
}

// sequenceNeedsOrgMigration checks raw JSON for organizationUuid key
// presence, mirroring Python's `"organizationUuid" not in acc` (a typed
// struct always has the field, so the raw file is the source of truth for
// "was it ever written").
func (s *ClaudeAccountSwitcher) sequenceNeedsOrgMigration() bool {
	raw := s.ReadJSON(s.SequenceFile)
	if raw == nil {
		return false
	}
	accounts, _ := raw["accounts"].(map[string]any)
	for _, v := range accounts {
		acc, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if _, present := acc["organizationUuid"]; !present {
			return true
		}
	}
	return false
}

// ReadAccountConfig mirrors _read_account_config: "" when missing.
func (s *ClaudeAccountSwitcher) ReadAccountConfig(accountNum, email string) string {
	configFile := filepath.Join(s.ConfigsDir, fmt.Sprintf(".claude-config-%s-%s.json", accountNum, email))
	data, err := os.ReadFile(configFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// MigrateOrgFields mirrors _migrate_org_fields: backfills organizationUuid/
// Name for accounts added before org support.
func (s *ClaudeAccountSwitcher) MigrateOrgFields() error {
	raw := s.ReadJSON(s.SequenceFile)
	if raw == nil {
		return nil
	}
	accounts, _ := raw["accounts"].(map[string]any)
	if accounts == nil {
		return nil
	}

	liveEmail, liveOrgUUID, liveOrgName := "", "", ""
	if configData := s.ReadJSON(s.GetClaudeConfigPath()); configData != nil {
		if oauthAccount, ok := configData["oauthAccount"].(map[string]any); ok {
			liveEmail, _ = oauthAccount["emailAddress"].(string)
			liveOrgUUID, _ = oauthAccount["organizationUuid"].(string)
			liveOrgName, _ = oauthAccount["organizationName"].(string)
		}
	}

	updated := false
	for num, v := range accounts {
		acc, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if _, present := acc["organizationUuid"]; present {
			continue // already migrated
		}
		email, _ := acc["email"].(string)
		if email == liveEmail && liveEmail != "" {
			acc["organizationUuid"] = liveOrgUUID
			acc["organizationName"] = liveOrgName
			updated = true
			continue
		}
		configText := s.ReadAccountConfig(num, email)
		if configText != "" {
			var configData map[string]any
			if err := json.Unmarshal([]byte(configText), &configData); err == nil {
				oauthAccount, _ := configData["oauthAccount"].(map[string]any)
				acc["organizationUuid"], _ = oauthAccount["organizationUuid"].(string)
				acc["organizationName"], _ = oauthAccount["organizationName"].(string)
			} else {
				acc["organizationUuid"] = ""
				acc["organizationName"] = ""
			}
		} else {
			acc["organizationUuid"] = ""
			acc["organizationName"] = ""
		}
		updated = true
	}

	if updated {
		raw["lastUpdated"] = GetTimestamp()
		return s.WriteJSON(s.SequenceFile, raw)
	}
	return nil
}

// ReadCredentials mirrors _read_credentials: delegates to the store.
func (s *ClaudeAccountSwitcher) ReadCredentials() string {
	return s.Store.readActiveCredentials().Value
}

// ReadActiveCredentials mirrors _read_active_credentials: delegates to the store.
func (s *ClaudeAccountSwitcher) ReadActiveCredentials() ActiveCredentials {
	return s.Store.readActiveCredentials()
}

// WriteCredentials mirrors _write_credentials: delegates to the store.
func (s *ClaudeAccountSwitcher) WriteCredentials(creds string) error {
	return s.Store.writeCredentials(creds)
}

// ReadAccountCredentials mirrors _read_account_credentials: delegates to the store.
func (s *ClaudeAccountSwitcher) ReadAccountCredentials(accountNum, email string) string {
	return s.Store.readAccountCredentials(accountNum, email)
}

// WriteAccountCredentials mirrors _write_account_credentials: writes via the
// store, then invalidates the slot's session profile on success.
func (s *ClaudeAccountSwitcher) WriteAccountCredentials(accountNum, email, credentials string) error {
	if err := s.Store.writeAccountCredentials(accountNum, email, credentials); err != nil {
		return err
	}
	s.PostBackupWrite(accountNum, email)
	return nil
}

// DeleteAccountCredentials mirrors _delete_account_credentials: delegates to the store.
func (s *ClaudeAccountSwitcher) DeleteAccountCredentials(accountNum, email string) {
	s.Store.deleteAccountCredentials(accountNum, email)
}

// WriteAccountConfig mirrors _write_account_config.
func (s *ClaudeAccountSwitcher) WriteAccountConfig(accountNum, email, config string) error {
	configFile := filepath.Join(s.ConfigsDir, fmt.Sprintf(".claude-config-%s-%s.json", accountNum, email))
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Chmod(configFile, 0o600)
	}
	return nil
}

// AccountIsSwitchable mirrors _account_is_switchable: whether a slot has
// both stored credentials and config backups.
func (s *ClaudeAccountSwitcher) AccountIsSwitchable(accountNum string) bool {
	data := s.GetSequenceData()
	if data == nil {
		return false
	}
	record, ok := data.Accounts[accountNum]
	if !ok {
		return false
	}
	if s.ReadAccountCredentials(accountNum, record.Email) == "" {
		return false
	}
	if s.ReadAccountConfig(accountNum, record.Email) == "" {
		return false
	}
	return true
}

// AccountEmail mirrors account_email: stored email for a slot, "" when
// unknown.
func (s *ClaudeAccountSwitcher) AccountEmail(accountNum string) string {
	data := s.GetSequenceData()
	if data == nil {
		return ""
	}
	return data.Accounts[accountNum].Email
}

// HasLiveLogin mirrors has_live_login: whether the live Claude Code config
// carries any account identity.
func (s *ClaudeAccountSwitcher) HasLiveLogin() bool {
	_, _, ok := s.GetCurrentAccount()
	return ok
}

// AccountIdentity mirrors account_identity: a slot's stored identity
// fields. The zero AccountIdentityInfo for an unknown slot, matching
// Python's dict.get(..., {}) chain of empty-string defaults.
func (s *ClaudeAccountSwitcher) AccountIdentity(accountNum string) AccountIdentityInfo {
	data := s.GetSequenceData()
	if data == nil {
		return AccountIdentityInfo{}
	}
	acct := data.Accounts[accountNum]
	return AccountIdentityInfo{
		Email:            acct.Email,
		OrganizationUUID: acct.OrganizationUUID,
		UUID:             strings.TrimSpace(acct.UUID),
	}
}

// SwitchableAccountNumbers mirrors switchable_account_numbers: account
// numbers in rotation order that have usable stored backups.
func (s *ClaudeAccountSwitcher) SwitchableAccountNumbers() []string {
	data := s.GetSequenceData()
	if data == nil {
		return nil
	}
	var out []string
	for _, num := range data.Sequence {
		numStr := strconv.Itoa(num)
		if s.AccountIsSwitchable(numStr) {
			out = append(out, numStr)
		}
	}
	return out
}

// DeleteAccountFiles mirrors _delete_account_files: the single chokepoint
// for every path that removes or displaces a slot. Refuses while a
// session-mode claude is live against the slot.
func (s *ClaudeAccountSwitcher) DeleteAccountFiles(accountNum, email string) error {
	if err := s.EnsureNoLiveSession(accountNum, email, "the operation"); err != nil {
		return err
	}
	s.DeleteAccountCredentials(accountNum, email)
	configFile := filepath.Join(s.ConfigsDir, fmt.Sprintf(".claude-config-%s-%s.json", accountNum, email))
	if fileExists(configFile) {
		_ = os.Remove(configFile)
	}
	s.DeleteSessionProfile(accountNum, email)
	return nil
}

// SessionDir mirrors _session_dir.
func (s *ClaudeAccountSwitcher) SessionDir(accountNum, email string) string {
	return SessionDirFor(s.BackupDir, accountNum, email)
}

// LiveSessionPIDs mirrors _live_session_pids.
func (s *ClaudeAccountSwitcher) LiveSessionPIDs(accountNum, email string) []int {
	sessions := LiveSessionsFor(s.SessionDir(accountNum, email))
	pids := make([]int, len(sessions))
	for i, sess := range sessions {
		pids[i] = sess.PID
	}
	return pids
}

// EnsureNoLiveSession mirrors _ensure_no_live_session: refuses a destructive
// operation while a session-mode claude is live.
func (s *ClaudeAccountSwitcher) EnsureNoLiveSession(accountNum, email, action string) error {
	pids := s.LiveSessionPIDs(accountNum, email)
	if len(pids) == 0 {
		return nil
	}
	return fmt.Errorf("account-%s (%s) has a live session-mode claude instance (pid %v); exit it first, then retry %s: %w",
		accountNum, email, pids, action, ErrSession)
}

// InvalidateSessionCredentials mirrors _invalidate_session_credentials:
// drops a session profile's credential material, keeping its history.
func (s *ClaudeAccountSwitcher) InvalidateSessionCredentials(accountNum, email string) {
	sessionDir := s.SessionDir(accountNum, email)
	if !fileExists(sessionDir) {
		return
	}
	DeleteMacOSKeychainEntry(sessionDir)
	_ = os.Remove(filepath.Join(sessionDir, ".credentials.json"))
	_ = os.Remove(filepath.Join(sessionDir, StaleMarker))
	s.Logger.Info("invalidated session credentials", "account", accountNum)
}

// DeleteSessionProfile mirrors _delete_session_profile: removes an account's
// session profile dir and its keychain entry. Keychain first: the hashed
// service name is derived from the dir path and cannot be recomputed once
// the dir is gone.
func (s *ClaudeAccountSwitcher) DeleteSessionProfile(accountNum, email string) {
	sessionDir := s.SessionDir(accountNum, email)
	if !fileExists(sessionDir) {
		return
	}
	DeleteMacOSKeychainEntry(sessionDir)
	_ = os.RemoveAll(sessionDir)
	s.Logger.Info("removed session profile", "account", accountNum, "path", sessionDir)
}

// PostBackupWrite mirrors _post_backup_write: invalidates or stale-marks a
// slot's session profile after any backup credential write.
func (s *ClaudeAccountSwitcher) PostBackupWrite(accountNum, email string) {
	if len(s.LiveSessionPIDs(accountNum, email)) > 0 {
		MarkSessionStale(s.SessionDir(accountNum, email))
	} else {
		s.InvalidateSessionCredentials(accountNum, email)
	}
}

// UsageEntriesByAccount mirrors usage_entries_by_account: store-backed
// usage entries (ages, errors, poll state) per account. fetch restricts
// which accounts may be fetched this pass (the auto engine's scheduler);
// nil means every stale account is eligible (on-demand callers).
func (s *ClaudeAccountSwitcher) UsageEntriesByAccount(fetch map[string]bool) map[string]UsageEntry {
	accountsInfo := s.BuildAccountsInfo()
	return s.CollectUsageEntries(accountsInfo, fetch)
}

// UsageFetchStamps mirrors usage_fetch_stamps: a per-slot fetchedAt
// snapshot from the usage store, a pure file read (no fetching, no
// credential access).
func (s *ClaudeAccountSwitcher) UsageFetchStamps() map[string]*float64 {
	data := s.GetSequenceData()
	if data == nil {
		return map[string]*float64{}
	}
	identities := make(map[string]Identity, len(data.Accounts))
	for num, info := range data.Accounts {
		identities[num] = Identity{Email: info.Email, OrganizationUUID: info.OrganizationUUID}
	}
	entries := s.UsageStore.Entries(identities)
	out := make(map[string]*float64, len(entries))
	for num, entry := range entries {
		out[num] = entry.FetchedAt
	}
	return out
}

// SetUsagePollPlan mirrors set_usage_poll_plan: persists the auto
// engine's per-slot (nextPollAt, pollIntervalS).
func (s *ClaudeAccountSwitcher) SetUsagePollPlan(plans map[string][2]*float64) {
	data := s.GetSequenceData()
	accounts := map[string]AccountRecord{}
	if data != nil {
		accounts = data.Accounts
	}
	identities := make(map[string]Identity, len(plans))
	for num := range plans {
		info := accounts[num]
		identities[num] = Identity{Email: info.Email, OrganizationUUID: info.OrganizationUUID}
	}
	s.UsageStore.SetPollPlan(plans, identities)
}

// SetPollPolicyInputs mirrors set_poll_policy_inputs: pins the
// threshold/models poll planning keys on (set by a hosted auto engine so
// cadence follows its effective, CLI-merged settings instead of the
// settings file).
func (s *ClaudeAccountSwitcher) SetPollPolicyInputs(threshold float64, models []string) {
	s.pollInputsMu.Lock()
	defer s.pollInputsMu.Unlock()
	s.pollInputsOverride = &pollPolicyInputsPair{threshold: threshold, models: models}
}

// ClearPollPolicyInputs mirrors clear_poll_policy_inputs: drops the
// hosted engine's pin so poll planning falls back to the settings file.
func (s *ClaudeAccountSwitcher) ClearPollPolicyInputs() {
	s.pollInputsMu.Lock()
	defer s.pollInputsMu.Unlock()
	s.pollInputsOverride = nil
}

// pollPolicyInputs mirrors _poll_policy_inputs: threshold + configured
// model names for poll planning, the hosting engine's pinned values when
// present, else the settings file (reloaded only when it changes).
func (s *ClaudeAccountSwitcher) pollPolicyInputs() (float64, []string) {
	s.pollInputsMu.Lock()
	defer s.pollInputsMu.Unlock()
	if s.pollInputsOverride != nil {
		return s.pollInputsOverride.threshold, s.pollInputsOverride.models
	}
	path := SettingsPath(s.BackupDir)
	var mtime *int64
	if info, err := os.Stat(path); err == nil {
		m := info.ModTime().UnixNano()
		mtime = &m
	}
	if s.pollInputsCache != nil && s.pollInputsCacheMtime != nil && mtime != nil && *s.pollInputsCacheMtime == *mtime {
		return s.pollInputsCache.threshold, s.pollInputsCache.models
	}
	if s.pollInputsCache != nil && s.pollInputsCacheMtime == nil && mtime == nil {
		return s.pollInputsCache.threshold, s.pollInputsCache.models
	}
	loaded := LoadSettings(s.BackupDir)
	var modelValue string
	if loaded.Model != nil {
		modelValue = *loaded.Model
	}
	models := ParseModelNames(modelValue)
	pair := &pollPolicyInputsPair{threshold: loaded.Threshold, models: models}
	s.pollInputsCache = pair
	s.pollInputsCacheMtime = mtime
	return pair.threshold, pair.models
}

// ReplanNewActive mirrors _replan_new_active: pulls the just-activated
// account's poll plan to the active floor.
//
// Its stored plan was computed while it was an idle candidate and may
// wait up to CandidateMaxIntervalS, too slow for the account whose usage
// is about to move. The deadline anchors on the last measurement (an
// already-old one comes due immediately, a never-measured account is
// left plan-less so nothing blocks its first fetch), and the next poll
// is only ever pulled earlier, never pushed later. Best-effort by
// contract: the switch this rides on has already committed, so any
// failure here must not surface as a switch failure.
func (s *ClaudeAccountSwitcher) ReplanNewActive(number, email, orgUUID string) {
	defer func() {
		if r := recover(); r != nil {
			s.Logger.Warn("post-switch poll re-plan failed (switch itself succeeded)", "panic", r)
		}
	}()
	identities := map[string]Identity{number: {Email: email, OrganizationUUID: orgUUID}}
	entry, ok := s.UsageStore.Entries(identities)[number]
	if !ok || entry.FetchedAt == nil {
		return
	}
	now := float64(s.UsageStore.clock().Unix())
	nextPoll := max(now, *entry.FetchedAt+MinIntervalS)
	if entry.NextPollAt != nil && *entry.NextPollAt <= nextPoll {
		return
	}
	interval := MinIntervalS
	s.UsageStore.SetPollPlan(map[string][2]*float64{number: {&nextPoll, &interval}}, identities)
}

// CurrentAccountNumber mirrors current_account_number: the slot of the
// live login, or "" when there is none or it is unmanaged.
//
// Deliberately no fallback to the recorded activeAccountNumber: an
// unmanaged live login must return "" (never a guessed slot) so the
// auto-switch engine can't evaluate the wrong account's usage and
// overwrite a login cswap doesn't own (PerformSwitch's direct-activation
// path is what would run instead). Use HasLiveLogin to tell the two ""
// cases (no login vs unmanaged login) apart.
func (s *ClaudeAccountSwitcher) CurrentAccountNumber() string {
	email, orgUUID, ok := s.GetCurrentAccount()
	if !ok {
		return ""
	}
	data := s.GetSequenceData()
	if data == nil {
		return ""
	}
	return FindAccountSlot(data, email, orgUUID)
}

// PersistBackupCredentials mirrors persist_backup_credentials: persists
// rotated credentials to a slot's backup store, under the lock. For
// inactive accounts only, never routes to the active store. The caller
// must NOT hold s.LockFile (FileLock is non-reentrant).
func (s *ClaudeAccountSwitcher) PersistBackupCredentials(accountNum, email, credentials string) error {
	lock := NewFileLock(s.LockFile, 10*time.Second)
	if err := lock.Lock(10 * time.Second); err != nil {
		return err
	}
	defer lock.Release()
	return s.WriteAccountCredentials(accountNum, email, credentials)
}

// BackfillAccountUUID mirrors backfill_account_uuid: records a resolved
// account uuid on a slot that lacks one. Only ever fills an empty uuid
// (add-token placeholders); an existing uuid is identity and is never
// rewritten here. Caller must NOT hold s.LockFile.
func (s *ClaudeAccountSwitcher) BackfillAccountUUID(accountNum, uuid string) error {
	if uuid == "" {
		return nil
	}
	lock := NewFileLock(s.LockFile, 10*time.Second)
	if err := lock.Lock(10 * time.Second); err != nil {
		return err
	}
	defer lock.Release()

	data := s.GetSequenceData()
	if data == nil {
		return nil
	}
	acct, ok := data.Accounts[accountNum]
	if !ok || strings.TrimSpace(acct.UUID) != "" {
		return nil
	}
	acct.UUID = uuid
	data.Accounts[accountNum] = acct
	data.LastUpdated = GetTimestamp()
	return s.WriteJSON(s.SequenceFile, data)
}

// AccountsSnapshot mirrors accounts_snapshot: a one-pass structured
// snapshot of every managed account, for the TUI. Metadata, active-slot
// detection, and usage entries all come from a single BuildAccountsInfo +
// CollectUsageEntries pass, so the view is coherent, two separate calls
// could interleave with other collectors and disagree about the active
// slot or freshness. fetch has CollectUsageEntries semantics: nil makes
// every stale account eligible; a set restricts which accounts may be
// fetched this pass.
func (s *ClaudeAccountSwitcher) AccountsSnapshot(fetch map[string]bool) AccountsSnapshotResult {
	accountsInfo := s.BuildAccountsInfo()
	entries := s.CollectUsageEntries(accountsInfo, fetch)

	var activeNumber *string
	accounts := make([]AccountSnapshot, 0, len(accountsInfo))
	for _, info := range accountsInfo {
		numStr := strconv.Itoa(info.Number)
		if info.IsActive {
			n := numStr
			activeNumber = &n
		}
		accounts = append(accounts, AccountSnapshot{
			Number:     numStr,
			Email:      info.Email,
			OrgName:    info.OrgName,
			OrgUUID:    info.OrgUUID,
			IsActive:   info.IsActive,
			Kind:       s.AccountKind(numStr),
			Switchable: s.AccountIsSwitchable(numStr),
			Usage:      entries[numStr],
		})
	}
	return AccountsSnapshotResult{
		ActiveNumber: activeNumber,
		Accounts:     accounts,
		TakenAt:      float64(s.UsageStore.clock().Unix()),
	}
}

// ListUnclaimedCredentials mirrors list_unclaimed_credentials: internal
// safety copies preserved at switch time (diagnostics only). Write-only
// storage: entries are created when a switch displaces live credential
// bytes it could not attribute to the outgoing slot, and are never
// consumed automatically, recovery from any such state is the documented
// re-login plus cswap add [--slot N].
func (s *ClaudeAccountSwitcher) ListUnclaimedCredentials() map[string]map[string]any {
	return s.Store.listUnclaimedCredentials()
}
