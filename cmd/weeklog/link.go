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
		Short: "Recompute work items for stored events",
		Long: `Recompute work items for every stored event.

Pass 1 stores a hard-link reason. A commit joins a pull request when that
pull request lists the commit SHA. A ticket key joins the events that mention
it. A pull request joins commits and sessions on its head branch in the same
repository. A pull request joins its reviews when their source ids share a
lineage.

Pass 2 stores a soft-link reason on an event that pass 1 left ungrouped. An
agent session joins commits in the repository its working directory resolves
to when they share a file and were authored between the session start and two
hours after the session end. With no shared file, the session joins those
commits only when it is the only session in the window. A session also joins
a branch named by a git checkout or switch. A revert joins the commit it
reverts.

Pass 3 groups anything still unlinked by repository and local day. A session
with no commits stays its own item. Each item then gets a status: shipped,
in progress, explored, or abandoned.

weeklog link and weeklog unlink store overrides. This command applies those
overrides after pass 3. They are kept for the next link run and the next
collect.

The window flags match weeklog collect. Linking still reads every stored
event, so a commit can join a ticket collected in another week. The window
end is the clock for a stale branch, and its timezone chooses the local day.
--week takes an ISO week, YYYY-Www. --since and --until accept RFC3339
timestamps or YYYY-MM-DD dates. --until is exclusive. With no window flags,
the window is the previous full week in the configured timezone.

Ticket keys come from each source's ticket_pattern. When none is set,
weeklog uses a built-in key shape such as GEN-301.

The database is the same file weeklog collect uses. --db selects another file.`,
		Example: `  weeklog link-run
  weeklog link-run --week 2026-W41
  weeklog link-run --since 2026-10-05 --until 2026-10-12`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runLinkRun,
	}
	addLinkFlags(cmd)
	return cmd
}

func runLinkRun(cmd *cobra.Command, _ []string) error {
	session, err := openLinkSession(cmd)
	if err != nil {
		return err
	}
	defer session.close()
	n, err := session.relink()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "db: %s\n%s\nlink: ok (%s)\n", session.dbPath, formatLinkWindow(session.window), workItemCount(n))
	return err
}

// linkSession is one configured linker run against an open store.
type linkSession struct {
	st     *store.Store
	window timeutil.Window
	dbPath string
	opt    linker.Options
	ctx    context.Context
}

func (s *linkSession) close() {
	if s != nil && s.st != nil {
		_ = s.st.Close()
	}
}

func (s *linkSession) relink() (int, error) {
	if err := linker.RunWith(s.ctx, s.st, s.window.Since, s.window.Until, s.opt); err != nil {
		return 0, err
	}
	items, err := s.st.ListWorkItems(s.ctx)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func addLinkFlags(cmd *cobra.Command) {
	cmd.Flags().String("week", "", "ISO week to select (YYYY-Www)")
	cmd.Flags().String("since", "", "window start (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().String("until", "", "window end, exclusive (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().String("db", "", "SQLite database (default is the weeklog data directory)")
}

// openLinkSession loads config, validates ticket patterns, and opens the
// database. Patterns are checked before the database is created.
func openLinkSession(cmd *cobra.Command) (*linkSession, error) {
	cfg, loc, weekStart, err := loadLinkConfig(cmd)
	if err != nil {
		return nil, err
	}
	week, err := cmd.Flags().GetString("week")
	if err != nil {
		return nil, err
	}
	since, err := cmd.Flags().GetString("since")
	if err != nil {
		return nil, err
	}
	until, err := cmd.Flags().GetString("until")
	if err != nil {
		return nil, err
	}
	window, err := linkWindow(week, since, until, loc, weekStart)
	if err != nil {
		return nil, err
	}
	dbFlag, err := cmd.Flags().GetString("db")
	if err != nil {
		return nil, err
	}
	dbPath, err := resolveDBPath(dbFlag)
	if err != nil {
		return nil, err
	}
	patterns := linker.TicketPatterns(cfg)
	for _, pattern := range patterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("ticket_pattern %q: %w", pattern, err)
		}
	}
	ctx := commandContext(cmd)
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	opt := linker.Options{}
	if len(patterns) > 0 {
		opt.TicketPatterns = patterns
	}
	return &linkSession{st: st, window: window, dbPath: dbPath, opt: opt, ctx: ctx}, nil
}

func loadLinkConfig(cmd *cobra.Command) (*config.Config, *time.Location, time.Weekday, error) {
	flag, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return nil, nil, 0, err
	}
	path, err := config.ResolveConfigPath(flag)
	if err != nil {
		return nil, nil, 0, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, 0, err
	}
	loc, err := cfg.Identity.Location()
	if err != nil {
		return nil, nil, 0, err
	}
	weekStart, err := cfg.Identity.WeekStart()
	if err != nil {
		return nil, nil, 0, err
	}
	return cfg, loc, weekStart, nil
}

func commandContext(cmd *cobra.Command) context.Context {
	if cmd == nil {
		return context.Background()
	}
	ctx := cmd.Context()
	if ctx == nil {
		return context.Background()
	}
	return ctx
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
