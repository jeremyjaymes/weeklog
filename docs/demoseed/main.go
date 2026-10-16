// Command demoseed writes a SQLite database of fictional weeklog activity.
//
// The rows are the report golden fixture: acme repositories and ticket
// GEN-341, for ISO week 2026-W41. Nothing in the database is a token or a
// personal activity log. docs/demo.tape records `weeklog week` against it.
//
// Event order matches seedGolden in internal/report/build_test.go. The work
// item links use the ids from that order.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func main() {
	dbPath := flag.String("db", "", "SQLite database to create")
	flag.Parse()
	if strings.TrimSpace(*dbPath) == "" {
		fmt.Fprintln(os.Stderr, "demoseed: -db is required")
		os.Exit(2)
	}
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "demoseed: unexpected arguments")
		os.Exit(2)
	}

	ctx := context.Background()
	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "demoseed: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = st.Close() }()

	if err := seed(ctx, st); err != nil {
		fmt.Fprintf(os.Stderr, "demoseed: %v\n", err)
		os.Exit(1)
	}
}

func seed(ctx context.Context, st *store.Store) error {
	endA := time.Date(2026, 10, 8, 15, 20, 0, 0, time.UTC)
	endB := time.Date(2026, 10, 8, 15, 40, 0, 0, time.UTC)
	endC := time.Date(2026, 10, 7, 10, 35, 0, 0, time.UTC)
	crossed := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	repoLaravel := "github.com/acme/smilepostcards-laravel"
	events := []event.Event{
		{
			Source: "linear", SourceID: "GEN-341", Kind: event.KindTicket,
			TS: time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC), Title: "Admin is_admin gate",
			State: event.StateDone, Repo: repoLaravel,
		},
		{
			Source: "github", SourceID: "acme/smilepostcards-laravel#5", Kind: event.KindPR,
			TS: time.Date(2026, 10, 8, 16, 0, 0, 0, time.UTC), Title: "Admin is_admin gate",
			State: event.StateMerged, URL: "https://github.com/acme/smilepostcards-laravel/pull/5",
			Repo: repoLaravel, Branch: "cursor/admin-gate", Refs: []string{"GEN-341"},
		},
		{
			Source: "git", SourceID: strings.Repeat("a", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC), Title: "Add is_admin gate",
			Repo: repoLaravel, Branch: "cursor/admin-gate",
		},
		{
			Source: "git", SourceID: strings.Repeat("b", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 8, 14, 20, 0, 0, time.UTC), Title: "Cover is_admin gate",
			Repo: repoLaravel, Branch: "cursor/admin-gate",
		},
		{
			Source: "claude-code", SourceID: "sess-1", Kind: event.KindAgentSession,
			TS: time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC), TSEnd: &endA, Title: "Admin gate",
			Repo: repoLaravel,
		},
		{
			Source: "claude-code", SourceID: "sess-2", Kind: event.KindAgentSession,
			TS: time.Date(2026, 10, 8, 15, 20, 0, 0, time.UTC), TSEnd: &endB, Title: "Admin gate tests",
			Repo: repoLaravel,
		},
		{
			Source: "github", SourceID: "acme/otto#12", Kind: event.KindPR,
			TS: time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC), Title: "Retry webhook delivery",
			State: event.StateOpen, URL: "https://github.com/acme/otto/pull/12",
			Repo: "github.com/acme/otto", Branch: "cursor/webhooks",
		},
		{
			Source: "git", SourceID: strings.Repeat("c", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC), Title: "Retry webhook delivery",
			Repo: "github.com/acme/otto", Branch: "cursor/webhooks",
		},
		{
			Source: "codex", SourceID: "roll-1", Kind: event.KindAgentSession,
			TS: time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC), TSEnd: &endC,
			Title: "Try Lob adapter spike", Repo: "github.com/acme/otto",
		},
		{
			Source: "git", SourceID: strings.Repeat("d", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC), Title: "Bump version",
			Repo: "github.com/acme/zebra", Branch: "main",
		},
		{
			Source: "git", SourceID: strings.Repeat("e", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC), Title: `Revert "ship the spike"`,
			Repo: "github.com/acme/old", Branch: "wip/old",
		},
		{
			Source: "github", SourceID: "review-1", Kind: event.KindPRReview,
			TS: time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC), Title: "LGTM",
			Repo: "github.com/acme/otto",
		},
		{
			Source: "linear", SourceID: "comment-1", Kind: event.KindComment,
			TS: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), Title: "note",
		},
		{
			Source: "linear", SourceID: "comment-2", Kind: event.KindComment,
			TS: time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC), Title: "note 2",
		},
		{
			Source: "git", SourceID: strings.Repeat("f", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 4, 23, 0, 0, 0, time.UTC), Title: "Too early",
			Repo: "github.com/acme/zebra", Branch: "main",
		},
		{
			Source: "claude-code", SourceID: "before", Kind: event.KindAgentSession,
			TS: time.Date(2026, 10, 4, 22, 0, 0, 0, time.UTC), TSEnd: &crossed,
			Title: "Crossed into the week",
		},
		{
			Source: "git", SourceID: strings.Repeat("9", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), Title: "Until exclusive",
			Repo: "github.com/acme/zebra", Branch: "main",
		},
	}
	res, err := st.UpsertEvents(ctx, events)
	if err != nil {
		return err
	}
	if len(res.IDs) != len(events) {
		return fmt.Errorf("stored %d events, wrote %d", len(events), len(res.IDs))
	}
	return st.ReplaceWorkItems(ctx, []store.WorkItemDraft{
		{
			Title: "Admin is_admin gate",
			Events: []store.WorkItemLink{
				{EventID: res.IDs[0], Reason: "ref:GEN-341 ticket"},
				{EventID: res.IDs[1], Reason: "ref:GEN-341 in title"},
				{EventID: res.IDs[2], Reason: "ref:GEN-341 in commit message"},
				{EventID: res.IDs[3], Reason: "ref:GEN-341 in commit message"},
				{EventID: res.IDs[4], Reason: "ref:GEN-341 in prompt"},
				{EventID: res.IDs[5], Reason: "ref:GEN-341 in prompt"},
			},
		},
		{
			Title: "Retry webhook delivery",
			Events: []store.WorkItemLink{
				{EventID: res.IDs[6], Reason: "branch:cursor/webhooks"},
				{EventID: res.IDs[7], Reason: "branch:cursor/webhooks"},
			},
		},
	})
}
