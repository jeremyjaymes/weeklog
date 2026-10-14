package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func TestOverridesRoundTrip(t *testing.T) {
	s := openMem(t)
	res, err := s.UpsertEvents(context.Background(), []event.Event{planEvent(), commitEvent("aaa1111")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddOverride(context.Background(), OverrideLink, res.IDs[0], res.IDs[0]); err == nil {
		t.Fatal("expected a self-link to fail")
	}
	if _, err := s.AddOverride(context.Background(), OverrideLink, res.IDs[0], 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing event err = %v", err)
	}
	if _, err := s.AddOverride(context.Background(), "nope", res.IDs[0], res.IDs[1]); err == nil {
		t.Fatal("expected an unknown kind to fail")
	}
	if _, err := s.AddOverride(context.Background(), OverrideUnlink, res.IDs[0], res.IDs[1]); err == nil {
		t.Fatal("expected unlink with two events to fail")
	}
	listed, err := s.ListOverrides(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("failed inserts stored %d rows", len(listed))
	}

	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return start }
	link, err := s.AddOverride(context.Background(), OverrideLink, res.IDs[1], res.IDs[0])
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return start }
	unlink, err := s.AddOverride(context.Background(), OverrideUnlink, res.IDs[0], 0)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return start.Add(time.Second) }
	again, err := s.AddOverride(context.Background(), OverrideLink, res.IDs[0], res.IDs[1])
	if err != nil {
		t.Fatal(err)
	}

	listed, err = s.ListOverrides(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Fatalf("overrides = %+v", listed)
	}
	if listed[0].ID != link.ID || listed[1].ID != unlink.ID || listed[2].ID != again.ID {
		t.Fatalf("order = %+v", listed)
	}
	if listed[0].Kind != OverrideLink || listed[0].EventA != res.IDs[1] || listed[0].EventB != res.IDs[0] {
		t.Fatalf("link = %+v", listed[0])
	}
	if !listed[0].CreatedAt.Equal(start) {
		t.Fatalf("created = %s", listed[0].CreatedAt)
	}
	if listed[1].Kind != OverrideUnlink || listed[1].EventA != res.IDs[0] || listed[1].EventB != 0 {
		t.Fatalf("unlink = %+v", listed[1])
	}
	if !listed[2].CreatedAt.Equal(start.Add(time.Second)) {
		t.Fatalf("later = %s", listed[2].CreatedAt)
	}

	if err := s.ReplaceWorkItems(context.Background(), []WorkItemDraft{{
		Title:  "kept",
		Events: []WorkItemLink{{EventID: res.IDs[0], Reason: "session only"}},
	}}); err != nil {
		t.Fatal(err)
	}
	listed, err = s.ListOverrides(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Fatalf("replace work items dropped overrides: %+v", listed)
	}

	if _, err := s.db.Exec(`DELETE FROM events WHERE id = ?`, res.IDs[0]); err != nil {
		t.Fatal(err)
	}
	listed, err = s.ListOverrides(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("delete event left overrides: %+v", listed)
	}
}

func TestGetEventByID(t *testing.T) {
	s := openMem(t)
	res, err := s.UpsertEvents(context.Background(), []event.Event{planEvent()})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEventByID(context.Background(), res.IDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceID != planEvent().SourceID || len(got.Files) != 1 || len(got.Refs) != 3 {
		t.Fatalf("event = %+v", got)
	}
	if _, err := s.GetEventByID(context.Background(), 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("zero id err = %v", err)
	}
	if _, err := s.GetEventByID(context.Background(), res.IDs[0]+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestAddOverrideRejectsCanceledContext(t *testing.T) {
	s := openMem(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.AddOverride(ctx, OverrideUnlink, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.ListOverrides(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("list err = %v", err)
	}
}

func TestUpgradeFromV1AddsOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (1, '2026-10-13T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range migrations[0].stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	version, err := s.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}
	if SchemaVersion != 2 {
		t.Fatalf("SchemaVersion = %d, want 2", SchemaVersion)
	}
	if !tableNames(t, s)["overrides"] {
		t.Fatal("upgraded database has no overrides table")
	}
	cols := columnNames(t, s, "overrides")
	want := "id,kind,event_a_id,event_b_id,created_at"
	if joinComma(cols) != want {
		t.Fatalf("columns = %v", cols)
	}
	if got := countQuery(t, s, `SELECT COUNT(*) FROM schema_migrations`); got != 2 {
		t.Fatalf("migration rows = %d", got)
	}

	res, err := s.UpsertEvents(context.Background(), []event.Event{planEvent(), commitEvent("aaa1111")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddOverride(context.Background(), OverrideLink, res.IDs[0], res.IDs[1]); err != nil {
		t.Fatal(err)
	}
}

func joinComma(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += ","
		}
		out += part
	}
	return out
}
