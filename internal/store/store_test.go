package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func openMem(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func openFile(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "weeklog.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s, path
}

func planEvent() event.Event {
	end := time.Date(2026, 10, 8, 15, 40, 0, 0, time.UTC)
	return event.Event{
		Source:   "claude-code",
		SourceID: "sess_abc#3",
		Kind:     event.KindAgentSession,
		TS:       time.Date(2026, 10, 8, 14, 2, 11, 0, time.UTC),
		TSEnd:    &end,
		Actor:    "jeremyjaymes",
		Title:    "Fix Stripe webhook retries",
		Summary:  "first user prompt or the commit subject, truncated to 500 chars",
		URL:      "https://github.com/jeremyjaymes/otto/pull/12",
		Repo:     "github.com/jeremyjaymes/otto",
		Branch:   "cursor/fix-webhooks",
		CWD:      "/Users/j/Projects/otto",
		Files:    []string{"app/Webhooks.php"},
		Refs:     []string{"GEN-301", "#12", "a1b2c3d"},
		State:    event.StateMerged,
		Metrics: map[string]float64{
			"tokens_in":   1200,
			"tokens_out":  800,
			"lines_added": 40,
		},
		BodyRef: "file:///tmp/sess.jsonl",
	}
}

func commitEvent(sourceID string) event.Event {
	return event.Event{
		Source:   "git",
		SourceID: sourceID,
		Kind:     event.KindCommit,
		TS:       time.Date(2026, 10, 8, 16, 0, 0, 0, time.UTC),
		Title:    "Fix webhook retries",
		Repo:     "github.com/jeremyjaymes/otto",
		Files:    []string{"app/Webhooks.php", "tests/WebhookTest.php"},
		Refs:     []string{"GEN-301"},
	}
}

func TestSchemaVersionMatchesMigrations(t *testing.T) {
	if len(migrations) == 0 {
		t.Fatal("no migrations")
	}
	if got := migrations[len(migrations)-1].version; got != SchemaVersion {
		t.Fatalf("last migration = %d, SchemaVersion = %d", got, SchemaVersion)
	}
}

func TestOpenAppliesSchema(t *testing.T) {
	s := openMem(t)
	version, err := s.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d", version)
	}

	want := []string{
		"events",
		"event_files",
		"event_refs",
		"work_items",
		"work_item_events",
		"source_state",
		"runs",
		"overrides",
		"schema_migrations",
	}
	got := tableNames(t, s)
	for _, name := range want {
		if !got[name] {
			t.Errorf("missing table %s (have %v)", name, got)
		}
	}

	cols := columnNames(t, s, "events")
	wantCols := []string{
		"id", "source", "source_id", "kind", "ts", "ts_end", "actor", "title",
		"summary", "url", "repo", "branch", "cwd", "state", "metrics", "body_ref",
		"created_at", "updated_at",
	}
	if strings.Join(cols, ",") != strings.Join(wantCols, ",") {
		t.Fatalf("events columns = %v", cols)
	}
}

func TestOpenFilePragmasAndReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "my db.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := pragmaString(t, s, "journal_mode"); got != "wal" {
		t.Fatalf("journal_mode = %q", got)
	}
	if got := pragmaInt(t, s, "foreign_keys"); got != 1 {
		t.Fatalf("foreign_keys = %d", got)
	}
	if got := pragmaInt(t, s, "busy_timeout"); got != 5000 {
		t.Fatalf("busy_timeout = %d", got)
	}
	if _, err := s.UpsertEvents(context.Background(), []event.Event{planEvent()}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	n, err := reopened.CountEvents(context.Background())
	if err != nil {
		t.Fatalf("CountEvents: %v", err)
	}
	if n != 1 {
		t.Fatalf("count = %d", n)
	}
	version, err := reopened.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d", version)
	}
}

func TestOpenRejectsBadPaths(t *testing.T) {
	ctx := context.Background()
	if _, err := Open(ctx, ""); err == nil {
		t.Fatal("expected empty path to fail")
	}
	if _, err := Open(ctx, "   "); err == nil {
		t.Fatal("expected blank path to fail")
	}
	if _, err := Open(ctx, t.TempDir()); err == nil {
		t.Fatal("expected a directory path to fail")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Open(canceled, filepath.Join(t.TempDir(), "x.db")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Open err = %v", err)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	s, path := openFile(t)
	if _, err := s.db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (99, '2026-10-13T00:00:00.000000000Z')`); err != nil {
		t.Fatalf("insert version: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := Open(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Open err = %v", err)
	}
}

func TestMemoryDatabasesAreIsolated(t *testing.T) {
	a := openMem(t)
	b := openMem(t)
	if _, err := a.UpsertEvents(context.Background(), []event.Event{planEvent()}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	n, err := b.CountEvents(context.Background())
	if err != nil {
		t.Fatalf("CountEvents: %v", err)
	}
	if n != 0 {
		t.Fatalf("other memory database has %d events", n)
	}
}

func TestForeignKeysAndChecks(t *testing.T) {
	s := openMem(t)
	_, err := s.db.Exec(`INSERT INTO event_files (event_id, path, position) VALUES (999, 'x', 0)`)
	if err == nil {
		t.Fatal("expected foreign key failure")
	}
	_, err = s.db.Exec(`
		INSERT INTO events (source, source_id, kind, ts, state, metrics, created_at, updated_at)
		VALUES ('git', 'x', 'commit', '2026-10-08T14:00:00.000000000Z', 'nope', '{}',
			'2026-10-13T00:00:00.000000000Z', '2026-10-13T00:00:00.000000000Z')`)
	if err == nil {
		t.Fatal("expected state check failure")
	}
	_, err = s.db.Exec(`
		INSERT INTO events (source, source_id, kind, ts, ts_end, metrics, created_at, updated_at)
		VALUES ('git', 'y', 'commit', '2026-10-08T15:00:00.000000000Z', '2026-10-08T14:00:00.000000000Z', '{}',
			'2026-10-13T00:00:00.000000000Z', '2026-10-13T00:00:00.000000000Z')`)
	if err == nil {
		t.Fatal("expected ts_end check failure")
	}
	_, err = s.db.Exec(`
		INSERT INTO events (source, source_id, kind, ts, metrics, created_at, updated_at)
		VALUES ('git', 'z', 'commit', '2026-10-08T14:00:00.000000000Z', '[]',
			'2026-10-13T00:00:00.000000000Z', '2026-10-13T00:00:00.000000000Z')`)
	if err == nil {
		t.Fatal("expected metrics object check failure")
	}

	res, err := s.UpsertEvents(context.Background(), []event.Event{planEvent()})
	if err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM events WHERE id = ?`, res.IDs[0]); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := countQuery(t, s, `SELECT COUNT(*) FROM event_files`); n != 0 {
		t.Fatalf("files left after cascade = %d", n)
	}
	if n := countQuery(t, s, `SELECT COUNT(*) FROM event_refs`); n != 0 {
		t.Fatalf("refs left after cascade = %d", n)
	}
}

func TestAncillaryTables(t *testing.T) {
	s := openMem(t)
	res, err := s.UpsertEvents(context.Background(), []event.Event{planEvent()})
	if err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	const stamp = "2026-10-13T12:00:00.000000000Z"
	if _, err := s.db.Exec(`INSERT INTO work_items (title, status, created_at, updated_at) VALUES ('Admin gate', 'shipped', ?, ?)`, stamp, stamp); err != nil {
		t.Fatalf("work item: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO work_items (title, status, created_at, updated_at) VALUES ('Other', 'explored', ?, ?)`, stamp, stamp); err != nil {
		t.Fatalf("second work item: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO work_item_events (work_item_id, event_id, reason) VALUES (1, ?, 'ref:GEN-301 in branch')`, res.IDs[0]); err != nil {
		t.Fatalf("link: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO work_item_events (work_item_id, event_id, reason) VALUES (2, ?, 'files overlap: 1')`, res.IDs[0]); err == nil {
		t.Fatal("expected an event to belong to one work item")
	}
	if _, err := s.db.Exec(`INSERT INTO source_state (source, cursor, last_run_at) VALUES ('claude-code', 'cursor-1', ?)`, stamp); err != nil {
		t.Fatalf("source state: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO runs (started_at, window_since, window_until, source, status, summary) VALUES (?, ?, ?, 'claude-code', 'ok', 'collected 1')`, stamp, stamp, stamp); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM events WHERE id = ?`, res.IDs[0]); err != nil {
		t.Fatalf("delete event: %v", err)
	}
	if n := countQuery(t, s, `SELECT COUNT(*) FROM work_item_events`); n != 0 {
		t.Fatalf("links left after event delete = %d", n)
	}
	if n := countQuery(t, s, `SELECT COUNT(*) FROM work_items`); n != 2 {
		t.Fatalf("work items = %d", n)
	}
}

func TestUpsertDuplicateCollectKeepsRowCountStable(t *testing.T) {
	s := openMem(t)
	created := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return created }

	batch := []event.Event{planEvent(), commitEvent("aaa111"), commitEvent("bbb222")}
	first, err := s.UpsertEvents(context.Background(), batch)
	if err != nil {
		t.Fatalf("first UpsertEvents: %v", err)
	}
	if first.Inserted != 3 || first.Updated != 0 {
		t.Fatalf("first result = %+v", first)
	}
	if n := mustCount(t, s); n != 3 {
		t.Fatalf("count after first collect = %d", n)
	}
	files := countQuery(t, s, `SELECT COUNT(*) FROM event_files`)
	refs := countQuery(t, s, `SELECT COUNT(*) FROM event_refs`)
	if files != 5 || refs != 5 {
		t.Fatalf("files=%d refs=%d", files, refs)
	}

	s.now = func() time.Time { return created.Add(time.Minute) }
	second, err := s.UpsertEvents(context.Background(), batch)
	if err != nil {
		t.Fatalf("second UpsertEvents: %v", err)
	}
	if second.Inserted != 0 || second.Updated != 3 {
		t.Fatalf("second result = %+v", second)
	}
	if n := mustCount(t, s); n != 3 {
		t.Fatalf("count after duplicate collect = %d", n)
	}
	if got := countQuery(t, s, `SELECT COUNT(*) FROM event_files`); got != files {
		t.Fatalf("files after duplicate collect = %d, want %d", got, files)
	}
	if got := countQuery(t, s, `SELECT COUNT(*) FROM event_refs`); got != refs {
		t.Fatalf("refs after duplicate collect = %d, want %d", got, refs)
	}
	for i, id := range second.IDs {
		if id != first.IDs[i] {
			t.Fatalf("id[%d] = %d, want %d", i, id, first.IDs[i])
		}
	}

	rec, err := s.GetEvent(context.Background(), "claude-code", "sess_abc#3")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if !rec.CreatedAt.Equal(created) {
		t.Fatalf("created_at = %s", rec.CreatedAt)
	}
	if !rec.UpdatedAt.Equal(created.Add(time.Minute)) {
		t.Fatalf("updated_at = %s", rec.UpdatedAt)
	}
	if strings.Join(rec.Refs, ",") != "GEN-301,#12,a1b2c3d" {
		t.Fatalf("refs = %#v", rec.Refs)
	}
	if rec.Metrics["tokens_out"] != 800 {
		t.Fatalf("metrics = %#v", rec.Metrics)
	}

	third, err := s.UpsertEvents(context.Background(), []event.Event{commitEvent("ccc333")})
	if err != nil {
		t.Fatalf("third UpsertEvents: %v", err)
	}
	if third.Inserted != 1 || mustCount(t, s) != 4 {
		t.Fatalf("after new event inserted=%d count=%d", third.Inserted, mustCount(t, s))
	}

	other := commitEvent("aaa111")
	other.Source = "github"
	if _, err := s.UpsertEvents(context.Background(), []event.Event{other}); err != nil {
		t.Fatalf("other source: %v", err)
	}
	if mustCount(t, s) != 5 {
		t.Fatalf("same source_id on another source did not insert a row, count=%d", mustCount(t, s))
	}
}

func TestUpsertUpdatesAndReplacesChildren(t *testing.T) {
	s := openMem(t)
	original := planEvent()
	original.Kind = " AGENT_SESSION "
	original.State = "MERGED"
	original.Summary = strings.Repeat("é", event.MaxSummaryRunes+25)
	original.Files = []string{"a.go", "a.go", " b.go "}
	if _, err := s.UpsertEvents(context.Background(), []event.Event{original}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	stored, err := s.GetEvent(context.Background(), "claude-code", "sess_abc#3")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if stored.Kind != event.KindAgentSession || stored.State != event.StateMerged {
		t.Fatalf("canonical fields = kind %q state %q", stored.Kind, stored.State)
	}
	if utf8.RuneCountInString(stored.Summary) != event.MaxSummaryRunes {
		t.Fatalf("summary runes = %d", utf8.RuneCountInString(stored.Summary))
	}
	if strings.Join(stored.Files, ",") != "a.go,b.go" {
		t.Fatalf("files = %#v", stored.Files)
	}

	updated := planEvent()
	updated.Title = "Updated title"
	updated.TSEnd = nil
	updated.Files = []string{"c.go"}
	updated.Refs = nil
	updated.Metrics = nil
	updated.State = ""
	if _, err := s.UpsertEvents(context.Background(), []event.Event{updated}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if mustCount(t, s) != 1 {
		t.Fatalf("count = %d", mustCount(t, s))
	}
	rec, err := s.GetEvent(context.Background(), "claude-code", "sess_abc#3")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if rec.Title != "Updated title" || rec.Kind != event.KindAgentSession || rec.State != "" {
		t.Fatalf("record = %#v", rec.Event)
	}
	if rec.TSEnd != nil {
		t.Fatalf("ts_end = %s", rec.TSEnd)
	}
	if strings.Join(rec.Files, ",") != "c.go" {
		t.Fatalf("files = %#v", rec.Files)
	}
	if rec.Refs != nil || rec.Metrics != nil {
		t.Fatalf("refs=%#v metrics=%#v", rec.Refs, rec.Metrics)
	}
	if countQuery(t, s, `SELECT COUNT(*) FROM event_files`) != 1 {
		t.Fatal("file rows were not replaced")
	}
	if countQuery(t, s, `SELECT COUNT(*) FROM event_refs`) != 0 {
		t.Fatal("ref rows were not cleared")
	}
}

func TestUpsertBatchIsAtomic(t *testing.T) {
	s := openMem(t)
	bad := planEvent()
	bad.SourceID = "bad"
	bad.State = "nope"
	_, err := s.UpsertEvents(context.Background(), []event.Event{commitEvent("ok"), bad})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var v *event.ValidationError
	if !errors.As(err, &v) || v.Field != "state" {
		t.Fatalf("err = %v", err)
	}
	if mustCount(t, s) != 0 {
		t.Fatalf("invalid batch wrote %d rows", mustCount(t, s))
	}
	if _, err := s.UpsertEvents(context.Background(), nil); err != nil {
		t.Fatalf("empty upsert: %v", err)
	}
}

func TestUpsertSameKeyInOneBatch(t *testing.T) {
	s := openMem(t)
	first := commitEvent("same")
	first.Title = "first"
	second := commitEvent("same")
	second.Title = "second"
	second.Files = []string{"only.go"}
	res, err := s.UpsertEvents(context.Background(), []event.Event{first, second})
	if err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	if res.Inserted != 1 || res.Updated != 1 || res.IDs[0] != res.IDs[1] {
		t.Fatalf("result = %+v", res)
	}
	if mustCount(t, s) != 1 {
		t.Fatalf("count = %d", mustCount(t, s))
	}
	rec, err := s.GetEvent(context.Background(), "git", "same")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if rec.Title != "second" || strings.Join(rec.Files, ",") != "only.go" {
		t.Fatalf("record = %#v files=%#v", rec.Event, rec.Files)
	}
}

func TestListEventsOrderAndNotFound(t *testing.T) {
	s := openMem(t)
	later := commitEvent("later")
	later.TS = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	earlier := planEvent()
	if _, err := s.UpsertEvents(context.Background(), []event.Event{later, earlier}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	list, err := s.ListEvents(context.Background())
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(list) != 2 || list[0].SourceID != "sess_abc#3" || list[1].SourceID != "later" {
		t.Fatalf("order = %#v", list)
	}
	if strings.Join(list[0].Files, ",") != "app/Webhooks.php" {
		t.Fatalf("listed files = %#v", list[0].Files)
	}
	_, err = s.GetEvent(context.Background(), "git", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetEvent err = %v", err)
	}
	empty, err := openMem(t).ListEvents(context.Background())
	if err != nil {
		t.Fatalf("empty ListEvents: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty list = %#v", empty)
	}
}

func TestTimestampRoundTrip(t *testing.T) {
	s := openMem(t)
	ev := commitEvent("nano")
	cdt := time.FixedZone("CDT", -5*60*60)
	ev.TS = time.Date(2026, 10, 8, 9, 2, 11, 123456789, cdt)
	end := ev.TS.Add(time.Hour)
	ev.TSEnd = &end
	if _, err := s.UpsertEvents(context.Background(), []event.Event{ev}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	rec, err := s.GetEvent(context.Background(), "git", "nano")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if !rec.TS.Equal(ev.TS) || rec.TS.Location() != time.UTC {
		t.Fatalf("ts = %s", rec.TS)
	}
	if rec.TSEnd == nil || !rec.TSEnd.Equal(end) {
		t.Fatalf("ts_end = %v", rec.TSEnd)
	}
	stored := formatTime(rec.TS)
	if stored != "2026-10-08T14:02:11.123456789Z" {
		t.Fatalf("stored ts = %s", stored)
	}
	earlier := formatTime(rec.TS.Add(-time.Nanosecond))
	later := formatTime(rec.TS.Add(time.Second))
	if earlier >= stored || stored >= later {
		t.Fatalf("order %s %s %s", earlier, stored, later)
	}
}

func TestConcurrentUpsert(t *testing.T) {
	s := openMem(t)
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ev := commitEvent("id")
			ev.SourceID = string(rune('a' + i))
			if _, err := s.UpsertEvents(context.Background(), []event.Event{ev}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("upsert: %v", err)
	}
	if mustCount(t, s) != n {
		t.Fatalf("count = %d", mustCount(t, s))
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func mustCount(t *testing.T, s *Store) int {
	t.Helper()
	n, err := s.CountEvents(context.Background())
	if err != nil {
		t.Fatalf("CountEvents: %v", err)
	}
	return n
}

func countQuery(t *testing.T, s *Store, query string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("query %s: %v", query, err)
	}
	return n
}

func tableNames(t *testing.T, s *Store) map[string]bool {
	t.Helper()
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("sqlite_master: %v", err)
	}
	defer func() { _ = rows.Close() }()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return names
}

func columnNames(t *testing.T, s *Store, table string) []string {
	t.Helper()
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return names
}

func pragmaString(t *testing.T, s *Store, name string) string {
	t.Helper()
	var value string
	if err := s.db.QueryRow(`PRAGMA ` + name).Scan(&value); err != nil {
		t.Fatalf("pragma %s: %v", name, err)
	}
	return value
}

func pragmaInt(t *testing.T, s *Store, name string) int {
	t.Helper()
	var value int
	if err := s.db.QueryRow(`PRAGMA ` + name).Scan(&value); err != nil {
		t.Fatalf("pragma %s: %v", name, err)
	}
	return value
}
