package github

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

const maxTitleRunes = 200

type actor struct {
	Login string `json:"login"`
}

type commitConn struct {
	PageInfo pageInfo `json:"pageInfo"`
	Nodes    []struct {
		Commit struct {
			OID string `json:"oid"`
		} `json:"commit"`
	} `json:"nodes"`
}

type reviewNode struct {
	DatabaseID  *int64  `json:"databaseId"`
	ID          string  `json:"id"`
	Author      *actor  `json:"author"`
	Body        string  `json:"body"`
	State       string  `json:"state"`
	SubmittedAt *string `json:"submittedAt"`
	URL         string  `json:"url"`
}

type reviewConn struct {
	PageInfo pageInfo     `json:"pageInfo"`
	Nodes    []reviewNode `json:"nodes"`
}

type pullRequest struct {
	Number     int     `json:"number"`
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	URL        string  `json:"url"`
	State      string  `json:"state"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
	ClosedAt   *string `json:"closedAt"`
	MergedAt   *string `json:"mergedAt"`
	Additions  int     `json:"additions"`
	Deletions  int     `json:"deletions"`
	Author     *actor  `json:"author"`
	Repository *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	HeadRefName *string    `json:"headRefName"`
	Commits     commitConn `json:"commits"`
	Reviews     reviewConn `json:"reviews"`
}

type issueItem struct {
	Number     int     `json:"number"`
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	URL        string  `json:"url"`
	State      string  `json:"state"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
	ClosedAt   *string `json:"closedAt"`
	Author     *actor  `json:"author"`
	Repository *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Assignees struct {
		Nodes []actor `json:"nodes"`
	} `json:"assignees"`
}

func (p pullRequest) key() string {
	if p.Repository == nil {
		return ""
	}
	return p.Repository.NameWithOwner + "#" + strconv.Itoa(p.Number)
}

func (it issueItem) key() string {
	if it.Repository == nil {
		return ""
	}
	return it.Repository.NameWithOwner + "#" + strconv.Itoa(it.Number)
}

func decodePR(raw json.RawMessage) (pullRequest, skipKind) {
	if isNull(raw) {
		return pullRequest{}, decodedSkip
	}
	kind, err := nodeType(raw)
	if err != nil {
		return pullRequest{}, decodedMalformed
	}
	if kind != "PullRequest" {
		if kind == "" {
			return pullRequest{}, decodedMalformed
		}
		return pullRequest{}, decodedUnexpected
	}
	var pr pullRequest
	if err := json.Unmarshal(raw, &pr); err != nil {
		return pullRequest{}, decodedMalformed
	}
	if pr.Repository == nil || pr.Number <= 0 || strings.TrimSpace(pr.CreatedAt) == "" {
		return pullRequest{}, decodedMalformed
	}
	if _, _, ok := splitRepo(pr.Repository.NameWithOwner); !ok {
		return pullRequest{}, decodedMalformed
	}
	return pr, decodedOK
}

func decodeIssue(raw json.RawMessage) (issueItem, skipKind) {
	if isNull(raw) {
		return issueItem{}, decodedSkip
	}
	kind, err := nodeType(raw)
	if err != nil {
		return issueItem{}, decodedMalformed
	}
	if kind != "Issue" {
		if kind == "" {
			return issueItem{}, decodedMalformed
		}
		return issueItem{}, decodedUnexpected
	}
	var item issueItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return issueItem{}, decodedMalformed
	}
	if item.Repository == nil || item.Number <= 0 || strings.TrimSpace(item.CreatedAt) == "" {
		return issueItem{}, decodedMalformed
	}
	if _, _, ok := splitRepo(item.Repository.NameWithOwner); !ok {
		return issueItem{}, decodedMalformed
	}
	return item, decodedOK
}

func nodeType(raw json.RawMessage) (string, error) {
	var head struct {
		TypeName string `json:"__typename"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return "", err
	}
	return head.TypeName, nil
}

func isNull(raw json.RawMessage) bool {
	text := strings.TrimSpace(string(raw))
	return text == "" || text == "null"
}

func (p pullRequest) activity(login string, since, until time.Time) (time.Time, bool) {
	created, err := parseTime(p.CreatedAt)
	if err != nil {
		return time.Time{}, false
	}
	merged := parseOptional(p.MergedAt)
	closed := parseOptional(p.ClosedAt)
	if merged != nil && inWindow(*merged, since, until) {
		return *merged, true
	}
	if closed != nil && inWindow(*closed, since, until) {
		return *closed, true
	}
	if inWindow(created, since, until) {
		return created, true
	}
	if ts, ok := earliestReview(p, login, since, until); ok {
		return ts, true
	}
	return time.Time{}, false
}

func (it issueItem) activity(login string, since, until time.Time) (time.Time, bool) {
	if !it.involves(login) {
		return time.Time{}, false
	}
	created, err := parseTime(it.CreatedAt)
	if err != nil {
		return time.Time{}, false
	}
	if closed := parseOptional(it.ClosedAt); closed != nil && inWindow(*closed, since, until) {
		return *closed, true
	}
	if inWindow(created, since, until) {
		return created, true
	}
	updated, err := parseTime(it.UpdatedAt)
	if err != nil {
		return time.Time{}, false
	}
	if inWindow(updated, since, until) {
		return updated, true
	}
	return time.Time{}, false
}

func (it issueItem) involves(login string) bool {
	if it.Author != nil && strings.EqualFold(it.Author.Login, login) {
		return true
	}
	for _, assignee := range it.Assignees.Nodes {
		if strings.EqualFold(assignee.Login, login) {
			return true
		}
	}
	return false
}

func earliestReview(p pullRequest, login string, since, until time.Time) (time.Time, bool) {
	var earliest time.Time
	found := false
	for _, review := range p.Reviews.Nodes {
		if review.Author == nil || !strings.EqualFold(review.Author.Login, login) || review.SubmittedAt == nil {
			continue
		}
		ts, err := parseTime(*review.SubmittedAt)
		if err != nil || !inWindow(ts, since, until) {
			continue
		}
		if !found || ts.Before(earliest) {
			earliest = ts
			found = true
		}
	}
	return earliest, found
}

func prEvents(p pullRequest, login string, since, until time.Time) (event.Event, []event.Event, error) {
	ts, ok := p.activity(login, since, until)
	if !ok {
		return event.Event{}, nil, nil
	}
	repo, ok := repoFields(p.Repository.NameWithOwner)
	if !ok {
		return event.Event{}, nil, nil
	}
	id := p.Repository.NameWithOwner + "#" + strconv.Itoa(p.Number)
	branch := ""
	if p.HeadRefName != nil {
		branch = strings.TrimSpace(*p.HeadRefName)
	}
	ev := event.Event{
		Source:   sourceName,
		SourceID: id,
		Kind:     event.KindPR,
		TS:       ts,
		Actor:    loginOf(p.Author),
		Title:    clipTitle(p.Title),
		Summary:  strings.TrimSpace(p.Body),
		URL:      itemURL(p.URL, repo, "pull", p.Number),
		Repo:     repo,
		Branch:   branch,
		Refs:     prRefs(p),
		State:    prState(p),
		Metrics: map[string]float64{
			"lines_added":   float64(p.Additions),
			"lines_removed": float64(p.Deletions),
		},
	}
	if merged := parseOptional(p.MergedAt); merged != nil && merged.After(ts) {
		end := *merged
		ev.TSEnd = &end
	}
	prepared, err := event.Prepare(ev)
	if err != nil {
		return event.Event{}, nil, failedErr("pull request " + id + " is invalid")
	}
	reviews, err := reviewEvents(p, login, since, until, id, repo)
	if err != nil {
		return event.Event{}, nil, err
	}
	return prepared, reviews, nil
}

func reviewEvents(p pullRequest, login string, since, until time.Time, prID, repo string) ([]event.Event, error) {
	var out []event.Event
	seen := map[string]struct{}{}
	for _, review := range p.Reviews.Nodes {
		if review.Author == nil || !strings.EqualFold(review.Author.Login, login) || review.SubmittedAt == nil {
			continue
		}
		ts, err := parseTime(*review.SubmittedAt)
		if err != nil || !inWindow(ts, since, until) {
			continue
		}
		suffix, ok := reviewSuffix(review)
		if !ok {
			continue
		}
		id := prID + "#review-" + suffix
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ev := event.Event{
			Source:   sourceName,
			SourceID: id,
			Kind:     event.KindPRReview,
			TS:       ts,
			Actor:    review.Author.Login,
			Title:    clipTitle(reviewTitle(review)),
			Summary:  strings.TrimSpace(review.Body),
			URL:      strings.TrimSpace(review.URL),
			Repo:     repo,
			Refs:     appendRefs([]string{"#" + strconv.Itoa(p.Number)}, textRefs(review.Body)),
		}
		prepared, err := event.Prepare(ev)
		if err != nil {
			return nil, failedErr("review " + id + " is invalid")
		}
		out = append(out, prepared)
	}
	return out, nil
}

func issueEvent(it issueItem, login string, since, until time.Time) (event.Event, error) {
	ts, ok := it.activity(login, since, until)
	if !ok {
		return event.Event{}, nil
	}
	repo, ok := repoFields(it.Repository.NameWithOwner)
	if !ok {
		return event.Event{}, nil
	}
	id := it.Repository.NameWithOwner + "#" + strconv.Itoa(it.Number)
	ev := event.Event{
		Source:   sourceName,
		SourceID: id,
		Kind:     event.KindIssue,
		TS:       ts,
		Actor:    loginOf(it.Author),
		Title:    clipTitle(it.Title),
		Summary:  strings.TrimSpace(it.Body),
		URL:      itemURL(it.URL, repo, "issues", it.Number),
		Repo:     repo,
		Refs:     appendRefs([]string{"#" + strconv.Itoa(it.Number)}, textRefs(it.Title, it.Body)),
		State:    issueState(it.State),
	}
	prepared, err := event.Prepare(ev)
	if err != nil {
		return event.Event{}, failedErr("issue " + id + " is invalid")
	}
	return prepared, nil
}

func prState(p pullRequest) event.State {
	if parseOptional(p.MergedAt) != nil || strings.EqualFold(p.State, "MERGED") {
		return event.StateMerged
	}
	switch strings.ToUpper(strings.TrimSpace(p.State)) {
	case "CLOSED":
		return event.StateClosed
	case "OPEN":
		return event.StateOpen
	default:
		return ""
	}
}

func issueState(state string) event.State {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "CLOSED":
		return event.StateClosed
	case "OPEN":
		return event.StateOpen
	default:
		return ""
	}
}

func reviewSuffix(review reviewNode) (string, bool) {
	if review.DatabaseID != nil && *review.DatabaseID > 0 {
		return strconv.FormatInt(*review.DatabaseID, 10), true
	}
	id := strings.TrimSpace(review.ID)
	if id == "" || strings.ContainsAny(id, "#/:-") {
		return "", false
	}
	return id, true
}

func reviewTitle(review reviewNode) string {
	if line := firstLine(review.Body); line != "" {
		return line
	}
	switch strings.ToUpper(strings.TrimSpace(review.State)) {
	case "APPROVED":
		return "Approved"
	case "CHANGES_REQUESTED":
		return "Changes requested"
	case "COMMENTED":
		return "Commented"
	case "DISMISSED":
		return "Dismissed"
	default:
		return ""
	}
}

func prRefs(p pullRequest) []string {
	seen := map[string]struct{}{}
	var refs []string
	for _, node := range p.Commits.Nodes {
		oid := strings.ToLower(strings.TrimSpace(node.Commit.OID))
		if !validOID(oid) {
			continue
		}
		if _, ok := seen[oid]; ok {
			continue
		}
		seen[oid] = struct{}{}
		refs = append(refs, oid)
	}
	return appendRefs(append(refs, "#"+strconv.Itoa(p.Number)), textRefs(p.Title, p.Body))
}

func appendRefs(base, more []string) []string {
	seen := map[string]struct{}{}
	for _, ref := range base {
		seen[ref] = struct{}{}
	}
	for _, ref := range more {
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		base = append(base, ref)
	}
	return base
}

func repoFields(nameWithOwner string) (string, bool) {
	owner, name, ok := splitRepo(nameWithOwner)
	if !ok {
		return "", false
	}
	return "github.com/" + owner + "/" + name, true
}

func itemURL(given, repo, kind string, number int) string {
	if text := strings.TrimSpace(given); text != "" {
		return text
	}
	hostPath := strings.TrimPrefix(repo, "github.com/")
	return "https://github.com/" + hostPath + "/" + kind + "/" + strconv.Itoa(number)
}

func loginOf(a *actor) string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.Login)
}

func inWindow(ts, since, until time.Time) bool {
	return !ts.Before(since) && ts.Before(until)
}

func parseTime(value string) (time.Time, error) {
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
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

func validOID(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
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
