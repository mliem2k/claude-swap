package cswap

import (
	"bufio"

	"github.com/spf13/cobra"
)

// newRemoveCommand mirrors cli.py's --remove-account dispatch:
// remove_account(identifier).
func newRemoveCommand() *cobra.Command {
	var assumeYes bool
	cmd := &cobra.Command{
		Use:     "remove <identifier>",
		Aliases: []string{"rm"},
		Short:   "Permanently remove a managed account (number or email)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			// One shared *bufio.Reader for both prompts: each of
			// confirmPrompt/disambiguatePrompt wraps its input in its own
			// bufio.NewReader internally, and bufio.NewReader reuses an
			// already-*bufio.Reader argument in place (stdlib fast path)
			// rather than layering a second buffer, so passing the same
			// instance to both avoids one prompt's read-ahead silently
			// consuming bytes the other prompt needed (e.g. a
			// disambiguation choice line swallowing the confirm answer
			// that followed it on stdin).
			stdin := bufio.NewReader(cliStdin())
			var confirm func(string) bool
			if !assumeYes {
				confirm = confirmPrompt(stdin, cmd.OutOrStdout())
			}
			disambiguate := disambiguatePrompt(s, args[0], "Enter account number to remove: ", stdin, cmd.OutOrStdout())
			msg, err := s.RemoveAccount(args[0], confirm, disambiguate)
			if err != nil {
				return err
			}
			cmd.Println(msg)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&assumeYes, "assume-yes", "y", false, "answer yes to the confirmation prompt")
	return cmd
}

// newPurgeCommand mirrors cli.py's --purge dispatch: purge().
func newPurgeCommand() *cobra.Command {
	var assumeYes bool
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Remove all claude-swap data from this system",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			var confirm func(string) bool
			if !assumeYes {
				confirm = confirmPrompt(cliStdin(), cmd.OutOrStdout())
			}
			res, err := s.Purge(confirm)
			if err != nil {
				return err
			}
			if res.Cancelled {
				cmd.Println("Cancelled")
				return nil
			}
			if len(res.RemovedItems) == 0 {
				cmd.Println("No claude-swap data found to remove.")
			} else {
				cmd.Println("Removed:")
				for _, item := range res.RemovedItems {
					cmd.Printf("  - %s\n", item)
				}
			}
			cmd.Println("Purge complete.")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&assumeYes, "assume-yes", "y", false, "answer yes to the confirmation prompt")
	return cmd
}
