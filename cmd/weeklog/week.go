package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyjaymes/weeklog/internal/config"
	"github.com/jeremyjaymes/weeklog/internal/report"
	"github.com/jeremyjaymes/weeklog/internal/store"
	"github.com/jeremyjaymes/weeklog/internal/timeutil"
)

func newWeekCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "week",
		Short: "Print or write the weekly log",
		Long: `Print or write the weekly log for one time window.

The window is the previous full week in the configured timezone unless --week
or both --since and --until are set. --week takes an ISO week, YYYY-Www.
--since and --until accept RFC3339 timestamps or YYYY-MM-DD dates in the
configured timezone. --until is exclusive. A session counts on the day it
starts, including when it runs past midnight.

Linked work is grouped into Shipped, In progress, Explored, and Abandoned /
stale. Activity that is not linked yet is still included. Reviews, comments,
issues, and unknown events are summarized under Other activity. Time is the
sum of agent-session durations, labeled as an estimate. An item with no
session uses 15 minutes for each cluster of commits.

--md writes Markdown, which is the default. --json writes the same report as
JSON. Pass only one of them.

With --out FILE, the report is written to FILE. "-" writes to standard
output. Without --out, a configured output.dir receives <week>.md (or .json).
Otherwise the report is printed to standard output.

Rewriting a Markdown file keeps the text between <!-- weeklog:notes -->
markers. output.template selects a text/template; a relative path is resolved
from the config file's directory. An empty template uses the built-in one.

The database is the same file weeklog collect uses. --db selects another file.
Run weeklog collect first so the week has events to render.`,
		Example: `  weeklog week
  weeklog week --week 2026-W41
  weeklog week --json
  weeklog week --out ~/journal/weeklog/2026-W41.md`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runWeek,
	}
	cmd.Flags().String("week", "", "ISO week to render (YYYY-Www)")
	cmd.Flags().String("since", "", "window start (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().String("until", "", "window end, exclusive (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().Bool("md", false, "write Markdown (the default)")
	cmd.Flags().Bool("json", false, "write JSON")
	cmd.Flags().String("out", "", "write to this file instead of output.dir or standard output")
	cmd.Flags().String("db", "", "SQLite database (default is the weeklog data directory)")
	return cmd
}

func runWeek(cmd *cobra.Command, _ []string) error {
	md, err := cmd.Flags().GetBool("md")
	if err != nil {
		return err
	}
	asJSON, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if md && asJSON {
		return errors.New("pass either --md or --json")
	}
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
	outFlag, err := cmd.Flags().GetString("out")
	if err != nil {
		return err
	}
	templatePath, err := reportTemplate(cfg)
	if err != nil {
		return err
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

	rep, err := report.Build(ctx, st, report.Options{
		Since:    window.Since,
		Until:    window.Until,
		Label:    weekLabel(window, loc),
		Location: loc,
		Now:      time.Now(),
		Version:  version,
	})
	if err != nil {
		return err
	}

	ext := ".md"
	if asJSON {
		ext = ".json"
	}
	dest, toStdout, err := weekDestination(outFlag, cfg.Output.Dir, rep.FileBase(), ext)
	if err != nil {
		return err
	}
	if asJSON {
		body, err := report.JSON(rep)
		if err != nil {
			return err
		}
		if toStdout {
			_, err = cmd.OutOrStdout().Write(body)
			return err
		}
		if err := report.WriteBytes(dest, body); err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", dest)
		return err
	}
	if toStdout {
		body, err := report.Markdown(rep, templatePath)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), body)
		return err
	}
	if err := report.WriteMarkdown(dest, templatePath, rep); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", dest)
	return err
}

func weekLabel(window timeutil.Window, loc *time.Location) string {
	if window.Label != "" {
		return window.Label
	}
	year, week := window.Since.In(loc).ISOWeek()
	iso, err := timeutil.ParseISOWeek(fmt.Sprintf("%04d-W%02d", year, week), loc)
	if err != nil {
		return ""
	}
	if iso.Since.Equal(window.Since) && iso.Until.Equal(window.Until) {
		return iso.Label
	}
	return ""
}

func weekDestination(outFlag, dir, base, ext string) (string, bool, error) {
	outFlag = strings.TrimSpace(outFlag)
	switch {
	case outFlag == "-":
		return "", true, nil
	case outFlag != "":
		path, err := config.ExpandPath(outFlag)
		if err != nil {
			return "", false, fmt.Errorf("output path: %w", err)
		}
		if path == "" {
			return "", false, errors.New("output path is empty")
		}
		return path, false, nil
	case strings.TrimSpace(dir) != "":
		return filepath.Join(dir, base+ext), false, nil
	default:
		return "", true, nil
	}
}

func reportTemplate(cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", nil
	}
	path := strings.TrimSpace(cfg.Output.Template)
	if path == "" || filepath.IsAbs(path) || cfg.Dir == "" {
		return path, nil
	}
	return filepath.Join(cfg.Dir, path), nil
}
