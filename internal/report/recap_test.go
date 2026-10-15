package report

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestRecapGolden(t *testing.T) {
	recap := goldenRecap(t)
	styled := RenderRecap(recap, RenderOptions{Width: DefaultRecapWidth, Color: true})
	plain := RenderRecap(recap, RenderOptions{Width: DefaultRecapWidth, Color: false})
	assertGolden(t, "recap.styled.txt", styled)
	assertGolden(t, "recap.plain.txt", plain)

	if strings.Contains(plain, "\x1b") {
		t.Fatal("plain recap contains an escape")
	}
	if !strings.Contains(styled, "\x1b[") {
		t.Fatal("styled recap has no color")
	}
	if stripANSI(styled) != plain {
		t.Fatalf("styled recap without ANSI differs from the plain recap\n--- plain ---\n%s", plain)
	}
	for _, want := range []string{
		"Week 2026-W41 (Oct 5 – Oct 11)",
		"Shipped 2 · In progress 1 · Explored 1 · Abandoned 1",
		"~2h15m agent time · 5 commits · 1 PR merged",
		"Claude Code",
		"Codex",
		"Git",
		"GitHub",
		"Linear",
		"acme/smilepostcards-laravel",
		"acme/otto",
		"#5",
		"Admin is_admin gate",
		"merged",
		"#12",
		"Retry webhook delivery",
		"open",
		"█",
		"·",
		"Session chain",
		"* Claude · Admin gate",
		"| ref:GEN-341 in prompt",
		"aaaaaaa",
		"bbbbbbb",
		"#5",
		"GEN-341",
		"ref:GEN-341 ticket",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain recap missing %q\n%s", want, plain)
		}
	}
	counts, detail := summaryParts(recap.Summary, time.Duration(recap.Summary.AgentSeconds)*time.Second)
	stripped := strings.ReplaceAll(recap.Summary.Line, "**", "")
	if stripped != counts+" · "+detail {
		t.Fatalf("summary parts = %q · %q, line = %q", counts, detail, recap.Summary.Line)
	}
}

func TestPlainReportGolden(t *testing.T) {
	st := openReportStore(t)
	seedGolden(t, st)
	rep := buildGolden(t, st)
	md, err := Markdown(rep, "")
	if err != nil {
		t.Fatal(err)
	}
	// Pipes, NO_COLOR, and --plain print this Markdown. The bytes stay on
	// testdata/report/week.md.
	assertGolden(t, "week.md", md)
}

func TestRecapExtensionSlots(t *testing.T) {
	recap := goldenRecap(t)
	plain := RenderRecap(recap, RenderOptions{Color: false})
	if strings.Contains(plain, "vs last week") {
		t.Fatalf("delta slot was drawn\n%s", plain)
	}
	if !strings.Contains(plain, "Session chain") || !strings.Contains(plain, "ref:GEN-341 in prompt") {
		t.Fatalf("linker graph missing from the plain recap\n%s", plain)
	}

	recap = recap.WithDelta(Delta{Line: "↑ 1 PR merged vs last week"}).WithGraph(Graph{
		Title: "Session chain",
		Lines: []string{"sess-1 ── aaaaaaa ── #5"},
	})
	plain = RenderRecap(recap, RenderOptions{Color: false})
	styled := RenderRecap(recap, RenderOptions{Color: true})
	for _, body := range []string{plain, stripANSI(styled)} {
		if !strings.Contains(body, "↑ 1 PR merged vs last week") {
			t.Fatalf("missing delta\n%s", body)
		}
		if !strings.Contains(body, "Session chain") || !strings.Contains(body, "sess-1 ── aaaaaaa ── #5") {
			t.Fatalf("missing graph\n%s", body)
		}
		deltaAt := strings.Index(body, "vs last week")
		daysAt := strings.Index(body, "Days")
		lanesAt := strings.Index(body, "Sources")
		graphAt := strings.Index(body, "Session chain")
		reposAt := strings.Index(body, "Top repos")
		if deltaAt >= daysAt || daysAt >= lanesAt || lanesAt >= graphAt || graphAt >= reposAt {
			t.Fatalf("section order delta=%d days=%d lanes=%d graph=%d repos=%d", deltaAt, daysAt, lanesAt, graphAt, reposAt)
		}
	}
	if strings.Contains(plain, "\x1b") {
		t.Fatal("plain extension recap contains an escape")
	}
}

func TestRecapCountsAndOrder(t *testing.T) {
	recap := goldenRecap(t)
	if len(recap.Days) != 7 {
		t.Fatalf("days = %d", len(recap.Days))
	}
	wantDays := []int{0, 1, 1, 7, 4, 1, 0}
	for i, want := range wantDays {
		if recap.Days[i].Count != want || recap.Days[i].Label != time.Weekday((int(time.Monday) + i) % 7).String()[:3] {
			t.Fatalf("day %d = %+v", i, recap.Days[i])
		}
	}
	var got []string
	totals := map[string]int{}
	for _, lane := range recap.Lanes {
		got = append(got, lane.Source)
		totals[lane.Source] = lane.Total
		if lane.Color == "" {
			t.Fatalf("lane %s has no color", lane.Source)
		}
	}
	wantLanes := []string{"claude-code", "codex", "git", "github", "linear"}
	if strings.Join(got, ",") != strings.Join(wantLanes, ",") {
		t.Fatalf("lanes = %v", got)
	}
	if totals["claude-code"] != 2 || totals["codex"] != 1 || totals["git"] != 5 || totals["github"] != 3 || totals["linear"] != 3 {
		t.Fatalf("totals = %#v", totals)
	}
	if len(recap.Repos) < 2 || recap.Repos[0].Name != "acme/smilepostcards-laravel" || recap.Repos[0].Commits != 2 || recap.Repos[0].PRs != 1 || recap.Repos[0].Sessions != 2 {
		t.Fatalf("repos = %+v", recap.Repos)
	}
	if recap.Repos[1].Name != "acme/otto" {
		t.Fatalf("second repo = %+v", recap.Repos[1])
	}
	if len(recap.PRs) != 2 || recap.PRs[0].Number != "5" || recap.PRs[0].State != "merged" || recap.PRs[1].Number != "12" || recap.PRs[1].State != "open" {
		t.Fatalf("prs = %+v", recap.PRs)
	}
}

func TestRecapIgnoresOutsideWindowAndUnknownSource(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, loc)
	until := since.AddDate(0, 0, 7)
	events := []store.Record{
		{Event: event.Event{
			Source: "todoist", SourceID: "1", Kind: event.KindComment,
			TS: time.Date(2026, 10, 12, 4, 30, 0, 0, time.UTC), Title: "Still Sunday",
			Repo: "github.com/acme/app",
		}},
		{Event: event.Event{
			Source: "todoist", SourceID: "2", Kind: event.KindComment,
			TS: time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC), Title: "Before the week",
		}},
		{Event: event.Event{
			Source: "git", SourceID: "c", Kind: event.KindCommit,
			TS: time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC), Title: "Inside",
			Repo: "github.com/acme/app",
		}},
	}
	rep := Report{
		Heading: "Week 2026-W41 (Oct 5 – Oct 11)",
		Since:   since.UTC(),
		Until:   until.UTC(),
	}
	recap := BuildRecap(rep, events, nil, loc)
	if len(recap.Days) != 7 || recap.Days[6].Count != 1 || recap.Days[6].Label != "Sun" {
		t.Fatalf("days = %+v", recap.Days)
	}
	if len(recap.Lanes) != 2 || recap.Lanes[0].Source != "git" || recap.Lanes[1].Source != "todoist" {
		t.Fatalf("lanes = %+v", recap.Lanes)
	}
	if recap.Lanes[1].Total != 1 || recap.Lanes[1].Color == "" {
		t.Fatalf("todoist lane = %+v", recap.Lanes[1])
	}
	plain := RenderRecap(recap, RenderOptions{Color: false})
	if strings.Contains(plain, "Before the week") || !strings.Contains(plain, "todoist") {
		t.Fatalf("plain = %s", plain)
	}
}

func TestLongWindowCollapsesDays(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	until := since.AddDate(0, 0, 30)
	events := []store.Record{
		{Event: event.Event{
			Source: "git", SourceID: "1", Kind: event.KindCommit,
			TS: since.AddDate(0, 0, 3), Title: "early", Repo: "github.com/acme/app",
		}},
		{Event: event.Event{
			Source: "github", SourceID: "acme/app#4", Kind: event.KindPR,
			TS: since.AddDate(0, 0, 20), Title: "Later", State: event.StateOpen,
			URL: "https://github.com/acme/app/pull/4", Repo: "github.com/acme/app",
		}},
	}
	rep := Report{Heading: "Week Jan 1 – Jan 30", Since: since, Until: until}
	recap := BuildRecap(rep, events, nil, time.UTC)
	if len(recap.Days) != 30 || recap.Days[3].Count != 1 || recap.Days[20].Count != 1 {
		t.Fatalf("days = %d", len(recap.Days))
	}
	plain := RenderRecap(recap, RenderOptions{Color: false})
	if strings.Contains(plain, "\x1b") || strings.Contains(plain, "Mon ") {
		t.Fatalf("long window should be one sparkline, not a day list\n%s", plain)
	}
	if !strings.Contains(plain, "Jan 1 – Jan 30") || !strings.Contains(plain, "#4") {
		t.Fatalf("plain = %s", plain)
	}
}

func TestEmptyRecap(t *testing.T) {
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	rep := Report{
		Heading: "Week 2026-W41 (Oct 5 – Oct 11)",
		Since:   since,
		Until:   since.AddDate(0, 0, 7),
		Summary: Summary{Line: "**Shipped 0 · In progress 0 · Explored 0** · ~0m agent time · 0 commits · 0 PRs merged"},
	}
	plain := RenderRecap(BuildRecap(rep, nil, nil, time.UTC), RenderOptions{Color: false})
	for _, want := range []string{"No source activity", "No repositories", "No pull requests", "Shipped 0"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "Session chain") {
		t.Fatalf("empty week drew a chain\n%s", plain)
	}
}

func goldenRecap(t *testing.T) Recap {
	t.Helper()
	st := openReportStore(t)
	seedGolden(t, st)
	rep := buildGolden(t, st)
	events, err := st.ListEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.ListWorkItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return BuildRecap(rep, events, items, time.UTC)
}

func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && ((s[j] >= '0' && s[j] <= '9') || s[j] == ';') {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
