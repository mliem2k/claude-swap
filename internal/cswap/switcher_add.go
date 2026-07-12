package cswap

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SetupTokenScopes mirrors SETUP_TOKEN_SCOPES.
var SetupTokenScopes = []string{"user:inference"}

// AddAccount mirrors add_account: adds the current live account to managed
// accounts. slot nil auto-assigns the next number; confirm nil mirrors
// assume_yes=True (used by non-interactive callers). Returns a human-readable
// result message.
func (s *ClaudeAccountSwitcher) AddAccount(slot *int, confirm func(string) bool) (string, error) {
	if err := s.SetupDirectories(); err != nil {
		return "", err
	}
	if err := s.InitSequenceFile(); err != nil {
		return "", err
	}
	_ = s.MigrateOrgFields()

	currentEmail, currentOrgUUID, ok := s.GetCurrentAccount()
	if !ok {
		return "", fmt.Errorf("no active claude account found, please log in first: %w", ErrConfig)
	}

	if slot == nil && s.AccountExists(currentEmail, currentOrgUUID) {
		seq := s.GetSequenceData()
		accountNum := FindAccountSlot(seq, currentEmail, currentOrgUUID)
		matchedOrgName := seq.Accounts[accountNum].OrganizationName

		currentCreds := s.ReadCredentials()
		if currentCreds == "" {
			return "", fmt.Errorf("no credentials found for current account: %w", ErrCredentialRead)
		}
		if LooksLikeAPIKey(currentCreds) {
			return "", fmt.Errorf("active login is an api-key account, add it with add-token instead: %w", ErrValidation)
		}

		currentConfig, err := os.ReadFile(s.GetClaudeConfigPath())
		if err != nil {
			return "", fmt.Errorf("claude config file not found: %w", ErrConfig)
		}

		if err := s.WriteAccountCredentials(accountNum, currentEmail, currentCreds); err != nil {
			return "", err
		}
		if err := s.WriteAccountConfig(accountNum, currentEmail, string(currentConfig)); err != nil {
			return "", err
		}
		s.UsageStore.ClearDeadToken([]string{accountNum},
			map[string]Identity{accountNum: {Email: currentEmail, OrganizationUUID: currentOrgUUID}})

		n, _ := strconv.Atoi(accountNum)
		seq.ActiveAccountNumber = &n
		seq.LastUpdated = GetTimestamp()
		if err := s.WriteJSON(s.SequenceFile, seq); err != nil {
			return "", err
		}

		tag := GetDisplayTag(currentEmail, matchedOrgName, currentOrgUUID)
		s.Logger.Info("updated credentials", "account", accountNum, "email", currentEmail)
		return fmt.Sprintf("Updated credentials for Account %s (%s [%s]).", accountNum, currentEmail, tag), nil
	}

	var displaceNum, displaceEmail, migrateFrom string
	var accountNum string

	if slot != nil {
		if *slot < 1 {
			return "", fmt.Errorf("slot number must be >= 1: %w", ErrConfig)
		}
		accountNum = strconv.Itoa(*slot)
		data := s.GetSequenceData()

		if s.AccountExists(currentEmail, currentOrgUUID) {
			oldNum := FindAccountSlot(data, currentEmail, currentOrgUUID)
			if oldNum != "" && oldNum != accountNum {
				migrateFrom = oldNum
			}
		}

		if existing, ok := data.Accounts[accountNum]; ok {
			isSame := existing.Email == currentEmail && existing.OrganizationUUID == currentOrgUUID
			if !isSame {
				if confirm != nil && !confirm(fmt.Sprintf("Slot %d already occupied by %s. Overwrite?", *slot, existing.Email)) {
					return "Cancelled", nil
				}
				displaceNum, displaceEmail = accountNum, existing.Email
			}
		}
	} else {
		accountNum = strconv.Itoa(s.GetNextAccountNumber())
	}

	currentCreds := s.ReadCredentials()
	if currentCreds == "" {
		return "", fmt.Errorf("no credentials found for current account: %w", ErrCredentialRead)
	}
	if LooksLikeAPIKey(currentCreds) {
		return "", fmt.Errorf("active login is an api-key account, add it with add-token instead: %w", ErrValidation)
	}

	currentConfig, err := os.ReadFile(s.GetClaudeConfigPath())
	if err != nil {
		return "", fmt.Errorf("claude config file not found: %w", ErrConfig)
	}
	var configData map[string]any
	_ = json.Unmarshal(currentConfig, &configData)
	oauthData, _ := configData["oauthAccount"].(map[string]any)
	accountUUID, _ := oauthData["accountUuid"].(string)
	organizationUUID, _ := oauthData["organizationUuid"].(string)
	organizationName, _ := oauthData["organizationName"].(string)

	if displaceNum != "" {
		if err := s.DeleteAccountFiles(displaceNum, displaceEmail); err != nil {
			return "", err
		}
		data := s.GetSequenceData()
		data.Sequence = removeInt(data.Sequence, mustAtoi(displaceNum))
		delete(data.Accounts, displaceNum)
		if err := s.WriteJSON(s.SequenceFile, data); err != nil {
			return "", err
		}
	}
	if migrateFrom != "" {
		data := s.GetSequenceData()
		oldEmail := data.Accounts[migrateFrom].Email
		if err := s.DeleteAccountFiles(migrateFrom, oldEmail); err != nil {
			return "", err
		}
		data = s.GetSequenceData()
		data.Sequence = removeInt(data.Sequence, mustAtoi(migrateFrom))
		delete(data.Accounts, migrateFrom)
		if err := s.WriteJSON(s.SequenceFile, data); err != nil {
			return "", err
		}
	}

	if err := s.WriteAccountCredentials(accountNum, currentEmail, currentCreds); err != nil {
		return "", err
	}
	if err := s.WriteAccountConfig(accountNum, currentEmail, string(currentConfig)); err != nil {
		return "", err
	}
	s.UsageStore.ClearDeadToken([]string{accountNum},
		map[string]Identity{accountNum: {Email: currentEmail, OrganizationUUID: organizationUUID}})

	data := s.GetSequenceData()
	data.Accounts[accountNum] = AccountRecord{
		Email: currentEmail, UUID: accountUUID,
		OrganizationUUID: organizationUUID, OrganizationName: organizationName,
		Added: GetTimestamp(),
	}
	n := mustAtoi(accountNum)
	if !containsInt(data.Sequence, n) {
		data.Sequence = append(data.Sequence, n)
		sortInts(data.Sequence)
	}
	data.ActiveAccountNumber = &n
	data.LastUpdated = GetTimestamp()
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		return "", err
	}

	tag := GetDisplayTag(currentEmail, organizationName, organizationUUID)
	s.Logger.Info("added account", "account", accountNum, "email", currentEmail)
	msg := fmt.Sprintf("Added Account %s: %s [%s]", accountNum, currentEmail, tag)
	if migrateFrom != "" {
		msg = fmt.Sprintf("Moved from slot %s to %s. %s", migrateFrom, accountNum, msg)
	}
	return msg, nil
}

// AddAccountFromToken mirrors add_account_from_token: registers a raw OAuth
// setup-token or managed API key as a new account. The token is already
// resolved by the caller (stdin/getpass handling is a CLI concern, Plan 7).
func (s *ClaudeAccountSwitcher) AddAccountFromToken(
	token, email string, slot *int, confirm func(string) bool,
) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("token cannot be empty: %w", ErrValidation)
	}
	isAPIKey := LooksLikeAPIKey(token)

	if email != "" && !s.ValidateEmail(email) {
		return "", fmt.Errorf("invalid email format %q: %w", email, ErrValidation)
	}

	if err := s.SetupDirectories(); err != nil {
		return "", err
	}
	if err := s.InitSequenceFile(); err != nil {
		return "", err
	}
	_ = s.MigrateOrgFields()

	if email == "" {
		effectiveSlot := slot
		if effectiveSlot == nil {
			n := s.GetNextAccountNumber()
			effectiveSlot = &n
		}
		label := "setup-token"
		if isAPIKey {
			label = "api-key"
		}
		email = fmt.Sprintf("%s-%d@token.local", label, *effectiveSlot)
	}

	if err := s.rejectCrossKindCollision(email, isAPIKey); err != nil {
		return "", err
	}

	var credentials string
	if isAPIKey {
		credentials = token
	} else {
		credBytes, _ := json.Marshal(map[string]any{
			"claudeAiOauth": map[string]any{"accessToken": token, "scopes": SetupTokenScopes},
		})
		credentials = string(credBytes)
	}
	configBytes, _ := json.Marshal(map[string]any{
		"oauthAccount": map[string]any{
			"emailAddress": email, "accountUuid": "", "organizationUuid": nil, "organizationName": nil,
		},
	})
	config := string(configBytes)

	if slot == nil && s.AccountExists(email, "") {
		seq := s.GetSequenceData()
		accountNum := FindAccountSlot(seq, email, "")
		if accountNum == "" {
			return "", fmt.Errorf("existing account metadata for %s is inconsistent: %w", email, ErrConfig)
		}
		if err := s.WriteAccountCredentials(accountNum, email, credentials); err != nil {
			return "", err
		}
		if err := s.WriteAccountConfig(accountNum, email, config); err != nil {
			return "", err
		}
		s.UsageStore.ClearDeadToken([]string{accountNum},
			map[string]Identity{accountNum: {Email: email}})
		seq.LastUpdated = GetTimestamp()
		if err := s.WriteJSON(s.SequenceFile, seq); err != nil {
			return "", err
		}
		kindLabel := "token"
		if isAPIKey {
			kindLabel = "API key"
		}
		return fmt.Sprintf("Updated %s for Account %s (%s [personal]).", kindLabel, accountNum, email), nil
	}

	var displaceNum, displaceEmail, migrateFrom, accountNum string

	if slot != nil {
		if *slot < 1 {
			return "", fmt.Errorf("slot number must be >= 1: %w", ErrConfig)
		}
		accountNum = strconv.Itoa(*slot)
		data := s.GetSequenceData()

		if s.AccountExists(email, "") {
			oldNum := FindAccountSlot(data, email, "")
			if oldNum != "" && oldNum != accountNum {
				migrateFrom = oldNum
			}
		}
		if existing, ok := data.Accounts[accountNum]; ok {
			isSame := existing.Email == email && existing.OrganizationUUID == ""
			if !isSame {
				if confirm != nil && !confirm(fmt.Sprintf("Slot %d already occupied by %s. Overwrite?", *slot, existing.Email)) {
					return "Cancelled", nil
				}
				displaceNum, displaceEmail = accountNum, existing.Email
			}
		}
	} else {
		accountNum = strconv.Itoa(s.GetNextAccountNumber())
	}

	if displaceNum != "" {
		if err := s.DeleteAccountFiles(displaceNum, displaceEmail); err != nil {
			return "", err
		}
		data := s.GetSequenceData()
		data.Sequence = removeInt(data.Sequence, mustAtoi(displaceNum))
		delete(data.Accounts, displaceNum)
		if err := s.WriteJSON(s.SequenceFile, data); err != nil {
			return "", err
		}
	}
	if migrateFrom != "" {
		data := s.GetSequenceData()
		oldEmail := data.Accounts[migrateFrom].Email
		if err := s.DeleteAccountFiles(migrateFrom, oldEmail); err != nil {
			return "", err
		}
		data = s.GetSequenceData()
		data.Sequence = removeInt(data.Sequence, mustAtoi(migrateFrom))
		delete(data.Accounts, migrateFrom)
		if err := s.WriteJSON(s.SequenceFile, data); err != nil {
			return "", err
		}
	}

	if err := s.WriteAccountCredentials(accountNum, email, credentials); err != nil {
		return "", err
	}
	if err := s.WriteAccountConfig(accountNum, email, config); err != nil {
		return "", err
	}
	s.UsageStore.ClearDeadToken([]string{accountNum}, map[string]Identity{accountNum: {Email: email}})

	data := s.GetSequenceData()
	record := AccountRecord{Email: email, Added: GetTimestamp()}
	if isAPIKey {
		record.Kind = "api_key"
	}
	data.Accounts[accountNum] = record
	n := mustAtoi(accountNum)
	if !containsInt(data.Sequence, n) {
		data.Sequence = append(data.Sequence, n)
		sortInts(data.Sequence)
	}
	data.LastUpdated = GetTimestamp()
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		return "", err
	}

	sourceLabel := "token"
	if isAPIKey {
		sourceLabel = "API key"
	}
	s.Logger.Info("added account from token", "account", accountNum, "email", email)
	msg := fmt.Sprintf("Added Account %s: %s [personal] (from %s)", accountNum, email, sourceLabel)
	if migrateFrom != "" {
		msg = fmt.Sprintf("Moved from slot %s to %s. %s", migrateFrom, accountNum, msg)
	}
	return msg, nil
}

// rejectCrossKindCollision mirrors _reject_cross_kind_collision.
func (s *ClaudeAccountSwitcher) rejectCrossKindCollision(email string, isAPIKey bool) error {
	data := s.GetSequenceData()
	if data == nil {
		return nil
	}
	slot := FindAccountSlot(data, email, "")
	if slot == "" {
		return nil
	}
	existingKind := s.AccountKind(slot)
	newKind := "oauth"
	if isAPIKey {
		newKind = "api_key"
	}
	if existingKind != newKind {
		return fmt.Errorf("%q already exists as a %s account (slot %s); cannot add as %s, pass a distinct email: %w",
			email, existingKind, slot, newKind, ErrValidation)
	}
	return nil
}

func mustAtoi(s string) int {
	n, _ := strconvAtoi(s)
	return n
}

func removeInt(s []int, v int) []int {
	out := s[:0]
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// RemoveAccount mirrors remove_account: removes an account from managed
// accounts. confirm nil mirrors assume_yes=True. disambiguate resolves an
// ambiguous email match interactively, matching SwitchTo's own
// disambiguate parameter exactly: a "" return cancels ("Cancelled",
// returned as a message not an error, matching this function's own
// confirm-declined path immediately below); nil skips straight to
// ResolveAccountIdentifier's own informative ambiguous-match ConfigError
// instead of ever prompting (Python's remove_account has no json_output
// parameter and cli.py never gates this behind --json, so every real CLI
// caller passes a real callback; nil exists for callers with no
// interactive capability, e.g. the TUI/menu bar, which resolve identifiers
// through their own account-number-based UI and never hit this path).
func (s *ClaudeAccountSwitcher) RemoveAccount(identifier string, confirm func(string) bool, disambiguate func(candidates []string) string) (string, error) {
	if !fileExists(s.SequenceFile) {
		return "", fmt.Errorf("no accounts are managed yet: %w", ErrConfig)
	}
	s.GetSequenceDataMigrated()

	if !isAllDigits(identifier) {
		if !s.ValidateEmail(identifier) {
			return "", fmt.Errorf("invalid email format %q: %w", identifier, ErrValidation)
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
		if len(matches) > 1 && disambiguate != nil {
			choice := disambiguate(matches)
			if choice == "" {
				return "Cancelled", nil
			}
			identifier = choice
		}
	}

	accountNum, err := s.ResolveAccountIdentifier(identifier)
	if err != nil {
		return "", err
	}
	if accountNum == "" {
		return "", fmt.Errorf("no account found with identifier %q: %w", identifier, ErrAccountNotFound)
	}

	data := s.GetSequenceData()
	accountInfo, ok := data.Accounts[accountNum]
	if !ok {
		return "", fmt.Errorf("account-%s does not exist: %w", accountNum, ErrAccountNotFound)
	}
	email := accountInfo.Email

	if err := s.EnsureNoLiveSession(accountNum, email, "remove-account"); err != nil {
		return "", err
	}

	if confirm != nil && !confirm(fmt.Sprintf("Permanently remove Account-%s (%s)?", accountNum, email)) {
		return "Cancelled", nil
	}

	if err := s.DeleteAccountFiles(accountNum, email); err != nil {
		return "", err
	}

	delete(data.Accounts, accountNum)
	data.Sequence = removeInt(data.Sequence, mustAtoi(accountNum))
	data.LastUpdated = GetTimestamp()
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		return "", err
	}

	s.Logger.Info("removed account", "account", accountNum, "email", email)
	return fmt.Sprintf("Removed Account-%s (%s)", accountNum, email), nil
}
