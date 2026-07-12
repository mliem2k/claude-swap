package main

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mliem2k/claude-swap/internal/cswap"
	"github.com/mliem2k/claude-swap/internal/cswap/menubar"
	"github.com/mliem2k/claude-swap/internal/cswap/tui"
)

func main() {
	// internal/cswap/tui imports internal/cswap (for *ClaudeAccountSwitcher
	// and friends), so internal/cswap can't import tui back without a
	// cycle. main is the one place both packages can meet safely, so it
	// wires cswap.TUIAppFactory here, once, before RunCLI ever runs.
	cswap.TUIAppFactory = func(switcher *cswap.ClaudeAccountSwitcher, start string) tea.Model {
		return tui.NewApp(switcher, start)
	}
	// internal/cswap/menubar imports internal/cswap the same way tui does,
	// so cswap.MenubarAppFactory is wired here for the same import-cycle
	// reason.
	cswap.MenubarAppFactory = func(switcher *cswap.ClaudeAccountSwitcher) error {
		return menubar.Run(switcher)
	}
	os.Exit(cswap.RunCLI(os.Args, os.Stdout, os.Stderr))
}
