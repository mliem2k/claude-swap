package cswap

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"
)

// Constants mirroring macos_keychain.py.
const (
	securityBinPath        = "/usr/bin/security"
	securityTimeout        = 5 * time.Second
	securityNotFoundRC     = 44
	securityStdinLineLimit = 4096 - 64
)

var (
	// ErrKeychainNotFound is rc 44 from find/delete: the item is absent.
	// It is NOT an availability failure; callers treat it as a genuine miss.
	ErrKeychainNotFound = errors.New("keychain item not found")

	// ErrKeychainUnavailable is the catch-all a caller treats as "fall back
	// to file storage": a security failure, a timeout, or a missing binary.
	ErrKeychainUnavailable = errors.New("keychain unavailable")
)

// IsUnavailable mirrors the KEYCHAIN_ERRORS tuple. True when err wraps
// ErrKeychainUnavailable (a security failure, a timeout, or a missing binary).
// False for nil and for ErrKeychainNotFound (a real miss, not a failure).
func IsUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrKeychainUnavailable) {
		return true
	}
	return false
}

// SecurityCLI is the seam tests swap (the autouse in-memory Keychain guard in
// the Python suite). The production impl is osSecurityCLI.
type SecurityCLI interface {
	GetPassword(service, account string) (string, error)
	SetPassword(service, account, password string) error
	DeletePassword(service, account string) error
	ItemExists(service, account string) bool
}

// Security is the package-level backend. Swap via swapSecurity in tests.
var Security SecurityCLI = osSecurityCLI{}

// swapSecurity replaces the backend for a test and returns a restore func.
func swapSecurity(s SecurityCLI) func() {
	prev := Security
	Security = s
	return func() { Security = prev }
}

// KeychainAccountName mirrors keychain_account_name: $USER, then the OS
// username, then a stable fallback. Matching Claude Code exactly matters on
// headless hosts where $USER is unset.
func KeychainAccountName() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "claude-code-user"
}

// GetPassword mirrors get_password. Returns the stored value, ErrKeychainNotFound
// when absent (rc 44), or an ErrKeychainUnavailable-wrapped error on any other
// failure or timeout.
func GetPassword(service, account string) (string, error) {
	return Security.GetPassword(service, account)
}

// SetPassword mirrors set_password. Prefers stdin (-i) so the secret stays out
// of argv; falls back to argv for payloads that would overflow the stdin line
// buffer.
func SetPassword(service, account, password string) error {
	return Security.SetPassword(service, account, password)
}

// DeletePassword mirrors delete_password. rc 44 (already absent) counts as
// success.
func DeletePassword(service, account string) error {
	return Security.DeletePassword(service, account)
}

// ItemExists mirrors item_exists. Non-raising: a timeout, error, or missing
// binary all return false, so it never feeds the capability cache.
func ItemExists(service, account string) bool {
	return Security.ItemExists(service, account)
}

// quote mirrors _quote: double-quote and backslash-escape for security -i.
func quote(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// osSecurityCLI calls /usr/bin/security. It is import-safe on every platform;
// it only shells out at call time, and is only meaningful on macOS.
type osSecurityCLI struct{}

func (osSecurityCLI) GetPassword(service, account string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), securityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, securityBinPath,
		"find-generic-password", "-a", account, "-w", "-s", service)
	out, err := runCapture(cmd)
	if err != nil {
		if isNotFound(err) {
			return "", ErrKeychainNotFound
		}
		return "", unavailable(err)
	}
	// -w prints the value followed by one newline; strip exactly that.
	return strings.TrimSuffix(out, "\n"), nil
}

func (osSecurityCLI) SetPassword(service, account, password string) error {
	ctx, cancel := context.WithTimeout(context.Background(), securityTimeout)
	defer cancel()
	hexValue := hexEncode(password)
	command := "add-generic-password -U -a " + quote(account) + " -s " + quote(service) +
		" -X " + hexValue + "\n"

	var cmd *exec.Cmd
	if len(command) <= securityStdinLineLimit {
		cmd = exec.CommandContext(ctx, securityBinPath, "-i")
		cmd.Stdin = strings.NewReader(command)
	} else {
		cmd = exec.CommandContext(ctx, securityBinPath,
			"add-generic-password", "-U",
			"-a", account, "-s", service, "-X", hexValue)
	}
	_, err := runCapture(cmd)
	if err != nil {
		return unavailable(err)
	}
	return nil
}

func (osSecurityCLI) DeletePassword(service, account string) error {
	ctx, cancel := context.WithTimeout(context.Background(), securityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, securityBinPath,
		"delete-generic-password", "-a", account, "-s", service)
	_, err := runCapture(cmd)
	if err != nil {
		if isNotFound(err) {
			return nil // rc 44 already absent counts as success
		}
		return unavailable(err)
	}
	return nil
}

func (osSecurityCLI) ItemExists(service, account string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), securityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, securityBinPath,
		"find-generic-password", "-a", account, "-s", service)
	_, err := runCapture(cmd)
	return err == nil
}

// runCapture runs cmd and captures stdout. A non-zero exit returns an
// *exec.ExitError carrying the return code; a context deadline returns
// context.DeadlineExceeded.
func runCapture(cmd *exec.Cmd) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

// isNotFound is true for rc 44 (errSecItemNotFound).
func isNotFound(err error) bool {
	type exitErr interface{ ExitCode() int }
	if e, ok := err.(exitErr); ok {
		return e.ExitCode() == securityNotFoundRC
	}
	return false
}

// unavailable wraps any security failure (non-44 exit, timeout, missing
// binary) in ErrKeychainUnavailable so IsUnavailable classifies it.
func unavailable(err error) error {
	return errors.Join(ErrKeychainUnavailable, err)
}

// hexEncode mirrors password.encode("utf-8").hex().
func hexEncode(s string) string {
	const hexdigits = "0123456789abcdef"
	b := []byte(s)
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, hexdigits[c>>4], hexdigits[c&0x0f])
	}
	return string(out)
}
