package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/config"
)

var errConfigCheck = errors.New("config check failed")

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "config",
		Short:        "Inspect weeklog configuration",
		Long:         "Inspect and validate the weeklog configuration file.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newConfigCheckCmd())
	return cmd
}

func newConfigCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Validate config and resolve secret references",
		Long: `Validate config.toml and resolve optional env:, cmd:, and file: secret references.

A literal secret in the file is the normal form. The report says whether each
secret resolved. It never prints secret values. On Unix, a config file that is
group- or world-readable is a warning.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flag, err := cmd.Root().PersistentFlags().GetString("config")
			if err != nil {
				return err
			}
			path, err := config.ResolveConfigPath(flag)
			if err != nil {
				return err
			}
			result, err := config.Check(cmd.Context(), path)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), result.Report()); err != nil {
				return err
			}
			if !result.OK() {
				return errConfigCheck
			}
			return nil
		},
	}
}
