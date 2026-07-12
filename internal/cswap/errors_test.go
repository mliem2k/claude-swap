package cswap

import (
	"errors"
	"fmt"
	"testing"
)

func TestCredentialErrorsAreCatchableByParent(t *testing.T) {
	readErr := fmt.Errorf("boom: %w", ErrCredentialRead)
	writeErr := fmt.Errorf("boom: %w", ErrCredentialWrite)

	if !errors.Is(readErr, ErrCredential) {
		t.Fatal("ErrCredentialRead should satisfy errors.Is(err, ErrCredential)")
	}
	if !errors.Is(writeErr, ErrCredential) {
		t.Fatal("ErrCredentialWrite should satisfy errors.Is(err, ErrCredential)")
	}
	if !errors.Is(readErr, ErrClaudeSwitch) {
		t.Fatal("all errors should chain back to ErrClaudeSwitch")
	}
}

func TestLockTimeoutIsALockError(t *testing.T) {
	err := fmt.Errorf("held: %w", ErrClaudeCodeLockTimeout)
	if !errors.Is(err, ErrLock) {
		t.Fatal("ErrClaudeCodeLockTimeout should satisfy errors.Is(err, ErrLock)")
	}
}
