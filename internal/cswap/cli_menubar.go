package cswap

import "fmt"

// MenubarAppFactory runs the menu bar app until Quit, returning any
// startup/run error. Mirrors TUIAppFactory's role: a plug point rather
// than a direct import of internal/cswap/menubar here, since that
// package imports internal/cswap back (for *ClaudeAccountSwitcher and
// friends) and this package importing it in turn would be a hard Go
// import cycle. cmd/cswap's main() is the one place both packages meet
// safely, so it sets this var once at process startup, before RunCLI.
var MenubarAppFactory func(switcher *ClaudeAccountSwitcher) error

// runMenubar mirrors menubar.run(switcher): checked separately from the
// root command's RunE so a nil-factory guard is testable without a real
// switcher/backup dir. Unlike runTUI (which returns a bare exit code),
// menubar.Run's signature is a plain error, so this returns one too and
// lets RunCLI's existing dispatchError/isUsageError machinery decide the
// exit code, the same as every other subcommand's RunE.
func runMenubar(switcher *ClaudeAccountSwitcher) error {
	if MenubarAppFactory == nil {
		return fmt.Errorf("menu bar support is not wired up in this build: %w", ErrClaudeSwitch)
	}
	return MenubarAppFactory(switcher)
}
