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

	"github.com/jeremyjaymes/weeklog/internal/browse"
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
lane for each source, the session chain, and the top repositories and pull
requests. The chain reads like git log --graph: an AI session (Claude or
Codex), the commits it produced, and the linked pull request or issue. The
linker's reason is dimmed. A hard link uses * and |. A soft link uses o and :.
Chains that do not fit collapse to a "+N more" row. --no-graph hides the
chain. With color off, the plain recap draws the same chain and the same
marks, with no ANSI.

Each item is tagged with an actor: Human, Claude, Codex, Cloud agent, or
Unknown. The actor comes from a Co-authored-by trailer, a session linked to
a commit, the session source when the item has no commit, or an agent branch
prefix such as cursor/. A commit or pull request with no agent evidence is
Human. Missing or conflicting evidence is Unknown. The recap and the
Markdown summary count items per actor. --json includes actor and
actor_evidence on each item. See docs/ATTRIBUTION.md.

The Markdown report is printed instead when stdout is not a terminal, when
NO_COLOR is set, or when --plain or --md is set. Pipes keep that Markdown.

With --out FILE, the report is written to FILE. "-" writes to standard
output. --out ignores output.mode. --save cannot be combined with --out.

Without --out, output.mode chooses the destination. both, the default,
writes <output.dir>/<week>.md (or .json) and, when stdout is a terminal,
also prints the recap followed by a dim line naming the saved file. file
writes that file and prints where it was written. terminal prints the
report and does not write a file. When output.dir is unset, the report is
printed to standard output.

--save writes that same file when output.mode is terminal, and when
output.dir is unset. The unset path is ~/.weeklog/weeks/<week>.md. An ISO
week is named YYYY-Www, such as 2026-W41.md. --json writes <week>.json in
that same directory. On a terminal, --save still prints the recap unless
output.mode is file.

Overwrite policy: every write replaces the weekly file. An existing file
is not skipped. Text between <!-- weeklog:notes --> markers is kept.
output.template selects a text/template; a relative path is resolved from
the config file's directory. An empty template uses the built-in one.

-i, or --interactive, opens a scrollable timeline on a terminal instead of
printing or writing the report. The week is one row per day. Enter opens
that day's items, and enter again opens the commits, pull requests, and
sessions the linker attached. The stored reason is dimmed, with * for a
hard link and o for a soft link, in the same colors as the recap. j and k
move, esc or h steps back, and q or ctrl+c quits and restores the terminal.
Esc from the day list also quits. -i does not write a file and ignores
output.mode. It cannot be combined with --md, --plain, --json, --out, or
--save. --no-graph applies only to the recap.

The database is the same file weeklog collect uses. --db selects another file.
Run weeklog collect first so the week has events to render.`,
		Example: `  weeklog week
  weeklog week --week 2026-W41
  weeklog week -i
  weeklog week -i --week 2026-W41
  weeklog week --plain
  weeklog week --no-graph
  weeklog week --json
  weeklog week --save
  weeklog week --week 2026-W41 --save
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
	cmd.Flags().Bool("no-graph", false, "hide the session chain in the recap")
	cmd.Flags().BoolP("interactive", "i", false, "open a scrollable timeline instead of printing the report")
	cmd.Flags().Bool("json", false, "write JSON")
	cmd.Flags().String("out", "", "write to this file instead of output.dir or standard output")
	cmd.Flags().Bool("save", false, "write the weekly file (output.dir, or ~/.weeklog/weeks)")
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
	noGraph, err := cmd.Flags().GetBool("no-graph")
	if err != nil {
		return err
	}
	if md && asJSON {
		return errors.New("pass either --md or --json")
	}
	if plain && asJSON {
		return errors.New("pass either --plain or --json")
	}
	interactive, err := cmd.Flags().GetBool("interactive")
	if err != nil {
		return err
	}
	if interactive {
		outFlag, err := cmd.Flags().GetString("out")
		if err != nil {
			return err
		}
		save, err := cmd.Flags().GetBool("save")
		if err != nil {
			return err
		}
		switch {
		case md:
			return errors.New("pass either --interactive or --md")
		case asJSON:
			return errors.New("pass either --interactive or --json")
		case plain:
			return errors.New("pass either --interactive or --plain")
		case strings.TrimSpace(outFlag) != "":
			return errors.New("pass either --interactive or --out")
		case save:
			return errors.New("pass either --interactive or --save")
		}
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
	save, err := cmd.Flags().GetBool("save")
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
	if interactive {
		return runInteractiveWeek(cmd, ctx, rep, loc)
	}

	ext := ".md"
	if asJSON {
		ext = ".json"
	}
	out := cmd.OutOrStdout()
	term := detectTerminal(out)
	plan, err := planWeekOutput(outFlag, cfg.Output.Dir, mode, rep.FileBase(), ext, term, asJSON, save)
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
		} else if err := printWeekReport(out, ctx, st, rep, loc, plain, md, noGraph, templatePath); err != nil {
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

func runInteractiveWeek(cmd *cobra.Command, ctx context.Context, rep report.Report, loc *time.Location) error {
	out := cmd.OutOrStdout()
	if !detectTerminal(os.Stdin) || !detectTerminal(out) {
		return errors.New("interactive week browser needs a terminal")
	}
	return browse.Run(ctx, out, report.BuildBrowser(rep, loc), browse.Options{
		Color: os.Getenv("NO_COLOR") == "",
		Width: terminalWidth(out),
	})
}

func printWeekReport(out io.Writer, ctx context.Context, st *store.Store, rep report.Report, loc *time.Location, plain, md, noGraph bool, templatePath string) error {
	if styledRecap(out, plain, md, templatePath) {
		events, err := st.ListEvents(ctx)
		if err != nil {
			return err
		}
		var items []store.WorkItem
		if !noGraph {
			items, err = st.ListWorkItems(ctx)
			if err != nil {
				return err
			}
		}
		body := report.RenderRecap(report.BuildRecap(rep, events, items, loc), report.RenderOptions{
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
// --out FILE and --out - win over output.mode. --save cannot be combined
// with --out. Without output.dir the report is printed, unless --save is
// set, which writes ~/.weeklog/weeks/<week>.md. mode file writes only.
// mode terminal prints and writes nothing, unless --save is set. mode both
// writes the file and, on a terminal, also prints the report plus a saved
// line. A pipe stays on the write-only path so the Markdown file is not
// copied onto stdout. JSON has no recap, so both writes the file and prints
// the wrote line. --save with terminal mode still prints Markdown on a pipe.
func planWeekOutput(outFlag, dir, mode, base, ext string, term, asJSON, save bool) (weekPlan, error) {
	outFlag = strings.TrimSpace(outFlag)
	if save && outFlag != "" {
		return weekPlan{}, errors.New("pass either --save or --out")
	}
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
	dir = strings.TrimSpace(dir)
	if dir == "" && save {
		fallback, err := defaultWeeksDir()
		if err != nil {
			return weekPlan{}, err
		}
		dir = fallback
	}
	if dir == "" {
		return weekPlan{report: true}, nil
	}
	path := filepath.Join(dir, base+ext)
	switch mode {
	case config.OutputModeFile:
		return weekPlan{path: path, status: statusWrote}, nil
	case config.OutputModeTerminal:
		if !save {
			return weekPlan{report: true}, nil
		}
		if asJSON {
			return weekPlan{path: path, status: statusWrote}, nil
		}
		if term {
			return weekPlan{path: path, report: true, status: statusSaved}, nil
		}
		return weekPlan{path: path, report: true}, nil
	case config.OutputModeBoth:
		if !term || asJSON {
			return weekPlan{path: path, status: statusWrote}, nil
		}
		return weekPlan{path: path, report: true, status: statusSaved}, nil
	default:
		return weekPlan{}, fmt.Errorf("invalid output.mode %s (use both, file, or terminal)", mode)
	}
}

// defaultWeeksDir is where --save writes when output.dir is unset.
// The path is ~/.weeklog/weeks. output.dir stays the only directory setting.
func defaultWeeksDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("weeks directory: %w", err)
	}
	return filepath.Join(home, ".weeklog", "weeks"), nil
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
