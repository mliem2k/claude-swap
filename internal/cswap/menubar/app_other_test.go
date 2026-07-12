//go:build !darwin

package menubar

import (
	"strings"
	"testing"

	"github.com/mliem2k/claude-swap/internal/cswap"
)

func TestRunOnNonDarwinReturnsClearError(t *testing.T) {
	err := Run(&cswap.ClaudeAccountSwitcher{})
	if err == nil {
		t.Fatal("Run on a non-darwin platform should return an error, got nil")
	}
	if !strings.Contains(err.Error(), "macOS") {
		t.Errorf("error = %q, want it to mention macOS", err.Error())
	}
}
