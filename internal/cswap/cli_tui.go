package cswap

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"golang.org/x/term"
)

// isInteractiveTerminal mirrors the two-sided isatty gate cli.py uses
// before auto-launching the TUI: sys.stdout.isatty() and
// sys.stdin.isatty(). Takes explicit files (not the ambient os.Stdin/
// os.Stdout) so it's testable with a pipe.
func isInteractiveTerminal(stdin, stdout *os.File) bool {
	return term.IsTerminal(int(stdin.Fd())) && term.IsTerminal(int(stdout.Fd()))
}

// TUIAppFactory builds the Bubble Tea model for the interactive dashboard.
// It's a plug point rather than a direct `import
// ".../internal/cswap/tui"` here because internal/cswap/tui itself
// imports internal/cswap (for *ClaudeAccountSwitcher, AccountSnapshot,
// etc. — see tui/app.go and friends), so this package importing tui back
// would be a hard Go import cycle (cswap -> tui -> cswap). cmd/cswap's
// main() is the only place both packages meet safely (main is never
// itself imported), so it sets this var once at process startup, before
// calling RunCLI. Mirrors the existing stdinOverride var below: a
// package-level injection point for exactly this "need an external
// dependency without importing it" shape.
var TUIAppFactory func(switcher *ClaudeAccountSwitcher, start string) tea.Model

// runTUI mirrors tui.run(switcher, start): builds and runs the Bubble Tea
// program, returning the process exit code.
func runTUI(switcher *ClaudeAccountSwitcher, start string) int {
	if TUIAppFactory == nil {
		Error("tui support is not wired up in this build")
		return 1
	}
	program := tea.NewProgram(TUIAppFactory(switcher, start), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		Error(err.Error())
		return 1
	}
	return 0
}

// newTUICommand mirrors `cswap tui`. Uses the codebase's real, existing
// helper for this exact job: every command pulls its own --debug flag and
// calls `newSwitcher(debug bool) (*ClaudeAccountSwitcher, error)`. RunE
// returns an error (or the special `*exitCodeError` this codebase already
// uses for `cswap auto`'s non-0/1 exit codes, see cli_auto.go:94,135)
// rather than calling os.Exit directly, so RunCLI's signal-handling select
// and tests both keep working, exactly the established convention.
func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			return &exitCodeError{code: runTUI(s, "dashboard")}
		},
	}
}

func newWatchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Open the dashboard on the live watch page",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			return &exitCodeError{code: runTUI(s, "watch")}
		},
	}
}
