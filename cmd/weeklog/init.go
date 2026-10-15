package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/config"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter config for the sources on this machine",
		Long: `Write a starter config and enable the sources init can find.

init checks Claude Code ($CLAUDE_CONFIG_DIR or ~/.claude), Codex ($CODEX_HOME
or ~/.codex), a logged-in gh CLI, and git repositories under ~/Projects.
The file enables only the sources it found.

The default path is $XDG_CONFIG_HOME/weeklog/config.toml, or
~/.config/weeklog/config.toml when XDG_CONFIG_HOME is unset. --config selects
another file.

The starter sets output.dir to ~/journal/weeklog and output.mode to both.
both writes the weekly file and prints the recap on a terminal. file writes
only. terminal prints only.

An existing file is left unchanged. init prints the config it would write.
--force replaces the file. Token values are never written or printed.`,
		Example: `  weeklog init
  weeklog init --config ./config.toml
  weeklog init --force`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runInit,
	}
	cmd.Flags().Bool("force", false, "replace an existing config file")
	return cmd
}

func runInit(cmd *cobra.Command, _ []string) error {
	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}
	flag, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return err
	}
	path, err := config.ResolveConfigPath(flag)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := config.Init(ctx, config.InitOptions{Path: path, Force: force})
	if text := result.Report(); text != "" {
		if _, werr := fmt.Fprint(cmd.OutOrStdout(), text); werr != nil {
			return werr
		}
	}
	return err
}
