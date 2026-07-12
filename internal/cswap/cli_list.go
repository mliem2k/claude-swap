package cswap

import (
	"github.com/spf13/cobra"
)

// newListCommand mirrors cli.py's --list dispatch: list_accounts(show_token_status, json_output).
func newListCommand() *cobra.Command {
	var jsonOutput, tokenStatus bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all managed accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Mirrors main()'s parser.error(): token status is not part of
			// the JSON v1 schema, reject rather than silently drop it (a
			// future additive field can add it).
			if jsonOutput && tokenStatus {
				return &cliUsageError{"--token-status cannot be combined with --json"}
			}
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			res, err := s.ListAccounts(tokenStatus, jsonOutput, nil)
			if err != nil {
				return err
			}
			if jsonOutput {
				printJSON(cmd.OutOrStdout(), res.Payload)
				return nil
			}
			printLines(cmd.OutOrStdout(), res.Lines)
			if res.NeedsFirstRunSetup {
				confirm := confirmPrompt(cliStdin(), cmd.OutOrStdout())
				msg, err := s.FirstRunSetup(confirm)
				if err != nil {
					return err
				}
				cmd.Println(msg)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&tokenStatus, "token-status", false, "show OAuth token debug status per account")
	return cmd
}

// newStatusCommand mirrors cli.py's --status dispatch: status(json_output).
func newStatusCommand() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the currently active account",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			res, err := s.Status(jsonOutput)
			if err != nil {
				return err
			}
			if jsonOutput {
				printJSON(cmd.OutOrStdout(), res.Payload)
				return nil
			}
			printLines(cmd.OutOrStdout(), res.Lines)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
