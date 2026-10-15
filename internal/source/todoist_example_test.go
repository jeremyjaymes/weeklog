package source_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/collect"
	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

const todoistToken = "tdst-9f3a"

func TestTodoistExample(t *testing.T) {
	githubToken := "ghp_" + strings.Repeat("A", 36)
	bin := installTodoistExample(t)
	var hits todoistHits
	srv := httptest.NewServer(todoistMock(&hits, githubToken))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("WEEKLOG_TODOIST_TOKEN", todoistToken)

	reg := source.NewRegistry()
	reg.SetPath(filepath.Dir(bin))
	reg.SetPluginDir(t.TempDir())
	reg.SetTimeout(15 * time.Second)

	cfgPath := writeTodoistConfig(t, srv.URL)
	report, err := reg.Doctor(context.Background(), source.DoctorOptions{
		ConfigPath: cfgPath,
		Configured: []source.Configured{{Index: 0, Name: "todoist"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() || !strings.Contains(report.Text(), "source todoist: ok (plugin, protocol 1, version 0.1.0, "+bin+")") {
		t.Fatalf("doctor:\n%s", report.Text())
	}

	dbPath := filepath.Join(t.TempDir(), "weeklog.db")
	opt := collect.Options{
		ConfigPath: cfgPath,
		DBPath:     dbPath,
		Week:       "2026-W41",
		Timeout:    15 * time.Second,
	}
	first := runTodoistCollect(t, reg, opt)
	if first.Warnings != "" {
		t.Fatalf("warnings = %q", first.Warnings)
	}
	for _, want := range []string{
		"source[0] todoist: warn: skipped 2 tasks outside the window",
		"source[0] todoist: info: collected 1 completed task",
		"source[0] todoist: ok (1 event)",
		"link: ok",
	} {
		if !strings.Contains(first.Text, want) {
			t.Fatalf("summary missing %q\n%s", want, first.Text)
		}
	}
	assertNoSecrets(t, "summary", first.Text, githubToken)

	st := openTodoistDB(t, dbPath)
	if n := mustCount(t, st); n != 1 {
		t.Fatalf("count after first collect = %d", n)
	}
	secretEvent := mustTodoistEvent(t, st, "task-1")
	if secretEvent.Title != "Fix [REDACTED:github-token] retries" {
		t.Fatalf("title = %q", secretEvent.Title)
	}
	if secretEvent.Summary != "remember [REDACTED:config-secret] for the standup" {
		t.Fatalf("summary = %q", secretEvent.Summary)
	}
	if secretEvent.Kind != event.KindTicket || secretEvent.State != event.StateDone {
		t.Fatalf("event = %#v", secretEvent.Event)
	}
	if secretEvent.URL != "https://app.todoist.com/app/task/task-1" {
		t.Fatalf("url = %q", secretEvent.URL)
	}
	wantTS := time.Date(2026, 10, 8, 14, 2, 11, 0, time.UTC)
	if !secretEvent.TS.Equal(wantTS) {
		t.Fatalf("ts = %s", secretEvent.TS)
	}
	if _, err := st.GetEvent(context.Background(), "todoist", "task-old"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("out-of-window event err = %v", err)
	}
	state, err := st.SourceState(context.Background(), "todoist#0")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Present || state.Cursor != "page-2" {
		t.Fatalf("cursor = %#v", state)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	second := runTodoistCollect(t, reg, opt)
	for _, want := range []string{
		"source[0] todoist: info: collected 1 completed task",
		"source[0] todoist: ok (1 event)",
		"link: ok",
	} {
		if !strings.Contains(second.Text, want) {
			t.Fatalf("second summary missing %q\n%s", want, second.Text)
		}
	}
	if strings.Contains(second.Text, "outside the window") {
		t.Fatalf("second summary skipped tasks\n%s", second.Text)
	}
	assertNoSecrets(t, "second summary", second.Text, githubToken)

	st = openTodoistDB(t, dbPath)
	if n := mustCount(t, st); n != 2 {
		t.Fatalf("count after second collect = %d", n)
	}
	clean := mustTodoistEvent(t, st, "task-2")
	if clean.Title != "Ship the weekly log" || clean.Summary != "plain notes" || clean.State != event.StateDone {
		t.Fatalf("clean event = %#v", clean.Event)
	}
	if !clean.TS.Equal(time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("clean ts = %s", clean.TS)
	}
	state, err = st.SourceState(context.Background(), "todoist#0")
	if err != nil {
		t.Fatal(err)
	}
	if state.Cursor != "page-2" {
		t.Fatalf("cursor after second collect = %q", state.Cursor)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	third := runTodoistCollect(t, reg, opt)
	if !strings.Contains(third.Text, "source[0] todoist: ok (1 event)") {
		t.Fatalf("third summary:\n%s", third.Text)
	}
	st = openTodoistDB(t, dbPath)
	if n := mustCount(t, st); n != 2 {
		t.Fatalf("duplicate collect count = %d", n)
	}
	again := mustTodoistEvent(t, st, "task-1")
	if again.Title != secretEvent.Title || again.Summary != secretEvent.Summary {
		t.Fatalf("redacted event changed: %#v", again.Event)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	raw := readDBFiles(t, dbPath)
	assertNoSecrets(t, "sqlite", raw, githubToken)
	if !strings.Contains(raw, "[REDACTED:github-token]") || !strings.Contains(raw, "[REDACTED:config-secret]") {
		t.Fatal("sqlite file is missing redaction markers")
	}
	if strings.Contains(raw, "Too early") || strings.Contains(raw, "On the boundary") {
		t.Fatal("sqlite file stored a task outside the window")
	}

	got := hits.snapshot()
	if len(got) != 3 {
		t.Fatalf("requests = %#v", got)
	}
	for i, hit := range got {
		if hit.path != "/api/v1/tasks/completed/by_completion_date" {
			t.Fatalf("request %d path = %s", i, hit.path)
		}
		if hit.auth != "Bearer "+todoistToken {
			t.Fatalf("request %d auth = %q", i, hit.auth)
		}
		if strings.Contains(hit.rawQuery, todoistToken) || strings.Contains(hit.rawQuery, githubToken) {
			t.Fatalf("request %d query leaked a secret: %s", i, hit.rawQuery)
		}
		if hit.since != "2026-10-05T00:00:00Z" || hit.until != "2026-10-12T00:00:00Z" {
			t.Fatalf("request %d window = %s .. %s", i, hit.since, hit.until)
		}
	}
	if got[0].cursor != "" || got[1].cursor != "page-2" || got[2].cursor != "page-2" {
		t.Fatalf("cursors = %#v", got)
	}
}

func TestTodoistExampleRejectsBadConfig(t *testing.T) {
	bin := installTodoistExample(t)
	proc := &source.Process{Name: "todoist", Path: bin, Timeout: 5 * time.Second}
	_, err := proc.Collect(context.Background(), source.CollectRequest{
		Since:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Config: map[string]any{},
	}, nil)
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "token is required") {
		t.Fatalf("missing token: %v", err)
	}

	_, err = proc.Collect(context.Background(), source.CollectRequest{
		Since:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Config: map[string]any{"token": todoistToken, "api_base": 12},
	}, nil)
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "api_base must be a string") {
		t.Fatalf("bad api_base: %v", err)
	}
	if strings.Contains(err.Error(), todoistToken) || strings.Contains(err.Error(), "[REDACTED:") {
		t.Fatalf("bad api_base leaked the token: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, todoistToken, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	_, err = proc.Collect(context.Background(), source.CollectRequest{
		Since:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Config: map[string]any{"token": todoistToken, "api_base": srv.URL},
	}, nil)
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "todoist rejected the token") {
		t.Fatalf("rejected token: %v", err)
	}
	if strings.Contains(err.Error(), todoistToken) || strings.Contains(err.Error(), "[REDACTED:") {
		t.Fatalf("rejection leaked the token: %v", err)
	}
}

func installTodoistExample(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatal("python3 is required")
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "examples", "weeklog-source-todoist", "weeklog-source-todoist"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, source.PluginPrefix+"todoist")
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTodoistConfig(t *testing.T, apiBase string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[identity]
emails = ["ada@example.com"]
github = "ada"
timezone = "UTC"
week_starts = "monday"

[output]
dir = "` + filepath.ToSlash(filepath.Join(dir, "out")) + `"

[[source]]
type = "todoist"
token = "env:WEEKLOG_TODOIST_TOKEN"
api_base = "` + apiBase + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runTodoistCollect(t *testing.T, reg *source.Registry, opt collect.Options) collect.Summary {
	t.Helper()
	summary, err := collect.Run(context.Background(), reg, opt)
	if err != nil {
		t.Fatalf("collect: %v\n%s", err, summary.Text)
	}
	return summary
}

func openTodoistDB(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func mustCount(t *testing.T, st *store.Store) int {
	t.Helper()
	n, err := st.CountEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func mustTodoistEvent(t *testing.T, st *store.Store, id string) store.Record {
	t.Helper()
	rec, err := st.GetEvent(context.Background(), "todoist", id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Source != "todoist" || rec.SourceID != id {
		t.Fatalf("event = %#v", rec.Event)
	}
	return rec
}

func assertNoSecrets(t *testing.T, where, text, githubToken string) {
	t.Helper()
	if strings.Contains(text, todoistToken) || strings.Contains(text, githubToken) || strings.Contains(text, "ghp_") {
		t.Fatalf("%s contains a secret", where)
	}
}

func readDBFiles(t *testing.T, path string) string {
	t.Helper()
	var b strings.Builder
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

type todoistHit struct {
	path     string
	auth     string
	rawQuery string
	since    string
	until    string
	cursor   string
}

type todoistHits struct {
	mu   sync.Mutex
	hits []todoistHit
}

func (h *todoistHits) add(hit todoistHit) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hits = append(h.hits, hit)
}

func (h *todoistHits) snapshot() []todoistHit {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]todoistHit, len(h.hits))
	copy(out, h.hits)
	return out
}

func todoistMock(hits *todoistHits, githubToken string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		hit := todoistHit{
			path:     r.URL.Path,
			auth:     r.Header.Get("Authorization"),
			rawQuery: r.URL.RawQuery,
			since:    query.Get("since"),
			until:    query.Get("until"),
			cursor:   query.Get("cursor"),
		}
		hits.add(hit)
		if r.Method != http.MethodGet || hit.auth != "Bearer "+todoistToken {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var payload any
		switch hit.cursor {
		case "":
			payload = map[string]any{
				"items": []map[string]string{
					{
						"id":           "task-1",
						"content":      "Fix " + githubToken + " retries",
						"description":  "remember " + todoistToken + " for the standup",
						"completed_at": "2026-10-08T14:02:11Z",
						"url":          "https://app.todoist.com/app/task/task-1",
					},
					{
						"id":           "task-old",
						"content":      "Too early",
						"completed_at": "2026-10-04T12:00:00Z",
					},
					{
						"id":           "task-until",
						"content":      "On the boundary",
						"completed_at": "2026-10-12T00:00:00Z",
					},
				},
				"next_cursor": "page-2",
			}
		case "page-2":
			payload = map[string]any{
				"items": []map[string]string{
					{
						"id":           "task-2",
						"content":      "Ship the weekly log",
						"description":  "plain notes",
						"completed_at": "2026-10-09T09:00:00Z",
					},
				},
				"next_cursor": nil,
			}
		default:
			http.Error(w, "unexpected cursor", http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			http.Error(w, "encode", http.StatusInternalServerError)
		}
	})
}
