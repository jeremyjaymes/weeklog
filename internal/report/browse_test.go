package report

import (
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestBuildBrowserLinksByDay(t *testing.T) {
	loc := time.UTC
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, loc)
	until := since.AddDate(0, 0, 7)
	shaA := strings.Repeat("a", 40)
	shaB := strings.Repeat("b", 40)
	events := []store.Record{
		rec(1, event.Event{
			Source: "claude-code", SourceID: "sess-1", Kind: event.KindAgentSession,
			TS: time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC), Title: "Admin gate",
			Repo: "github.com/acme/app",
		}),
		rec(2, event.Event{
			Source: "git", SourceID: shaA, Kind: event.KindCommit,
			TS: time.Date(2026, 10, 8, 14, 30, 0, 0, time.UTC), Title: "Add the gate",
			Repo: "github.com/acme/app", Branch: "main",
		}),
		rec(3, event.Event{
			Source: "github", SourceID: "acme/app#8", Kind: event.KindPR,
			TS: time.Date(2026, 10, 8, 16, 0, 0, 0, time.UTC), Title: "Add the gate",
			State: event.StateMerged, URL: "https://github.com/acme/app/pull/8",
			Repo: "github.com/acme/app",
		}),
		rec(4, event.Event{
			Source: "linear", SourceID: "GEN-1", Kind: event.KindTicket,
			TS: time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC), Title: "GEN-1 Gate",
			Repo: "github.com/acme/app",
		}),
		rec(5, event.Event{
			Source: "github", SourceID: "acme/app#8/review", Kind: event.KindPRReview,
			TS: time.Date(2026, 10, 8, 17, 0, 0, 0, time.UTC), Title: "LGTM",
			Repo: "github.com/acme/app",
		}),
		rec(6, event.Event{
			Source: "git", SourceID: strings.Repeat("c", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), Title: "Too early",
			Repo: "github.com/acme/app", Branch: "main",
		}),
		rec(7, event.Event{
			Source: "codex", SourceID: "roll-1", Kind: event.KindAgentSession,
			TS: time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC), Title: "Spike",
			Repo: "github.com/acme/otto",
		}),
		rec(8, event.Event{
			Source: "git", SourceID: shaB, Kind: event.KindCommit,
			TS: time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC), Title: "Try the spike",
			Repo: "github.com/acme/otto", Branch: "spike",
		}),
		rec(9, event.Event{
			Source: "git", SourceID: strings.Repeat("d", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC), Title: "Loose note",
			Repo: "github.com/acme/app", Branch: "main",
		}),
		rec(10, event.Event{
			Source: "git", SourceID: strings.Repeat("e", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC), Title: "Earlier gate",
			Repo: "github.com/acme/app", Branch: "main",
		}),
	}
	items := []store.WorkItem{
		{
			ID: 1, Title: "Add the gate",
			Events: []store.WorkItemMember{
				{EventID: 6, Reason: "ref:GEN-1 in commit message"},
				{EventID: 4, Reason: "ref:GEN-1 in title"},
				{EventID: 1, Reason: "ref:GEN-1 in prompt"},
				{EventID: 2, Reason: "ref:GEN-1 in commit message"},
				{EventID: 3, Reason: "ref:GEN-1 in title"},
				{EventID: 5, Reason: "ref:GEN-1 in review"},
			},
		},
		{
			ID: 2, Title: "Earlier gate",
			Events: []store.WorkItemMember{
				{EventID: 10, Reason: "sha:eeeeeee"},
			},
		},
		{
			ID: 3, Title: "Spike",
			Events: []store.WorkItemMember{
				{EventID: 7, Reason: "files overlap: 1"},
				{EventID: 8, Reason: "files overlap: 1"},
			},
		},
	}
	rep, err := assemble(events, items, Options{
		Since: since, Until: until, Label: "2026-W41", Location: loc,
		Now: until, Version: "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := JSON(rep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"members"`) || strings.Contains(string(body), `"Members"`) {
		t.Fatalf("members leaked into JSON\n%s", body)
	}

	browser := BuildBrowser(rep, loc)
	if browser.Heading == "" || len(browser.Days) != 7 {
		t.Fatalf("heading %q days %d", browser.Heading, len(browser.Days))
	}
	if got := browser.Days[0]; got.Label != "Mon" || got.When != "Oct 5" || len(got.Items) != 0 {
		t.Fatalf("monday = %+v", got)
	}

	tue := browser.Days[1]
	if tue.Label != "Tue" || len(tue.Items) != 1 || tue.Items[0].Title != "Loose note" || len(tue.Items[0].Links) != 0 {
		t.Fatalf("tuesday = %+v", tue.Items)
	}

	thu := browser.Days[3]
	if thu.Label != "Thu" || thu.When != "Oct 8" || len(thu.Items) != 2 {
		t.Fatalf("thursday items = %+v", thu.Items)
	}
	if thu.Items[0].Title != "Earlier gate" || thu.Items[1].Title != "Add the gate" {
		t.Fatalf("thursday order = %q, %q", thu.Items[0].Title, thu.Items[1].Title)
	}
	gate := thu.Items[1]
	if gate.Status != "Shipped" || gate.Repo != "app" || gate.Actor == "" {
		t.Fatalf("gate meta = %+v", gate)
	}
	if len(gate.Links) != 3 {
		t.Fatalf("links = %+v", gate.Links)
	}
	if gate.Links[0].Kind != event.KindAgentSession || gate.Links[0].Label != "Claude" || gate.Links[0].Soft {
		t.Fatalf("session = %+v", gate.Links[0])
	}
	if gate.Links[1].Kind != event.KindCommit || gate.Links[1].Label != "aaaaaaa" || gate.Links[1].Reason != "ref:GEN-1 in commit message" {
		t.Fatalf("commit = %+v", gate.Links[1])
	}
	if gate.Links[2].Kind != event.KindPR || gate.Links[2].Label != "#8" || gate.Links[2].Soft {
		t.Fatalf("pr = %+v", gate.Links[2])
	}
	for _, link := range gate.Links {
		if link.Detail == "Too early" || link.Label == "GEN-1" {
			t.Fatalf("unexpected link %+v", link)
		}
	}

	fri := browser.Days[4]
	if len(fri.Items) != 1 || fri.Items[0].Title != "Spike" || len(fri.Items[0].Links) != 2 {
		t.Fatalf("friday = %+v", fri.Items)
	}
	if !fri.Items[0].Links[0].Soft || fri.Items[0].Links[0].Label != "Codex" {
		t.Fatalf("soft session = %+v", fri.Items[0].Links[0])
	}
	if !fri.Items[0].Links[1].Soft || fri.Items[0].Links[1].Label != "bbbbbbb" {
		t.Fatalf("soft commit = %+v", fri.Items[0].Links[1])
	}
}

func TestBuildBrowserEmptyWindow(t *testing.T) {
	rep := Report{
		Heading: "Week 2026-W41",
		Since:   time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Until:   time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
	}
	browser := BuildBrowser(rep, nil)
	if len(browser.Days) != 7 {
		t.Fatalf("days = %d", len(browser.Days))
	}
	for _, day := range browser.Days {
		if len(day.Items) != 0 {
			t.Fatalf("day %s has items", day.Label)
		}
	}
}

func rec(id int64, ev event.Event) store.Record {
	ev.TS = ev.TS.UTC()
	return store.Record{ID: id, Event: ev}
}
