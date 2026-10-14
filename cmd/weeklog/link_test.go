package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestLinkRunHelp(t *testing.T) {
	stdout, stderr, err := runWeeklog(t, "link-run", "--help")
	if err != nil {
		t.Fatalf("help: %v\n%s", err, stderr)
	}
	for _, want := range []string{"--week", "--since", "--until", "--db", "ticket_pattern", "GEN-301", "reason"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help missing %q\n%s", want, stdout)
		}
	}
}

func TestLinkRunWritesHardLinks(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := filepath.Join(home, "config.toml")
	body := `
[identity]
timezone = "UTC"
week_starts = "monday"

[[source]]
type = "linear"
ticket_pattern = "GEN-\\d+"
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(home, "weeklog.db")
	st, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.UpsertEvents(context.Background(), []event.Event{
		{
			Source: "linear", SourceID: "GEN-301", Kind: event.KindTicket,
			TS: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), Title: "Webhook retries",
		},
		{
			Source: "git", SourceID: "0123456789abcdef0123456789abcdef01234567", Kind: event.KindCommit,
			TS:    time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC),
			Title: "ship it", Summary: "ship GEN-301 and ABC-99",
		},
		{
			Source: "linear", SourceID: "ABC-99", Kind: event.KindTicket,
			TS: time.Date(2026, 10, 6, 13, 0, 0, 0, time.UTC), Title: "Other",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "link-run", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("link-run: %v\n%s\n%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	for _, want := range []string{
		"db: " + dbPath,
		"window: 2026-W41 (2026-10-05T00:00:00Z .. 2026-10-12T00:00:00Z)",
		"link: ok (2 work items)",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout)
		}
	}

	st = reopenLinkDB(t, dbPath)
	items, err := st.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Title != "Webhook retries" || items[0].Status != "" || len(items[0].Events) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Events[0].Reason != "ref:GEN-301 ticket" || items[0].Events[1].Reason != "ref:GEN-301 in commit message" {
		t.Fatalf("reasons = %+v", items[0].Events)
	}
	if items[1].Title != "Other" || len(items[1].Events) != 1 || items[1].Events[0].SourceID != "ABC-99" {
		t.Fatalf("leftover = %+v", items[1])
	}
	if items[1].Events[0].Reason != "leftover:- 2026-10-06" {
		t.Fatalf("leftover reason = %+v", items[1].Events[0])
	}
}

func TestLinkRunRejectsBadPattern(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := filepath.Join(home, "config.toml")
	body := `
[identity]
timezone = "UTC"

[[source]]
type = "linear"
ticket_pattern = "("
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(home, "weeklog.db")
	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "link-run", "--since", "2026-10-05", "--until", "2026-10-12", "--db", dbPath)
	if err == nil {
		t.Fatal("expected an invalid pattern to fail")
	}
	if !strings.Contains(err.Error(), "ticket_pattern") && !strings.Contains(stderr, "ticket_pattern") {
		t.Fatalf("err = %v\nstderr = %s\nstdout = %s", err, stderr, stdout)
	}
	if strings.Contains(stdout, "link: ok") {
		t.Fatalf("stdout = %s", stdout)
	}
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatalf("invalid pattern created the database: %v", statErr)
	}
}

func reopenLinkDB(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
