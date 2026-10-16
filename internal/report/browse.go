package report

import (
	"sort"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/actor"
	"github.com/jeremyjaymes/weeklog/internal/event"
)

// Browser is the interactive week: one row per local day, the items that
// started that day, and the commits, pull requests, and sessions the linker
// attached to each item.
type Browser struct {
	Heading string
	Days    []BrowserDay
}

// BrowserDay is one local day in the report window.
type BrowserDay struct {
	Date  time.Time
	Label string
	When  string
	Items []BrowserItem
}

// BrowserItem is one unit of work on a day.
// Links holds only linker-attached commits, pull requests, and sessions.
type BrowserItem struct {
	Title   string
	Status  string
	Actor   string
	Repo    string
	Started time.Time
	Links   []BrowserLink
}

// BrowserLink is one linker-attached commit, pull request, or session.
// Soft is true when every stored reason is a soft link. Reason is the
// linker's stored text.
type BrowserLink struct {
	Kind   event.Kind
	Label  string
	Detail string
	Reason string
	Soft   bool
	When   time.Time
}

// BuildBrowser groups rep onto local days. loc nil means UTC. An item lands
// on the local day of its start. Links are the in-window commits, pull
// requests, and sessions whose stored reason is non-empty. Reviews,
// comments, issues, and tickets stay off the link list.
func BuildBrowser(rep Report, loc *time.Location) Browser {
	if loc == nil {
		loc = time.UTC
	}
	days := daySpan(rep.Since, rep.Until, loc)
	out := Browser{
		Heading: headingOrWeek(rep),
		Days:    make([]BrowserDay, len(days)),
	}
	for i, day := range days {
		out.Days[i] = BrowserDay{
			Date:  day,
			Label: day.Weekday().String()[:3],
			When:  day.Format("Jan 2"),
			Items: []BrowserItem{},
		}
	}
	for _, section := range rep.Sections {
		for _, item := range section.Items {
			idx := dayIndex(days, item.Started, loc)
			if idx < 0 {
				continue
			}
			out.Days[idx].Items = append(out.Days[idx].Items, browserItem(item))
		}
	}
	for i := range out.Days {
		sort.SliceStable(out.Days[i].Items, func(a, b int) bool {
			left, right := out.Days[i].Items[a], out.Days[i].Items[b]
			if !left.Started.Equal(right.Started) {
				return left.Started.Before(right.Started)
			}
			if left.Title != right.Title {
				return left.Title < right.Title
			}
			return left.Repo < right.Repo
		})
	}
	return out
}

func browserItem(it Item) BrowserItem {
	title := strings.TrimSpace(it.Title)
	if title == "" {
		title = strings.TrimSpace(it.Heading)
	}
	status := sectionTitle(it.Status)
	if status == "" {
		status = strings.TrimSpace(it.Status)
	}
	item := BrowserItem{
		Title:   title,
		Status:  status,
		Actor:   actor.Label(it.Actor),
		Repo:    strings.TrimSpace(it.RepoName),
		Started: it.Started,
		Links:   browserLinks(it.Members),
	}
	return item
}

func browserLinks(members []Member) []BrowserLink {
	var links []BrowserLink
	for _, member := range members {
		reason := strings.TrimSpace(member.Reason)
		if reason == "" {
			continue
		}
		switch member.Event.Kind {
		case event.KindCommit, event.KindPR, event.KindAgentSession:
		default:
			continue
		}
		hard, soft := classifyReason(reason)
		links = append(links, BrowserLink{
			Kind:   member.Event.Kind,
			Label:  graphLabel(member.Event),
			Detail: strings.TrimSpace(member.Event.Title),
			Reason: reason,
			Soft:   soft && !hard,
			When:   member.Event.TS,
		})
	}
	sort.SliceStable(links, func(i, j int) bool {
		return linkBefore(links[i], links[j])
	})
	if links == nil {
		return []BrowserLink{}
	}
	return links
}

func linkBefore(a, b BrowserLink) bool {
	if !a.When.Equal(b.When) {
		return a.When.Before(b.When)
	}
	if ra, rb := linkRank(a.Kind), linkRank(b.Kind); ra != rb {
		return ra < rb
	}
	if a.Label != b.Label {
		return a.Label < b.Label
	}
	return a.Detail < b.Detail
}

func linkRank(kind event.Kind) int {
	switch kind {
	case event.KindAgentSession:
		return 0
	case event.KindCommit:
		return 1
	case event.KindPR:
		return 2
	default:
		return 3
	}
}
