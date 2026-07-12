package cswap

import (
	"errors"
	"fmt"
)

// Error sentinels mirror exceptions.py's class hierarchy. Each concrete
// sentinel wraps its parent so errors.Is matches by category, mirroring
// Python's except CredentialError catching read and write errors.

// ErrClaudeSwitch is the base for every cswap error (ClaudeSwitchError).
var ErrClaudeSwitch = errors.New("claude-swap error")

// ErrCredential and its children (CredentialError family).
var (
	ErrCredential      = wrap(ErrClaudeSwitch, "credential error")
	ErrCredentialRead  = wrap(ErrCredential, "credential read error")
	ErrCredentialWrite = wrap(ErrCredential, "credential write error")
)

// ErrConfig (ConfigError).
var ErrConfig = wrap(ErrClaudeSwitch, "config error")

// ErrSwitch (SwitchError).
var ErrSwitch = wrap(ErrClaudeSwitch, "switch error")

// ErrSession (SessionError).
var ErrSession = wrap(ErrClaudeSwitch, "session error")

// ErrLock and the Claude Code lock timeout subclass (LockError family).
var (
	ErrLock                  = wrap(ErrClaudeSwitch, "lock error")
	ErrClaudeCodeLockTimeout = wrap(ErrLock, "claude code lock timeout")
)

// ErrAccountNotFound (AccountNotFoundError).
var ErrAccountNotFound = wrap(ErrClaudeSwitch, "account not found")

// ErrValidation (ValidationError).
var ErrValidation = wrap(ErrClaudeSwitch, "validation error")

// ErrTransfer (TransferError).
var ErrTransfer = wrap(ErrClaudeSwitch, "transfer error")

// ErrMigration and its incomplete subclass (MigrationError family).
var (
	ErrMigration           = wrap(ErrClaudeSwitch, "migration error")
	ErrMigrationIncomplete = wrap(ErrMigration, "migration incomplete")
)

// wrap returns an error whose Unwrap yields parent, so errors.Is matches the
// whole chain. The message names the child then the parent for readability.
func wrap(parent error, msg string) error {
	return fmt.Errorf("%s: %w", msg, parent)
}

// errorClassName mirrors type(exc).__name__ from Python's error_envelope:
// the specific exception class name for a handled ClaudeSwitchError, not
// just the base "ClaudeSwitchError" every sentinel here ultimately wraps.
// Checked most-specific first (a leaf sentinel's wrap chain also matches
// errors.Is against every one of its ancestors), falling back to the base
// name for anything that only matches ErrClaudeSwitch itself.
func errorClassName(err error) string {
	switch {
	case errors.Is(err, ErrCredentialRead):
		return "CredentialReadError"
	case errors.Is(err, ErrCredentialWrite):
		return "CredentialWriteError"
	case errors.Is(err, ErrCredential):
		return "CredentialError"
	case errors.Is(err, ErrConfig):
		return "ConfigError"
	case errors.Is(err, ErrSwitch):
		return "SwitchError"
	case errors.Is(err, ErrSession):
		return "SessionError"
	case errors.Is(err, ErrClaudeCodeLockTimeout):
		return "ClaudeCodeLockTimeout"
	case errors.Is(err, ErrLock):
		return "LockError"
	case errors.Is(err, ErrAccountNotFound):
		return "AccountNotFoundError"
	case errors.Is(err, ErrValidation):
		return "ValidationError"
	case errors.Is(err, ErrTransfer):
		return "TransferError"
	case errors.Is(err, ErrMigrationIncomplete):
		return "MigrationIncomplete"
	case errors.Is(err, ErrMigration):
		return "MigrationError"
	default:
		return "ClaudeSwitchError"
	}
}
