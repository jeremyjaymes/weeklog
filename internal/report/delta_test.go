package report

import (
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestWeekDeltaFromStoredEvents(t *testing.T) {
	loc := time.UTC
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, loc)
	until := since.AddDate(0, 0, 7)
	events := []store.Record{
		deltaRec(event.KindPR, event.StateMerged, since.AddDate(0, 0, -4), "github.com/acme/app"),
		deltaRec(event.KindPR, event.StateMerged, since.AddDate(0, 0, -3), "github.com/acme/old"),
		deltaRec(event.KindPR, event.StateOpen, since.AddDate(0, 0, -2), "github.com/acme/old"),
		deltaRec(event.KindAgentSession, "", since.AddDate(0, 0, -2), "github.com/acme/app"),
		deltaRec(event.KindComment, "", since.AddDate(0, 0, -1), ""),
		deltaRec(event.KindPR, event.StateMerged, since.Add(24*time.Hour), "github.com/acme/app"),
		deltaRec(event.KindAgentSession, "", since.Add(24*time.Hour), "github.com/acme/app"),
		deltaRec(event.KindAgentSession, "", since.Add(48*time.Hour), "github.com/acme/other"),
		deltaRec(event.KindCommit, "", since.Add(72*time.Hour), "github.com/acme/third"),
		deltaRec(event.KindPR, event.StateOpen, since.Add(72*time.Hour), "github.com/acme/third"),
		// Two weeks ago, and the instant the current week starts, stay out of
		// the previous week. The current week's start is this week's event.
		deltaRec(event.KindPR, event.StateMerged, since.AddDate(0, 0, -10), "github.com/acme/ancient"),
		deltaRec(event.KindAgentSession, "", since, "github.com/acme/app"),
		deltaRec(event.KindCommit, "", until, "github.com/acme/next"),
	}
	// Previous: 2 merged PRs, repos app+old, 1 session.
	// Current: 1 merged PR, repos app+other+third, 3 sessions.
	const want = "↓ 1 PR merged · ↑ 1 repo touched · ↑ 2 sessions vs last week"
	if got := deltaLine(events, since, until, loc); got != want {
		t.Fatalf("delta = %q", got)
	}

	rep, err := assemble(events, nil, Options{
		Since: since, Until: until, Label: "2026-W41", Location: loc,
		Now: time.Date(2026, 10, 13, 0, 0, 0, 0, loc), Version: "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.DeltaLine != want {
		t.Fatalf("report delta = %q", rep.DeltaLine)
	}
	recap := BuildRecap(rep, events, nil, loc)
	if recap.Delta == nil || recap.Delta.Line != want {
		t.Fatalf("recap delta = %+v", recap.Delta)
	}
	md, err := Markdown(rep, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, rep.Summary.Line+"\n"+want+"\n") {
		t.Fatalf("markdown missing the delta line\n%s", md)
	}
	plain := RenderRecap(recap, RenderOptions{Width: DefaultRecapWidth, Color: false})
	styled := RenderRecap(recap, RenderOptions{Width: DefaultRecapWidth, Color: true})
	if !strings.Contains(plain, want) || strings.Contains(plain, "\x1b") {
		t.Fatalf("plain recap = %s", plain)
	}
	if !strings.Contains(styled, "\x1b[") || stripANSI(styled) != plain {
		t.Fatal("styled recap dropped the delta text")
	}
}

func TestWeekDeltaMissingPriorWeek(t *testing.T) {
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	until := since.AddDate(0, 0, 7)
	current := []store.Record{
		deltaRec(event.KindPR, event.StateMerged, since.Add(time.Hour), "github.com/acme/app"),
		deltaRec(event.KindAgentSession, "", since.Add(2*time.Hour), "github.com/acme/app"),
		deltaRec(event.KindCommit, "", since.AddDate(0, 0, -14), "github.com/acme/old"),
	}
	if got := deltaLine(current, since, until, time.UTC); got != noPriorWeekLine {
		t.Fatalf("first week = %q", got)
	}
	if got := deltaLine(nil, since, until, time.UTC); got != noPriorWeekLine {
		t.Fatalf("empty store = %q", got)
	}

	// A comment in the previous week is stored data, so the counts compare
	// with that week even when its three metrics are zero.
	withComment := append(current, deltaRec(event.KindComment, "", since.AddDate(0, 0, -1), ""))
	const want = "↑ 1 PR merged · ↑ 1 repo touched · ↑ 1 session vs last week"
	if got := deltaLine(withComment, since, until, time.UTC); got != want {
		t.Fatalf("prior comment = %q", got)
	}
}

func TestWeekDeltaFlatAndPlural(t *testing.T) {
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	until := since.AddDate(0, 0, 7)
	var events []store.Record
	for _, weekStart := range []time.Time{since.AddDate(0, 0, -7), since} {
		events = append(events,
			deltaRec(event.KindPR, event.StateMerged, weekStart.Add(time.Hour), "github.com/acme/app"),
			deltaRec(event.KindPR, event.StateMerged, weekStart.Add(2*time.Hour), "github.com/acme/app"),
			deltaRec(event.KindAgentSession, "", weekStart.Add(3*time.Hour), "github.com/acme/app"),
			deltaRec(event.KindCommit, "", weekStart.Add(4*time.Hour), "github.com/acme/other"),
		)
	}
	const want = "→ 0 PRs merged · → 0 repos touched · → 0 sessions vs last week"
	if got := deltaLine(events, since, until, time.UTC); got != want {
		t.Fatalf("flat = %q", got)
	}
}

func TestWeekDeltaUsesLocation(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, loc)
	until := since.AddDate(0, 0, 7)
	events := []store.Record{
		// 04:00 UTC is still Sunday evening in Chicago, so it is last week.
		deltaRec(event.KindAgentSession, "", time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC), "github.com/acme/app"),
		// 05:00 UTC is Monday 00:00 in Chicago, the start of this week.
		deltaRec(event.KindPR, event.StateMerged, time.Date(2026, 10, 5, 5, 0, 0, 0, time.UTC), "github.com/acme/app"),
		deltaRec(event.KindAgentSession, "", time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC), "github.com/acme/other"),
	}
	const want = "↑ 1 PR merged · ↑ 1 repo touched · → 0 sessions vs last week"
	if got := deltaLine(events, since, until, loc); got != want {
		t.Fatalf("chicago = %q", got)
	}
	// The same instants are a different week in UTC, where Monday starts at
	// 00:00 UTC and the Sunday-evening session is already inside the week.
	utcSince := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	if got := deltaLine(events, utcSince, utcSince.AddDate(0, 0, 7), time.UTC); got != noPriorWeekLine {
		t.Fatalf("utc = %q", got)
	}
}

func TestWeekDeltaSkipsNonISOWindow(t *testing.T) {
	monday := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	events := []store.Record{
		deltaRec(event.KindPR, event.StateMerged, monday.AddDate(0, 0, -3), "github.com/acme/app"),
		deltaRec(event.KindPR, event.StateMerged, monday.Add(time.Hour), "github.com/acme/app"),
	}
	cases := []struct {
		name  string
		since time.Time
		until time.Time
	}{
		{name: "six days", since: monday, until: monday.AddDate(0, 0, 6)},
		{name: "tuesday", since: monday.AddDate(0, 0, 1), until: monday.AddDate(0, 0, 8)},
		{name: "not midnight", since: monday.Add(time.Hour), until: monday.Add(time.Hour).AddDate(0, 0, 7)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deltaLine(events, tc.since, tc.until, time.UTC); got != "" {
				t.Fatalf("delta = %q", got)
			}
			rep, err := assemble(events, nil, Options{
				Since: tc.since, Until: tc.until, Location: time.UTC,
				Now: monday, Version: "dev",
			})
			if err != nil {
				t.Fatal(err)
			}
			if rep.DeltaLine != "" {
				t.Fatalf("report delta = %q", rep.DeltaLine)
			}
			recap := BuildRecap(rep, events, nil, time.UTC)
			if recap.Delta != nil {
				t.Fatalf("recap delta = %+v", recap.Delta)
			}
			plain := RenderRecap(recap, RenderOptions{Color: false})
			if strings.Contains(plain, "vs last week") || strings.Contains(plain, noPriorWeekLine) {
				t.Fatalf("non-ISO recap drew a delta\n%s", plain)
			}
		})
	}
}

func deltaRec(kind event.Kind, state event.State, ts time.Time, repo string) store.Record {
	return rec(0, event.Event{
		Source: "git", SourceID: ts.Format(time.RFC3339Nano) + "/" + string(kind) + "/" + repo,
		Kind: kind, State: state, TS: ts, Repo: repo, Title: "t",
	})
}
