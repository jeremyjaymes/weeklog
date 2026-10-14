package linear

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

const maxTitleRunes = 200

type user struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

type workflowState struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type historyConn struct {
	PageInfo pageInfo          `json:"pageInfo"`
	Nodes    []json.RawMessage `json:"nodes"`
}

type historyNode struct {
	ID        string         `json:"id"`
	CreatedAt string         `json:"createdAt"`
	From      *workflowState `json:"fromState"`
	To        *workflowState `json:"toState"`
	Actor     *user          `json:"actor"`
}

type issueItem struct {
	ID          string         `json:"id"`
	Identifier  string         `json:"identifier"`
	Title       string         `json:"title"`
	Description *string        `json:"description"`
	URL         *string        `json:"url"`
	BranchName  *string        `json:"branchName"`
	CreatedAt   string         `json:"createdAt"`
	UpdatedAt   string         `json:"updatedAt"`
	CompletedAt *string        `json:"completedAt"`
	CanceledAt  *string        `json:"canceledAt"`
	State       *workflowState `json:"state"`
	Creator     *user          `json:"creator"`
	Assignee    *user          `json:"assignee"`
	History     historyConn    `json:"history"`
}

func decodeIssue(raw json.RawMessage) (issueItem, skipKind) {
	if isNull(raw) {
		return issueItem{}, decodedSkip
	}
	var head struct {
		TypeName string `json:"__typename"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return issueItem{}, decodedMalformed
	}
	if head.TypeName != "" && head.TypeName != "Issue" {
		return issueItem{}, decodedUnexpected
	}
	var item issueItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return issueItem{}, decodedMalformed
	}
	item.ID = strings.TrimSpace(item.ID)
	item.Identifier = strings.TrimSpace(item.Identifier)
	if !validKey(item.Identifier) || strings.TrimSpace(item.UpdatedAt) == "" {
		return issueItem{}, decodedMalformed
	}
	return item, decodedOK
}

func decodeHistory(raw json.RawMessage) (historyNode, skipKind) {
	if isNull(raw) {
		return historyNode{}, decodedSkip
	}
	var node historyNode
	if err := json.Unmarshal(raw, &node); err != nil {
		return historyNode{}, decodedMalformed
	}
	node.ID = strings.TrimSpace(node.ID)
	if !validKey(node.ID) || strings.TrimSpace(node.CreatedAt) == "" {
		return historyNode{}, decodedMalformed
	}
	return node, decodedOK
}

func validKey(s string) bool {
	if s == "" || strings.ContainsAny(s, "#/:") {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool {
		return r <= ' ' || r == 0x7f
	}) < 0
}

func isNull(raw json.RawMessage) bool {
	text := strings.TrimSpace(string(raw))
	return text == "" || text == "null"
}

// activity is the in-window time for an issue that changed in the window.
// updatedAt must fall in the window. A completion, cancellation, or creation
// in the window is the event time; otherwise the time is updatedAt.
func (it issueItem) activity(since, until time.Time) (time.Time, skipKind) {
	updated, err := parseTime(it.UpdatedAt)
	if err != nil {
		return time.Time{}, decodedMalformed
	}
	if !inWindow(updated, since, until) {
		return time.Time{}, decodedSkip
	}
	if ts := parseOptional(it.CompletedAt); ts != nil && inWindow(*ts, since, until) {
		return *ts, decodedOK
	}
	if ts := parseOptional(it.CanceledAt); ts != nil && inWindow(*ts, since, until) {
		return *ts, decodedOK
	}
	if created, err := parseTime(it.CreatedAt); err == nil && inWindow(created, since, until) {
		return created, decodedOK
	}
	return updated, decodedOK
}

func buildEvents(issues []issueItem, match matcher, since, until time.Time) ([]event.Event, skipStats, error) {
	var (
		events  []event.Event
		skipped skipStats
	)
	for _, issue := range issues {
		ts, stat := issue.activity(since, until)
		if stat != decodedOK {
			skipped.add(stat)
			continue
		}
		ev, err := ticketEvent(issue, match, ts)
		if err != nil {
			return nil, skipped, err
		}
		events = append(events, ev)
		transitions, hist, err := transitionEvents(issue, match, since, until)
		skipped.history += hist
		if err != nil {
			return nil, skipped, err
		}
		events = append(events, transitions...)
	}
	return events, skipped, nil
}

func ticketEvent(issue issueItem, match matcher, ts time.Time) (event.Event, error) {
	ev := event.Event{
		Source:   sourceName,
		SourceID: issue.Identifier,
		Kind:     event.KindTicket,
		TS:       ts,
		Actor:    ticketActor(issue),
		Title:    clipTitle(issue.Title),
		Summary:  textOf(issue.Description),
		URL:      textOf(issue.URL),
		Branch:   textOf(issue.BranchName),
		Refs:     issueRefs(issue, match),
		State:    mapState(issue.State),
	}
	prepared, err := event.Prepare(ev)
	if err != nil {
		return event.Event{}, failedErr("issue " + issue.Identifier + " is invalid")
	}
	return prepared, nil
}

func transitionEvents(issue issueItem, match matcher, since, until time.Time) ([]event.Event, int, error) {
	var (
		out       []event.Event
		malformed int
		seen      = map[string]struct{}{}
	)
	refs := issueRefs(issue, match)
	for _, raw := range issue.History.Nodes {
		node, stat := decodeHistory(raw)
		if stat == decodedSkip {
			continue
		}
		if stat != decodedOK {
			malformed++
			continue
		}
		if _, ok := seen[node.ID]; ok {
			continue
		}
		if !isStateChange(node) {
			continue
		}
		ts, err := parseTime(node.CreatedAt)
		if err != nil {
			malformed++
			continue
		}
		if !inWindow(ts, since, until) {
			continue
		}
		seen[node.ID] = struct{}{}
		id := issue.Identifier + "#" + node.ID
		title := transitionTitle(node)
		ev := event.Event{
			Source:   sourceName,
			SourceID: id,
			Kind:     event.KindTicketTransition,
			TS:       ts,
			Actor:    userName(node.Actor),
			Title:    clipTitle(title),
			Summary:  title,
			URL:      textOf(issue.URL),
			Branch:   textOf(issue.BranchName),
			Refs:     refs,
			State:    mapState(node.To),
		}
		prepared, err := event.Prepare(ev)
		if err != nil {
			return nil, malformed, failedErr("transition " + id + " is invalid")
		}
		out = append(out, prepared)
	}
	return out, malformed, nil
}

func issueRefs(issue issueItem, match matcher) []string {
	return match.refs(issue.Identifier, issue.Title, textOf(issue.Description), textOf(issue.BranchName))
}

func isStateChange(node historyNode) bool {
	to := stateLabel(node.To)
	if to == "" {
		return false
	}
	from := stateLabel(node.From)
	if from != "" && strings.EqualFold(from, to) {
		return false
	}
	return true
}

func transitionTitle(node historyNode) string {
	to := stateLabel(node.To)
	from := stateLabel(node.From)
	if from == "" {
		return "→ " + to
	}
	return from + " → " + to
}

func ticketActor(issue issueItem) string {
	if name := userName(issue.Creator); name != "" {
		return name
	}
	return userName(issue.Assignee)
}

func userName(u *user) string {
	if u == nil {
		return ""
	}
	for _, value := range []string{u.DisplayName, u.Name, u.Email} {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

func stateLabel(s *workflowState) string {
	if s == nil {
		return ""
	}
	if name := strings.TrimSpace(s.Name); name != "" {
		return name
	}
	return strings.TrimSpace(s.Type)
}

func mapState(s *workflowState) event.State {
	if s == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(s.Type)) {
	case "completed":
		return event.StateDone
	case "canceled", "cancelled":
		return event.StateAbandoned
	case "started", "unstarted", "backlog", "triage":
		return event.StateOpen
	}
	switch strings.ToLower(strings.TrimSpace(s.Name)) {
	case "done", "completed", "complete":
		return event.StateDone
	case "canceled", "cancelled":
		return event.StateAbandoned
	case "closed":
		return event.StateClosed
	}
	if strings.TrimSpace(s.Name) != "" || strings.TrimSpace(s.Type) != "" {
		return event.StateOpen
	}
	return ""
}

func textOf(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func inWindow(ts, since, until time.Time) bool {
	return !ts.Before(since) && ts.Before(until)
}

func parseTime(value string) (time.Time, error) {
	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, err
	}
	return ts.UTC(), nil
}

func parseOptional(value *string) *time.Time {
	if value == nil {
		return nil
	}
	ts, err := parseTime(*value)
	if err != nil {
		return nil
	}
	return &ts
}

func clipTitle(s string) string {
	s = firstLine(s)
	if utf8.RuneCountInString(s) <= maxTitleRunes {
		return s
	}
	i := 0
	for r := 0; r < maxTitleRunes; r++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
