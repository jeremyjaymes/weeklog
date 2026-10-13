package linker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestRunReadsStore(t *testing.T) {
	st := openLinkerStore(t)
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	until := since.AddDate(0, 0, 7)
	if err := Run(context.Background(), st, since, until); err != nil {
		t.Fatal(err)
	}
	items, err := st.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %d", len(items))
	}
	if err := Run(context.Background(), nil, since, until); err == nil {
		t.Fatal("expected a nil store to fail")
	}
	if err := Run(context.Background(), st, until, since); err == nil {
		t.Fatal("expected a reversed window to fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, st, since, until); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err = %v", err)
	}
}

func TestRunPersistsHardLinksAndReruns(t *testing.T) {
	st := openLinkerStore(t)
	events := []event.Event{
		{
			Source: "linear", SourceID: "GEN-301", Kind: event.KindTicket,
			TS: at(6, 9), Title: "Webhook retries",
		},
		{
			Source: "git", SourceID: fullSHA, Kind: event.KindCommit,
			TS: at(8, 10), Title: "ship it", Summary: "ship GEN-301",
			Repo: repoOtto, Branch: "main",
		},
		{
			Source: "git", SourceID: otherSHA, Kind: event.KindCommit,
			TS: at(8, 11), Title: "unrelated", Summary: "unrelated",
			Repo: repoOtto, Branch: "main",
		},
	}
	if _, err := st.UpsertEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	since := at(5, 0)
	until := at(12, 0)
	if err := Run(context.Background(), st, since, until); err != nil {
		t.Fatal(err)
	}
	first, err := st.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ID != 1 || first[0].Title != "Webhook retries" || first[0].Status != "" {
		t.Fatalf("first = %+v", first)
	}
	if len(first[0].Events) != 2 {
		t.Fatalf("members = %+v", first[0].Events)
	}
	if first[0].Events[0].SourceID != "GEN-301" || first[0].Events[0].Reason != "ref:GEN-301 ticket" {
		t.Fatalf("ticket = %+v", first[0].Events[0])
	}
	if first[0].Events[1].SourceID != fullSHA || first[0].Events[1].Reason != "ref:GEN-301 in commit message" {
		t.Fatalf("commit = %+v", first[0].Events[1])
	}

	if err := Run(context.Background(), st, since, until); err != nil {
		t.Fatal(err)
	}
	second, err := st.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].ID != first[0].ID || second[0].Title != first[0].Title {
		t.Fatalf("rerun = %+v", second)
	}
	if second[0].Events[0].Reason != first[0].Events[0].Reason || second[0].Events[1].Reason != first[0].Events[1].Reason {
		t.Fatalf("rerun reasons = %+v", second[0].Events)
	}
}

func TestRunInvalidPatternLeavesItems(t *testing.T) {
	st := openLinkerStore(t)
	if _, err := st.UpsertEvents(context.Background(), []event.Event{{
		Source: "linear", SourceID: "GEN-301", Kind: event.KindTicket,
		TS: at(6, 9), Title: "Webhook retries",
	}}); err != nil {
		t.Fatal(err)
	}
	recs, err := st.ListEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceWorkItems(context.Background(), []store.WorkItemDraft{{
		Title:  "kept",
		Events: []store.WorkItemLink{{EventID: recs[0].ID, Reason: "ref:GEN-301 ticket"}},
	}}); err != nil {
		t.Fatal(err)
	}
	err = RunWith(context.Background(), st, at(5, 0), at(12, 0), Options{TicketPatterns: []string{"("}})
	if err == nil || !strings.Contains(err.Error(), "ticket_pattern") {
		t.Fatalf("err = %v", err)
	}
	items, err := st.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "kept" {
		t.Fatalf("items = %+v", items)
	}
}

func TestRunClearsStaleHardLinks(t *testing.T) {
	st := openLinkerStore(t)
	if _, err := st.UpsertEvents(context.Background(), []event.Event{{
		Source: "git", SourceID: fullSHA, Kind: event.KindCommit,
		TS: at(8, 10), Title: "alone",
	}}); err != nil {
		t.Fatal(err)
	}
	recs, err := st.ListEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceWorkItems(context.Background(), []store.WorkItemDraft{{
		Title:  "stale",
		Status: "shipped",
		Events: []store.WorkItemLink{{EventID: recs[0].ID, Reason: "ref:GEN-301 in branch"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), st, at(5, 0), at(12, 0)); err != nil {
		t.Fatal(err)
	}
	items, err := st.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %+v", items)
	}
}

func openLinkerStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
