package cswap

import "github.com/spf13/cobra"

// newUpgradeCommand mirrors --upgrade's dispatch (run_self_upgrade),
// adapted per update_check.go's own doc comments: this checks GitHub
// releases, it does not shell out to a Python package manager.
func newUpgradeCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "upgrade",
		Aliases: []string{"update"},
		Short:   "Check for a newer release of cswap",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if code := RunSelfUpgrade(version, cmd.OutOrStdout()); code != 0 {
				return &exitCodeError{code: code}
			}
			return nil
		},
	}
}
