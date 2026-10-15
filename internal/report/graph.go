package report

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

const (
	// maxGraphChains is how many work items the panel draws. The rest are
	// one "+N more" row.
	maxGraphChains = 4
	// maxGraphNodes is how many events one chain draws before the middle
	// collapses. The session stays at the head and the pull request or
	// issue stays at the tail.
	maxGraphNodes = 6

	graphTitle = "Session chain"
)

// GraphChain is one work item drawn like git log --graph.
// Head is the visible prefix and Tail is the visible suffix. More is the
// number of events omitted between them. More is zero when Tail is empty.
type GraphChain struct {
	Head []GraphNode
	Tail []GraphNode
	More int
}

// GraphNode is one event in a session chain.
// Soft is true when every stored reason is a soft link. A hard reason,
// including one mixed with a soft reason, leaves Soft false.
// Reason is the linker's stored text. The renderer draws it dimmed.
type GraphNode struct {
	Label  string
	Detail string
	Reason string
	Soft   bool
	Kind   event.Kind
}

type graphMember struct {
	rec    store.Record
	reason string
	soft   bool
	when   time.Time
}

type graphDraft struct {
	when  time.Time
	id    int64
	chain GraphChain
}

// buildSessionGraph turns linker work items into the recap chain.
// An item is included when the window holds an agent session and at least
// one commit, pull request, issue, or ticket, and a hard or soft reason
// ties them together. Leftover groups and sessions left on their own are
// omitted. Events outside [since, until) are omitted.
func buildSessionGraph(items []store.WorkItem, events []store.Record, since, until time.Time) *Graph {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[int64]store.Record, len(events))
	for _, ev := range events {
		byID[ev.ID] = ev
	}
	var drafts []graphDraft
	for _, item := range items {
		nodes, when := chainNodes(item, byID, since, until)
		if len(nodes) == 0 {
			continue
		}
		head, tail, more := collapseNodes(nodes)
		drafts = append(drafts, graphDraft{
			when:  when,
			id:    item.ID,
			chain: GraphChain{Head: head, Tail: tail, More: more},
		})
	}
	if len(drafts) == 0 {
		return nil
	}
	// Newest session first, matching git log. Equal times stay in work-item
	// id order so the panel does not move between renders.
	sort.Slice(drafts, func(i, j int) bool {
		return draftBefore(drafts[i], drafts[j])
	})
	more := 0
	if len(drafts) > maxGraphChains {
		more = len(drafts) - maxGraphChains
		drafts = drafts[:maxGraphChains]
	}
	chains := make([]GraphChain, len(drafts))
	for i, draft := range drafts {
		chains[i] = draft.chain
	}
	return &Graph{Title: graphTitle, Chains: chains, More: more}
}

func draftBefore(a, b graphDraft) bool {
	if !a.when.Equal(b.when) {
		return a.when.After(b.when)
	}
	return a.id < b.id
}

func chainNodes(item store.WorkItem, byID map[int64]store.Record, since, until time.Time) ([]GraphNode, time.Time) {
	var sessions, commits, prs, issues []graphMember
	linked := false
	for _, member := range item.Events {
		rec, ok := byID[member.EventID]
		if !ok {
			rec = store.Record{
				ID: member.EventID,
				Event: event.Event{
					Source:   member.Source,
					SourceID: member.SourceID,
					Kind:     member.Kind,
				},
			}
		}
		if !inWindow(rec.TS, since, until) {
			continue
		}
		hard, soft := classifyReason(member.Reason)
		if hard || soft {
			linked = true
		}
		gm := graphMember{
			rec:    rec,
			reason: strings.TrimSpace(member.Reason),
			soft:   soft && !hard,
			when:   rec.TS,
		}
		switch rec.Kind {
		case event.KindAgentSession:
			sessions = append(sessions, gm)
		case event.KindCommit:
			commits = append(commits, gm)
		case event.KindPR:
			prs = append(prs, gm)
		case event.KindIssue, event.KindTicket:
			issues = append(issues, gm)
		}
	}
	if len(sessions) == 0 || len(commits)+len(prs)+len(issues) == 0 || !linked {
		return nil, time.Time{}
	}
	sortMembers(sessions)
	sortMembers(commits)
	sortMembers(prs)
	sortMembers(issues)
	when := sessions[0].when
	nodes := make([]GraphNode, 0, len(sessions)+len(commits)+len(prs)+len(issues))
	for _, group := range [][]graphMember{sessions, commits, prs, issues} {
		for _, gm := range group {
			nodes = append(nodes, nodeFrom(gm))
		}
	}
	return nodes, when
}

func sortMembers(members []graphMember) {
	sort.Slice(members, func(i, j int) bool {
		return memberBefore(members[i], members[j])
	})
}

func memberBefore(a, b graphMember) bool {
	if !a.when.Equal(b.when) {
		return a.when.Before(b.when)
	}
	if a.rec.Source != b.rec.Source {
		return a.rec.Source < b.rec.Source
	}
	if a.rec.SourceID != b.rec.SourceID {
		return a.rec.SourceID < b.rec.SourceID
	}
	return a.rec.ID < b.rec.ID
}

func nodeFrom(gm graphMember) GraphNode {
	return GraphNode{
		Label:  graphLabel(gm.rec),
		Detail: strings.TrimSpace(gm.rec.Title),
		Reason: gm.reason,
		Soft:   gm.soft,
		Kind:   gm.rec.Kind,
	}
}

func graphLabel(ev store.Record) string {
	switch ev.Kind {
	case event.KindAgentSession:
		return sessionGraphLabel(ev.Source)
	case event.KindCommit:
		return shortGraphID(ev.SourceID, 7)
	case event.KindPR:
		if n := prNumber(ev); n != "" {
			return "#" + n
		}
		return "PR"
	case event.KindIssue:
		if n := prNumber(ev); n != "" {
			return "#" + n
		}
		return shortGraphID(ev.SourceID, 16)
	case event.KindTicket:
		return shortGraphID(ev.SourceID, 16)
	default:
		return "event"
	}
}

func sessionGraphLabel(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "claude-code", "claude":
		return "Claude"
	case "codex":
		return "Codex"
	default:
		label := sourceLabel(source)
		if label == "" || label == "session" {
			return "session"
		}
		return label
	}
}

func shortGraphID(id string, n int) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "event"
	}
	if n >= 7 && len(id) >= 7 && isHexID(id) {
		return id[:7]
	}
	if utf8.RuneCountInString(id) <= n {
		return id
	}
	return truncate(id, n)
}

func isHexID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// collapseNodes keeps the chain readable. The head keeps the session and the
// earliest commits. The tail keeps the pull request and issue when both end
// the chain. More is the number of events left out of the middle.
func collapseNodes(nodes []GraphNode) (head, tail []GraphNode, more int) {
	if len(nodes) <= maxGraphNodes {
		return append([]GraphNode{}, nodes...), nil, 0
	}
	tailN := 1
	if len(nodes) >= 2 && isGraphEndpoint(nodes[len(nodes)-1].Kind) && isGraphEndpoint(nodes[len(nodes)-2].Kind) {
		tailN = 2
	}
	if tailN >= maxGraphNodes {
		tailN = maxGraphNodes - 1
	}
	headN := maxGraphNodes - tailN
	if headN < 1 {
		headN = 1
	}
	if headN+tailN >= len(nodes) {
		return append([]GraphNode{}, nodes...), nil, 0
	}
	more = len(nodes) - headN - tailN
	return append([]GraphNode{}, nodes[:headN]...), append([]GraphNode{}, nodes[len(nodes)-tailN:]...), more
}

func isGraphEndpoint(kind event.Kind) bool {
	return kind == event.KindPR || kind == event.KindIssue || kind == event.KindTicket
}

// classifyReason reports whether the stored reason list contains a hard link,
// a soft link, or both. Reasons are separated by ", " the same way the linker
// stores them. Leftovers and session-only rows are neither.
func classifyReason(reason string) (hard, soft bool) {
	for _, part := range strings.Split(reason, ", ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch {
		case softReason(part):
			soft = true
		case hardReason(part):
			hard = true
		}
	}
	return hard, soft
}

func softReason(part string) bool {
	switch {
	case strings.HasPrefix(part, "files overlap:"):
		return true
	case strings.HasPrefix(part, "head branch:"):
		return true
	case strings.HasPrefix(part, "only session"):
		return true
	case strings.HasPrefix(part, "git branch:"):
		return true
	case strings.HasPrefix(part, "revert:"):
		return true
	default:
		return false
	}
}

func hardReason(part string) bool {
	switch {
	case strings.HasPrefix(part, "ref:"):
		return true
	case strings.HasPrefix(part, "sha:"):
		return true
	case strings.HasPrefix(part, "branch:"):
		return true
	case strings.HasPrefix(part, "lineage:"):
		return true
	case part == "override:link":
		return true
	default:
		return false
	}
}
