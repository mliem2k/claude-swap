package cswap

import (
	"os"
	"runtime"
	"time"
)

// Platform mirrors models.Platform. Values are comparable with ==.
type Platform string

const (
	PlatformMacOS   Platform = "macos"
	PlatformLinux   Platform = "linux"
	PlatformWSL     Platform = "wsl"
	PlatformWindows Platform = "windows"
	PlatformUnknown Platform = "unknown"
)

// runtimeGOOS is a var so tests can override it to exercise platform
// branches on a host whose real GOOS differs (e.g. testing the linux/WSL
// split on a darwin CI runner).
var runtimeGOOS = func() string { return runtime.GOOS }

// DetectPlatform mirrors Platform.detect(). Uses runtime.GOOS rather than a
// uname query, which can hang on slow WMI on Windows. WSL is a Linux kernel
// with WSL_DISTRO_NAME set.
func DetectPlatform() Platform {
	switch runtimeGOOS() {
	case "darwin":
		return PlatformMacOS
	case "windows":
		return PlatformWindows
	case "linux":
		if os.Getenv("WSL_DISTRO_NAME") != "" {
			return PlatformWSL
		}
		return PlatformLinux
	default:
		return PlatformUnknown
	}
}

// AccountInfo mirrors models.AccountInfo.
type AccountInfo struct {
	Email            string
	UUID             string
	OrganizationUUID string
	OrganizationName string
	Added            string
	Number           int
}

// IsOrganization mirrors the is_organization property.
func (a AccountInfo) IsOrganization() bool {
	return a.OrganizationUUID != ""
}

// DisplayLabel mirrors the display_label property.
func (a AccountInfo) DisplayLabel() string {
	tag := a.OrganizationName
	if tag == "" {
		tag = "personal"
	}
	return a.Email + " [" + tag + "]"
}

// AccountInfoFromDict mirrors AccountInfo.from_dict.
func AccountInfoFromDict(number int, data map[string]any) AccountInfo {
	str := func(k string) string {
		v, ok := data[k]
		if !ok || v == nil {
			return ""
		}
		s, _ := v.(string)
		return s
	}
	return AccountInfo{
		Email:            str("email"),
		UUID:             str("uuid"),
		OrganizationUUID: str("organizationUuid"),
		OrganizationName: str("organizationName"),
		Added:            str("added"),
		Number:           number,
	}
}

// ToDict mirrors AccountInfo.to_dict.
func (a AccountInfo) ToDict() map[string]any {
	return map[string]any{
		"email":            a.Email,
		"uuid":             a.UUID,
		"organizationUuid": a.OrganizationUUID,
		"organizationName": a.OrganizationName,
		"added":            a.Added,
	}
}

// GetTimestamp mirrors get_timestamp: current UTC timestamp in ISO format.
func GetTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// AccountRef mirrors the {"number", "email"} ref shape used for switch
// from/to fields (see json_output.go's AccountRefDict for the JSON-boundary
// equivalent; this is the internal plumbing type switch methods pass around).
type AccountRef struct {
	Number *int
	Email  string
}

// AccountIdentityInfo mirrors account_identity's returned dict shape: a
// slot's stored identity fields, for auto-switch's identity comparisons.
type AccountIdentityInfo struct {
	Email            string
	OrganizationUUID string
	UUID             string
}

// AccountSnapshot mirrors AccountSnapshot (models.py): one managed
// account as seen by interactive UIs (the TUI). Usage is the store-backed
// UsageEntry read model; display code reads LastGood/AgeS directly (may
// show old data, annotated with its age), while Sentinel carries derived
// states ("api key", "token expired", ...) that replace the bars entirely.
type AccountSnapshot struct {
	Number     string
	Email      string
	OrgName    string
	OrgUUID    string
	IsActive   bool
	Kind       string // "oauth" | "api_key"
	Switchable bool
	Usage      UsageEntry
}

// DisplayTag mirrors the display_tag property: the org name, or
// "personal".
func (a AccountSnapshot) DisplayTag() string {
	if a.OrgName != "" {
		return a.OrgName
	}
	return "personal"
}

// AccountsSnapshotResult mirrors AccountsSnapshot (models.py): a coherent
// one-pass view of every managed account. Named with a Result suffix to
// avoid colliding with the AccountsSnapshot method on ClaudeAccountSwitcher
// that produces it (this port's established convention for this exact
// kind of collision, see AccountRefDict in json_output.go).
type AccountsSnapshotResult struct {
	ActiveNumber *string
	Accounts     []AccountSnapshot
	TakenAt      float64
}

// SwitchTransaction mirrors models.SwitchTransaction: a switch operation
// that can be rolled back.
type SwitchTransaction struct {
	OriginalCredentials string
	OriginalConfig      string
	OriginalAccountNum  string
	OriginalEmail       string
	ConfigPath          string
	CompletedSteps      []string
}

// RecordStep mirrors record_step.
func (t *SwitchTransaction) RecordStep(step string) {
	t.CompletedSteps = append(t.CompletedSteps, step)
}

// Rollback mirrors rollback: undoes all completed steps in reverse order.
// Returns true if every step's rollback succeeded, false if any failed
// (logged, not returned as an error, matching the Python's best-effort
// rollback that always finishes the loop).
func (t *SwitchTransaction) Rollback(s *ClaudeAccountSwitcher) bool {
	success := true
	for i := len(t.CompletedSteps) - 1; i >= 0; i-- {
		step := t.CompletedSteps[i]
		switch step {
		case "credentials_written":
			if err := s.WriteCredentials(t.OriginalCredentials); err != nil {
				s.Logger.Error("failed to rollback credentials", "error", err)
				success = false
				continue
			}
		case "config_written":
			if err := os.WriteFile(t.ConfigPath, []byte(t.OriginalConfig), 0o600); err != nil {
				s.Logger.Error("failed to rollback config", "error", err)
				success = false
				continue
			}
		case "sequence_updated":
			data := s.GetSequenceData()
			if data != nil {
				n, err := strconvAtoi(t.OriginalAccountNum)
				if err != nil {
					s.Logger.Error("failed to rollback sequence", "error", err)
					success = false
					continue
				}
				data.ActiveAccountNumber = &n
				data.LastUpdated = GetTimestamp()
				if err := s.WriteJSON(s.SequenceFile, data); err != nil {
					s.Logger.Error("failed to rollback sequence", "error", err)
					success = false
					continue
				}
			}
		}
		s.Logger.Info("rolled back step", "step", step)
	}
	return success
}
