package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

func newSourcesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sources",
		Short: "List built-in sources and PATH plugins",
		Long: `List built-in sources and plugins named weeklog-source-<name>.

Plugins are read from the weeklog plugin directory and from PATH.
The plugin directory is $XDG_DATA_HOME/weeklog/plugins, or
~/.local/share/weeklog/plugins when XDG_DATA_HOME is unset.

Status comes from a protocol describe check. A built-in collector that is
not implemented yet still describes successfully. The command exits 0 when
the list is printed, including when a plugin's describe check fails.
--timeout limits each plugin.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			timeout, err := timeoutFlag(cmd)
			if err != nil {
				return err
			}
			reg := newSourceRegistry()
			reg.SetTimeout(timeout)
			list, err := reg.List(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if list.DirError != "" {
				if _, err := fmt.Fprintf(out, "plugin dir: %s\n", list.DirError); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprint(out, source.FormatSources(list.Sources)); err != nil {
				return err
			}
			_, err = fmt.Fprint(out, source.FormatIgnored(list.Ignored))
			return err
		},
	}
	addTimeoutFlag(cmd, "time limit for each plugin")
	return cmd
}
