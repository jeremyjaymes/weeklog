package report

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

const (
	// commitEstimate is the stand-in for a cluster of commits that has no
	// agent session.
	commitEstimate = 15 * time.Minute
	// clusterGap splits commit clusters. Commits further apart than this are
	// separate estimates.
	clusterGap = 2 * time.Hour
)

var (
	ticketKeyPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)
	prNumberPattern  = regexp.MustCompile(`(?:^|/)(?:pull|pulls)/(\d+)(?:/|$|\?)`)
)

func humanRange(since, until time.Time, loc *time.Location) string {
	start := since.In(loc)
	end := until.In(loc).Add(-time.Nanosecond)
	if end.Before(start) {
		end = start
	}
	if start.Year() != end.Year() {
		return start.Format("Jan 2, 2006") + " – " + end.Format("Jan 2, 2006")
	}
	return start.Format("Jan 2") + " – " + end.Format("Jan 2")
}

func weekKey(label string, since, until time.Time, loc *time.Location) string {
	if strings.TrimSpace(label) != "" {
		return strings.TrimSpace(label)
	}
	return since.In(loc).Format("2006-01-02") + ".." + until.In(loc).Format("2006-01-02")
}

func heading(label, dates string) string {
	if strings.TrimSpace(label) != "" {
		return "Week " + strings.TrimSpace(label) + " (" + dates + ")"
	}
	return "Week " + dates
}

func summaryLine(sum Summary, agent time.Duration) string {
	counts := fmt.Sprintf("Shipped %d · In progress %d · Explored %d", sum.Shipped, sum.InProgress, sum.Explored)
	if sum.Abandoned > 0 {
		counts += fmt.Sprintf(" · Abandoned %d", sum.Abandoned)
	}
	return fmt.Sprintf("**%s** · ~%s agent time · %s · %s",
		counts,
		formatDuration(agent),
		plural(sum.Commits, "commit", "commits"),
		prMergedPhrase(sum.PRsMerged),
	)
}

func prMergedPhrase(n int) string {
	if n == 1 {
		return "1 PR merged"
	}
	return fmt.Sprintf("%d PRs merged", n)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	mins := int(d.Round(time.Minute) / time.Minute)
	h := mins / 60
	m := mins % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func durationSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d.Round(time.Second) / time.Second)
}

func sourceLabel(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "claude-code":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "github":
		return "GitHub"
	case "linear":
		return "Linear"
	case "git":
		return "Git"
	case "":
		return "session"
	default:
		return strings.TrimSpace(name)
	}
}

func repoName(repo string) string {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.Trim(repo, "/")
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		repo = repo[i+1:]
	}
	return repo
}

func dayKey(ts time.Time, loc *time.Location) string {
	return ts.In(loc).Format("2006-01-02")
}

func formatDay(ts time.Time, loc *time.Location) string {
	return ts.In(loc).Format("Jan 2")
}

func sessionDuration(ev store.Record) time.Duration {
	if ev.Kind != event.KindAgentSession || ev.TSEnd == nil {
		return 0
	}
	d := ev.TSEnd.Sub(ev.TS)
	if d < 0 {
		return 0
	}
	return d
}

func commitClusterCount(commits []store.Record) int {
	if len(commits) == 0 {
		return 0
	}
	times := make([]time.Time, len(commits))
	for i, ev := range commits {
		times[i] = ev.TS
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	n := 1
	for i := 1; i < len(times); i++ {
		if times[i].Sub(times[i-1]) > clusterGap {
			n++
		}
	}
	return n
}

func prNumber(ev store.Record) string {
	if ev.URL != "" {
		if n := prNumberInURL(ev.URL); n != "" {
			return n
		}
	}
	if i := strings.LastIndex(ev.SourceID, "#"); i >= 0 {
		rest := ev.SourceID[i+1:]
		if n, err := strconv.Atoi(rest); err == nil && n > 0 {
			return strconv.Itoa(n)
		}
	}
	for _, ref := range ev.Refs {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(ref, "#") {
			if n, err := strconv.Atoi(strings.TrimPrefix(ref, "#")); err == nil && n > 0 {
				return strconv.Itoa(n)
			}
		}
	}
	return ""
}

func prNumberInURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if m := prNumberPattern.FindStringSubmatch(u.Path); m != nil {
		return m[1]
	}
	return ""
}

func prBullet(ev store.Record, loc *time.Location) string {
	var parts []string
	label := "PR"
	if n := prNumber(ev); n != "" && ev.URL != "" {
		label += " [#" + n + "](" + ev.URL + ")"
	} else if n != "" {
		label += " #" + n
	}
	parts = append(parts, label)
	if state := stateWord(ev.State); state != "" {
		parts = append(parts, state)
	}
	parts = append(parts, formatDay(ev.TS, loc))
	line := strings.Join(parts, " ")
	if name := repoName(ev.Repo); name != "" {
		line += " · " + name
	}
	return line
}

func stateWord(state event.State) string {
	switch state {
	case event.StateMerged, event.StateOpen, event.StateClosed, event.StateDone, event.StateAbandoned:
		return string(state)
	default:
		return ""
	}
}

type sessionGroup struct {
	label    string
	count    int
	duration time.Duration
	first    time.Time
}

func groupSessions(sessions []store.Record) []sessionGroup {
	by := map[string]*sessionGroup{}
	var order []string
	for _, ev := range sessions {
		label := sourceLabel(ev.Source)
		g := by[label]
		if g == nil {
			g = &sessionGroup{label: label, first: ev.TS}
			by[label] = g
			order = append(order, label)
		}
		g.count++
		g.duration += sessionDuration(ev)
		if ev.TS.Before(g.first) {
			g.first = ev.TS
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if !by[order[i]].first.Equal(by[order[j]].first) {
			return by[order[i]].first.Before(by[order[j]].first)
		}
		return order[i] < order[j]
	})
	out := make([]sessionGroup, len(order))
	for i, label := range order {
		out[i] = *by[label]
	}
	return out
}

func activityBullet(commits int, groups []sessionGroup, estimate time.Duration, repo string, showRepo bool) string {
	if commits == 0 && len(groups) == 0 {
		return ""
	}
	var parts []string
	if commits > 0 {
		text := plural(commits, "commit", "commits")
		if len(groups) == 0 && estimate > 0 {
			text += " (~" + formatDuration(estimate) + " estimate)"
		}
		parts = append(parts, text)
	}
	for _, g := range groups {
		one := g.label + " session"
		many := g.label + " sessions"
		text := plural(g.count, one, many)
		if g.duration > 0 {
			text += " (" + formatDuration(g.duration) + ")"
		}
		parts = append(parts, text)
	}
	line := strings.Join(parts, " · ")
	if commits == 0 {
		line += ", no commits"
	}
	if showRepo && repo != "" {
		line += " · " + repo
	}
	return line
}

func linkedBullet(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	quoted := make([]string, len(reasons))
	for i, reason := range reasons {
		quoted[i] = "`" + reason + "`"
	}
	return "Linked by: " + strings.Join(quoted, ", ")
}

func collectReasons(reasons []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, reason := range reasons {
		for _, part := range strings.Split(reason, ", ") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := reasonRank(out[i]), reasonRank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}

func reasonRank(reason string) int {
	switch {
	case strings.HasPrefix(reason, "ref:"):
		return 0
	case strings.HasPrefix(reason, "sha:"):
		return 1
	case strings.HasPrefix(reason, "branch:"):
		return 2
	case strings.HasPrefix(reason, "lineage:"):
		return 3
	case strings.HasPrefix(reason, "files overlap"):
		return 4
	case strings.HasPrefix(reason, "override:"):
		return 6
	default:
		return 5
	}
}

func firstTicketKey(text string) string {
	return ticketKeyPattern.FindString(text)
}

func itemHeading(title, key string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Untitled"
	}
	if key == "" || strings.Contains(title, key) {
		return title
	}
	return title + " — " + key
}

func chooseTitle(events []store.Record) string {
	ordered := append([]store.Record(nil), events...)
	sort.Slice(ordered, func(i, j int) bool { return eventLess(ordered[i], ordered[j]) })
	if title := firstKindTitle(ordered, event.KindTicket); title != "" {
		return title
	}
	if title := firstKindTitle(ordered, event.KindPR); title != "" {
		return title
	}
	if title := commonCommitTitle(ordered); title != "" {
		return title
	}
	for _, ev := range ordered {
		if ev.Kind != event.KindAgentSession {
			continue
		}
		if title := strings.TrimSpace(ev.Title); title != "" {
			return title
		}
		if line := firstLine(ev.Summary); line != "" {
			return line
		}
	}
	for _, ev := range ordered {
		if title := strings.TrimSpace(ev.Title); title != "" {
			return title
		}
	}
	return ""
}

func firstKindTitle(events []store.Record, kind event.Kind) string {
	for _, ev := range events {
		if ev.Kind != kind {
			continue
		}
		if title := strings.TrimSpace(ev.Title); title != "" {
			return title
		}
	}
	return ""
}

func commonCommitTitle(events []store.Record) string {
	type stat struct {
		n     int
		first store.Record
	}
	stats := map[string]*stat{}
	var best string
	var bestStat *stat
	for _, ev := range events {
		if ev.Kind != event.KindCommit {
			continue
		}
		title := strings.TrimSpace(ev.Title)
		if title == "" {
			continue
		}
		st := stats[title]
		if st == nil {
			st = &stat{first: ev}
			stats[title] = st
		}
		st.n++
		if bestStat == nil || st.n > bestStat.n || (st.n == bestStat.n && eventLess(st.first, bestStat.first)) {
			best = title
			bestStat = st
		}
	}
	return best
}

func eventLess(a, b store.Record) bool {
	if !a.TS.Equal(b.TS) {
		return a.TS.Before(b.TS)
	}
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	if a.SourceID != b.SourceID {
		return a.SourceID < b.SourceID
	}
	return a.ID < b.ID
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func primaryRepo(events []store.Record) string {
	counts := map[string]int{}
	for _, ev := range events {
		if ev.Repo == "" {
			continue
		}
		counts[ev.Repo]++
	}
	best := ""
	bestN := 0
	for repo, n := range counts {
		if n > bestN || (n == bestN && (best == "" || repo < best)) {
			best = repo
			bestN = n
		}
	}
	return best
}

func commonBranch(events []store.Record) string {
	branch := ""
	set := false
	for _, ev := range events {
		b := normBranch(ev.Branch)
		if b == "" {
			continue
		}
		if !set {
			branch = b
			set = true
			continue
		}
		if branch != b {
			return ""
		}
	}
	return branch
}

func otherPhrase(kind event.Kind, source string, n int) string {
	switch kind {
	case event.KindPRReview:
		return plural(n, "PR review", "PR reviews")
	case event.KindComment:
		label := sourceLabel(source)
		return plural(n, label+" comment", label+" comments")
	case event.KindIssue:
		return plural(n, "issue", "issues")
	case event.KindTicketTransition:
		return plural(n, "ticket update", "ticket updates")
	default:
		name := strings.TrimSpace(string(kind))
		if name == "" {
			name = "event"
		}
		return plural(n, name, name+"s")
	}
}
