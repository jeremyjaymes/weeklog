package report

import (
	"sort"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

// Recap is the terminal view of one week. RenderRecap draws it. The Markdown
// and JSON reports stay on Report; this value is not part of those documents.
//
// Delta is an optional panel. It stays nil until a later change fills it.
// Graph is filled from linker work items when those items are passed to
// BuildRecap. RenderRecap reserves both places:
//
//   - Delta is the week-over-week line (issue 55). It is drawn under the
//     summary. Actor counts are drawn under that, above the day bars.
//   - Graph is the session → commit → pull-request or issue chain. It is
//     drawn under the source lanes and above the top repositories.
type Recap struct {
	Heading string
	Summary Summary
	Days    []DayStat
	Lanes   []Lane
	Repos   []RepoStat
	PRs     []PRStat
	Delta   *Delta
	Graph   *Graph
}

// DayStat is one local day in the window and how many events landed on it.
type DayStat struct {
	Date  time.Time
	Label string
	Count int
}

// Lane is one source across the days of the window. Counts aligns with Days.
type Lane struct {
	Source string
	Label  string
	Color  string
	Counts []int
	Total  int
}

// RepoStat is one repository touched during the window.
// Other counts events that are not commits, pull requests, or sessions,
// such as reviews. It is shown only when the repository has none of those.
type RepoStat struct {
	Name     string
	Commits  int
	PRs      int
	Sessions int
	Other    int
}

// PRStat is one pull request in the window.
type PRStat struct {
	Number string
	Title  string
	State  string
	Repo   string
	When   time.Time
}

// Delta is the week-over-week comparison. Issue 55 sets it.
type Delta struct {
	// Line is the short comparison, already formatted.
	Line string
}

// Graph is the session → commit → pull-request or issue chain.
// Chains are built from linker work items. Lines is a preformatted fallback
// drawn when Chains is empty, so a caller can still inject a row.
// More is the number of chains omitted after Chains.
type Graph struct {
	// Title is the panel heading. Empty uses "Chain".
	Title  string
	Lines  []string
	Chains []GraphChain
	More   int
}

// WithDelta returns a copy of r that shows the week-over-week line.
func (r Recap) WithDelta(d Delta) Recap {
	r.Delta = &d
	return r
}

// WithGraph returns a copy of r that shows the session chain.
func (r Recap) WithGraph(g Graph) Recap {
	r.Graph = &g
	return r
}

const recapTopN = 5

// knownLaneOrder is the lane sequence for built-in sources. Any other source
// follows, ordered by its display label.
var knownLaneOrder = []string{
	"claude-code",
	"codex",
	"git",
	"github",
	"linear",
}

// knownLaneColors are the stable colors for built-in sources. Other sources
// take a color from extraLaneColors, chosen from the source name so a plugin
// keeps the same color from week to week.
var knownLaneColors = map[string]string{
	"claude-code": "#CC785C",
	"codex":       "#10A37F",
	"git":         "#F05033",
	"github":      "#A78BFA",
	"linear":      "#5E6AD2",
}

var extraLaneColors = []string{
	"#38BDF8",
	"#F472B6",
	"#FACC15",
	"#FB7185",
	"#2DD4BF",
}

// BuildRecap summarizes rep and the stored events that fall in its window.
// Events outside [Since, Until) are ignored. loc buckets events onto days;
// nil means UTC. items are the linker's work items. A nil or empty list
// leaves the session chain hidden.
func BuildRecap(rep Report, events []store.Record, items []store.WorkItem, loc *time.Location) Recap {
	if loc == nil {
		loc = time.UTC
	}
	days := daySpan(rep.Since, rep.Until, loc)
	dayCounts := make([]int, len(days))
	lanes := map[string]*laneAcc{}
	var laneKeys []string
	repos := map[string]*repoAcc{}
	prs := map[string]*prAcc{}
	var prKeys []string

	for _, ev := range events {
		if !inWindow(ev.TS, rep.Since, rep.Until) {
			continue
		}
		idx := dayIndex(days, ev.TS, loc)
		if idx >= 0 {
			dayCounts[idx]++
		}
		key := laneKey(ev.Source)
		lane := lanes[key]
		if lane == nil {
			lane = &laneAcc{
				source: key,
				label:  sourceLabel(ev.Source),
				counts: make([]int, len(days)),
			}
			lanes[key] = lane
			laneKeys = append(laneKeys, key)
		}
		if idx >= 0 {
			lane.counts[idx]++
		}
		lane.total++

		if repo := strings.TrimSpace(ev.Repo); repo != "" {
			acc := repos[repo]
			if acc == nil {
				acc = &repoAcc{name: repoDisplay(repo)}
				repos[repo] = acc
			}
			switch ev.Kind {
			case event.KindCommit:
				acc.commits++
			case event.KindPR:
				acc.prs++
			case event.KindAgentSession:
				acc.sessions++
			default:
				acc.other++
			}
		}
		if ev.Kind == event.KindPR {
			addPR(prs, &prKeys, ev)
		}
	}

	recap := Recap{
		Heading: headingOrWeek(rep),
		Summary: rep.Summary,
		Days:    make([]DayStat, len(days)),
		Lanes:   orderedLanes(lanes, laneKeys),
		Repos:   orderedRepos(repos),
		PRs:     orderedPRs(prs, prKeys),
	}
	for i, day := range days {
		recap.Days[i] = DayStat{
			Date:  day,
			Label: day.Weekday().String()[:3],
			Count: dayCounts[i],
		}
	}
	if g := buildSessionGraph(items, events, rep.Since, rep.Until); g != nil {
		recap.Graph = g
	}
	return recap
}

func headingOrWeek(rep Report) string {
	if text := strings.TrimSpace(rep.Heading); text != "" {
		return text
	}
	if text := strings.TrimSpace(rep.Week); text != "" {
		return "Week " + text
	}
	return "Week"
}

type laneAcc struct {
	source string
	label  string
	counts []int
	total  int
}

type repoAcc struct {
	name     string
	commits  int
	prs      int
	sessions int
	other    int
}

type prAcc struct {
	stat PRStat
}

func laneKey(source string) string {
	key := strings.ToLower(strings.TrimSpace(source))
	if key == "" {
		return "unknown"
	}
	return key
}

func laneColor(source string) string {
	if color, ok := knownLaneColors[source]; ok {
		return color
	}
	if len(extraLaneColors) == 0 {
		return "#38BDF8"
	}
	hash := 0
	for _, r := range source {
		hash = int(r) + 31*hash
	}
	if hash < 0 {
		hash = -hash
	}
	return extraLaneColors[hash%len(extraLaneColors)]
}

func orderedLanes(lanes map[string]*laneAcc, keys []string) []Lane {
	sort.Slice(keys, func(i, j int) bool {
		ri, rj := laneRank(keys[i]), laneRank(keys[j])
		if ri != rj {
			return ri < rj
		}
		li, lj := lanes[keys[i]].label, lanes[keys[j]].label
		if li != lj {
			return li < lj
		}
		return keys[i] < keys[j]
	})
	out := make([]Lane, 0, len(keys))
	for _, key := range keys {
		acc := lanes[key]
		if acc.total == 0 {
			continue
		}
		counts := append([]int(nil), acc.counts...)
		out = append(out, Lane{
			Source: key,
			Label:  acc.label,
			Color:  laneColor(key),
			Counts: counts,
			Total:  acc.total,
		})
	}
	return out
}

func laneRank(source string) int {
	for i, name := range knownLaneOrder {
		if source == name {
			return i
		}
	}
	return len(knownLaneOrder)
}

func orderedRepos(repos map[string]*repoAcc) []RepoStat {
	names := make([]string, 0, len(repos))
	for name := range repos {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := repos[names[i]], repos[names[j]]
		if a.weight() != b.weight() {
			return a.weight() > b.weight()
		}
		if a.commits != b.commits {
			return a.commits > b.commits
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return names[i] < names[j]
	})
	if len(names) > recapTopN {
		names = names[:recapTopN]
	}
	out := make([]RepoStat, 0, len(names))
	for _, name := range names {
		acc := repos[name]
		out = append(out, RepoStat{
			Name:     acc.name,
			Commits:  acc.commits,
			PRs:      acc.prs,
			Sessions: acc.sessions,
			Other:    acc.other,
		})
	}
	return out
}

func (r *repoAcc) weight() int {
	return r.commits + r.prs + r.sessions + r.other
}

func addPR(prs map[string]*prAcc, keys *[]string, ev store.Record) {
	key := prKey(ev)
	next := prAcc{stat: PRStat{
		Number: prNumber(ev),
		Title:  strings.TrimSpace(ev.Title),
		State:  string(ev.State),
		Repo:   repoDisplay(ev.Repo),
		When:   ev.TS,
	}}
	if next.stat.Title == "" {
		next.stat.Title = "(untitled)"
	}
	cur := prs[key]
	if cur == nil {
		prs[key] = &next
		*keys = append(*keys, key)
		return
	}
	if prBetter(next.stat, cur.stat) {
		prs[key] = &next
	}
}

func prKey(ev store.Record) string {
	if n := prNumber(ev); n != "" {
		return strings.TrimSpace(ev.Repo) + "#" + n
	}
	return ev.Source + "/" + ev.SourceID
}

func prBetter(next, cur PRStat) bool {
	nr, cr := stateRank(next.State), stateRank(cur.State)
	if nr != cr {
		return nr < cr
	}
	return next.When.After(cur.When)
}

func stateRank(state string) int {
	switch event.State(state) {
	case event.StateMerged:
		return 0
	case event.StateOpen:
		return 1
	case event.StateClosed:
		return 2
	case event.StateDone:
		return 3
	case event.StateAbandoned:
		return 4
	default:
		return 5
	}
}

func orderedPRs(prs map[string]*prAcc, keys []string) []PRStat {
	sort.Slice(keys, func(i, j int) bool {
		a, b := prs[keys[i]].stat, prs[keys[j]].stat
		ra, rb := stateRank(a.State), stateRank(b.State)
		if ra != rb {
			return ra < rb
		}
		if !a.When.Equal(b.When) {
			return a.When.After(b.When)
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return keys[i] < keys[j]
	})
	if len(keys) > recapTopN {
		keys = keys[:recapTopN]
	}
	out := make([]PRStat, 0, len(keys))
	for _, key := range keys {
		out = append(out, prs[key].stat)
	}
	return out
}

func daySpan(since, until time.Time, loc *time.Location) []time.Time {
	if !until.After(since) {
		return nil
	}
	start := since.In(loc)
	end := until.In(loc)
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	var days []time.Time
	for day.Before(end) {
		days = append(days, day)
		next := day.AddDate(0, 0, 1)
		if !next.After(day) || len(days) >= 366 {
			break
		}
		day = next
	}
	return days
}

func dayIndex(days []time.Time, ts time.Time, loc *time.Location) int {
	local := ts.In(loc)
	y, m, d := local.Date()
	for i, day := range days {
		yy, mm, dd := day.Date()
		if y == yy && m == mm && d == dd {
			return i
		}
	}
	return -1
}

// repoDisplay shortens a stored repository to owner/name. A leading host is
// dropped when the path has at least three parts.
func repoDisplay(repo string) string {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.Trim(repo, "/")
	if repo == "" {
		return ""
	}
	parts := strings.Split(repo, "/")
	if len(parts) >= 3 && strings.Contains(parts[0], ".") {
		parts = parts[1:]
	}
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return parts[len(parts)-1]
}
