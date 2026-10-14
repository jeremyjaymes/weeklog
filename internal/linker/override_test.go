package linker

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestOverridesJoinLeftovers(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "One"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
		rec(2, event.KindCommit, "git", otherSHA, at(9, 10), func(e *event.Event) {
			e.Title = "Two"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
	}
	if describeItems(linkAll(events)) != describeItems(linkOver(events, nil)) || describeItems(linkAll(events)) != describeItems(linkOver(events, []store.Override{})) {
		t.Fatalf("empty overrides changed grouping\n%s", describeItems(linkOver(events, []store.Override{})))
	}
	got := linkOver(events, []store.Override{{Kind: store.OverrideLink, EventA: 1, EventB: 2}})
	assertLinked(t, got, []linkedItem{{
		Title:  "One",
		Status: StatusShipped,
		Events: []linkedEvent{
			linked(events[0], "leftover:"+repoOtto+" 2026-10-08, override:link"),
			linked(events[1], "leftover:"+repoOtto+" 2026-10-09, override:link"),
		},
	}})
}

func TestOverrideUnlinkSplitsHardLink(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "GEN-301", at(6, 9), func(e *event.Event) {
			e.Title = "Webhook retries"
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "ship it"
			e.Summary = "ship GEN-301"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
		rec(3, event.KindCommit, "git", otherSHA, at(8, 11), func(e *event.Event) {
			e.Title = "also GEN-301"
			e.Summary = "also GEN-301"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
	}
	got := linkOver(events, []store.Override{{Kind: store.OverrideUnlink, EventA: 1}})
	assertLinked(t, got, []linkedItem{
		{
			Title:  "Webhook retries",
			Status: "",
			Events: []linkedEvent{
				linked(events[0], "override:unlink"),
			},
		},
		{
			Title:  "ship it",
			Status: StatusShipped,
			Events: []linkedEvent{
				linked(events[1], "ref:GEN-301 in commit message"),
				linked(events[2], "ref:GEN-301 in commit message"),
			},
		},
	})
}

func TestOverrideLinkMergesHardGroups(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "GEN-301", at(6, 9), func(e *event.Event) {
			e.Title = "Webhook retries"
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "ship GEN-301"
			e.Summary = "ship GEN-301"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
		rec(3, event.KindTicket, "linear", "ABC-99", at(6, 10), func(e *event.Event) {
			e.Title = "Other ticket"
		}),
		rec(4, event.KindCommit, "git", otherSHA, at(8, 11), func(e *event.Event) {
			e.Title = "ship ABC-99"
			e.Summary = "ship ABC-99"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
	}
	got := linkOver(events, []store.Override{{Kind: store.OverrideLink, EventA: 2, EventB: 4}})
	assertLinked(t, got, []linkedItem{{
		Title:  "Webhook retries",
		Status: StatusShipped,
		Events: []linkedEvent{
			linked(events[0], "ref:GEN-301 ticket"),
			linked(events[2], "ref:ABC-99 ticket"),
			linked(events[1], "ref:GEN-301 in commit message, override:link"),
			linked(events[3], "ref:ABC-99 in commit message, override:link"),
		},
	}})
}

func TestOverrideReplayOrder(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "One"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
		rec(2, event.KindCommit, "git", otherSHA, at(9, 10), func(e *event.Event) {
			e.Title = "Two"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
	}
	ovs := []store.Override{
		{Kind: store.OverrideLink, EventA: 1, EventB: 2},
		{Kind: store.OverrideUnlink, EventA: 1},
		{Kind: store.OverrideLink, EventA: 2, EventB: 1},
	}
	got := linkOver(events, ovs)
	assertLinked(t, got, []linkedItem{{
		Title:  "One",
		Status: StatusShipped,
		Events: []linkedEvent{
			linked(events[0], "leftover:"+repoOtto+" 2026-10-08, override:link"),
			linked(events[1], "leftover:"+repoOtto+" 2026-10-09, override:link"),
		},
	}})

	undone := linkOver(events, ovs[:2])
	assertLinked(t, undone, []linkedItem{
		{
			Title:  "One",
			Status: StatusShipped,
			Events: []linkedEvent{linked(events[0], "override:unlink")},
		},
		{
			Title:  "Two",
			Status: StatusShipped,
			Events: []linkedEvent{linked(events[1], "leftover:"+repoOtto+" 2026-10-09")},
		},
	})
}

func TestOverrideUnlinkBeatsSoftLink(t *testing.T) {
	end := at(8, 11)
	events := []store.Record{
		rec(1, event.KindCommit, "git", fullSHA, at(8, 12), func(e *event.Event) {
			e.Title = "Fix webhooks"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php"}
		}),
		rec(2, event.KindAgentSession, "claude-code", "sess-1", at(8, 10), func(e *event.Event) {
			e.Title = "Edit webhooks"
			e.TSEnd = &end
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php"}
		}),
	}
	got := linkOver(events, []store.Override{{Kind: store.OverrideUnlink, EventA: 2}})
	assertLinked(t, got, []linkedItem{
		{
			Title:  "Edit webhooks",
			Status: StatusExplored,
			Events: []linkedEvent{linked(events[1], "override:unlink")},
		},
		{
			Title:  "Fix webhooks",
			Status: StatusInProgress,
			Events: []linkedEvent{linked(events[0], "files overlap: 1")},
		},
	})
}

func TestOverrideIgnoresMissingEvent(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "GEN-301", at(6, 9), func(e *event.Event) {
			e.Title = "Webhook retries"
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "ship it"
			e.Summary = "ship GEN-301"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
	}
	plain := linkOver(events, nil)
	got := linkOver(events, []store.Override{{Kind: store.OverrideLink, EventA: 2, EventB: 99}})
	if describeItems(got) != describeItems(plain) {
		t.Fatalf("missing event changed grouping\n%s", describeItems(got))
	}
}

func TestRunKeepsOverridesAcrossRelinkAndUpsert(t *testing.T) {
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
			TS: at(9, 10), Title: "notes",
			Repo: repoOtto, Branch: "main",
		},
	}
	res, err := st.UpsertEvents(context.Background(), events)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddOverride(context.Background(), store.OverrideLink, res.IDs[0], res.IDs[2]); err != nil {
		t.Fatal(err)
	}
	since, until := at(5, 0), at(12, 0)
	if err := Run(context.Background(), st, since, until); err != nil {
		t.Fatal(err)
	}
	assertOverrideGroup(t, st, true)

	if err := Run(context.Background(), st, since, until); err != nil {
		t.Fatal(err)
	}
	assertOverrideGroup(t, st, true)

	again, err := st.UpsertEvents(context.Background(), events)
	if err != nil {
		t.Fatal(err)
	}
	if again.Updated != len(events) || again.Inserted != 0 {
		t.Fatalf("upsert = %+v", again)
	}
	for i := range res.IDs {
		if again.IDs[i] != res.IDs[i] {
			t.Fatalf("id %d changed from %d to %d", i, res.IDs[i], again.IDs[i])
		}
	}
	if err := Run(context.Background(), st, since, until); err != nil {
		t.Fatal(err)
	}
	assertOverrideGroup(t, st, true)

	if _, err := st.AddOverride(context.Background(), store.OverrideUnlink, res.IDs[2], 0); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), st, since, until); err != nil {
		t.Fatal(err)
	}
	assertOverrideGroup(t, st, false)
	if err := Run(context.Background(), st, since, until); err != nil {
		t.Fatal(err)
	}
	assertOverrideGroup(t, st, false)

	listed, err := st.ListOverrides(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("overrides = %+v", listed)
	}
}

func assertOverrideGroup(t *testing.T, st *store.Store, linkedTogether bool) {
	t.Helper()
	items, err := st.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if linkedTogether {
		if len(items) != 1 || len(items[0].Events) != 3 {
			t.Fatalf("items = %+v", items)
		}
		reasons := map[string]string{}
		for _, member := range items[0].Events {
			reasons[member.SourceID] = member.Reason
		}
		if !strings.Contains(reasons["GEN-301"], "override:link") || !strings.Contains(reasons[otherSHA], "override:link") {
			t.Fatalf("reasons = %+v", reasons)
		}
		if strings.Contains(reasons[fullSHA], "override:") {
			t.Fatalf("unrelated commit picked up an override reason: %+v", reasons)
		}
		return
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	var notes, ticket string
	for _, item := range items {
		for _, member := range item.Events {
			switch member.SourceID {
			case otherSHA:
				notes = member.Reason
				if len(item.Events) != 1 {
					t.Fatalf("unlinked item = %+v", item)
				}
			case "GEN-301":
				ticket = member.Reason
				if len(item.Events) != 2 {
					t.Fatalf("ticket item = %+v", item)
				}
			}
		}
	}
	if notes != "override:unlink" {
		t.Fatalf("notes reason = %q", notes)
	}
	if strings.Contains(ticket, "override:") {
		t.Fatalf("ticket reason = %q", ticket)
	}
}

func linkOver(events []store.Record, overrides []store.Override) []linkedItem {
	return linkEventsWith(events, []*regexp.Regexp{defaultTicketRE}, true, asOf(), time.UTC, overrides)
}
