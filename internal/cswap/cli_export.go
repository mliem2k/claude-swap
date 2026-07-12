package cswap

import "github.com/spf13/cobra"

// newExportCommand mirrors --export's dispatch (export_accounts).
func newExportCommand() *cobra.Command {
	var account string
	var full bool

	cmd := &cobra.Command{
		Use:   "export <path>",
		Short: "Export accounts to a portable JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			return s.ExportAccounts(args[0], account, full, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "limit export to a single account (NUM|EMAIL)")
	cmd.Flags().BoolVar(&full, "full", false, "include the entire ~/.claude.json snapshot per account")
	return cmd
}
