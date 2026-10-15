package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
	"github.com/jeremyjaymes/weeklog/internal/sources/claudecode"
	"github.com/jeremyjaymes/weeklog/internal/sources/codex"
	"github.com/jeremyjaymes/weeklog/internal/sources/git"
	"github.com/jeremyjaymes/weeklog/internal/sources/github"
	"github.com/jeremyjaymes/weeklog/internal/sources/linear"
)

// TestBuiltinProtocolParity runs every built-in source in process and through
// the subprocess adapter against the same fixtures.
//
// The adapter is source.Process. It executes this test binary, which TestMain
// serves as the named built-in when WEEKLOG_SERVE_BUILTIN is set. A shell
// wrapper sets that variable because the adapter does not pass it through.
//
// describe, the collect event stream, log lines, and the cursor are compared.
//
// Normalization before comparison, and only this:
//   - Event timestamps are converted to UTC and the monotonic clock is removed
//     with time.Round(0). The protocol carries RFC3339 timestamps. Those have
//     an offset and no monotonic reading, so a location pointer or a monotonic
//     clock is not part of the event stream.
//   - A nil event slice and an empty one compare equal. The protocol emits no
//     event line in either case. The same rule applies to log lines.
//
// Result.Dropped is not compared. The in-process collector counts unreadable
// input records. The adapter counts invalid protocol lines, and that count
// must be zero: a built-in speaks valid JSON lines. The protocol has no field
// for the collector's own drop count. The log lines carry it, and those lines
// are compared.
//
// Fixtures, with no network access:
//   - git uses a temporary repository plus a missing root for the warning.
//   - claude-code and codex use the golden session files under testdata,
//     copied to $HOME/.claude and $HOME/.codex. CLAUDE_CONFIG_DIR and
//     CODEX_HOME are cleared because the adapter does not forward them.
//   - github and linear replay testdata over an httptest server. The collect
//     config key "endpoint" carries that loopback URL. It is not a describe
//     field. Collect honors it only for loopback http.
func TestBuiltinProtocolParity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	copyTree(t, testdataPath(t, "claude"), filepath.Join(home, ".claude"))
	copyTree(t, testdataPath(t, "codex"), filepath.Join(home, ".codex"))

	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	base := source.CollectRequest{
		Protocol: source.ProtocolVersion,
		Since:    since,
		Until:    until,
		Identity: source.Identity{
			Emails:  []string{"bob@example.com"},
			Handles: map[string]string{"github": "alice"},
		},
	}

	t.Run("git", func(t *testing.T) {
		assertDescribe(t, "git", git.New())
		root, missing := buildParityRepo(t)
		req := base
		req.Config = map[string]any{
			"roots":   []string{root, missing},
			"authors": []string{"Alice@Example.com"},
		}
		events, result := assertCollect(t, "git", git.New(), req)
		if len(events) != 2 || events[0].Title != "Start GEN-100" || events[1].Title != "Handle retries #12" {
			t.Fatalf("events = %#v", titlesOf(events))
		}
		if events[1].Branch != "feature" || events[1].Repo != "github.com/acme/otto" {
			t.Fatalf("feature commit = %#v", events[1])
		}
		if result.HasCursor || result.Dropped != 0 || !logEqual(result.Logs, "root not found: "+missing) {
			t.Fatalf("result = %#v", result)
		}
		saved := `prev"quote`
		req.Cursor = &saved
		again, againResult := assertCollect(t, "git", git.New(), req)
		if canonicalEvents(t, again) != canonicalEvents(t, events) || againResult.HasCursor {
			t.Fatal("a cursor changed the git collect")
		}
	})

	t.Run("claude-code", func(t *testing.T) {
		assertDescribe(t, "claude-code", claudecode.New())
		events, result := assertCollect(t, "claude-code", claudecode.New(), base)
		if !hasID(events, "11111111-1111-4111-8111-111111111111") || len(events) < 2 {
			t.Fatalf("events = %#v", titlesOf(events))
		}
		if !result.HasCursor || result.Cursor == "" || result.Dropped != 4 {
			t.Fatalf("result dropped=%d cursor=%q", result.Dropped, result.Cursor)
		}
		for _, msg := range []string{
			"skipped 4 unreadable session lines",
			"skipped 1 session with no timestamp",
		} {
			if !logEqual(result.Logs, msg) {
				t.Fatalf("logs = %#v", result.Logs)
			}
		}
		req := base
		req.Cursor = &result.Cursor
		again, againResult := assertCollect(t, "claude-code", claudecode.New(), req)
		if canonicalEvents(t, again) != canonicalEvents(t, events) || !againResult.HasCursor {
			t.Fatal("cursor round trip changed the claude-code collect")
		}
		bad := "{"
		req.Cursor = &bad
		badEvents, badResult := assertCollect(t, "claude-code", claudecode.New(), req)
		if canonicalEvents(t, badEvents) != canonicalEvents(t, events) || !logEqual(badResult.Logs, "ignored unreadable cursor") {
			t.Fatalf("bad cursor logs = %#v", badResult.Logs)
		}
	})

	t.Run("codex", func(t *testing.T) {
		assertDescribe(t, "codex", codex.New())
		events, result := assertCollect(t, "codex", codex.New(), base)
		if !hasID(events, "11111111-1111-4111-8111-111111111111") || len(events) < 2 {
			t.Fatalf("events = %#v", titlesOf(events))
		}
		if !result.HasCursor || result.Cursor == "" || result.Dropped != 5 {
			t.Fatalf("result dropped=%d cursor=%q", result.Dropped, result.Cursor)
		}
		for _, msg := range []string{
			"skipped 5 unreadable session lines",
			"skipped 1 session with no timestamp",
		} {
			if !logEqual(result.Logs, msg) {
				t.Fatalf("logs = %#v", result.Logs)
			}
		}
		req := base
		req.Cursor = &result.Cursor
		again, againResult := assertCollect(t, "codex", codex.New(), req)
		if canonicalEvents(t, again) != canonicalEvents(t, events) || !againResult.HasCursor {
			t.Fatal("cursor round trip changed the codex collect")
		}
		bad := "{"
		req.Cursor = &bad
		badEvents, badResult := assertCollect(t, "codex", codex.New(), req)
		if canonicalEvents(t, badEvents) != canonicalEvents(t, events) || !logEqual(badResult.Logs, "ignored unreadable cursor") {
			t.Fatalf("bad cursor logs = %#v", badResult.Logs)
		}
	})

	t.Run("github", func(t *testing.T) {
		assertDescribe(t, "github", github.New())
		srv := githubFixtureServer(t)
		req := base
		req.Config = map[string]any{"token": "fixture-token", "endpoint": srv.URL}
		events, result := assertCollect(t, "github", github.New(), req)
		if !hasID(events, "acme/otto#12") || len(events) < 2 {
			t.Fatalf("events = %#v", idsOf(events))
		}
		if result.HasCursor || result.Dropped != 0 || !logEqual(result.Logs, "skipped 1 malformed pull request") {
			t.Fatalf("result = %#v", result)
		}
		saved := "github-page"
		req.Cursor = &saved
		again, againResult := assertCollect(t, "github", github.New(), req)
		if canonicalEvents(t, again) != canonicalEvents(t, events) || againResult.HasCursor {
			t.Fatal("a cursor changed the github collect")
		}
	})

	t.Run("linear", func(t *testing.T) {
		assertDescribe(t, "linear", linear.New())
		srv := linearFixtureServer(t)
		req := base
		req.Config = map[string]any{
			"token":          "fixture-token",
			"ticket_pattern": `GEN-\d+`,
			"endpoint":       srv.URL,
		}
		events, result := assertCollect(t, "linear", linear.New(), req)
		if !hasID(events, "GEN-301") || len(events) < 2 {
			t.Fatalf("events = %#v", idsOf(events))
		}
		if result.HasCursor || result.Dropped != 0 || !logEqual(result.Logs, "skipped 1 malformed issue") {
			t.Fatalf("result = %#v", result)
		}
		saved := "linear-page"
		req.Cursor = &saved
		again, againResult := assertCollect(t, "linear", linear.New(), req)
		if canonicalEvents(t, again) != canonicalEvents(t, events) || againResult.HasCursor {
			t.Fatal("a cursor changed the linear collect")
		}
	})
}

func assertDescribe(t *testing.T, name string, src source.Source) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	direct, err := src.Describe(ctx)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if direct.Name != name || direct.Protocol != source.ProtocolVersion || direct.Version == "" {
		t.Fatalf("manifest = %#v", direct)
	}
	proc := &source.Process{Name: name, Path: builtinPlugin(t, name), Timeout: 30 * time.Second}
	got, err := proc.Describe(ctx)
	if err != nil {
		t.Fatalf("subprocess describe: %v", err)
	}
	if !reflect.DeepEqual(direct, got) {
		t.Fatalf("describe mismatch\nin-process: %#v\nsubprocess: %#v", direct, got)
	}
}

// assertCollect compares one in-process collect with one subprocess collect.
// It returns the in-process events and result.
func assertCollect(t *testing.T, name string, src source.Source, req source.CollectRequest) ([]event.Event, source.Result) {
	t.Helper()
	inEvents, inResult := collectDirect(t, src, req)
	outEvents, outResult := collectProcess(t, name, req)
	inJSON := canonicalEvents(t, inEvents)
	outJSON := canonicalEvents(t, outEvents)
	if inJSON != outJSON {
		t.Fatalf("event stream mismatch\nin-process:\n%s\nsubprocess:\n%s", inJSON, outJSON)
	}
	if !reflect.DeepEqual(normalizeLogs(inResult.Logs), normalizeLogs(outResult.Logs)) {
		t.Fatalf("log mismatch\nin-process: %#v\nsubprocess: %#v", inResult.Logs, outResult.Logs)
	}
	if inResult.HasCursor != outResult.HasCursor || inResult.Cursor != outResult.Cursor {
		t.Fatalf("cursor mismatch\nin-process: has=%v %q\nsubprocess: has=%v %q",
			inResult.HasCursor, inResult.Cursor, outResult.HasCursor, outResult.Cursor)
	}
	if inResult.Events != outResult.Events {
		t.Fatalf("event count in-process=%d subprocess=%d", inResult.Events, outResult.Events)
	}
	return inEvents, inResult
}

func collectDirect(t *testing.T, src source.Source, req source.CollectRequest) ([]event.Event, source.Result) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var events []event.Event
	result, err := src.Collect(ctx, req, func(ev event.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if result.Events != len(events) {
		t.Fatalf("Events = %d, emitted %d", result.Events, len(events))
	}
	return events, result
}

func collectProcess(t *testing.T, name string, req source.CollectRequest) ([]event.Event, source.Result) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	proc := &source.Process{Name: name, Path: builtinPlugin(t, name), Timeout: 30 * time.Second}
	var events []event.Event
	result, err := proc.Collect(ctx, req, func(ev event.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("subprocess collect: %v", err)
	}
	if result.Dropped != 0 {
		t.Fatalf("adapter dropped %d protocol lines", result.Dropped)
	}
	if result.Events != len(events) {
		t.Fatalf("Events = %d, emitted %d", result.Events, len(events))
	}
	return events, result
}

// builtinPlugin is a weeklog-source-<name> wrapper around this test binary.
// The adapter's environment omits WEEKLOG_SERVE_BUILTIN, so the wrapper sets
// it and then execs the binary with the same arguments.
func builtinPlugin(t *testing.T, name string) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), source.PluginPrefix+name)
	script := "#!/bin/sh\nset -eu\nexport WEEKLOG_SERVE_BUILTIN=" + shellQuote(name) + "\nexec " + shellQuote(bin) + " \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// normalizeParityEvent makes the protocol timestamp comparable.
// See TestBuiltinProtocolParity for the field list.
func normalizeParityEvent(ev event.Event) event.Event {
	ev.TS = ev.TS.UTC().Round(0)
	if ev.TSEnd != nil {
		end := ev.TSEnd.UTC().Round(0)
		ev.TSEnd = &end
	}
	return ev
}

func canonicalEvents(t *testing.T, events []event.Event) string {
	t.Helper()
	if events == nil {
		events = []event.Event{}
	}
	norm := make([]event.Event, len(events))
	for i, ev := range events {
		norm[i] = normalizeParityEvent(ev)
	}
	buf, err := json.MarshalIndent(norm, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

func normalizeLogs(logs []source.LogLine) []source.LogLine {
	if len(logs) == 0 {
		return nil
	}
	return logs
}

func hasID(events []event.Event, id string) bool {
	for _, ev := range events {
		if ev.SourceID == id {
			return true
		}
	}
	return false
}

func logEqual(logs []source.LogLine, msg string) bool {
	for _, line := range logs {
		if line.Level == "warn" && line.Msg == msg {
			return true
		}
	}
	return false
}

func titlesOf(events []event.Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Title
	}
	return out
}

func idsOf(events []event.Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.SourceID
	}
	return out
}

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", name)
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func buildParityRepo(t *testing.T) (root, missing string) {
	t.Helper()
	root = t.TempDir()
	repo := filepath.Join(root, "org", "otto")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	parityGit(t, repo, nil, "init", "-b", "main")
	parityGit(t, repo, nil, "remote", "add", "origin", "git@github.com:acme/otto.git")

	writeRepoFile(t, filepath.Join(repo, "old.txt"), "old\n")
	parityGit(t, repo, nil, "add", "old.txt")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-09-01T12:00:00Z", "Old work")

	writeRepoFile(t, filepath.Join(repo, "README.md"), "hello\n")
	parityGit(t, repo, nil, "add", "README.md")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-10-06T12:00:00Z", "Start GEN-100")

	writeRepoFile(t, filepath.Join(repo, "bob.txt"), "bob\n")
	parityGit(t, repo, nil, "add", "bob.txt")
	commitAt(t, repo, "Bob", "bob@example.com", "2026-10-07T12:00:00Z", "Bob only")

	parityGit(t, repo, nil, "checkout", "-b", "feature")
	writeRepoFile(t, filepath.Join(repo, "app", "retry.go"), "retry\n")
	parityGit(t, repo, nil, "add", "app/retry.go")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-10-08T10:00:00Z", "Handle retries #12\n\nFixes GEN-301")

	return root, filepath.Join(root, "missing")
}

func commitAt(t *testing.T, dir, name, email, date, message string) {
	t.Helper()
	parityGit(t, dir, []string{
		"GIT_AUTHOR_NAME=" + name,
		"GIT_AUTHOR_EMAIL=" + email,
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=" + name,
		"GIT_COMMITTER_EMAIL=" + email,
		"GIT_COMMITTER_DATE=" + date,
	}, "commit", "-m", message)
}

func writeRepoFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func parityGit(t *testing.T, dir string, extra []string, args ...string) {
	t.Helper()
	base := []string{
		"-c", "user.name=Alice",
		"-c", "user.email=alice@example.com",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
		"-c", "core.autocrlf=false",
	}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Dir = dir
	env := append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
	)
	cmd.Env = lastEnv(append(env, extra...))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// lastEnv keeps the last value of each variable. Git reads the first
// occurrence, so a later override has to replace the earlier one.
func lastEnv(all []string) []string {
	last := make(map[string]int, len(all))
	for i, item := range all {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			last[key] = i
		}
	}
	out := make([]string, 0, len(all))
	for i, item := range all {
		key, _, ok := strings.Cut(item, "=")
		if !ok || last[key] == i {
			out = append(out, item)
		}
	}
	return out
}

func githubFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := testdataPath(t, "github")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		var payload struct {
			Query     string `json:"query"`
			Variables struct {
				Q      string  `json:"q"`
				Cursor *string `json:"cursor"`
			} `json:"variables"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "json", http.StatusBadRequest)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		cursor := ""
		if payload.Variables.Cursor != nil {
			cursor = *payload.Variables.Cursor
		}
		name := githubFixtureName(payload.Query, payload.Variables.Q, cursor)
		if name == "" {
			http.Error(w, "no fixture", http.StatusInternalServerError)
			return
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			http.Error(w, "fixture", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func githubFixtureName(query, q, cursor string) string {
	switch {
	case strings.Contains(query, "viewer"):
		return "viewer.json"
	case strings.Contains(query, "repository(owner:"):
		if strings.Contains(query, "reviews(first:") {
			return "reviews-otto-12.json"
		}
		return "commits-otto-12.json"
	case strings.Contains(q, "is:pr") && strings.Contains(q, "author:"):
		if cursor == "" {
			return "search-pr-author-1.json"
		}
		return "search-pr-author-2.json"
	case strings.Contains(q, "is:pr") && strings.Contains(q, "reviewed-by:"):
		return "search-pr-reviewed.json"
	case strings.Contains(q, "is:issue") && strings.Contains(q, "author:"):
		return "search-issue-author.json"
	case strings.Contains(q, "is:issue") && strings.Contains(q, "assignee:"):
		return "search-issue-assigned.json"
	default:
		return ""
	}
}

func linearFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := testdataPath(t, "linear")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		var payload struct {
			Query     string `json:"query"`
			Variables struct {
				Cursor *string `json:"cursor"`
			} `json:"variables"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "json", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		cursor := ""
		if payload.Variables.Cursor != nil {
			cursor = *payload.Variables.Cursor
		}
		name := linearFixtureName(payload.Query, cursor)
		if name == "" {
			http.Error(w, "no fixture", http.StatusInternalServerError)
			return
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			http.Error(w, "fixture", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func linearFixtureName(query, cursor string) string {
	switch {
	case strings.Contains(query, "issue(id:"):
		return "history-gen-301.json"
	case cursor == "":
		return "issues-1.json"
	case cursor == "issues-2":
		return "issues-2.json"
	default:
		return ""
	}
}
