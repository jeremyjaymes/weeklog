package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/config"
	"github.com/jeremyjaymes/weeklog/internal/linker"
	"github.com/jeremyjaymes/weeklog/internal/store"
	"github.com/jeremyjaymes/weeklog/internal/timeutil"
)

func newLinkRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link-run",
		Short: "Recompute hard links for stored events",
		Long: `Recompute pass-1 hard links for every stored event.

A commit joins a pull request when that pull request lists the commit SHA.
A ticket key joins the events that mention it. A pull request joins commits
and sessions on its head branch in the same repository. A pull request joins
its reviews when their source ids share a lineage. Each link stores the
reason it was made.

The window flags match weeklog collect. Pass 1 still reads every stored
event, so a commit can join a ticket collected in another week. --week takes
an ISO week, YYYY-Www. --since and --until accept RFC3339 timestamps or
YYYY-MM-DD dates. --until is exclusive. With no window flags, the window is
the previous full week in the configured timezone.

Ticket keys come from each source's ticket_pattern. When none is set,
weeklog uses a built-in key shape such as GEN-301.

The database is the same file weeklog collect uses. --db selects another file.`,
		Example: `  weeklog link-run
  weeklog link-run --week 2026-W41
  weeklog link-run --since 2026-10-05 --until 2026-10-12`,
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
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			loc, err := cfg.Identity.Location()
			if err != nil {
				return err
			}
			weekStart, err := cfg.Identity.WeekStart()
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
			window, err := linkWindow(week, since, until, loc, weekStart)
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
			patterns := linker.TicketPatterns(cfg)
			for _, pattern := range patterns {
				if _, err := regexp.Compile(pattern); err != nil {
					return fmt.Errorf("ticket_pattern %q: %w", pattern, err)
				}
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			st, err := store.Open(ctx, dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			opt := linker.Options{}
			if len(patterns) > 0 {
				opt.TicketPatterns = patterns
			}
			if err := linker.RunWith(ctx, st, window.Since, window.Until, opt); err != nil {
				return err
			}
			items, err := st.ListWorkItems(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "db: %s\n%s\nlink: ok (%s)\n", dbPath, formatLinkWindow(window), workItemCount(len(items))); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().String("week", "", "ISO week to select (YYYY-Www)")
	cmd.Flags().String("since", "", "window start (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().String("until", "", "window end, exclusive (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().String("db", "", "SQLite database (default is the weeklog data directory)")
	return cmd
}

func linkWindow(week, since, until string, loc *time.Location, weekStart time.Weekday) (timeutil.Window, error) {
	week = strings.TrimSpace(week)
	since = strings.TrimSpace(since)
	until = strings.TrimSpace(until)
	switch {
	case week != "" && (since != "" || until != ""):
		return timeutil.Window{}, errors.New("pass either --week or --since and --until")
	case (since == "") != (until == ""):
		return timeutil.Window{}, errors.New("--since and --until must be set together")
	case week != "":
		return timeutil.ParseISOWeek(week, loc)
	case since != "":
		return timeutil.ParseRange(since, until, loc)
	default:
		return timeutil.PreviousWeek(time.Now().In(loc), weekStart), nil
	}
}

func formatLinkWindow(window timeutil.Window) string {
	since := window.Since.Format(time.RFC3339)
	until := window.Until.Format(time.RFC3339)
	if window.Label != "" {
		return fmt.Sprintf("window: %s (%s .. %s)", window.Label, since, until)
	}
	return fmt.Sprintf("window: %s .. %s", since, until)
}

func workItemCount(n int) string {
	if n == 1 {
		return "1 work item"
	}
	return fmt.Sprintf("%d work items", n)
}
