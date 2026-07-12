package cswap

import "github.com/spf13/cobra"

// newImportCommand mirrors --import's dispatch (import_accounts).
func newImportCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "import <path>",
		Short: "Import accounts from a portable JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			return s.ImportAccounts(args[0], force, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing matching slot in place")
	return cmd
}
