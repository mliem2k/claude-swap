package cswap

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newConfigCommand mirrors cli.py's `config` pre-dispatch (_config_command):
// list/get/set/unset/path subactions over the settings.json registry.
// A bare `cswap config` defaults to `list`, matching Python's
// `action = args.action or "list"`. --json is registered here too (not
// just on the `list`/`get` children), matching Python's argparse
// registering --json on the parent `config` parser itself, so
// `cswap config --json` behaves like `cswap config list --json` instead
// of failing with "unknown flag".
func newConfigCommand() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or change auto-switch settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigList(cmd, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.AddCommand(newConfigListCommand())
	cmd.AddCommand(newConfigGetCommand())
	cmd.AddCommand(newConfigSetCommand())
	cmd.AddCommand(newConfigUnsetCommand())
	cmd.AddCommand(newConfigPathCommand())
	return cmd
}

func newConfigListCommand() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every setting and its effective value",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigList(cmd, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// runConfigList mirrors _config_command's "list" action body.
func runConfigList(cmd *cobra.Command, jsonOutput bool) error {
	debug, _ := cmd.Flags().GetBool("debug")
	s, err := newSwitcher(debug)
	if err != nil {
		return err
	}
	rows, err := EffectiveSettings(s.BackupDir)
	if err != nil {
		return err
	}
	if jsonOutput {
		settings := make([]map[string]any, len(rows))
		for i, row := range rows {
			settings[i] = map[string]any{
				"key":   row.Spec.Dotted(),
				"value": row.Value,
				"isSet": row.IsSet,
			}
		}
		printJSON(cmd.OutOrStdout(), map[string]any{
			"schemaVersion": JSONSchemaVersion,
			"path":          SettingsPath(s.BackupDir),
			"settings":      settings,
		})
		return nil
	}
	keyWidth, valWidth := 0, 0
	for _, row := range rows {
		if l := len(row.Spec.Dotted()); l > keyWidth {
			keyWidth = l
		}
		if l := len(FormatSettingValue(row.Value)); l > valWidth {
			valWidth = l
		}
	}
	var lines []string
	for _, row := range rows {
		line := fmt.Sprintf("%-*s  %-*s", keyWidth, row.Spec.Dotted(), valWidth, FormatSettingValue(row.Value))
		if !row.IsSet {
			line += "  " + Dimmed("(default)")
		}
		lines = append(lines, line)
	}
	printLines(cmd.OutOrStdout(), lines)
	return nil
}

func newConfigGetCommand() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Print one setting's effective value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			spec, err := SettingSpecFor(args[0])
			if err != nil {
				return err
			}
			rows, err := EffectiveSettings(s.BackupDir)
			if err != nil {
				return err
			}
			var value any
			var isSet bool
			for _, row := range rows {
				if row.Spec.Dotted() == spec.Dotted() {
					value, isSet = row.Value, row.IsSet
					break
				}
			}
			if jsonOutput {
				printJSON(cmd.OutOrStdout(), map[string]any{
					"schemaVersion": JSONSchemaVersion,
					"key":           spec.Dotted(),
					"value":         value,
					"isSet":         isSet,
				})
				return nil
			}
			cmd.Println(FormatSettingValue(value))
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func newConfigSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set one setting",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			value, err := SetSetting(s.BackupDir, args[0], args[1])
			if err != nil {
				return err
			}
			cmd.Printf("%s = %s\n", args[0], FormatSettingValue(value))
			return nil
		},
	}
}

func newConfigUnsetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Clear one setting back to its default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			ok, err := UnsetSetting(s.BackupDir, args[0])
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(cmd.ErrOrStderr(), Muted(fmt.Sprintf("%s is not set; nothing to do", args[0])))
				return nil
			}
			spec, err := SettingSpecFor(args[0])
			if err != nil {
				return err
			}
			cmd.Printf("%s unset (default: %s)\n", args[0], FormatSettingValue(spec.Default()))
			return nil
		},
	}
}

func newConfigPathCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print settings.json's path",
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}
			cmd.Println(SettingsPath(s.BackupDir))
			return nil
		},
	}
}
