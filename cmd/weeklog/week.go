package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
	"github.com/muesli/termenv"
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

--md writes Markdown, which is the default when the report is saved or
piped. --json writes the same report as JSON. Pass only one of --md and
--json. --plain cannot be combined with --json.

On a terminal, standard output is a boxed recap: one bar per day, a colored
lane for each source, and the top repositories and pull requests. The
Markdown report is printed instead when stdout is not a terminal, when
NO_COLOR is set, or when --plain or --md is set. Pipes keep that Markdown.

With --out FILE, the report is written to FILE. "-" writes to standard
output. --out ignores output.mode.

Without --out, output.mode chooses the destination. both, the default,
writes <output.dir>/<week>.md (or .json) and, when stdout is a terminal,
also prints the recap followed by a dim line naming the saved file. file
writes that file and prints where it was written. terminal prints the
report and does not write a file. When output.dir is unset, the report is
printed to standard output.

Rewriting a Markdown file keeps the text between <!-- weeklog:notes -->
markers. output.template selects a text/template; a relative path is resolved
from the config file's directory. An empty template uses the built-in one.

The database is the same file weeklog collect uses. --db selects another file.
Run weeklog collect first so the week has events to render.`,
		Example: `  weeklog week
  weeklog week --week 2026-W41
  weeklog week --plain
  weeklog week --json
  weeklog week --out ~/journal/weeklog/2026-W41.md`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE:         runWeek,
	}
	cmd.Flags().String("week", "", "ISO week to render (YYYY-Www)")
	cmd.Flags().String("since", "", "window start (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().String("until", "", "window end, exclusive (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().Bool("md", false, "write Markdown (the default for a pipe or a file)")
	cmd.Flags().Bool("plain", false, "print Markdown even when stdout is a terminal")
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
	plain, err := cmd.Flags().GetBool("plain")
	if err != nil {
		return err
	}
	if md && asJSON {
		return errors.New("pass either --md or --json")
	}
	if plain && asJSON {
		return errors.New("pass either --plain or --json")
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
	mode, err := cfg.Output.ResolvedMode()
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
	out := cmd.OutOrStdout()
	term := detectTerminal(out)
	plan, err := planWeekOutput(outFlag, cfg.Output.Dir, mode, rep.FileBase(), ext, term, asJSON)
	if err != nil {
		return err
	}
	if plan.path != "" {
		if asJSON {
			body, err := report.JSON(rep)
			if err != nil {
				return err
			}
			if err := report.WriteBytes(plan.path, body); err != nil {
				return err
			}
		} else if err := report.WriteMarkdown(plan.path, templatePath, rep); err != nil {
			return err
		}
	}
	if plan.report {
		if asJSON {
			body, err := report.JSON(rep)
			if err != nil {
				return err
			}
			if _, err := out.Write(body); err != nil {
				return err
			}
		} else if err := printWeekReport(out, ctx, st, rep, loc, plain, md, templatePath); err != nil {
			return err
		}
	}
	switch plan.status {
	case statusWrote:
		_, err = fmt.Fprintf(out, "wrote %s\n", plan.path)
		return err
	case statusSaved:
		_, err = fmt.Fprint(out, formatSavedLine(plan.path, colorSavedLine(plain, term)))
		return err
	default:
		return nil
	}
}

func printWeekReport(out io.Writer, ctx context.Context, st *store.Store, rep report.Report, loc *time.Location, plain, md bool, templatePath string) error {
	if styledRecap(out, plain, md, templatePath) {
		events, err := st.ListEvents(ctx)
		if err != nil {
			return err
		}
		body := report.RenderRecap(report.BuildRecap(rep, events, loc), report.RenderOptions{
			Width: terminalWidth(out),
			Color: true,
		})
		_, err = fmt.Fprint(out, body)
		return err
	}
	body, err := report.Markdown(rep, templatePath)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(out, body)
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

type weekStatus int

const (
	statusNone weekStatus = iota
	statusWrote
	statusSaved
)

// weekPlan is where one week command sends the report.
// path is the file to write. report means the recap or Markdown goes to
// stdout. status is the trailing line: wrote, saved, or none.
type weekPlan struct {
	path   string
	report bool
	status weekStatus
}

// planWeekOutput chooses the file and the terminal text.
// --out FILE and --out - win over output.mode. Without output.dir the report
// is printed. mode file keeps the previous write-only behavior. mode terminal
// prints and writes nothing. mode both writes the file and, on a terminal,
// also prints the report plus a saved line. A pipe stays on the write-only
// path so the Markdown file is not copied onto stdout. JSON has no recap, so
// both writes the file and prints the wrote line.
func planWeekOutput(outFlag, dir, mode, base, ext string, term, asJSON bool) (weekPlan, error) {
	outFlag = strings.TrimSpace(outFlag)
	switch {
	case outFlag == "-":
		return weekPlan{report: true}, nil
	case outFlag != "":
		path, err := config.ExpandPath(outFlag)
		if err != nil {
			return weekPlan{}, fmt.Errorf("output path: %w", err)
		}
		if path == "" {
			return weekPlan{}, errors.New("output path is empty")
		}
		return weekPlan{path: path, status: statusWrote}, nil
	}
	if strings.TrimSpace(dir) == "" {
		return weekPlan{report: true}, nil
	}
	path := filepath.Join(dir, base+ext)
	switch mode {
	case config.OutputModeFile:
		return weekPlan{path: path, status: statusWrote}, nil
	case config.OutputModeTerminal:
		return weekPlan{report: true}, nil
	case config.OutputModeBoth:
		if !term || asJSON {
			return weekPlan{path: path, status: statusWrote}, nil
		}
		return weekPlan{path: path, report: true, status: statusSaved}, nil
	default:
		return weekPlan{}, fmt.Errorf("invalid output.mode %s (use both, file, or terminal)", mode)
	}
}

// formatSavedLine is the dim confirmation printed after the recap.
// color false is plain text, used for NO_COLOR and --plain.
func formatSavedLine(path string, color bool) string {
	text := "saved " + shortenSavedPath(path)
	if !color {
		return text + "\n"
	}
	ren := lipgloss.NewRenderer(io.Discard)
	ren.SetColorProfile(termenv.TrueColor)
	ren.SetHasDarkBackground(true)
	return ren.NewStyle().Faint(true).Render(text) + "\n"
}

func colorSavedLine(plain, term bool) bool {
	return term && !plain && os.Getenv("NO_COLOR") == ""
}

// shortenSavedPath renders a home path as ~/…/file when directories sit
// between the home directory and the file. Other paths stay as written.
func shortenSavedPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if strings.Contains(rel, string(filepath.Separator)) {
		return filepath.Join("~", "…", filepath.Base(path))
	}
	return filepath.Join("~", rel)
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

// styledRecap reports whether stdout should be the colored recap.
// A pipe, NO_COLOR, --plain, --md, and a custom template keep the Markdown
// report so that text stays stable for anything that reads it.
func styledRecap(w io.Writer, plain, md bool, templatePath string) bool {
	return wantStyledRecap(plain, md, templatePath, os.Getenv("NO_COLOR"), detectTerminal(w))
}

// detectTerminal reports whether w is a terminal. Tests replace it.
var detectTerminal = isTerminal

func wantStyledRecap(plain, md bool, templatePath, noColor string, term bool) bool {
	if plain || md || strings.TrimSpace(templatePath) != "" {
		return false
	}
	if noColor != "" || !term {
		return false
	}
	return true
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

func terminalWidth(w io.Writer) int {
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return report.DefaultRecapWidth
	}
	width, _, err := term.GetSize(f.Fd())
	if err != nil || width <= 0 {
		return report.DefaultRecapWidth
	}
	// The border takes one column on each side of the lipgloss width.
	inner := width - 2
	if inner < 48 {
		return 48
	}
	if inner > 100 {
		return 100
	}
	return inner
}
