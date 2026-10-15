package report

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestSessionGraphHardChain(t *testing.T) {
	since, until := graphWindow()
	sha := strings.Repeat("a", 40)
	events := []store.Record{
		graphRec(1, "claude-code", "sess-1", event.KindAgentSession, "Admin gate", since.Add(48*time.Hour), ""),
		graphRec(2, "git", sha, event.KindCommit, "Add is_admin gate", since.Add(49*time.Hour), ""),
		graphRec(3, "github", "acme/app#5", event.KindPR, "Admin is_admin gate", since.Add(50*time.Hour), "https://github.com/acme/app/pull/5"),
		graphRec(4, "linear", "GEN-341", event.KindTicket, "Admin is_admin gate", since.Add(24*time.Hour), ""),
	}
	items := []store.WorkItem{{
		ID: 1,
		Events: []store.WorkItemMember{
			{EventID: 1, Source: "claude-code", SourceID: "sess-1", Kind: event.KindAgentSession, Reason: "ref:GEN-341 in prompt"},
			{EventID: 2, Source: "git", SourceID: sha, Kind: event.KindCommit, Reason: "ref:GEN-341 in commit message"},
			{EventID: 3, Source: "github", SourceID: "acme/app#5", Kind: event.KindPR, Reason: "ref:GEN-341 in title"},
			{EventID: 4, Source: "linear", SourceID: "GEN-341", Kind: event.KindTicket, Reason: "ref:GEN-341 ticket"},
		},
	}}
	g := buildSessionGraph(items, events, since, until)
	if g == nil || len(g.Chains) != 1 || g.Chains[0].More != 0 || g.More != 0 {
		t.Fatalf("graph = %+v", g)
	}
	got := renderGraphText(t, g, 70, false)
	want := strings.Join([]string{
		"Session chain",
		"* Claude · Admin gate",
		"| ref:GEN-341 in prompt",
		"* aaaaaaa · Add is_admin gate",
		"| ref:GEN-341 in commit message",
		"* #5 · Admin is_admin gate",
		"| ref:GEN-341 in title",
		"* GEN-341 · Admin is_admin gate",
		"  ref:GEN-341 ticket",
	}, "\n")
	if got != want {
		t.Fatalf("hard chain:\n%s", got)
	}
	if strings.Contains(got, "o ") || strings.Contains(got, ": ") {
		t.Fatalf("hard chain used a soft mark\n%s", got)
	}
	full := RenderRecap(Recap{Heading: "Week", Graph: g}, RenderOptions{Width: 70, Color: true})
	if stripANSI(full) == "" || graphPanel(t, full, true) != got {
		t.Fatalf("styled hard chain differs")
	}
	assertReasonDimmed(t, full, "ref:GEN-341 in prompt")
}

func TestSessionGraphSoftAndMixed(t *testing.T) {
	since, until := graphWindow()
	sha := strings.Repeat("c", 40)
	events := []store.Record{
		graphRec(1, "codex", "roll-1", event.KindAgentSession, "Try Lob adapter", since.Add(48*time.Hour), ""),
		graphRec(2, "git", sha, event.KindCommit, "Retry webhook delivery", since.Add(49*time.Hour), ""),
		graphRec(3, "github", "acme/otto#12", event.KindPR, "Retry webhook delivery", since.Add(50*time.Hour), "https://github.com/acme/otto/pull/12"),
	}
	soft := []store.WorkItem{{
		ID: 1,
		Events: []store.WorkItemMember{
			{EventID: 1, Kind: event.KindAgentSession, Reason: "files overlap: 2"},
			{EventID: 2, Kind: event.KindCommit, Reason: "files overlap: 2"},
			{EventID: 3, Kind: event.KindPR, Reason: "files overlap: 2"},
		},
	}}
	got := renderGraphText(t, buildSessionGraph(soft, events, since, until), 70, false)
	want := strings.Join([]string{
		"Session chain",
		"o Codex · Try Lob adapter",
		": files overlap: 2",
		"o ccccccc · Retry webhook delivery",
		": files overlap: 2",
		"o #12 · Retry webhook delivery",
		"  files overlap: 2",
	}, "\n")
	if got != want {
		t.Fatalf("soft chain:\n%s", got)
	}

	mixed := []store.WorkItem{{
		ID: 1,
		Events: []store.WorkItemMember{
			{EventID: 1, Kind: event.KindAgentSession, Reason: "files overlap: 2"},
			{EventID: 2, Kind: event.KindCommit, Reason: "sha:ccccccc in pr, files overlap: 2"},
			{EventID: 3, Kind: event.KindPR, Reason: "sha:ccccccc in pr"},
		},
	}}
	got = renderGraphText(t, buildSessionGraph(mixed, events, since, until), 70, false)
	want = strings.Join([]string{
		"Session chain",
		"o Codex · Try Lob adapter",
		": files overlap: 2",
		"* ccccccc · Retry webhook delivery",
		"| sha:ccccccc in pr, files overlap: 2",
		"* #12 · Retry webhook delivery",
		"  sha:ccccccc in pr",
	}, "\n")
	if got != want {
		t.Fatalf("mixed chain:\n%s", got)
	}
}

func TestSessionGraphCollapsesNodesAndChains(t *testing.T) {
	since, until := graphWindow()
	var events []store.Record
	var members []store.WorkItemMember
	events = append(events, graphRec(1, "claude-code", "sess", event.KindAgentSession, "Long run", since.Add(time.Hour), ""))
	members = append(members, store.WorkItemMember{EventID: 1, Kind: event.KindAgentSession, Reason: "branch:feature"})
	for i := 0; i < 8; i++ {
		id := int64(i + 2)
		sha := strings.Repeat(string(rune('a'+i)), 40)
		title := "commit-" + string(rune('1'+i))
		events = append(events, graphRec(id, "git", sha, event.KindCommit, title, since.Add(time.Duration(i+2)*time.Hour), ""))
		members = append(members, store.WorkItemMember{EventID: id, Kind: event.KindCommit, Reason: "branch:feature"})
	}
	events = append(events, graphRec(20, "github", "acme/app#9", event.KindPR, "Land it", since.Add(20*time.Hour), "https://github.com/acme/app/pull/9"))
	members = append(members, store.WorkItemMember{EventID: 20, Kind: event.KindPR, Reason: "branch:feature"})
	events = append(events, graphRec(21, "linear", "GEN-9", event.KindTicket, "Land it", since.Add(21*time.Hour), ""))
	members = append(members, store.WorkItemMember{EventID: 21, Kind: event.KindTicket, Reason: "ref:GEN-9 ticket"})

	g := buildSessionGraph([]store.WorkItem{{ID: 1, Events: members}}, events, since, until)
	if g == nil || len(g.Chains) != 1 || g.Chains[0].More == 0 {
		t.Fatalf("chain = %+v", g)
	}
	got := renderGraphText(t, g, 70, false)
	if !strings.Contains(got, "+"+strconv.Itoa(g.Chains[0].More)+" more") {
		t.Fatalf("missing collapse\n%s", got)
	}
	if !strings.Contains(got, "* #9 · Land it") || !strings.Contains(got, "* GEN-9 · Land it") {
		t.Fatalf("endpoints dropped\n%s", got)
	}
	if !strings.Contains(got, "commit-1") || strings.Contains(got, "commit-8") {
		t.Fatalf("collapse kept the wrong commits\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if utf8.RuneCountInString(line) > 70 {
			t.Fatalf("line wider than 70: %q", line)
		}
	}

	var many []store.WorkItem
	var all []store.Record
	for i := 0; i < 6; i++ {
		sid := int64(100 + i*2)
		cid := sid + 1
		when := since.Add(time.Duration(i+1) * time.Hour)
		title := "item-" + string(rune('a'+i))
		all = append(all,
			graphRec(sid, "codex", "s", event.KindAgentSession, title, when, ""),
			graphRec(cid, "git", strings.Repeat("d", 40), event.KindCommit, title, when.Add(time.Minute), ""),
		)
		many = append(many, store.WorkItem{
			ID: int64(i + 1),
			Events: []store.WorkItemMember{
				{EventID: sid, Kind: event.KindAgentSession, Reason: "only session in window"},
				{EventID: cid, Kind: event.KindCommit, Reason: "only session in window"},
			},
		})
	}
	g = buildSessionGraph(many, all, since, until)
	if g == nil || len(g.Chains) != maxGraphChains || g.More != 2 {
		t.Fatalf("chains = %d more = %d", len(g.Chains), g.More)
	}
	got = renderGraphText(t, g, 70, false)
	if !strings.Contains(got, "+2 more") {
		t.Fatalf("missing chain overflow\n%s", got)
	}
	// Newest sessions are item-f and item-e. The oldest, item-a, is omitted.
	if !strings.Contains(got, "item-f") || strings.Contains(got, "item-a") {
		t.Fatalf("chain order\n%s", got)
	}
}

func TestSessionGraphSkipsUnlinkedAndOutsideWindow(t *testing.T) {
	since, until := graphWindow()
	events := []store.Record{
		graphRec(1, "claude-code", "alone", event.KindAgentSession, "Notes", since.Add(time.Hour), ""),
		graphRec(2, "claude-code", "early", event.KindAgentSession, "Before", since.Add(-time.Hour), ""),
		graphRec(3, "git", strings.Repeat("e", 40), event.KindCommit, "Inside", since.Add(2*time.Hour), ""),
		graphRec(4, "codex", "soft", event.KindAgentSession, "Spike", since.Add(3*time.Hour), ""),
		graphRec(5, "git", strings.Repeat("f", 40), event.KindCommit, "Outside", until.Add(time.Hour), ""),
	}
	items := []store.WorkItem{
		{ID: 1, Events: []store.WorkItemMember{
			{EventID: 1, Kind: event.KindAgentSession, Reason: "session only"},
		}},
		{ID: 2, Events: []store.WorkItemMember{
			{EventID: 2, Kind: event.KindAgentSession, Reason: "files overlap: 1"},
			{EventID: 3, Kind: event.KindCommit, Reason: "files overlap: 1"},
		}},
		{ID: 3, Events: []store.WorkItemMember{
			{EventID: 4, Kind: event.KindAgentSession, Reason: "git branch:wip"},
			{EventID: 5, Kind: event.KindCommit, Reason: "git branch:wip"},
		}},
		{ID: 4, Events: []store.WorkItemMember{
			{EventID: 1, Kind: event.KindAgentSession, Reason: "leftover:acme/app 2026-10-08"},
			{EventID: 3, Kind: event.KindCommit, Reason: "leftover:acme/app 2026-10-08"},
		}},
	}
	g := buildSessionGraph(items, events, since, until)
	if g != nil {
		t.Fatalf("unlinked graph = %+v\n%s", g, renderGraphText(t, g, 70, false))
	}
}

func TestSessionGraphIssueAndOverride(t *testing.T) {
	since, until := graphWindow()
	events := []store.Record{
		graphRec(1, "claude-code", "s", event.KindAgentSession, "File the bug", since.Add(time.Hour), ""),
		graphRec(2, "github", "acme/app#4", event.KindIssue, "Login loop", since.Add(2*time.Hour), "https://github.com/acme/app/issues/4"),
	}
	g := buildSessionGraph([]store.WorkItem{{
		ID: 7,
		Events: []store.WorkItemMember{
			{EventID: 1, Kind: event.KindAgentSession, Reason: "override:link"},
			{EventID: 2, Kind: event.KindIssue, Reason: "override:link"},
		},
	}}, events, since, until)
	got := renderGraphText(t, g, 70, false)
	want := strings.Join([]string{
		"Session chain",
		"* Claude · File the bug",
		"| override:link",
		"* #4 · Login loop",
		"  override:link",
	}, "\n")
	if got != want {
		t.Fatalf("issue chain:\n%s", got)
	}
}

func TestSessionGraphFits80Columns(t *testing.T) {
	since, until := graphWindow()
	long := strings.Repeat("very-long-title ", 12)
	reason := strings.Repeat("ref:GEN-341 in a very long prompt ", 8)
	events := []store.Record{
		graphRec(1, "claude-code", "s", event.KindAgentSession, long, since.Add(time.Hour), ""),
		graphRec(2, "git", strings.Repeat("a", 40), event.KindCommit, long, since.Add(2*time.Hour), ""),
		graphRec(3, "github", "acme/app#5", event.KindPR, long, since.Add(3*time.Hour), "https://github.com/acme/app/pull/5"),
	}
	items := []store.WorkItem{{
		ID: 1,
		Events: []store.WorkItemMember{
			{EventID: 1, Kind: event.KindAgentSession, Reason: reason},
			{EventID: 2, Kind: event.KindCommit, Reason: reason},
			{EventID: 3, Kind: event.KindPR, Reason: reason},
		},
	}}
	rep := Report{
		Heading: "Week 2026-W41 (Oct 5 – Oct 11)",
		Since:   since,
		Until:   until,
		Summary: Summary{Line: "**Shipped 1** · ~1h agent time · 1 commit · 1 PR merged"},
	}
	for _, width := range []int{DefaultRecapWidth, 78} {
		body := RenderRecap(BuildRecap(rep, events, items, time.UTC), RenderOptions{Width: width, Color: false})
		if strings.Contains(body, "\x1b") {
			t.Fatalf("plain recap has ANSI at width %d", width)
		}
		limit := width + 2 // border sits outside the lipgloss width
		for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
			if n := utf8.RuneCountInString(line); n > limit || n > 80 {
				t.Fatalf("width %d line is %d runes: %s", width, n, line)
			}
		}
		if !strings.Contains(body, "Session chain") || !strings.Contains(body, "…") {
			t.Fatalf("long text was not truncated\n%s", body)
		}
		if !strings.Contains(body, "* #5") {
			t.Fatalf("pull request dropped\n%s", body)
		}
	}
}

func TestPlainRecapDrawsGraph(t *testing.T) {
	recap := goldenRecap(t)
	plain := RenderRecap(recap, RenderOptions{Width: DefaultRecapWidth, Color: false})
	styled := RenderRecap(recap, RenderOptions{Width: DefaultRecapWidth, Color: true})
	if strings.Contains(plain, "\x1b") {
		t.Fatal("plain recap contains an escape")
	}
	if stripANSI(styled) != plain {
		t.Fatal("plain recap dropped graph styling text")
	}
	for _, want := range []string{"Session chain", "* Claude · Admin gate", "| ref:GEN-341 in prompt"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain recap missing %q\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "o Codex") || strings.Contains(plain, ": files overlap") {
		t.Fatalf("golden hard chain marked soft\n%s", plain)
	}
	assertReasonDimmed(t, styled, "ref:GEN-341 in prompt")
}

func graphWindow() (time.Time, time.Time) {
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	return since, since.AddDate(0, 0, 7)
}

func graphRec(id int64, source, sourceID string, kind event.Kind, title string, ts time.Time, url string) store.Record {
	return store.Record{
		ID: id,
		Event: event.Event{
			Source:   source,
			SourceID: sourceID,
			Kind:     kind,
			Title:    title,
			TS:       ts,
			URL:      url,
		},
	}
}

func renderGraphText(t *testing.T, g *Graph, width int, color bool) string {
	t.Helper()
	if g == nil {
		t.Fatal("nil graph")
	}
	body := RenderRecap(Recap{Heading: "Week", Graph: g}, RenderOptions{Width: width, Color: color})
	return graphPanel(t, body, color)
}

func graphPanel(t *testing.T, body string, color bool) string {
	t.Helper()
	if color {
		body = stripANSI(body)
	}
	var rows []string
	seen := false
	for _, line := range strings.Split(body, "\n") {
		text, ok := recapRow(line)
		if !ok {
			continue
		}
		if !seen {
			if text == "Session chain" {
				seen = true
				rows = append(rows, text)
			}
			continue
		}
		switch text {
		case "Top repos", "Days", "Sources", "Top PRs":
			return joinGraphRows(rows)
		}
		rows = append(rows, text)
	}
	if !seen {
		t.Fatalf("graph panel missing\n%s", body)
	}
	return joinGraphRows(rows)
}

func joinGraphRows(rows []string) string {
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return strings.Join(rows, "\n")
}

// recapRow returns one content line from a rendered recap. The border and
// the single cell of padding are removed. ok is false for the rounded
// border itself.
func recapRow(line string) (string, bool) {
	line = stripANSI(line)
	if line == "" || strings.Contains(line, "╭") || strings.Contains(line, "╰") {
		return "", false
	}
	start := strings.Index(line, "│")
	end := strings.LastIndex(line, "│")
	if start < 0 || end <= start {
		return "", false
	}
	inner := line[start+len("│") : end]
	inner = strings.TrimPrefix(inner, " ")
	return strings.TrimRight(inner, " "), true
}

func assertReasonDimmed(t *testing.T, styled, reason string) {
	t.Helper()
	for _, line := range strings.Split(styled, "\n") {
		if strings.Contains(stripANSI(line), reason) && strings.Contains(line, "\x1b[2") {
			return
		}
	}
	t.Fatalf("reason %q is not dimmed\n%s", reason, styled)
}
