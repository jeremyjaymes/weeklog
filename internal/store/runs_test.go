package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunsAndCursors(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	since := time.Date(2026, 10, 5, 5, 0, 0, 0, time.UTC)
	until := time.Date(2026, 10, 12, 5, 0, 0, 0, time.UTC)
	started := time.Date(2026, 10, 13, 15, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return started }

	id, err := s.StartRun(ctx, "", since, until)
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Fatalf("id = %d", id)
	}
	if err := s.FinishRun(ctx, id, "ok", "collected"); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(ctx, 99, "ok", "missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing finish err = %v", err)
	}
	if _, err := s.RecordRun(ctx, Run{Status: "  "}); err == nil {
		t.Fatal("expected an empty status to fail")
	}

	cursor := "v1"
	at := time.Date(2026, 10, 13, 16, 0, 0, 0, time.UTC)
	if err := s.SaveCursor(ctx, "git#0", &cursor, at); err != nil {
		t.Fatal(err)
	}
	empty := ""
	if err := s.SaveCursor(ctx, "plug#1", &empty, at); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCursor(ctx, "git#0", nil, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCursor(ctx, "missing#2", nil, at); err != nil {
		t.Fatal(err)
	}

	git, err := s.SourceState(ctx, "git#0")
	if err != nil {
		t.Fatal(err)
	}
	if !git.Present || git.Cursor != "v1" || !git.LastRun.Equal(at.Add(time.Hour)) {
		t.Fatalf("git state = %+v", git)
	}
	plug, err := s.SourceState(ctx, "plug#1")
	if err != nil {
		t.Fatal(err)
	}
	if !plug.Present || plug.Cursor != "" {
		t.Fatalf("empty cursor = %+v", plug)
	}
	missing, err := s.SourceState(ctx, "missing#2")
	if err != nil {
		t.Fatal(err)
	}
	if missing.Present {
		t.Fatalf("missing state = %+v", missing)
	}

	next := "v2"
	if err := s.SaveCursor(ctx, "git#0", &next, at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	git, err = s.SourceState(ctx, "git#0")
	if err != nil {
		t.Fatal(err)
	}
	if git.Cursor != "v2" || !git.LastRun.Equal(at.Add(2*time.Hour)) {
		t.Fatalf("replaced cursor = %+v", git)
	}
	if n := countQuery(t, s, `SELECT COUNT(*) FROM source_state`); n != 2 {
		t.Fatalf("source_state rows = %d", n)
	}

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
	run := runs[0]
	if run.Status != "ok" || run.Summary != "collected" || run.Source != "" {
		t.Fatalf("run = %+v", run)
	}
	if !run.Since.Equal(since) || !run.Until.Equal(until) || run.FinishedAt.IsZero() {
		t.Fatalf("run times = %+v", run)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ListRuns(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list err = %v", err)
	}
	if err := s.SaveCursor(canceled, "git#0", &next, at); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled save err = %v", err)
	}
}
