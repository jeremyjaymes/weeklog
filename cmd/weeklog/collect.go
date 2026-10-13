package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/collect"
	"github.com/jeremyjaymes/weeklog/internal/config"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func newCollectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect events for a week and store them",
		Long: `Collect events from configured sources and store them in SQLite.

The window is the previous full week in the configured timezone unless --week
or both --since and --until are set. --week takes an ISO week, YYYY-Www.
--since and --until accept RFC3339 timestamps or YYYY-MM-DD dates in the
configured timezone. --until is exclusive.

--source limits collection to configured sources and can be repeated.
A source that does not collect yet is skipped and listed. A source that
fails does not stop the others. Events from sources that succeed are stored,
then the linker runs.

Cursors are saved per configured source, in config order. A source that
fails or is skipped does not advance its cursor. Cancelling the command
stops the current source and does not start another.

Secrets resolved from the config are redacted before events are written.
--timeout limits each source.

The database is $XDG_DATA_HOME/weeklog/weeklog.db when XDG_DATA_HOME is set.
Otherwise it is ~/.local/share/weeklog/weeklog.db, or
%USERPROFILE%\AppData\Local\weeklog\weeklog.db on Windows. --db selects
another file.`,
		Example: `  weeklog collect
  weeklog collect --week 2026-W41
  weeklog collect --since 2026-10-05 --until 2026-10-12 --source git`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			timeout, err := timeoutFlag(cmd)
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
			dbFlag, err := cmd.Flags().GetString("db")
			if err != nil {
				return err
			}
			dbPath, err := resolveDBPath(dbFlag)
			if err != nil {
				return err
			}
			week, err := cmd.Flags().GetString("week")
			if err != nil {
				return err
			}
			since, err := cmd.Flags().GetString("since")
			if err != nil {
				return err
			}
			until, err := cmd.Flags().GetString("until")
			if err != nil {
				return err
			}
			sources, err := cmd.Flags().GetStringArray("source")
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			reg := newSourceRegistry()
			summary, err := collect.Run(ctx, reg, collect.Options{
				ConfigPath: path,
				DBPath:     dbPath,
				Week:       week,
				Since:      since,
				Until:      until,
				Sources:    sources,
				Timeout:    timeout,
			})
			if summary.Warnings != "" {
				if _, werr := fmt.Fprint(cmd.ErrOrStderr(), summary.Warnings); werr != nil {
					return werr
				}
			}
			if summary.Text != "" {
				if _, werr := fmt.Fprint(cmd.OutOrStdout(), summary.Text); werr != nil {
					return werr
				}
			}
			return err
		},
	}
	cmd.Flags().String("week", "", "ISO week to collect (YYYY-Www)")
	cmd.Flags().String("since", "", "window start (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().String("until", "", "window end, exclusive (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().StringArray("source", nil, "collect only this configured source (repeatable)")
	cmd.Flags().String("db", "", "SQLite database (default is the weeklog data directory)")
	addTimeoutFlag(cmd, "time limit for each source")
	return cmd
}

func resolveDBPath(flag string) (string, error) {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return store.DefaultPath()
	}
	path, err := config.ExpandPath(flag)
	if err != nil {
		return "", fmt.Errorf("db path: %w", err)
	}
	if path == "" {
		return "", errors.New("db path is empty")
	}
	return path, nil
}
