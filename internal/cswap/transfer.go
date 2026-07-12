package cswap

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// TransferFormatVersion mirrors FORMAT_VERSION.
const TransferFormatVersion = 1

// TransferEnvelope mirrors the top-level export/import JSON envelope
// shape: version, provenance metadata, and the account list.
type TransferEnvelope struct {
	Version             int               `json:"version"`
	ExportedAt          string            `json:"exportedAt"`
	ExportedFrom        string            `json:"exportedFrom"`
	SwapVersion         string            `json:"swapVersion"`
	Encrypted           bool              `json:"encrypted"`
	ActiveAccountNumber *int              `json:"activeAccountNumber"`
	Accounts            []TransferAccount `json:"accounts"`
}

// TransferAccount mirrors one exported account entry. Credentials is
// either a raw string (API-key accounts) or a JSON object (OAuth
// accounts), matching Python's isinstance(creds_obj, str) branch; kept
// as `any` so encoding/json's existing untyped marshal/unmarshal
// handles both shapes without a custom (Un)MarshalJSON.
type TransferAccount struct {
	Number           int            `json:"number"`
	Email            string         `json:"email"`
	UUID             string         `json:"uuid"`
	OrganizationUUID string         `json:"organizationUuid"`
	OrganizationName string         `json:"organizationName"`
	Added            string         `json:"added"`
	Credentials      any            `json:"credentials"`
	Config           map[string]any `json:"config"`
	Kind             string         `json:"kind,omitempty"`
}

// parsePayload mirrors _parse_payload: parses a JSON string that should
// decode to an object.
func parsePayload(text, label string) (map[string]any, error) {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %v: %w", label, err, ErrTransfer)
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a JSON object: %w", label, ErrTransfer)
	}
	return obj, nil
}

// slimConfig mirrors _slim_config: reduces a parsed ~/.claude.json to
// just the keys a switch will consume (today, only oauthAccount),
// keeping cross-machine transfers small and avoiding leaking
// source-machine identity into the destination.
func slimConfig(configObj map[string]any, label string) (map[string]any, error) {
	oauth, ok := configObj["oauthAccount"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s is missing oauthAccount, cannot export: %w", label, ErrTransfer)
	}
	return map[string]any{"oauthAccount": oauth}, nil
}

// atomicWriteFile mirrors _atomic_write_file: writes pre-serialized text
// atomically at 0600, the same temp-file-then-rename-then-chmod pattern
// already established in ClaudeAccountSwitcher.WriteJSON (switcher.go),
// minus the marshal step (this content is already serialized). Like
// WriteJSON, skips the final chmod on Windows.
func atomicWriteFile(path, content string) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Chmod(path, 0o600)
	}
	return nil
}

// validateImportedAccount mirrors _validate_imported_account: validates
// per-account fields BEFORE any filename construction. Defends against
// path traversal, email and slot number flow into filenames in
// ReadAccountCredentials etc., so they must be constrained before use.
func validateImportedAccount(s *ClaudeAccountSwitcher, account map[string]any) (email, number string, err error) {
	emailVal, ok := account["email"].(string)
	if !ok || !s.ValidateEmail(emailVal) {
		return "", "", fmt.Errorf("invalid or missing email in imported account: %#v: %w", account["email"], ErrTransfer)
	}

	rawNumber, ok := account["number"].(float64)
	if !ok || rawNumber != float64(int(rawNumber)) || int(rawNumber) < 1 {
		return "", "", fmt.Errorf("invalid slot number in imported account (%s): %#v: %w", emailVal, account["number"], ErrTransfer)
	}

	// Org/uuid/added must be strings (or absent). A non-string here would
	// otherwise corrupt sequence.json or break composite-key matching.
	for _, field := range []string{"organizationUuid", "organizationName", "uuid", "added"} {
		if v, present := account[field]; present && v != nil {
			if _, ok := v.(string); !ok {
				return "", "", fmt.Errorf("%s for %s must be a string, got %T: %w", field, emailVal, v, ErrTransfer)
			}
		}
	}

	return emailVal, strconv.Itoa(int(rawNumber)), nil
}

// ExportAccounts mirrors export_accounts: exports accounts to a JSON
// file or stdout ("-"). account limits export to a single NUM|EMAIL;
// full includes the entire ~/.claude.json snapshot per account (default
// writes only oauthAccount). stderr receives progress/skip notices, the
// same role Python's _eprint plays, routed through an explicit writer
// instead of a package-level stream so callers (and tests) control it.
func (s *ClaudeAccountSwitcher) ExportAccounts(destination, account string, full bool, stderr io.Writer) error {
	sequenceData := s.GetSequenceDataMigrated()
	if sequenceData == nil || len(sequenceData.Accounts) == 0 {
		return fmt.Errorf("no accounts to export, run cswap --add-account first: %w", ErrTransfer)
	}

	explicitAccount := account != ""
	var targetNums []string
	if explicitAccount {
		resolved, err := s.ResolveAccountIdentifier(account)
		if err != nil {
			return err
		}
		if resolved == "" {
			return fmt.Errorf("account not found: %s: %w", account, ErrTransfer)
		}
		if _, ok := sequenceData.Accounts[resolved]; !ok {
			return fmt.Errorf("account not found: %s: %w", account, ErrTransfer)
		}
		targetNums = []string{resolved}
	} else {
		targetNums = make([]string, 0, len(sequenceData.Accounts))
		for num := range sequenceData.Accounts {
			targetNums = append(targetNums, num)
		}
		sort.Slice(targetNums, func(i, j int) bool {
			ni, _ := strconv.Atoi(targetNums[i])
			nj, _ := strconv.Atoi(targetNums[j])
			return ni < nj
		})
	}

	currentEmail, currentOrgUUID, hasCurrent := s.GetCurrentAccount()

	var accountsPayload []TransferAccount
	for _, num := range targetNums {
		record := sequenceData.Accounts[num]
		email := record.Email
		orgUUID := record.OrganizationUUID

		isActive := hasCurrent && currentEmail == email && currentOrgUUID == orgUUID

		var credsText, configText string
		if isActive {
			credsText = s.ReadCredentials()
			if credsText == "" {
				return fmt.Errorf("failed to read live credentials for active account %s: %w", email, ErrCredentialRead)
			}
			configPath := s.GetClaudeConfigPath()
			data, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("Claude config file not found: %w", ErrConfig)
			}
			configText = string(data)
		} else {
			credsText = s.ReadAccountCredentials(num, email)
			configText = s.ReadAccountConfig(num, email)
			if credsText == "" || configText == "" {
				if explicitAccount {
					if credsText == "" {
						return fmt.Errorf("no backup credentials found for account %s (%s): %w", num, email, ErrCredentialRead)
					}
					return fmt.Errorf("no backup config found for account %s (%s): %w", num, email, ErrConfig)
				}
				fmt.Fprintf(stderr, "Skipping Account-%s (%s): no stored credentials/config, re-add with: cswap --add-account --slot %s\n", num, email, num)
				continue
			}
		}

		configObj, err := parsePayload(configText, fmt.Sprintf("config for %s", email))
		if err != nil {
			return err
		}
		if !full {
			configObj, err = slimConfig(configObj, fmt.Sprintf("config for %s", email))
			if err != nil {
				return err
			}
		}

		isAPIKey := LooksLikeAPIKey(credsText)
		numInt, _ := strconv.Atoi(num)
		entry := TransferAccount{
			Number:           numInt,
			Email:            email,
			UUID:             record.UUID,
			OrganizationUUID: orgUUID,
			OrganizationName: record.OrganizationName,
			Added:            record.Added,
			Config:           configObj,
		}
		if isAPIKey {
			entry.Credentials = strings.TrimSpace(credsText)
			entry.Kind = "api_key"
		} else {
			credsObj, err := parsePayload(credsText, fmt.Sprintf("credentials for %s", email))
			if err != nil {
				return err
			}
			entry.Credentials = credsObj
		}
		accountsPayload = append(accountsPayload, entry)
	}

	if len(accountsPayload) == 0 {
		return fmt.Errorf("no exportable accounts, all managed slots are missing stored credentials/config, re-add with: cswap --add-account --slot <number>: %w", ErrTransfer)
	}

	var activeInPayload *int
	if sequenceData.ActiveAccountNumber != nil {
		for _, a := range accountsPayload {
			if a.Number == *sequenceData.ActiveAccountNumber {
				n := *sequenceData.ActiveAccountNumber
				activeInPayload = &n
				break
			}
		}
	}

	envelope := TransferEnvelope{
		Version:             TransferFormatVersion,
		ExportedAt:          GetTimestamp(),
		ExportedFrom:        string(s.Platform),
		SwapVersion:         version,
		Encrypted:           false,
		ActiveAccountNumber: activeInPayload,
		Accounts:            accountsPayload,
	}

	serialized, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}

	if destination == "-" {
		fmt.Println(string(serialized))
		return nil
	}

	outPath := expandHome(destination)
	if err := atomicWriteFile(outPath, string(serialized)+"\n"); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Exported %d account(s) to %s\n", len(accountsPayload), outPath)
	return nil
}

// ImportAccounts mirrors import_accounts: imports accounts from a JSON
// file or stdin ("-"). force overwrites an existing matching slot in
// place. Two passes: validate every account first (a malformed account
// later in the list must not leave earlier accounts half-imported), then
// write.
func (s *ClaudeAccountSwitcher) ImportAccounts(source string, force bool, stderr io.Writer) error {
	var text string
	if source == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		text = string(data)
	} else {
		expandedSource := expandHome(source)
		data, err := os.ReadFile(expandedSource)
		if err != nil {
			return fmt.Errorf("import file not found: %s: %w", expandedSource, ErrTransfer)
		}
		text = string(data)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return fmt.Errorf("export file is not valid JSON: %v: %w", err, ErrTransfer)
	}

	versionVal, _ := envelope["version"].(float64)
	if int(versionVal) != TransferFormatVersion {
		return fmt.Errorf("unsupported export version: %#v (expected %d): %w", envelope["version"], TransferFormatVersion, ErrTransfer)
	}

	if encrypted, _ := envelope["encrypted"].(bool); encrypted {
		return fmt.Errorf("encrypted exports are not supported in this version, decrypt before piping (e.g. gpg -d backup.gpg | cswap import -): %w", ErrTransfer)
	}

	rawAccounts, ok := envelope["accounts"].([]any)
	if !ok || len(rawAccounts) == 0 {
		return fmt.Errorf("export file has no accounts to import: %w", ErrTransfer)
	}

	type normalizedAccount struct {
		email       string
		exportedNum string
		orgUUID     string
		orgName     string
		uuid        string
		added       string
		kind        string
		credsText   string
		configText  string
	}

	var normalized []normalizedAccount
	seenKeys := map[[2]string]bool{}
	for _, rawAny := range rawAccounts {
		raw, ok := rawAny.(map[string]any)
		if !ok {
			return fmt.Errorf("account entry must be a JSON object: %w", ErrTransfer)
		}
		email, exportedNum, err := validateImportedAccount(s, raw)
		if err != nil {
			return err
		}
		orgUUID, _ := raw["organizationUuid"].(string)

		configObj, ok := raw["config"].(map[string]any)
		if !ok {
			return fmt.Errorf("config for %s must be a JSON object: %w", email, ErrTransfer)
		}

		credsObjAny := raw["credentials"]
		credsStr, isStringCreds := credsObjAny.(string)
		kindVal, _ := raw["kind"].(string)
		isAPIKey := kindVal == "api_key" || isStringCreds

		var credsText string
		if isAPIKey {
			if !isStringCreds || !LooksLikeAPIKey(credsStr) {
				return fmt.Errorf("API-key credentials for %s must be a raw sk-ant-api... string: %w", email, ErrTransfer)
			}
			credsText = strings.TrimSpace(credsStr)
		} else {
			credsObj, ok := credsObjAny.(map[string]any)
			if !ok {
				return fmt.Errorf("credentials for %s must be a JSON object: %w", email, ErrTransfer)
			}
			b, err := json.Marshal(credsObj)
			if err != nil {
				return err
			}
			credsText = string(b)
		}

		key := [2]string{email, orgUUID}
		if seenKeys[key] {
			orgLabel := orgUUID
			if orgLabel == "" {
				orgLabel = "personal"
			}
			return fmt.Errorf("duplicate account in export: %s (org=%s): %w", email, orgLabel, ErrTransfer)
		}
		seenKeys[key] = true

		orgName, _ := raw["organizationName"].(string)
		uuidVal, _ := raw["uuid"].(string)
		added, _ := raw["added"].(string)
		if added == "" {
			added = GetTimestamp()
		}
		configBytes, err := json.MarshalIndent(configObj, "", "  ")
		if err != nil {
			return err
		}
		kind := "oauth"
		if isAPIKey {
			kind = "api_key"
		}
		normalized = append(normalized, normalizedAccount{
			email:       email,
			exportedNum: exportedNum,
			orgUUID:     orgUUID,
			orgName:     orgName,
			uuid:        uuidVal,
			added:       added,
			kind:        kind,
			credsText:   credsText,
			configText:  string(configBytes),
		})
	}

	// Pass 2: writes. Validation is complete; remaining failures are
	// environmental (disk I/O) and don't reflect on the file's integrity.
	if err := s.SetupDirectories(); err != nil {
		return err
	}
	if err := s.InitSequenceFile(); err != nil {
		return err
	}

	imported, skipped, overwritten := 0, 0, 0
	writtenSlots := map[string]bool{}

	envelopeActiveVal, _ := envelope["activeAccountNumber"].(float64)
	hasEnvelopeActive := envelope["activeAccountNumber"] != nil
	envelopeActiveStr := ""
	if hasEnvelopeActive {
		envelopeActiveStr = fmt.Sprintf("%d", int(envelopeActiveVal))
	}
	resolvedActiveSlot := ""

	for _, entry := range normalized {
		isEnvelopeActive := hasEnvelopeActive && entry.exportedNum == envelopeActiveStr

		data := s.GetSequenceDataMigrated()
		if data == nil {
			data = &SequenceData{Accounts: map[string]AccountRecord{}}
		}
		existingSlot := FindAccountSlot(data, entry.email, entry.orgUUID)

		var targetNum string
		if existingSlot != "" {
			if !force {
				fmt.Fprintf(stderr, "Skipped %s (already exists, use --force)\n", entry.email)
				skipped++
				if isEnvelopeActive {
					resolvedActiveSlot = existingSlot
				}
				continue
			}
			targetNum = existingSlot
			livePIDs := s.LiveSessionPIDs(targetNum, entry.email)
			if len(livePIDs) > 0 {
				pidStrs := make([]string, len(livePIDs))
				for i, p := range livePIDs {
					pidStrs[i] = strconv.Itoa(p)
				}
				fmt.Fprintf(stderr, "Warning: %s (slot %s) has a live session-mode instance (PID %s); its session profile keeps the pre-import credentials until it is restarted via 'cswap run'.\n", entry.email, targetNum, strings.Join(pidStrs, ", "))
			}
		} else {
			if _, occupied := data.Accounts[entry.exportedNum]; !occupied {
				targetNum = entry.exportedNum
			} else {
				targetNum = strconv.Itoa(s.GetNextAccountNumber())
			}
		}

		if err := s.WriteAccountCredentials(targetNum, entry.email, entry.credsText); err != nil {
			return err
		}
		if err := s.WriteAccountConfig(targetNum, entry.email, entry.configText); err != nil {
			return err
		}

		if data.Accounts == nil {
			data.Accounts = map[string]AccountRecord{}
		}
		newRecord := AccountRecord{
			Email:            entry.email,
			UUID:             entry.uuid,
			OrganizationUUID: entry.orgUUID,
			OrganizationName: entry.orgName,
			Added:            entry.added,
		}
		if entry.kind == "api_key" {
			newRecord.Kind = "api_key"
		}
		data.Accounts[targetNum] = newRecord
		targetInt, _ := strconv.Atoi(targetNum)
		if !slices.Contains(data.Sequence, targetInt) {
			data.Sequence = append(data.Sequence, targetInt)
			sort.Ints(data.Sequence)
		}
		data.LastUpdated = GetTimestamp()
		if err := s.WriteJSON(s.SequenceFile, data); err != nil {
			return err
		}

		if isEnvelopeActive {
			resolvedActiveSlot = targetNum
		}
		writtenSlots[targetNum] = true

		existedBefore := existingSlot != ""
		if existedBefore {
			fmt.Fprintf(stderr, "Overwrote %s (slot %s)\n", entry.email, targetNum)
			overwritten++
		} else {
			fmt.Fprintf(stderr, "Imported %s -> slot %s\n", entry.email, targetNum)
			imported++
		}
	}

	// Migration UX: seed activeAccountNumber from the resolved slot of
	// the envelope's active account only when the destination has none
	// recorded yet (clean home, no prior preference).
	final := s.GetSequenceData()
	if final != nil && (final.ActiveAccountNumber == nil || *final.ActiveAccountNumber == 0) && resolvedActiveSlot != "" {
		n, _ := strconv.Atoi(resolvedActiveSlot)
		final.ActiveAccountNumber = &n
		final.LastUpdated = GetTimestamp()
		if err := s.WriteJSON(s.SequenceFile, final); err != nil {
			return err
		}
	}

	fmt.Fprintf(stderr, "Done: %d imported, %d overwritten, %d skipped\n", imported, overwritten, skipped)

	// If we just rewrote the stored backup for the account that is the
	// current live login, a plain switch would back the (possibly stale)
	// live credentials up over it, point at the explicit activation path
	// instead.
	identity, identityOrgUUID, hasIdentity := s.GetCurrentAccount()
	if hasIdentity && final != nil {
		liveSlot := FindAccountSlot(final, identity, identityOrgUUID)
		if liveSlot != "" && writtenSlots[liveSlot] {
			fmt.Fprintf(stderr, "Note: %s is your current live login, activate the imported credentials with: cswap switch %s --force\n", identity, liveSlot)
		}
	}

	return nil
}
