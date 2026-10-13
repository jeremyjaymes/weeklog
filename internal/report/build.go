package report

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

// Report is one weekly log. Markdown and JSON are rendered from this value.
type Report struct {
	Week        string    `json:"week"`
	Since       time.Time `json:"since"`
	Until       time.Time `json:"until"`
	GeneratedAt time.Time `json:"generated_at"`
	Version     string    `json:"weeklog_version"`
	Heading     string    `json:"heading"`
	Range       string    `json:"range"`
	Summary     Summary   `json:"summary"`
	Sections    []Section `json:"sections"`
	Other       []Other   `json:"other"`
	// OtherLine is the Markdown summary of Other. It is not repeated in JSON.
	OtherLine string `json:"-"`
	// Notes is the preserved manual block. It is Markdown-only.
	Notes string `json:"-"`
}

// Summary counts the week. Line is the Markdown summary, including the
// estimate marker.
type Summary struct {
	Shipped      int    `json:"shipped"`
	InProgress   int    `json:"in_progress"`
	Explored     int    `json:"explored"`
	Abandoned    int    `json:"abandoned"`
	AgentSeconds int    `json:"agent_seconds"`
	Commits      int    `json:"commits"`
	PRsMerged    int    `json:"prs_merged"`
	Line         string `json:"line"`
}

// Section is one status group, in display order.
type Section struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Items []Item `json:"items"`
}

// Item is one unit of work inside a section.
type Item struct {
	ID              int64     `json:"id,omitempty"`
	Title           string    `json:"title"`
	Heading         string    `json:"heading"`
	Status          string    `json:"status"`
	Repo            string    `json:"repo,omitempty"`
	RepoName        string    `json:"repo_name,omitempty"`
	Branch          string    `json:"branch,omitempty"`
	Ticket          string    `json:"ticket,omitempty"`
	Started         time.Time `json:"started"`
	Commits         int       `json:"commits"`
	Sessions        int       `json:"sessions"`
	AgentSeconds    int       `json:"agent_seconds"`
	EstimateSeconds int       `json:"estimate_seconds"`
	Reasons         []string  `json:"reasons,omitempty"`
	Bullets         []string  `json:"bullets"`
}

// Other is one rollup of events that are not rendered as work items.
type Other struct {
	Kind   string `json:"kind"`
	Source string `json:"source,omitempty"`
	Label  string `json:"label"`
	Count  int    `json:"count"`
}

// Options selects the window and the clock used for generated_at.
type Options struct {
	// Since and Until are the half-open window [Since, Until).
	Since time.Time
	Until time.Time
	// Label is an ISO week such as 2026-W41. Empty labels use the date span.
	Label string
	// Location formats dates. Nil means UTC.
	Location *time.Location
	// Now is the generated_at clock. The zero time means time.Now.
	Now time.Time
	// Version is written into the report. Empty means dev.
	Version string
}

// GeneratedAtUTC formats generated_at for the Markdown front matter.
func (r Report) GeneratedAtUTC() string {
	return r.GeneratedAt.UTC().Format(time.RFC3339)
}

// FileBase is the file name without an extension: 2026-W41, or a date span.
func (r Report) FileBase() string {
	name := strings.TrimSpace(r.Week)
	if name == "" {
		name = "week"
	}
	return strings.NewReplacer("..", "--", "/", "-", "\\", "-").Replace(name)
}

// Build loads events and work items from st and renders the week in memory.
func Build(ctx context.Context, st *store.Store, opt Options) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if st == nil {
		return Report{}, errors.New("nil store")
	}
	events, err := st.ListEvents(ctx)
	if err != nil {
		return Report{}, err
	}
	items, err := st.ListWorkItems(ctx)
	if err != nil {
		return Report{}, err
	}
	return assemble(events, items, opt)
}

type member struct {
	store.Record
	Reason string
}

func assemble(events []store.Record, items []store.WorkItem, opt Options) (Report, error) {
	if opt.Since.IsZero() || opt.Until.IsZero() || !opt.Until.After(opt.Since) {
		return Report{}, errors.New("invalid window")
	}
	loc := opt.Location
	if loc == nil {
		loc = time.UTC
	}
	now := opt.Now
	if now.IsZero() {
		now = time.Now()
	}
	version := opt.Version
	if strings.TrimSpace(version) == "" {
		version = "dev"
	}

	byID := make(map[int64]store.Record, len(events))
	for _, ev := range events {
		byID[ev.ID] = ev
	}
	linked := map[int64]struct{}{}
	var built []Item
	for _, item := range items {
		members := make([]member, 0, len(item.Events))
		for _, link := range item.Events {
			rec, ok := byID[link.EventID]
			if !ok {
				return Report{}, fmt.Errorf("work item %d references missing event %d", item.ID, link.EventID)
			}
			linked[rec.ID] = struct{}{}
			members = append(members, member{Record: rec, Reason: link.Reason})
		}
		next, ok := buildLinkedItem(item, members, opt.Since, opt.Until, loc)
		if !ok {
			continue
		}
		built = append(built, next)
	}

	var loose []store.Record
	for _, ev := range events {
		if _, ok := linked[ev.ID]; ok {
			continue
		}
		if !inWindow(ev.TS, opt.Since, opt.Until) {
			continue
		}
		loose = append(loose, ev)
	}
	built = append(built, looseItems(loose, opt.Until, loc)...)

	rep := Report{
		Week:        weekKey(opt.Label, opt.Since, opt.Until, loc),
		Since:       opt.Since.UTC(),
		Until:       opt.Until.UTC(),
		GeneratedAt: now.UTC().Truncate(time.Second),
		Version:     version,
		Range:       humanRange(opt.Since, opt.Until, loc),
		Sections:    []Section{},
		Other:       otherActivity(events, opt.Since, opt.Until),
	}
	rep.Heading = heading(opt.Label, rep.Range)
	rep.OtherLine = joinOther(rep.Other)
	rep.Sections = sectionsOf(built)
	rep.Summary = summarize(rep.Sections, events, opt.Since, opt.Until)
	return rep, nil
}

func inWindow(ts, since, until time.Time) bool {
	return !ts.Before(since) && ts.Before(until)
}

func displayKind(kind event.Kind) bool {
	switch kind {
	case event.KindCommit, event.KindAgentSession, event.KindPR, event.KindTicket:
		return true
	default:
		return false
	}
}

func buildLinkedItem(item store.WorkItem, members []member, since, until time.Time, loc *time.Location) (Item, bool) {
	var inWindowMembers []member
	for _, m := range members {
		if inWindow(m.TS, since, until) && displayKind(m.Kind) {
			inWindowMembers = append(inWindowMembers, m)
		}
	}
	if len(inWindowMembers) == 0 {
		return Item{}, false
	}
	records := make([]store.Record, len(members))
	reasons := make([]string, 0, len(members))
	for i, m := range members {
		records[i] = m.Record
		reasons = append(reasons, m.Reason)
	}
	shown := make([]store.Record, len(inWindowMembers))
	for i, m := range inWindowMembers {
		shown[i] = m.Record
	}
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = chooseTitle(records)
	}
	return newItem(item.ID, title, classify(records, item.Status, until), shown, records, reasons, loc), true
}

func looseItems(events []store.Record, until time.Time, loc *time.Location) []Item {
	var sessions []store.Record
	groups := map[string][]store.Record{}
	var keys []string
	for _, ev := range events {
		if !displayKind(ev.Kind) {
			continue
		}
		if ev.Kind == event.KindAgentSession {
			sessions = append(sessions, ev)
			continue
		}
		key := looseKey(ev, loc)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], ev)
	}
	var items []Item
	for _, ev := range sessions {
		items = append(items, newItem(0, chooseTitle([]store.Record{ev}), statusExplored, []store.Record{ev}, []store.Record{ev}, nil, loc))
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		items = append(items, newItem(0, chooseTitle(group), classify(group, "", until), group, group, nil, loc))
	}
	return items
}

func looseKey(ev store.Record, loc *time.Location) string {
	day := dayKey(ev.TS, loc)
	if ev.Repo != "" {
		return ev.Repo + "\x00" + day
	}
	return string(ev.Kind) + "\x00" + ev.Source + "\x00" + ev.SourceID + "\x00" + day
}

func newItem(id int64, title, status string, shown, all []store.Record, reasons []string, loc *time.Location) Item {
	if len(shown) == 0 {
		shown = all
	}
	sort.Slice(shown, func(i, j int) bool { return eventLess(shown[i], shown[j]) })
	var commits, prs, sessions, tickets []store.Record
	for _, ev := range shown {
		switch ev.Kind {
		case event.KindCommit:
			commits = append(commits, ev)
		case event.KindPR:
			prs = append(prs, ev)
		case event.KindAgentSession:
			sessions = append(sessions, ev)
		case event.KindTicket:
			tickets = append(tickets, ev)
		}
	}
	var agent time.Duration
	for _, ev := range sessions {
		agent += sessionDuration(ev)
	}
	estimate := time.Duration(0)
	if agent == 0 && len(sessions) == 0 {
		estimate = time.Duration(commitClusterCount(commits)) * commitEstimate
	}
	repo := primaryRepo(shown)
	if repo == "" {
		repo = primaryRepo(all)
	}
	name := repoName(repo)
	key := ticketKeyOf(all, reasons)
	reasonList := collectReasons(reasons)
	bullets := itemBullets(prs, commits, sessions, tickets, estimate, name, loc, reasonList)

	it := Item{
		ID:              id,
		Title:           strings.TrimSpace(title),
		Heading:         itemHeading(title, key),
		Status:          status,
		Repo:            repo,
		RepoName:        name,
		Branch:          commonBranch(shown),
		Ticket:          key,
		Started:         shown[0].TS.UTC(),
		Commits:         len(commits),
		Sessions:        len(sessions),
		AgentSeconds:    durationSeconds(agent),
		EstimateSeconds: durationSeconds(estimate),
		Reasons:         reasonList,
		Bullets:         bullets,
	}
	if it.Title == "" {
		it.Title = strings.TrimSuffix(it.Heading, " — "+key)
	}
	return it
}

func itemBullets(prs, commits, sessions, tickets []store.Record, estimate time.Duration, repoName string, loc *time.Location, reasons []string) []string {
	var bullets []string
	for _, ev := range prs {
		bullets = append(bullets, prBullet(ev, loc))
	}
	groups := groupSessions(sessions)
	showRepo := len(prs) == 0 && len(commits) > 0 && repoName != ""
	if line := activityBullet(len(commits), groups, estimate, repoName, showRepo); line != "" {
		bullets = append(bullets, line)
	} else if len(commits) == 0 && len(sessions) == 0 && len(prs) == 0 && len(tickets) > 0 {
		line := plural(len(tickets), "ticket", "tickets")
		if repoName != "" {
			line += " · " + repoName
		}
		bullets = append(bullets, line)
	}
	if line := linkedBullet(reasons); line != "" {
		bullets = append(bullets, line)
	}
	if bullets == nil {
		bullets = []string{}
	}
	return bullets
}

func ticketKeyOf(events []store.Record, reasons []string) string {
	for _, ev := range events {
		if ev.Kind != event.KindTicket {
			continue
		}
		if key := firstTicketKey(ev.SourceID); key != "" && key == strings.TrimSpace(ev.SourceID) {
			return key
		}
		if key := firstTicketKey(ev.Title); key != "" {
			return key
		}
		if key := firstTicketKey(strings.Join(ev.Refs, " ")); key != "" {
			return key
		}
	}
	for _, reason := range reasons {
		if key := firstTicketKey(reason); key != "" {
			return key
		}
	}
	for _, ev := range events {
		for _, ref := range ev.Refs {
			if key := firstTicketKey(ref); key != "" {
				return key
			}
		}
		if ev.Kind == event.KindTicket {
			continue
		}
		if key := firstTicketKey(ev.Title + " " + ev.Summary + " " + ev.Branch); key != "" {
			return key
		}
	}
	return ""
}

func sectionsOf(items []Item) []Section {
	order := []string{statusShipped, statusInProgress, statusExplored, statusAbandoned}
	buckets := map[string][]Item{}
	for _, item := range items {
		status := item.Status
		if sectionTitle(status) == "" {
			status = statusExplored
			item.Status = status
		}
		buckets[status] = append(buckets[status], item)
	}
	var sections []Section
	for _, status := range order {
		group := buckets[status]
		if len(group) == 0 {
			continue
		}
		sortItems(group)
		sections = append(sections, Section{
			ID:    status,
			Title: sectionTitle(status),
			Items: group,
		})
	}
	if sections == nil {
		sections = []Section{}
	}
	return sections
}

func sortItems(items []Item) {
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		an := strings.ToLower(a.RepoName)
		bn := strings.ToLower(b.RepoName)
		if an != bn {
			if an == "" {
				return false
			}
			if bn == "" {
				return true
			}
			return an < bn
		}
		if at, bt := a.timeSpent(), b.timeSpent(); at != bt {
			return at > bt
		}
		if !a.Started.Equal(b.Started) {
			return a.Started.Before(b.Started)
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.ID < b.ID
	})
}

func (it Item) timeSpent() int {
	if it.AgentSeconds > 0 {
		return it.AgentSeconds
	}
	return it.EstimateSeconds
}

func summarize(sections []Section, events []store.Record, since, until time.Time) Summary {
	var sum Summary
	var agent time.Duration
	for _, section := range sections {
		switch section.ID {
		case statusShipped:
			sum.Shipped = len(section.Items)
		case statusInProgress:
			sum.InProgress = len(section.Items)
		case statusExplored:
			sum.Explored = len(section.Items)
		case statusAbandoned:
			sum.Abandoned = len(section.Items)
		}
	}
	for _, ev := range events {
		if !inWindow(ev.TS, since, until) {
			continue
		}
		switch ev.Kind {
		case event.KindCommit:
			sum.Commits++
		case event.KindPR:
			if ev.State == event.StateMerged {
				sum.PRsMerged++
			}
		case event.KindAgentSession:
			agent += sessionDuration(ev)
		}
	}
	sum.AgentSeconds = durationSeconds(agent)
	sum.Line = summaryLine(sum, agent)
	return sum
}

type otherAcc struct {
	kind   event.Kind
	source string
	order  int
	count  int
}

func otherActivity(events []store.Record, since, until time.Time) []Other {
	acc := map[string]*otherAcc{}
	var keys []string
	for _, ev := range events {
		if !inWindow(ev.TS, since, until) || displayKind(ev.Kind) {
			continue
		}
		order, source := otherOrder(ev)
		key := fmt.Sprintf("%d\x00%s\x00%s", order, ev.Kind, source)
		g := acc[key]
		if g == nil {
			g = &otherAcc{kind: ev.Kind, source: source, order: order}
			acc[key] = g
			keys = append(keys, key)
		}
		g.count++
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := acc[keys[i]], acc[keys[j]]
		if a.order != b.order {
			return a.order < b.order
		}
		if a.source != b.source {
			return sourceLabel(a.source) < sourceLabel(b.source)
		}
		return a.kind < b.kind
	})
	out := make([]Other, 0, len(keys))
	for _, key := range keys {
		g := acc[key]
		out = append(out, Other{
			Kind:   string(g.kind),
			Source: g.source,
			Label:  otherPhrase(g.kind, g.source, g.count),
			Count:  g.count,
		})
	}
	return out
}

func otherOrder(ev store.Record) (int, string) {
	switch ev.Kind {
	case event.KindPRReview:
		return 0, ""
	case event.KindComment:
		return 1, ev.Source
	case event.KindIssue:
		return 2, ""
	case event.KindTicketTransition:
		return 3, ""
	default:
		return 4, ""
	}
}

func joinOther(items []Other) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = item.Label
	}
	return strings.Join(parts, " · ")
}
