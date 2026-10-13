// Package collect runs configured sources for one time window and stores the
// events. A source that fails does not stop the others. A source that cannot
// collect yet is skipped. Cursors advance only after a source succeeds.
package collect

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/config"
	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/linker"
	"github.com/jeremyjaymes/weeklog/internal/redact"
	"github.com/jeremyjaymes/weeklog/internal/source"
	"github.com/jeremyjaymes/weeklog/internal/store"
	"github.com/jeremyjaymes/weeklog/internal/timeutil"
)

const auditTimeout = 5 * time.Second

var (
	// ErrConfig means the config file has errors. Summary.Text is the check report.
	ErrConfig = errors.New("config check failed")
	// ErrFailed means a source failed or linking failed. Events from sources
	// that succeeded are already stored.
	ErrFailed = errors.New("collect failed")
)

// LinkFunc regroups stored events after sources finish.
// The default is linker.Run.
type LinkFunc func(ctx context.Context, st *store.Store, since, until time.Time) error

// Options selects the window, sources, and database for one collect.
type Options struct {
	// ConfigPath is the TOML file. It must already be resolved.
	ConfigPath string
	// DBPath is the SQLite file. The parent directory is created.
	DBPath string
	// Week is an ISO week, YYYY-Www. It cannot be combined with Since or Until.
	Week string
	// Since and Until are RFC3339 timestamps or YYYY-MM-DD dates.
	// They must be set together. Until is exclusive.
	Since string
	Until string
	// Sources limits collection to these configured source types.
	// Names are case-sensitive. Nil collects every configured source.
	Sources []string
	// Timeout limits each source. Zero selects the default plugin timeout.
	Timeout time.Duration
	// Now overrides the clock used for the default previous-week window.
	Now func() time.Time
	// Link overrides the linker. Nil uses linker.Run.
	Link LinkFunc
}

// Summary is the text collect prints. Text is the collect report, or the
// config check report when the config has errors. Warnings are config
// warnings for a collect that was allowed to run.
type Summary struct {
	Text     string
	Warnings string
}

// Run collects the configured sources into DBPath.
// The context is the process signal context. Cancellation stops the current
// source and does not start another. Resolved config secrets are installed
// on the store redactor before any event is written.
//
// Run sets the registry timeout to Options.Timeout so subprocess plugins use
// the same limit as in-process sources.
func Run(ctx context.Context, reg *source.Registry, opt Options) (summary Summary, err error) {
	if ctx == nil {
		return Summary{}, errors.New("nil context")
	}
	if err = ctx.Err(); err != nil {
		return Summary{}, err
	}
	if reg == nil {
		return Summary{}, errors.New("source registry is missing")
	}
	if strings.TrimSpace(opt.DBPath) == "" {
		return Summary{}, errors.New("db path is empty")
	}
	if opt.Timeout <= 0 {
		opt.Timeout = source.DefaultTimeout
	}
	reg.SetTimeout(opt.Timeout)

	checked, err := config.Check(ctx, opt.ConfigPath)
	if err != nil {
		return Summary{}, err
	}
	if !checked.OK() {
		return Summary{Text: checked.Report()}, ErrConfig
	}
	summary.Warnings = warningText(checked.Issues)

	cfg := checked.Config
	loc, err := cfg.Identity.Location()
	if err != nil {
		return summary, err
	}
	weekStart, err := cfg.Identity.WeekStart()
	if err != nil {
		return summary, err
	}
	window, err := resolveWindow(opt, loc, weekStart)
	if err != nil {
		return summary, err
	}
	indexes, err := selectSources(cfg, opt.Sources)
	if err != nil {
		return summary, err
	}
	redactor, err := redact.New(redact.Options{
		ExtraPatterns: cfg.Redaction.ExtraPatterns,
		Allow:         cfg.Redaction.Allow,
		Secrets:       checked.SecretValues(),
	})
	if err != nil {
		return summary, err
	}

	st, err := store.Open(ctx, opt.DBPath)
	if err != nil {
		return summary, err
	}
	defer func() {
		if cerr := st.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	st.SetRedactor(redactor)

	runID, err := st.StartRun(ctx, "", window.Since, window.Until)
	if err != nil {
		return summary, err
	}

	job := collector{
		st:       st,
		reg:      reg,
		redactor: redactor,
		checked:  checked,
		identity: identityOf(cfg),
		window:   window,
		timeout:  opt.Timeout,
		now:      opt.now(),
	}

	var (
		b        strings.Builder
		okN      int
		failN    int
		skipN    int
		eventsN  int
		aborted  bool
		auditErr error
	)
	fmt.Fprintf(&b, "db: %s\n", publicText(redactor, opt.DBPath))
	fmt.Fprintf(&b, "%s\n", formatWindow(window))
	if len(indexes) == 0 {
		fmt.Fprintf(&b, "no sources configured\n")
	}

	for _, index := range indexes {
		if err = ctx.Err(); err != nil {
			aborted = true
			break
		}
		outcome := job.one(ctx, index)
		if outcome.recordErr != nil && auditErr == nil {
			auditErr = outcome.recordErr
		}
		writeOutcome(&b, outcome)
		switch outcome.status {
		case statusSkipped:
			skipN++
		case statusFailed:
			failN++
			eventsN += outcome.events
		case statusCanceled:
			aborted = true
		default:
			okN++
			eventsN += outcome.events
		}
		if outcome.abort {
			aborted = true
			break
		}
	}

	var linkErr error
	if !aborted {
		linkErr = job.link(ctx, opt.Link)
		switch {
		case ctx.Err() != nil || errors.Is(linkErr, context.Canceled):
			aborted = true
		case linkErr != nil:
			fmt.Fprintf(&b, "link: failed: %s\n", publicText(redactor, linkErr.Error()))
		default:
			fmt.Fprintf(&b, "link: ok\n")
		}
	}
	fmt.Fprintf(&b, "%s\n", countsLine(eventsN, failN, skipN, aborted))
	summary.Text = b.String()

	status := overallStatus(okN, failN, linkErr, aborted)
	if ferr := finishRun(runID, st, status, summary.Text); ferr != nil && auditErr == nil {
		auditErr = ferr
	}
	switch {
	case aborted:
		if ctx.Err() != nil {
			err = ctx.Err()
		} else {
			err = context.Canceled
		}
	case auditErr != nil && (failN > 0 || linkErr != nil):
		err = fmt.Errorf("%w: %v", ErrFailed, auditErr)
	case auditErr != nil:
		err = fmt.Errorf("record collect run: %w", auditErr)
	case failN > 0 || linkErr != nil:
		err = ErrFailed
	}
	return summary, err
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func resolveWindow(opt Options, loc *time.Location, weekStart time.Weekday) (timeutil.Window, error) {
	week := strings.TrimSpace(opt.Week)
	since := strings.TrimSpace(opt.Since)
	until := strings.TrimSpace(opt.Until)
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
		return timeutil.PreviousWeek(opt.now().In(loc), weekStart), nil
	}
}

func selectSources(cfg *config.Config, names []string) ([]int, error) {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("source name is empty")
		}
		wanted[name] = struct{}{}
	}
	var indexes []int
	seen := make(map[string]struct{}, len(cfg.Sources))
	for i := range cfg.Sources {
		typ := cfg.Sources[i].Type
		if len(wanted) > 0 {
			if _, ok := wanted[typ]; !ok {
				continue
			}
		}
		indexes = append(indexes, i)
		seen[typ] = struct{}{}
	}
	var missing []string
	for name := range wanted {
		if _, ok := seen[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return indexes, nil
	}
	sort.Strings(missing)
	if len(missing) == 1 {
		return nil, fmt.Errorf("source %s is not configured", missing[0])
	}
	return nil, fmt.Errorf("sources %s are not configured", strings.Join(missing, ", "))
}

func identityOf(cfg *config.Config) source.Identity {
	id := source.Identity{
		Emails:  append([]string(nil), cfg.Identity.Emails...),
		Handles: map[string]string{},
	}
	if github := strings.TrimSpace(cfg.Identity.GitHub); github != "" {
		id.Handles["github"] = github
	}
	return id
}

func warningText(issues []config.Issue) string {
	var b strings.Builder
	for _, issue := range issues {
		if issue.Severity != config.SeverityWarning {
			continue
		}
		fmt.Fprintf(&b, "warning: %s: %s\n", oneLine(issue.Field), oneLine(issue.Message))
	}
	return b.String()
}

func formatWindow(window timeutil.Window) string {
	since := window.Since.Format(time.RFC3339)
	until := window.Until.Format(time.RFC3339)
	if window.Label != "" {
		return fmt.Sprintf("window: %s (%s .. %s)", window.Label, since, until)
	}
	return fmt.Sprintf("window: %s .. %s", since, until)
}

const (
	statusOK       = "ok"
	statusFailed   = "failed"
	statusSkipped  = "skipped"
	statusCanceled = "canceled"
	statusPartial  = "partial"
)

type collector struct {
	st       *store.Store
	reg      *source.Registry
	redactor *redact.Redactor
	checked  config.CheckResult
	identity source.Identity
	window   timeutil.Window
	timeout  time.Duration
	now      time.Time
}

type outcome struct {
	label     string
	status    string
	detail    string
	events    int
	dropped   int
	logs      []source.LogLine
	abort     bool
	recordErr error
}

func (c collector) one(ctx context.Context, index int) outcome {
	srcCfg := c.checked.Config.Sources[index]
	label := publicText(c.redactor, fmt.Sprintf("source[%d] %s", index, srcCfg.Type))
	key := fmt.Sprintf("%s#%d", srcCfg.Type, index)
	out := outcome{label: label}

	if err := ctx.Err(); err != nil {
		out.status = statusCanceled
		out.abort = true
		out.recordErr = c.record(key, statusCanceled, label+": canceled")
		return out
	}

	state, err := c.st.SourceState(ctx, key)
	if err != nil {
		if ctx.Err() != nil {
			out.status = statusCanceled
			out.abort = true
			out.recordErr = c.record(key, statusCanceled, label+": canceled")
			return out
		}
		out.status = statusFailed
		out.detail = publicText(c.redactor, err.Error())
		out.recordErr = c.record(key, statusFailed, label+": failed ("+out.detail+")")
		return out
	}

	src, err := c.reg.Open(srcCfg.Type)
	if err != nil {
		out.status = statusFailed
		out.detail = publicText(c.redactor, err.Error())
		out.recordErr = c.record(key, statusFailed, label+": failed ("+out.detail+")")
		return out
	}

	var cursor *string
	if state.Present {
		saved := state.Cursor
		cursor = &saved
	}
	pluginCfg, ok := c.checked.PluginConfig(index)
	if !ok {
		pluginCfg = map[string]any{}
	}
	req := source.CollectRequest{
		Protocol: source.ProtocolVersion,
		Since:    c.window.Since,
		Until:    c.window.Until,
		Cursor:   cursor,
		Config:   pluginCfg,
		Identity: c.identity,
	}

	sctx, cancel := context.WithTimeout(ctx, c.timeout)
	var events []event.Event
	result, collectErr := src.Collect(sctx, req, func(ev event.Event) error {
		if err := sctx.Err(); err != nil {
			return err
		}
		events = append(events, ev)
		return nil
	})
	parentErr := ctx.Err()
	timedOut := errors.Is(sctx.Err(), context.DeadlineExceeded)
	cancel()

	out.logs = redactLogs(c.redactor, result.Logs)
	out.dropped = result.Dropped
	if parentErr != nil {
		out.status = statusCanceled
		out.abort = true
		out.recordErr = c.record(key, statusCanceled, label+": canceled")
		return out
	}
	if collectErr != nil {
		if errors.Is(collectErr, source.ErrNotImplemented) {
			out.status = statusSkipped
			out.detail = source.ErrNotImplemented.Error()
			out.recordErr = c.record(key, statusSkipped, label+": skipped ("+out.detail+")")
			return out
		}
		if timedOut && errors.Is(collectErr, context.DeadlineExceeded) {
			collectErr = fmt.Errorf("timed out after %s", c.timeout)
		}
		out.status = statusFailed
		out.detail = publicText(c.redactor, collectErr.Error())
		if result.Dropped > 0 {
			out.detail = fmt.Sprintf("%s (%d dropped)", out.detail, result.Dropped)
		}
		out.recordErr = c.record(key, statusFailed, label+": failed ("+out.detail+")")
		return out
	}

	if len(events) > 0 {
		if _, err := c.st.UpsertEvents(ctx, events); err != nil {
			if ctx.Err() != nil {
				out.status = statusCanceled
				out.abort = true
				out.recordErr = c.record(key, statusCanceled, label+": canceled")
				return out
			}
			out.status = statusFailed
			out.detail = publicText(c.redactor, err.Error())
			out.recordErr = c.record(key, statusFailed, label+": failed ("+out.detail+")")
			return out
		}
	}
	out.events = len(events)

	var saved *string
	if result.HasCursor {
		value := result.Cursor
		saved = &value
	}
	if err := c.saveCursor(key, saved); err != nil {
		out.status = statusFailed
		out.detail = publicText(c.redactor, err.Error())
		out.recordErr = c.record(key, statusFailed, label+": failed ("+out.detail+")")
		return out
	}

	out.status = statusOK
	out.recordErr = c.record(key, statusOK, label+": ok ("+eventDetail(out.events, out.dropped)+")")
	return out
}

func (c collector) link(ctx context.Context, fn LinkFunc) error {
	if fn != nil {
		return fn(ctx, c.st, c.window.Since, c.window.Until)
	}
	patterns := linker.TicketPatterns(c.checked.Config)
	opt := linker.Options{}
	if len(patterns) > 0 {
		opt.TicketPatterns = patterns
	}
	return linker.RunWith(ctx, c.st, c.window.Since, c.window.Until, opt)
}

func (c collector) saveCursor(key string, cursor *string) error {
	ctx, cancel := context.WithTimeout(context.Background(), auditTimeout)
	defer cancel()
	return c.st.SaveCursor(ctx, key, cursor, c.now)
}

func (c collector) record(key, status, summary string) error {
	ctx, cancel := context.WithTimeout(context.Background(), auditTimeout)
	defer cancel()
	_, err := c.st.RecordRun(ctx, store.Run{
		StartedAt:  c.now,
		FinishedAt: c.now,
		Since:      c.window.Since,
		Until:      c.window.Until,
		Source:     key,
		Status:     status,
		Summary:    summary,
	})
	return err
}

func finishRun(id int64, st *store.Store, status, summary string) error {
	ctx, cancel := context.WithTimeout(context.Background(), auditTimeout)
	defer cancel()
	return st.FinishRun(ctx, id, status, summary)
}
