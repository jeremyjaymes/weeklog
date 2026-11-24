package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

func TestDescribe(t *testing.T) {
	manifest, err := New().Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "claude-code" || manifest.Version != Version || manifest.Protocol != source.ProtocolVersion {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Config) != 0 {
		t.Fatalf("config = %#v", manifest.Config)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Describe(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Describe err = %v", err)
	}
}

func TestCollectGolden(t *testing.T) {
	dir := fixtureRoot(t)
	got, result := collectDir(t, dir, oct5, oct12, nil)
	if result.Dropped != 4 {
		t.Fatalf("Dropped = %d, logs = %#v", result.Dropped, result.Logs)
	}
	if !logContains(result.Logs, "skipped 4 unreadable session lines") {
		t.Fatalf("logs = %#v", result.Logs)
	}
	if !logContains(result.Logs, "skipped 1 session with no timestamp") {
		t.Fatalf("logs = %#v", result.Logs)
	}
	if !result.HasCursor {
		t.Fatal("missing cursor")
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "SHOULD NOT APPEAR") {
		t.Fatalf("scanned a file outside projects/*/*.jsonl: %s", blob)
	}
	if strings.Contains(string(blob), "sidechain prompt should not win") || strings.Contains(string(blob), "Look at the retry helper") {
		t.Fatalf("sub-agent prompt replaced the session: %s", blob)
	}
	if strings.Contains(string(blob), "A generated summary") || strings.Contains(string(blob), "README.md") {
		t.Fatalf("unexpected text in events: %s", blob)
	}
	if strings.Contains(string(blob), "Old September") || strings.Contains(string(blob), "22222222") {
		t.Fatalf("session outside the window was emitted: %s", blob)
	}

	normalized := normalizeEvents(dir, got)
	want := readGolden(t, filepath.Join(dir, "golden", "events.json"))
	if canonical(t, normalized) != canonical(t, want) {
		t.Fatalf("events:\n%s\nwant:\n%s", canonical(t, normalized), canonical(t, want))
	}

	again, againResult := collectDir(t, dir, oct5, oct12, &result.Cursor)
	if canonical(t, normalizeEvents(dir, again)) != canonical(t, want) {
		t.Fatal("in-window files were not reread")
	}
	if againResult.Dropped != 2 {
		t.Fatalf("second Dropped = %d, want the in-window malformed lines only", againResult.Dropped)
	}
}

func TestCursorSkipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, fixtureRoot(t), dir)
	_, first := collectDir(t, dir, oct5, oct12, nil)

	quietSince := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	quietUntil := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	quiet, quietResult := collectDir(t, dir, quietSince, quietUntil, &first.Cursor)
	if len(quiet) != 0 || quietResult.Dropped != 0 {
		t.Fatalf("unchanged files outside the window were read: events=%d dropped=%d", len(quiet), quietResult.Dropped)
	}

	septPath := filepath.Join(dir, "projects", "-Users-me-Projects-acme-app", "22222222-2222-4222-8222-222222222222.jsonl")
	f, err := os.OpenFile(septPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, changed := collectDir(t, dir, oct5, oct12, &first.Cursor)
	if changed.Dropped != 3 {
		t.Fatalf("Dropped = %d, want October lines plus the changed September file", changed.Dropped)
	}

	septSince := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	septUntil := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	backfill, septResult := collectDir(t, dir, septSince, septUntil, nil)
	if len(backfill) != 1 || backfill[0].Title != "Old September prompt" {
		t.Fatalf("backfill = %#v", titles(backfill))
	}
	october, octoberResult := collectDir(t, dir, oct5, oct12, &septResult.Cursor)
	if len(october) != 3 || octoberResult.Dropped != 2 {
		t.Fatalf("october events=%v dropped=%d", titles(october), octoberResult.Dropped)
	}
}

func TestInvalidCursorRescans(t *testing.T) {
	bad := "{"
	got, result := collectDir(t, fixtureRoot(t), oct5, oct12, &bad)
	if len(got) != 3 {
		t.Fatalf("events = %d", len(got))
	}
	if !logContains(result.Logs, "ignored unreadable cursor") {
		t.Fatalf("logs = %#v", result.Logs)
	}
}

func TestWindowBounds(t *testing.T) {
	dir := t.TempDir()
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	writeSession(t, filepath.Join(dir, "projects", "proj", "at-since.jsonl"), sessionLine("at-since", since, "Included start"))
	writeSession(t, filepath.Join(dir, "projects", "proj", "inside.jsonl"), sessionLine("inside", since.Add(time.Hour), "Included middle"))
	writeSession(t, filepath.Join(dir, "projects", "proj", "at-until.jsonl"), sessionLine("at-until", until, "Excluded end"))
	writeSession(t, filepath.Join(dir, "projects", "proj", "before.jsonl"), sessionLine("before", since.Add(-time.Second), "Excluded before"))

	got, _ := collectDir(t, dir, since, until, nil)
	if strings.Join(titles(got), ",") != "Included start,Included middle" {
		t.Fatalf("titles = %v", titles(got))
	}
}

func TestLongPromptIsTruncated(t *testing.T) {
	dir := t.TempDir()
	prompt := strings.Repeat("a", 600)
	writeSession(t, filepath.Join(dir, "projects", "proj", "long.jsonl"), sessionLine("long", oct5, prompt))
	got, _ := collectDir(t, dir, oct5, oct12, nil)
	if len(got) != 1 {
		t.Fatalf("events = %d", len(got))
	}
	if utf8.RuneCountInString(got[0].Title) != maxTitleRunes {
		t.Fatalf("title runes = %d", utf8.RuneCountInString(got[0].Title))
	}
	if utf8.RuneCountInString(got[0].Summary) != event.MaxSummaryRunes {
		t.Fatalf("summary runes = %d", utf8.RuneCountInString(got[0].Summary))
	}
}

func TestSubagentWithoutParent(t *testing.T) {
	dir := t.TempDir()
	body := `{"type":"user","timestamp":"2026-10-08T10:00:00Z","cwd":"/proj","message":{"role":"user","content":"Folded sub agent"}}
{"type":"assistant","timestamp":"2026-10-08T10:05:00Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Edit","input":{"filePath":"/proj/file.go"}}]}}
`
	path := filepath.Join(dir, "projects", "-proj", "only-sub", "subagents", "agent-1.jsonl")
	writeSession(t, path, body)
	got, _ := collectDir(t, dir, oct5, oct12, nil)
	if len(got) != 1 {
		t.Fatalf("events = %#v", got)
	}
	ev := got[0]
	if ev.SourceID != "only-sub" || ev.Title != "Folded sub agent" || strings.Join(ev.Files, ",") != "file.go" {
		t.Fatalf("event = %#v", ev)
	}
	if !strings.HasSuffix(ev.BodyRef, "/subagents/agent-1.jsonl") {
		t.Fatalf("body_ref = %s", ev.BodyRef)
	}
}

func TestConfigDirSelection(t *testing.T) {
	home := t.TempDir()
	envDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeSession(t, filepath.Join(home, ".claude", "projects", "homeproj", "from-home.jsonl"), sessionLine("from-home", oct5, "From home"))
	writeSession(t, filepath.Join(envDir, "projects", "envproj", "from-env.jsonl"), sessionLine("from-env", oct5, "From env"))

	t.Setenv("CLAUDE_CONFIG_DIR", "")
	got, _ := collectDir(t, "", oct5, oct12, nil)
	if strings.Join(titles(got), ",") != "From home" {
		t.Fatalf("home titles = %v", titles(got))
	}

	t.Setenv("CLAUDE_CONFIG_DIR", envDir)
	got, _ = collectDir(t, "", oct5, oct12, nil)
	if strings.Join(titles(got), ",") != "From env" {
		t.Fatalf("env titles = %v", titles(got))
	}

	t.Setenv("CLAUDE_CONFIG_DIR", "~/claude-tilde")
	writeSession(t, filepath.Join(home, "claude-tilde", "projects", "tilde", "from-tilde.jsonl"), sessionLine("from-tilde", oct5, "From tilde"))
	got, _ = collectDir(t, "", oct5, oct12, nil)
	if strings.Join(titles(got), ",") != "From tilde" {
		t.Fatalf("tilde titles = %v", titles(got))
	}
}

func TestMissingConfigDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	got, result := collectDir(t, missing, oct5, oct12, nil)
	if len(got) != 0 || !result.HasCursor {
		t.Fatalf("events=%d cursor=%v", len(got), result.HasCursor)
	}
	if !logContains(result.Logs, "claude config dir not found: "+missing) {
		t.Fatalf("logs = %#v", result.Logs)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "projects"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, result = collectDir(t, dir, oct5, oct12, nil)
	if len(got) != 0 || !logHasPrefix(result.Logs, "projects path is not a directory:") {
		t.Fatalf("events=%d logs=%#v", len(got), result.Logs)
	}
}

func TestCollectRejectsBadWindow(t *testing.T) {
	_, err := New().Collect(context.Background(), source.CollectRequest{}, nil)
	if !errors.Is(err, source.ErrConfig) {
		t.Fatalf("err = %v", err)
	}
	_, err = New().Collect(context.Background(), source.CollectRequest{Since: oct12, Until: oct5}, nil)
	if !errors.Is(err, source.ErrConfig) {
		t.Fatalf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = New().Collect(ctx, source.CollectRequest{Since: oct5, Until: oct12}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "~nobody/claude")
	_, err = New().Collect(context.Background(), source.CollectRequest{Since: oct5, Until: oct12}, nil)
	if !errors.Is(err, source.ErrConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestEmitError(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, filepath.Join(dir, "projects", "proj", "one.jsonl"), sessionLine("one", oct5, "Hello"))
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	want := errors.New("stop")
	result, err := New().Collect(context.Background(), source.CollectRequest{Since: oct5, Until: oct12}, func(event.Event) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
	if result.HasCursor || result.Events != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func collectDir(t *testing.T, dir string, since, until time.Time, cursor *string) ([]event.Event, source.Result) {
	t.Helper()
	if dir != "" {
		t.Setenv("CLAUDE_CONFIG_DIR", dir)
	}
	var got []event.Event
	result, err := New().Collect(context.Background(), source.CollectRequest{
		Protocol: source.ProtocolVersion,
		Since:    since,
		Until:    until,
		Cursor:   cursor,
	}, func(ev event.Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if result.Events != len(got) {
		t.Fatalf("Events = %d, emitted %d", result.Events, len(got))
	}
	return got, result
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "claude")
}

func readGolden(t *testing.T, path string) []event.Event {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var events []event.Event
	if err := json.Unmarshal(data, &events); err != nil {
		t.Fatal(err)
	}
	return events
}

func normalizeEvents(dir string, events []event.Event) []event.Event {
	prefix := fileURL(dir)
	out := make([]event.Event, len(events))
	for i, ev := range events {
		ev.BodyRef = strings.TrimPrefix(ev.BodyRef, prefix)
		out[i] = ev
	}
	return out
}

func canonical(t *testing.T, events []event.Event) string {
	t.Helper()
	buf, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(buf) + "\n"
}

func sessionLine(id string, when time.Time, prompt string) string {
	payload := map[string]any{
		"type":      "user",
		"timestamp": when.UTC().Format(time.RFC3339Nano),
		"sessionId": id,
		"cwd":       "/work",
		"message": map[string]any{
			"role":    "user",
			"content": prompt,
		},
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(buf) + "\n"
}

func writeSession(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
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

func titles(events []event.Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Title
	}
	return out
}

func logContains(logs []source.LogLine, msg string) bool {
	for _, line := range logs {
		if line.Level == "warn" && line.Msg == msg {
			return true
		}
	}
	return false
}

func logHasPrefix(logs []source.LogLine, prefix string) bool {
	for _, line := range logs {
		if line.Level == "warn" && strings.HasPrefix(line.Msg, prefix) {
			return true
		}
	}
	return false
}

var (
	oct5  = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	oct12 = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
)
