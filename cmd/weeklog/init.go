package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/config"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter config for the sources on this machine",
		Long: `Write a starter config and enable the sources init can find.

On a terminal, init opens a form. Pick sources (git, Claude Code, Codex,
GitHub, Linear, and Cursor Cloud), paste tokens, and set the output dir and
mode. A pasted token is stored as a literal in the config file and is not
printed. --no-input skips the form.

Without a terminal, or with --no-input, init checks Claude Code
($CLAUDE_CONFIG_DIR or ~/.claude), Codex ($CODEX_HOME or ~/.codex), a
logged-in gh CLI, and git repositories under ~/Projects. The file enables
only the sources it found.

The file is one config.toml, written with mode 0600. No .env file is written.
GitHub uses token = "cmd:gh auth token" unless a token is pasted in the form.
Linear and cursor-cloud are commented. Uncomment a block and paste your key
into token. A cursor-cloud token is one literal value. env:, cmd:, and file:
references are optional. Token values are never printed.

The default path is $XDG_CONFIG_HOME/weeklog/config.toml, or
~/.config/weeklog/config.toml when XDG_CONFIG_HOME is unset. --config selects
another file.

The starter sets output.dir to ~/journal/weeklog and output.mode to both.
both writes the weekly file and prints the recap on a terminal. file writes
only. terminal prints only.

An existing file is left unchanged unless you confirm the replacement.
On a terminal, init asks before replacing it. Without a terminal, init
prints the config it would write. --force replaces the file when the form
is skipped.`,
		Example: `  weeklog init
  weeklog init --no-input
  weeklog init --config ./config.toml
  weeklog init --force`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runInit,
	}
	cmd.Flags().Bool("force", false, "replace an existing config file when the form is skipped")
	cmd.Flags().Bool("no-input", false, "skip the form and write the detected starter")
	return cmd
}

func runInit(cmd *cobra.Command, _ []string) error {
	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}
	noInput, err := cmd.Flags().GetBool("no-input")
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
	if useInitForm(noInput, cmd.InOrStdin()) {
		return runInitForm(cmd, ctx, path)
	}
	result, err := config.Init(ctx, config.InitOptions{Path: path, Force: force})
	if text := result.Report(); text != "" {
		if _, werr := fmt.Fprint(cmd.OutOrStdout(), text); werr != nil {
			return werr
		}
	}
	return err
}

// useInitForm reports whether init should open the form.
// --no-input and a non-terminal stdin keep the detected starter.
func useInitForm(noInput bool, stdin io.Reader) bool {
	if noInput {
		return false
	}
	f, ok := stdin.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

func printInitResult(w io.Writer, result config.InitResult) error {
	text := result.Report()
	if text == "" {
		return nil
	}
	_, err := fmt.Fprint(w, text)
	return err
}

// errInitCancelled is returned when the form is closed before it is submitted.
var errInitCancelled = errors.New("init cancelled")
