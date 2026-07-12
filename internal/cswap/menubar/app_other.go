//go:build !darwin

package menubar

import (
	"fmt"

	"github.com/mliem2k/claude-swap/internal/cswap"
)

// Run mirrors the "rumps not installed" behavior Python has on every
// non-macOS platform (rumps is a macOS-only optional extra): a clear,
// non-panicking error instead of attempting anything.
func Run(switcher *cswap.ClaudeAccountSwitcher) error {
	return fmt.Errorf("the menu bar app is macOS only")
}
