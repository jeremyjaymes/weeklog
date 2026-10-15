package report

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/linker"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestGoldenWeek(t *testing.T) {
	st := openReportStore(t)
	seedGolden(t, st)
	rep := buildGolden(t, st)
	md, err := Markdown(rep, "")
	if err != nil {
		t.Fatal(err)
	}
	js, err := JSON(rep)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "week.md", md)
	assertGolden(t, "week.json", string(js))

	for _, absent := range []string{"Too early", "Crossed into the week", "Until exclusive"} {
		if strings.Contains(md, absent) {
			t.Fatalf("markdown includes %q\n%s", absent, md)
		}
	}
	if !strings.Contains(md, "Admin is_admin gate — GEN-341") {
		t.Fatalf("missing ticket heading\n%s", md)
	}
	if !strings.Contains(md, "~2h15m agent time") || !strings.Contains(md, "5 commits") || !strings.Contains(md, "1 PR merged") {
		t.Fatalf("summary = %s", rep.Summary.Line)
	}
	if !strings.Contains(md, "Actors · Human 2 · Claude 1 · Codex 1 · Cloud agent 1") {
		t.Fatalf("missing actor counts\n%s", md)
	}
	for _, want := range []string{
		"Actor: Claude · session-commit",
		"Actor: Cloud agent · branch",
		"Actor: Codex · session",
		"Actor: Human · human",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q\n%s", want, md)
		}
	}
	for _, want := range []string{
		`"actor": "claude"`,
		`"actor_evidence": "session-commit"`,
		`"actor": "cloud_agent"`,
		`"actor_evidence": "branch"`,
		`"id": "codex"`,
		`"label": "Cloud agent"`,
	} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("json missing %q\n%s", want, js)
		}
	}
}

func TestEmptyWeek(t *testing.T) {
	st := openReportStore(t)
	rep, err := Build(context.Background(), st, goldenOptions())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Line != "**Shipped 0 · In progress 0 · Explored 0** · ~0m agent time · 0 commits · 0 PRs merged" {
		t.Fatalf("line = %s", rep.Summary.Line)
	}
	if len(rep.Sections) != 0 || len(rep.Other) != 0 {
		t.Fatalf("sections=%d other=%d", len(rep.Sections), len(rep.Other))
	}
	md, err := Markdown(rep, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "week: 2026-W41\n") || !strings.Contains(md, "# Week 2026-W41 (Oct 5 – Oct 11)\n") {
		t.Fatalf("markdown = %s", md)
	}
}

func TestWindowBoundariesAndTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, loc)
	until := since.AddDate(0, 0, 7)
	events := []event.Event{
		{
			Source: "github", SourceID: "acme/app#9", Kind: event.KindPR,
			TS: time.Date(2026, 10, 8, 4, 30, 0, 0, time.UTC), State: event.StateMerged,
			Title: "Night merge", URL: "https://github.com/acme/app/pull/9",
			Repo: "github.com/acme/app",
		},
		{
			Source: "claude-code", SourceID: "before", Kind: event.KindAgentSession,
			TS: time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC), TSEnd: timePtr(time.Date(2026, 10, 5, 6, 0, 0, 0, time.UTC)),
			Title: "Started yesterday",
		},
		{
			Source: "claude-code", SourceID: "inside", Kind: event.KindAgentSession,
			TS: time.Date(2026, 10, 12, 4, 30, 0, 0, time.UTC), TSEnd: timePtr(time.Date(2026, 10, 12, 6, 0, 0, 0, time.UTC)),
			Title: "Crosses out of the week",
		},
	}
	// 2026-10-12 04:30 UTC is 2026-10-11 23:30 in Chicago, still inside the week.
	// The session end is ignored for membership. 2026-10-05 04:00 UTC is
	// 2026-10-04 23:00 in Chicago, the previous day, so it stays out.
	st := openReportStore(t)
	if _, err := st.UpsertEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	rep, err := Build(context.Background(), st, Options{
		Since: since, Until: until, Label: "2026-W41", Location: loc,
		Now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), Version: "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	md, err := Markdown(rep, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "Started yesterday") {
		t.Fatalf("session before the window was included\n%s", md)
	}
	if !strings.Contains(md, "Crosses out of the week") {
		t.Fatalf("session that starts in the window was dropped\n%s", md)
	}
	if !strings.Contains(md, "PR [#9](https://github.com/acme/app/pull/9) merged Oct 7 · app") {
		t.Fatalf("chicago date missing\n%s", md)
	}
	if rep.Range != "Oct 5 – Oct 11" {
		t.Fatalf("range = %s", rep.Range)
	}
}

func TestStatusRules(t *testing.T) {
	until := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	commit := func(title, branch string, day int) store.Record {
		return store.Record{Event: event.Event{
			Source: "git", SourceID: title, Kind: event.KindCommit,
			TS: time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC), Title: title, Branch: branch,
			Repo: "github.com/acme/app",
		}}
	}
	pr := func(state event.State) store.Record {
		return store.Record{Event: event.Event{
			Source: "github", SourceID: "acme/app#1", Kind: event.KindPR,
			TS: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), State: state, Branch: "feature",
		}}
	}
	ticket := func(state event.State) store.Record {
		return store.Record{Event: event.Event{
			Source: "linear", SourceID: "GEN-1", Kind: event.KindTicket,
			TS: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), State: state, Title: "Gate",
		}}
	}
	session := store.Record{Event: event.Event{
		Source: "codex", SourceID: "s", Kind: event.KindAgentSession,
		TS: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), Title: "Look",
	}}

	cases := []struct {
		name   string
		stored string
		events []store.Record
		want   string
	}{
		{name: "stored wins", stored: "explored", events: []store.Record{pr(event.StateMerged)}, want: statusExplored},
		{name: "merged pr", events: []store.Record{pr(event.StateMerged), commit("ship", "feature", 18)}, want: statusShipped},
		{name: "done ticket", events: []store.Record{ticket(event.StateDone)}, want: statusShipped},
		{name: "main commit", events: []store.Record{commit("ship", "main", 18)}, want: statusShipped},
		{name: "revert on main", events: []store.Record{commit(`Revert "ship"`, "main", 18)}, want: statusAbandoned},
		{name: "revert and real commit", events: []store.Record{commit(`Revert "ship"`, "main", 18), commit("follow up", "main", 18)}, want: statusShipped},
		{name: "stale branch", events: []store.Record{commit("old", "wip/old", 10)}, want: statusAbandoned},
		{name: "recent feature", events: []store.Record{commit("wip", "feature", 18)}, want: statusInProgress},
		{name: "open pr", events: []store.Record{pr(event.StateOpen)}, want: statusInProgress},
		{name: "closed pr", events: []store.Record{pr(event.StateClosed)}, want: statusExplored},
		{name: "session", events: []store.Record{session}, want: statusExplored},
		{name: "abandoned ticket", events: []store.Record{ticket(event.StateAbandoned)}, want: statusAbandoned},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.events, tc.stored, until); got != tc.want {
				t.Fatalf("status = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCommitClusters(t *testing.T) {
	at := func(hour int) store.Record {
		return store.Record{Event: event.Event{
			Kind: event.KindCommit, TS: time.Date(2026, 10, 8, hour, 0, 0, 0, time.UTC),
		}}
	}
	if n := commitClusterCount([]store.Record{at(10), at(11), at(12)}); n != 1 {
		t.Fatalf("one cluster = %d", n)
	}
	if n := commitClusterCount([]store.Record{at(10), at(13)}); n != 2 {
		t.Fatalf("two clusters = %d", n)
	}
	events := []store.Record{at(10), at(13)}
	for i := range events {
		events[i].Source = "git"
		events[i].SourceID = string(rune('a' + i))
		events[i].Title = "cluster"
		events[i].Repo = "github.com/acme/app"
		events[i].Branch = "feature"
	}
	it := newItem(0, "cluster", statusInProgress, events, events, nil, time.UTC)
	if it.EstimateSeconds != int((30 * time.Minute).Seconds()) {
		t.Fatalf("estimate = %d", it.EstimateSeconds)
	}
	if len(it.Bullets) != 2 || it.Bullets[0] != "Actor: Human · human" || it.Bullets[1] != "2 commits (~30m estimate) · app" {
		t.Fatalf("bullet = %#v", it.Bullets)
	}
	if it.Actor != "human" || it.ActorEvidence != "human" {
		t.Fatalf("actor = %s evidence = %s", it.Actor, it.ActorEvidence)
	}
}

func TestSortByRepoThenTime(t *testing.T) {
	st := openReportStore(t)
	end := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	longEnd := end.Add(2 * time.Hour)
	events := []event.Event{
		{
			Source: "codex", SourceID: "short", Kind: event.KindAgentSession,
			TS: end.Add(-30 * time.Minute), TSEnd: &end, Title: "Short",
			Repo: "github.com/acme/zebra",
		},
		{
			Source: "codex", SourceID: "long", Kind: event.KindAgentSession,
			TS: end, TSEnd: &longEnd, Title: "Long",
			Repo: "github.com/acme/zebra",
		},
		{
			Source: "codex", SourceID: "other", Kind: event.KindAgentSession,
			TS: end, TSEnd: &longEnd, Title: "Other repo",
			Repo: "github.com/acme/alpha",
		},
	}
	if _, err := st.UpsertEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	rep, err := Build(context.Background(), st, goldenOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Sections) != 1 || len(rep.Sections[0].Items) != 3 {
		t.Fatalf("sections = %+v", rep.Sections)
	}
	var titles []string
	for _, item := range rep.Sections[0].Items {
		titles = append(titles, item.Title)
	}
	want := []string{"Other repo", "Long", "Short"}
	if strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v", titles)
	}
}

func TestLinkerItemsRender(t *testing.T) {
	st := openReportStore(t)
	events := []event.Event{
		{
			Source: "linear", SourceID: "GEN-341", Kind: event.KindTicket,
			TS: time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC), Title: "Admin is_admin gate",
			State: event.StateDone,
		},
		{
			Source: "git", SourceID: strings.Repeat("a", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC), Title: "Add the gate",
			Summary: "Add the gate for GEN-341", Repo: "github.com/acme/app", Branch: "main",
		},
	}
	if _, err := st.UpsertEvents(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	until := since.AddDate(0, 0, 7)
	if err := linker.Run(context.Background(), st, since, until); err != nil {
		t.Fatal(err)
	}
	rep, err := Build(context.Background(), st, goldenOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Sections) != 1 || rep.Sections[0].ID != statusShipped || len(rep.Sections[0].Items) != 1 {
		t.Fatalf("sections = %+v", rep.Sections)
	}
	item := rep.Sections[0].Items[0]
	if item.Heading != "Admin is_admin gate — GEN-341" || item.ID == 0 {
		t.Fatalf("item = %+v", item)
	}
	if len(item.Reasons) == 0 || !strings.Contains(item.Bullets[len(item.Bullets)-1], "Linked by:") {
		t.Fatalf("bullets = %#v reasons = %#v", item.Bullets, item.Reasons)
	}
}

func TestBuildRejects(t *testing.T) {
	st := openReportStore(t)
	if _, err := Build(context.Background(), nil, goldenOptions()); err == nil {
		t.Fatal("expected a nil store to fail")
	}
	opt := goldenOptions()
	opt.Until = opt.Since
	if _, err := Build(context.Background(), st, opt); err == nil {
		t.Fatal("expected an empty window to fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, st, goldenOptions()); err == nil {
		t.Fatal("expected a canceled context to fail")
	}
}

func TestYearBoundaryRange(t *testing.T) {
	since := time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	if got := humanRange(since, until, time.UTC); got != "Dec 29, 2025 – Jan 4, 2026" {
		t.Fatalf("range = %s", got)
	}
	rep, err := assemble(nil, nil, Options{Since: since, Until: until, Now: since, Version: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Week != "2025-12-29..2026-01-05" || rep.Heading != "Week Dec 29, 2025 – Jan 4, 2026" {
		t.Fatalf("week = %s heading = %s", rep.Week, rep.Heading)
	}
	if rep.FileBase() != "2025-12-29--2026-01-05" {
		t.Fatalf("file = %s", rep.FileBase())
	}
}

func seedGolden(t *testing.T, st *store.Store) {
	t.Helper()
	endA := time.Date(2026, 10, 8, 15, 20, 0, 0, time.UTC)
	endB := time.Date(2026, 10, 8, 15, 40, 0, 0, time.UTC)
	endC := time.Date(2026, 10, 7, 10, 35, 0, 0, time.UTC)
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
			TS: time.Date(2026, 10, 4, 22, 0, 0, 0, time.UTC), TSEnd: timePtr(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)),
			Title: "Crossed into the week",
		},
		{
			Source: "git", SourceID: strings.Repeat("9", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), Title: "Until exclusive",
			Repo: "github.com/acme/zebra", Branch: "main",
		},
	}
	res, err := st.UpsertEvents(context.Background(), events)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.IDs) != len(events) {
		t.Fatalf("ids = %d", len(res.IDs))
	}
	err = st.ReplaceWorkItems(context.Background(), []store.WorkItemDraft{
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
	if err != nil {
		t.Fatal(err)
	}
}

func buildGolden(t *testing.T, st *store.Store) Report {
	t.Helper()
	rep, err := Build(context.Background(), st, goldenOptions())
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func goldenOptions() Options {
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	return Options{
		Since:    since,
		Until:    since.AddDate(0, 0, 7),
		Label:    "2026-W41",
		Location: time.UTC,
		Now:      time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
		Version:  "dev",
	}
}

func openReportStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return st
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "report", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\n--- got ---\n%s", name, err, got)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- got ---\n%s", name, got)
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
