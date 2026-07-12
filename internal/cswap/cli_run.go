package cswap

import "github.com/spf13/cobra"

// newRunCommand mirrors _run_command's dispatch (SessionManager.run).
func newRunCommand() *cobra.Command {
	var noShare bool
	var shareHistory bool

	cmd := &cobra.Command{
		Use:   "run <num|email> [-- <claude args>]",
		Short: "[EXPERIMENTAL] Launch Claude Code as a stored account in this terminal only",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			manager := NewSessionManager(s)
			return manager.Run(args[0], args[1:], !noShare, shareHistory)
		},
	}
	cmd.Flags().BoolVar(&noShare, "no-share", false, "don't share settings/keybindings/CLAUDE.md/skills/commands/agents from ~/.claude into the session profile")
	cmd.Flags().BoolVar(&shareHistory, "share-history", false, "share conversation history (projects/ and history.jsonl) from ~/.claude into the session profile")
	return cmd
}
