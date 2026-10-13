// Command weeklog builds a developer's weekly log from pluggable sources.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// version is the build version. Release builds override it with
// -ldflags "-X main.version=<version>". Development builds keep "dev".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "weeklog",
		Short: "Build a Markdown weekly log from pluggable sources",
		Long: `weeklog pulls a developer's week from pluggable sources (git, coding agents,
pull requests, and tickets), links the activity into units of work, and writes
a plain Markdown weekly log.

This build is pre-alpha. Implemented commands: version, config check, sources, doctor, collect, link-run.`,
		SilenceUsage: true,
		Version:      version,
	}
	cmd.PersistentFlags().String("config", "", "config file (default $XDG_CONFIG_HOME/weeklog/config.toml)")
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newSourcesCmd())
	cmd.AddCommand(newCollectCmd())
	cmd.AddCommand(newLinkRunCmd())
	cmd.AddCommand(newDoctorCmd())
	return cmd
}
