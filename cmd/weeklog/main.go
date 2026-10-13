// Command weeklog builds a developer's weekly log from pluggable sources.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

// version is the build version. Release builds override it with
// -ldflags "-X main.version=<version>". Development builds keep "dev".
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
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

This build is pre-alpha: only the version command is implemented.`,
		SilenceUsage: true,
		Version:      version,
	}
	cmd.AddCommand(newVersionCmd())
	return cmd
}
