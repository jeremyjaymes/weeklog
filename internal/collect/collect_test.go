package collect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

const testSecret = "weeklog-test-secret"

func TestCollectPartialFailureSkipsStubsAndRedacts(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "weeklog.db")
	alpha := &scripted{
		name: "alpha",
		events: []event.Event{{
			Source:   "alpha",
			SourceID: "a1",
			Kind:     event.KindComment,
			TS:       time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
			Title:    "saw " + testSecret,
		}},
		logs:      []source.LogLine{{Level: "warn", Msg: "token " + testSecret}},
		cursor:    "cur-1",
		hasCursor: true,
	}
	beta := &scripted{
		name: "beta",
		err:  errors.New("boom " + testSecret),
		logs: []source.LogLine{{Level: "error", Msg: "stderr " + testSecret}},
	}
	reg := newRegistry(t, map[string]source.Source{
		"alpha": alpha,
		"beta":  beta,
		"codex": stubSource{Unimplemented: source.Unimplemented{Name: "codex"}},
	})
	var linked int
	summary, err := Run(context.Background(), reg, Options{
		ConfigPath: writeConfig(t, dir, baseConfig(`
[[source]]
type = "alpha"
token = "`+testSecret+`"

[[source]]
type = "beta"

[[source]]
type = "codex"
`)),
		DBPath:  dbPath,
		Since:   "2026-10-05",
		Until:   "2026-10-12",
		Timeout: time.Second,
		Link: func(ctx context.Context, st *store.Store, since, until time.Time) error {
			linked++
			if !until.After(since) {
				t.Fatalf("window %s .. %s", since, until)
			}
			n, err := st.CountEvents(ctx)
			if err != nil {
				return err
			}
			if n != 1 {
				t.Fatalf("events at link = %d", n)
			}
			return nil
		},
	})
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v\n%s", err, summary.Text)
	}
	if linked != 1 {
		t.Fatalf("linker calls = %d", linked)
	}
	assertNoSecret(t, summary.Text+"\n"+summary.Warnings)
	for _, want := range []string{
		"source[0] alpha: warn: token [REDACTED:config-secret]",
		"source[0] alpha: ok (1 event)",
		"source[1] beta: error: stderr [REDACTED:config-secret]",
		"source[1] beta: failed (boom [REDACTED:config-secret])",
		"source[2] codex: skipped (collector is not implemented)",
		"link: ok",
		"1 event, 1 source failed, 1 source skipped",
	} {
		if !strings.Contains(summary.Text, want) {
			t.Fatalf("summary missing %q\n%s", want, summary.Text)
		}
	}
	if !strings.Contains(summary.Warnings, "warning: source[0].token: literal secret") {
		t.Fatalf("warnings = %s", summary.Warnings)
	}
	if alpha.calls != 1 || beta.calls != 1 {
		t.Fatalf("calls alpha=%d beta=%d", alpha.calls, beta.calls)
	}
	if alpha.configs[0]["token"] != testSecret {
		t.Fatalf("plugin config = %#v", alpha.configs[0])
	}
	if alpha.identityOK() != nil {
		t.Fatal(alpha.identityOK())
	}

	st := reopen(t, dbPath)
	rec, err := st.GetEvent(context.Background(), "alpha", "a1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Title, testSecret) || !strings.Contains(rec.Title, "[REDACTED:config-secret]") {
		t.Fatalf("title = %q", rec.Title)
	}
	state, err := st.SourceState(context.Background(), "alpha#0")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Present || state.Cursor != "cur-1" {
		t.Fatalf("alpha cursor = %+v", state)
	}
	betaState, err := st.SourceState(context.Background(), "beta#1")
	if err != nil {
		t.Fatal(err)
	}
	if betaState.Present {
		t.Fatalf("failed source saved a cursor: %+v", betaState)
	}
	runs, err := st.ListRuns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 4 {
		t.Fatalf("runs = %d", len(runs))
	}
	if runs[0].Source != "" || runs[0].Status != "partial" || runs[0].FinishedAt.IsZero() {
		t.Fatalf("overall run = %+v", runs[0])
	}
	for _, run := range runs {
		assertNoSecret(t, run.Summary)
	}
	if !strings.Contains(runs[0].Summary, "source[0] alpha: ok") {
		t.Fatalf("overall summary = %s", runs[0].Summary)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		raw, err := os.ReadFile(dbPath + suffix)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		if strings.Contains(string(raw), testSecret) {
			t.Fatalf("database file %s contains the config secret", suffix)
		}
	}
}

func TestCollectCursorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "weeklog.db")
	alpha := &scripted{
		name:      "alpha",
		cursor:    "cur-1",
		hasCursor: true,
		events: []event.Event{{
			Source:   "alpha",
			SourceID: "a1",
			Kind:     event.KindComment,
			TS:       time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
			Title:    "once",
		}},
	}
	reg := newRegistry(t, map[string]source.Source{"alpha": alpha})
	opt := Options{
		ConfigPath: writeConfig(t, dir, baseConfig("[[source]]\ntype = \"alpha\"\n")),
		DBPath:     dbPath,
		Week:       "2026-W41",
		Timeout:    time.Second,
		Link:       func(context.Context, *store.Store, time.Time, time.Time) error { return nil },
	}
	if _, err := Run(context.Background(), reg, opt); err != nil {
		t.Fatal(err)
	}
	alpha.events = nil
	alpha.cursor = "cur-2"
	if _, err := Run(context.Background(), reg, opt); err != nil {
		t.Fatal(err)
	}
	if alpha.calls != 2 || alpha.cursors[0] != nil || alpha.cursors[1] == nil || *alpha.cursors[1] != "cur-1" {
		t.Fatalf("cursors = %#v", alpha.cursors)
	}
	st := reopen(t, dbPath)
	state, err := st.SourceState(context.Background(), "alpha#0")
	if err != nil {
		t.Fatal(err)
	}
	if state.Cursor != "cur-2" {
		t.Fatalf("cursor = %q", state.Cursor)
	}
	n, err := st.CountEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("events = %d", n)
	}
}

func TestCollectKeepsSourcesThatDoNotSendACursor(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "weeklog.db")
	alpha := &scripted{name: "alpha"}
	reg := newRegistry(t, map[string]source.Source{"alpha": alpha})
	opt := Options{
		ConfigPath: writeConfig(t, dir, baseConfig("[[source]]\ntype = \"alpha\"\n")),
		DBPath:     dbPath,
		Since:      "2026-10-05T00:00:00Z",
		Until:      "2026-10-12T00:00:00Z",
		Timeout:    time.Second,
	}
	for i := 0; i < 2; i++ {
		summary, err := Run(context.Background(), reg, opt)
		if err != nil {
			t.Fatalf("run %d: %v\n%s", i, err, summary.Text)
		}
	}
	if alpha.cursors[0] != nil || alpha.cursors[1] != nil {
		t.Fatalf("cursors = %#v", alpha.cursors)
	}
	st := reopen(t, dbPath)
	state, err := st.SourceState(context.Background(), "alpha#0")
	if err != nil {
		t.Fatal(err)
	}
	if state.Present {
		t.Fatalf("state = %+v", state)
	}
}

func TestCollectSeparateCursorsForDuplicateTypes(t *testing.T) {
	dir := t.TempDir()
	alpha := &scripted{name: "alpha", cursor: "shared", hasCursor: true}
	reg := newRegistry(t, map[string]source.Source{"alpha": alpha})
	summary, err := Run(context.Background(), reg, Options{
		ConfigPath: writeConfig(t, dir, baseConfig("[[source]]\ntype = \"alpha\"\n[[source]]\ntype = \"alpha\"\n")),
		DBPath:     filepath.Join(dir, "weeklog.db"),
		Since:      "2026-10-05",
		Until:      "2026-10-12",
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, summary.Text)
	}
	if alpha.calls != 2 || alpha.cursors[0] != nil || alpha.cursors[1] != nil {
		t.Fatalf("cursors = %#v calls=%d", alpha.cursors, alpha.calls)
	}
}

func TestCollectWindowAndFilters(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "weeklog.db")
	alpha := &scripted{name: "alpha"}
	reg := newRegistry(t, map[string]source.Source{
		"alpha": alpha,
		"codex": stubSource{Unimplemented: source.Unimplemented{Name: "codex"}},
	})
	cfg := writeConfig(t, dir, `
[identity]
timezone = "UTC"
week_starts = "monday"
emails = ["ada@example.com"]
github = "ada"

[output]
dir = "/tmp/weeklog-out"

[[source]]
type = "alpha"

[[source]]
type = "codex"
`)
	summary, err := Run(context.Background(), reg, Options{
		ConfigPath: cfg,
		DBPath:     dbPath,
		Now:        func() time.Time { return time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC) },
		Sources:    []string{"codex"},
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, summary.Text)
	}
	if alpha.calls != 0 {
		t.Fatalf("filtered source ran")
	}
	if !strings.Contains(summary.Text, "window: 2026-10-05T00:00:00Z .. 2026-10-12T00:00:00Z") {
		t.Fatalf("summary = %s", summary.Text)
	}
	if !strings.Contains(summary.Text, "source[1] codex: skipped") || strings.Contains(summary.Text, "alpha") {
		t.Fatalf("summary = %s", summary.Text)
	}

	if _, err := Run(context.Background(), reg, Options{
		ConfigPath: cfg,
		DBPath:     dbPath,
		Week:       "2026-W41",
		Since:      "2026-10-05",
		Timeout:    time.Second,
	}); err == nil || !strings.Contains(err.Error(), "pass either --week or --since") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Run(context.Background(), reg, Options{
		ConfigPath: cfg,
		DBPath:     dbPath,
		Since:      "2026-10-05",
		Timeout:    time.Second,
	}); err == nil || !strings.Contains(err.Error(), "must be set together") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Run(context.Background(), reg, Options{
		ConfigPath: cfg,
		DBPath:     dbPath,
		Sources:    []string{"nope"},
		Timeout:    time.Second,
	}); err == nil || !strings.Contains(err.Error(), "source nope is not configured") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Run(context.Background(), reg, Options{
		ConfigPath: cfg,
		DBPath:     dbPath,
		Sources:    []string{" "},
		Timeout:    time.Second,
	}); err == nil || !strings.Contains(err.Error(), "source name is empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectISOWeekUsesConfiguredTimezone(t *testing.T) {
	dir := t.TempDir()
	alpha := &scripted{name: "alpha"}
	reg := newRegistry(t, map[string]source.Source{"alpha": alpha})
	summary, err := Run(context.Background(), reg, Options{
		ConfigPath: writeConfig(t, dir, `
[identity]
timezone = "America/Chicago"
week_starts = "monday"

[output]
dir = "/tmp/weeklog-out"

[[source]]
type = "alpha"
`),
		DBPath:  filepath.Join(dir, "weeklog.db"),
		Week:    "2026-W41",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, summary.Text)
	}
	want := "window: 2026-W41 (2026-10-05T00:00:00-05:00 .. 2026-10-12T00:00:00-05:00)"
	if !strings.Contains(summary.Text, want) {
		t.Fatalf("summary = %s", summary.Text)
	}
	if alpha.calls != 1 {
		t.Fatal("source did not run")
	}
	if !alpha.since.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, mustLoc(t, "America/Chicago"))) {
		t.Fatalf("since = %s", alpha.since)
	}
}

func TestCollectConfigErrorDoesNotOpenTheDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "weeklog.db")
	summary, err := Run(context.Background(), source.NewRegistry(), Options{
		ConfigPath: writeConfig(t, dir, `
[identity]
timezone = "UTC"

[[source]]
type = "linear"
token = "env:WEEKLOG_MISSING_LINEAR"
`),
		DBPath:  dbPath,
		Timeout: time.Second,
	})
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(summary.Text, "error:") {
		t.Fatalf("summary = %s", summary.Text)
	}
	assertNoSecret(t, summary.Text)
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatal("config failure created the database")
	}
}

func TestCollectNoSourcesStillLinks(t *testing.T) {
	dir := t.TempDir()
	var linked int
	summary, err := Run(context.Background(), source.NewRegistry(), Options{
		ConfigPath: writeConfig(t, dir, baseConfig("")),
		DBPath:     filepath.Join(dir, "weeklog.db"),
		Since:      "2026-10-05",
		Until:      "2026-10-12",
		Timeout:    time.Second,
		Link: func(context.Context, *store.Store, time.Time, time.Time) error {
			linked++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, summary.Text)
	}
	if linked != 1 || !strings.Contains(summary.Text, "no sources configured") || !strings.Contains(summary.Text, "link: ok") {
		t.Fatalf("linked=%d\n%s", linked, summary.Text)
	}
}

func TestCollectLinkerFailureKeepsEvents(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "weeklog.db")
	alpha := &scripted{
		name:      "alpha",
		cursor:    "cur-1",
		hasCursor: true,
		events: []event.Event{{
			Source: "alpha", SourceID: "a1", Kind: event.KindComment,
			TS: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), Title: "kept",
		}},
	}
	reg := newRegistry(t, map[string]source.Source{"alpha": alpha})
	summary, err := Run(context.Background(), reg, Options{
		ConfigPath: writeConfig(t, dir, baseConfig("[[source]]\ntype = \"alpha\"\n")),
		DBPath:     dbPath,
		Since:      "2026-10-05",
		Until:      "2026-10-12",
		Timeout:    time.Second,
		Link: func(context.Context, *store.Store, time.Time, time.Time) error {
			return errors.New("link broke")
		},
	})
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(summary.Text, "source[0] alpha: ok (1 event)") || !strings.Contains(summary.Text, "link: failed: link broke") {
		t.Fatalf("summary = %s", summary.Text)
	}
	st := reopen(t, dbPath)
	n, err := st.CountEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("events = %d", n)
	}
	state, err := st.SourceState(context.Background(), "alpha#0")
	if err != nil {
		t.Fatal(err)
	}
	if state.Cursor != "cur-1" {
		t.Fatalf("cursor = %+v", state)
	}
}

func TestCollectTimeoutContinues(t *testing.T) {
	dir := t.TempDir()
	alpha := &scripted{name: "alpha", block: make(chan struct{})}
	beta := &scripted{name: "beta", events: []event.Event{{
		Source: "beta", SourceID: "b1", Kind: event.KindComment,
		TS: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), Title: "after timeout",
	}}}
	reg := newRegistry(t, map[string]source.Source{"alpha": alpha, "beta": beta})
	summary, err := runWithin(t, 5*time.Second, reg, Options{
		ConfigPath: writeConfig(t, dir, baseConfig("[[source]]\ntype = \"alpha\"\n[[source]]\ntype = \"beta\"\n")),
		DBPath:     filepath.Join(dir, "weeklog.db"),
		Since:      "2026-10-05",
		Until:      "2026-10-12",
		Timeout:    50 * time.Millisecond,
	})
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v\n%s", err, summary.Text)
	}
	if beta.calls != 1 || !strings.Contains(summary.Text, "timed out after 50ms") || !strings.Contains(summary.Text, "source[1] beta: ok (1 event)") {
		t.Fatalf("calls=%d\n%s", beta.calls, summary.Text)
	}
}

func TestCollectCancelStopsLaterSources(t *testing.T) {
	dir := t.TempDir()
	started := make(chan struct{})
	alpha := &scripted{name: "alpha", started: started, block: make(chan struct{})}
	beta := &scripted{name: "beta"}
	reg := newRegistry(t, map[string]source.Source{"alpha": alpha, "beta": beta})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-started:
			cancel()
		case <-time.After(5 * time.Second):
			cancel()
		}
	}()
	summary, err := runWithin(t, 5*time.Second, reg, Options{
		ConfigPath: writeConfig(t, dir, baseConfig("[[source]]\ntype = \"alpha\"\n[[source]]\ntype = \"beta\"\n")),
		DBPath:     filepath.Join(dir, "weeklog.db"),
		Since:      "2026-10-05",
		Until:      "2026-10-12",
		Timeout:    time.Minute,
	}, ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v\n%s", err, summary.Text)
	}
	if beta.calls != 0 || !strings.Contains(summary.Text, "source[0] alpha: canceled") || strings.Contains(summary.Text, "link: ok") {
		t.Fatalf("beta calls=%d\n%s", beta.calls, summary.Text)
	}
}

func TestCollectRejectsNilInputs(t *testing.T) {
	if _, err := Run(context.Background(), nil, Options{DBPath: "x"}); err == nil {
		t.Fatal("expected a nil registry to fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, source.NewRegistry(), Options{DBPath: "x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

type stubSource struct {
	source.Unimplemented
}

func (s stubSource) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     s.Name,
		Version:  source.BuiltinVersion,
	}, nil
}

type scripted struct {
	name      string
	events    []event.Event
	logs      []source.LogLine
	cursor    string
	hasCursor bool
	err       error
	block     <-chan struct{}
	started   chan struct{}
	calls     int
	cursors   []*string
	configs   []map[string]any
	since     time.Time
	emails    []string
	github    string
}

func (s *scripted) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{Protocol: source.ProtocolVersion, Name: s.name, Version: "1"}, nil
}

func (s *scripted) Collect(ctx context.Context, req source.CollectRequest, emit func(event.Event) error) (source.Result, error) {
	s.calls++
	s.cursors = append(s.cursors, req.Cursor)
	s.configs = append(s.configs, req.Config)
	s.since = req.Since
	s.emails = append([]string(nil), req.Identity.Emails...)
	s.github = req.Identity.Handles["github"]
	if s.started != nil {
		close(s.started)
		s.started = nil
	}
	if s.block != nil {
		select {
		case <-ctx.Done():
			return source.Result{}, ctx.Err()
		case <-s.block:
		}
	}
	if err := ctx.Err(); err != nil {
		return source.Result{}, err
	}
	if s.err != nil {
		return source.Result{Logs: s.logs}, s.err
	}
	for _, ev := range s.events {
		if emit != nil {
			if err := emit(ev); err != nil {
				return source.Result{}, err
			}
		}
	}
	return source.Result{Logs: s.logs, Cursor: s.cursor, HasCursor: s.hasCursor}, nil
}

func (s *scripted) identityOK() error {
	if s.github != "ada" || len(s.emails) != 1 || s.emails[0] != "ada@example.com" {
		return errors.New("identity was not forwarded")
	}
	return nil
}

func newRegistry(t *testing.T, sources map[string]source.Source) *source.Registry {
	t.Helper()
	reg := source.NewRegistry()
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	// Registration order is not the collect order. Collect follows config order.
	for _, name := range names {
		if err := reg.Register(name, sources[name]); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func baseConfig(sources string) string {
	return `
[identity]
timezone = "UTC"
week_starts = "monday"
emails = ["ada@example.com"]
github = "ada"

[output]
dir = "/tmp/weeklog-out"
` + sources
}

func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func reopen(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func assertNoSecret(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(text, testSecret) {
		t.Fatalf("output contains the config secret\n%s", strings.ReplaceAll(text, testSecret, "[canary]"))
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func runWithin(t *testing.T, limit time.Duration, reg *source.Registry, opt Options, ctxs ...context.Context) (Summary, error) {
	t.Helper()
	ctx := context.Background()
	if len(ctxs) > 0 {
		ctx = ctxs[0]
	}
	type result struct {
		summary Summary
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		summary, err := Run(ctx, reg, opt)
		ch <- result{summary, err}
	}()
	select {
	case got := <-ch:
		return got.summary, got.err
	case <-time.After(limit):
		t.Fatal("collect did not return")
		return Summary{}, nil
	}
}
