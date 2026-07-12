package cswap

import (
	"github.com/spf13/cobra"
)

// newAddCommand mirrors cli.py's --add-account dispatch: add_account(slot).
func newAddCommand() *cobra.Command {
	var slot int
	var assumeYes bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add the currently active Claude account to the managed list",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			var slotPtr *int
			if cmd.Flags().Changed("slot") {
				slotPtr = &slot
			}
			var confirm func(string) bool
			if !assumeYes {
				confirm = confirmPrompt(cliStdin(), cmd.OutOrStdout())
			}
			msg, err := s.AddAccount(slotPtr, confirm)
			if err != nil {
				return err
			}
			cmd.Println(msg)
			return nil
		},
	}
	cmd.Flags().IntVar(&slot, "slot", 0, "account slot number (auto-assigned if omitted)")
	cmd.Flags().BoolVarP(&assumeYes, "assume-yes", "y", false, "answer yes to any confirmation prompt")
	return cmd
}

// newAddTokenCommand mirrors cli.py's --add-token dispatch:
// add_account_from_token(token, email, slot).
func newAddTokenCommand() *cobra.Command {
	var slot int
	var email string
	var assumeYes bool
	cmd := &cobra.Command{
		Use:   "add-token [token]",
		Short: "Add an account from a setup token (sk-ant-oat...) instead of the live login",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			token := ""
			if len(args) > 0 {
				token = args[0]
			}
			var slotPtr *int
			if cmd.Flags().Changed("slot") {
				slotPtr = &slot
			}
			var confirm func(string) bool
			if !assumeYes {
				confirm = confirmPrompt(cliStdin(), cmd.OutOrStdout())
			}
			msg, err := s.AddAccountFromToken(token, email, slotPtr, confirm)
			if err != nil {
				return err
			}
			cmd.Println(msg)
			return nil
		},
	}
	cmd.Flags().IntVar(&slot, "slot", 0, "account slot number (auto-assigned if omitted)")
	cmd.Flags().StringVar(&email, "email", "", "email to associate with this token (prompted if omitted, matching AddAccountFromToken's own behavior)")
	cmd.Flags().BoolVarP(&assumeYes, "assume-yes", "y", false, "answer yes to any confirmation prompt")
	return cmd
}
