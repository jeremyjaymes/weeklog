package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Version is the describe version of the github collector.
const Version = "0.1.0"

const sourceName = "github"

// Source collects pull request, review, and issue events from GitHub.
type Source struct {
	endpoint string
	client   *http.Client
	// ghToken overrides `gh auth token` in tests. Production leaves it nil.
	ghToken func(context.Context) (string, error)
}

// New returns the built-in github source.
func New() *Source {
	return &Source{}
}

// Describe returns the github config schema.
func (s *Source) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     sourceName,
		Version:  Version,
		Config: map[string]source.Field{
			"token": {Type: source.TypeSecret},
		},
	}, nil
}

// Collect emits pull requests, reviews, and issues for the login in the window.
// The window includes since and excludes until. token comes from config, or
// from `gh auth token` when config does not set one. The login is
// identity.handles github, or the authenticated viewer when that handle is unset.
func (s *Source) Collect(ctx context.Context, req source.CollectRequest, emit func(event.Event) error) (source.Result, error) {
	if err := ctx.Err(); err != nil {
		return source.Result{}, err
	}
	s = s.withLoopback(req.Config)
	since, until, err := collectionWindow(req)
	if err != nil {
		return source.Result{}, err
	}
	token, err := s.resolveToken(ctx, req.Config)
	if err != nil {
		return source.Result{}, err
	}
	login, err := s.login(ctx, token, req.Identity)
	if err != nil {
		return source.Result{}, err
	}

	prs, logs, err := s.pullRequests(ctx, token, login, since, until)
	if err != nil {
		return source.Result{Logs: logs}, err
	}
	issues, issueLogs, err := s.issues(ctx, token, login, since, until)
	logs = append(logs, issueLogs...)
	if err != nil {
		return source.Result{Logs: logs}, err
	}

	events := make([]event.Event, 0, len(prs)*2+len(issues))
	for _, pr := range prs {
		ev, reviews, err := prEvents(pr, login, since, until)
		if err != nil {
			return source.Result{Logs: logs}, err
		}
		if ev.SourceID != "" {
			events = append(events, ev)
		}
		events = append(events, reviews...)
	}
	for _, issue := range issues {
		ev, err := issueEvent(issue, login, since, until)
		if err != nil {
			return source.Result{Logs: logs}, err
		}
		if ev.SourceID != "" {
			events = append(events, ev)
		}
	}
	sortEvents(events)

	result := source.Result{Logs: logs}
	for _, ev := range events {
		if emit != nil {
			if err := emit(ev); err != nil {
				return result, err
			}
		}
		result.Events++
	}
	return result, nil
}

func collectionWindow(req source.CollectRequest) (time.Time, time.Time, error) {
	if req.Since.IsZero() || req.Until.IsZero() {
		return time.Time{}, time.Time{}, configErr("since and until are required")
	}
	if !req.Until.After(req.Since) {
		return time.Time{}, time.Time{}, configErr("until must be after since")
	}
	return req.Since, req.Until, nil
}

func (s *Source) login(ctx context.Context, token string, id source.Identity) (string, error) {
	login := ""
	if id.Handles != nil {
		login = strings.TrimSpace(id.Handles["github"])
	}
	if login == "" {
		var err error
		login, err = s.viewerLogin(ctx, token)
		if err != nil {
			return "", err
		}
		login = strings.TrimSpace(login)
	}
	if !validLogin(login) {
		return "", configErr("github login is invalid")
	}
	return login, nil
}

func (s *Source) pullRequests(ctx context.Context, token, login string, since, until time.Time) ([]pullRequest, []source.LogLine, error) {
	var (
		logs    []source.LogLine
		prs     []pullRequest
		skipped skipStats
		seen    = map[string]struct{}{}
	)
	for _, kind := range []string{"author", "reviewed-by"} {
		nodes, err := s.search(ctx, token, searchText(kind, login, since, until, true))
		if err != nil {
			return nil, logs, err
		}
		for _, node := range nodes {
			pr, stat := decodePR(node)
			skipped.add(stat)
			if stat != decodedOK {
				continue
			}
			key := pr.key()
			if _, ok := seen[key]; ok {
				continue
			}
			if err := s.pageReviews(ctx, token, &pr); err != nil {
				return nil, logs, err
			}
			seen[key] = struct{}{}
			prs = append(prs, pr)
		}
	}

	kept := make([]pullRequest, 0, len(prs))
	for _, pr := range prs {
		if _, ok := pr.activity(login, since, until); !ok {
			continue
		}
		if err := s.pageCommits(ctx, token, &pr); err != nil {
			return nil, logs, err
		}
		kept = append(kept, pr)
	}
	return kept, skipped.logs("malformed pull request", "malformed pull requests"), nil
}

func (s *Source) issues(ctx context.Context, token, login string, since, until time.Time) ([]issueItem, []source.LogLine, error) {
	var (
		items   []issueItem
		skipped skipStats
		seen    = map[string]struct{}{}
	)
	for _, kind := range []string{"author", "assignee"} {
		nodes, err := s.search(ctx, token, searchText(kind, login, since, until, false))
		if err != nil {
			return nil, skipped.logs("malformed issue", "malformed issues"), err
		}
		for _, node := range nodes {
			item, stat := decodeIssue(node)
			skipped.add(stat)
			if stat != decodedOK {
				continue
			}
			key := item.key()
			if _, ok := seen[key]; ok {
				continue
			}
			if _, ok := item.activity(login, since, until); !ok {
				continue
			}
			seen[key] = struct{}{}
			items = append(items, item)
		}
	}
	return items, skipped.logs("malformed issue", "malformed issues"), nil
}

func (s *Source) pageReviews(ctx context.Context, token string, pr *pullRequest) error {
	owner, name, ok := splitRepo(pr.Repository.NameWithOwner)
	if !ok {
		return nil
	}
	if !pr.Reviews.PageInfo.HasNextPage {
		return nil
	}
	cursor, ok := cursorOf(pr.Reviews.PageInfo)
	if !ok {
		return failedErr("GitHub review page is missing a cursor")
	}
	for page := 0; page < maxPages; page++ {
		data, err := s.gql(ctx, token, reviewQuery, map[string]any{
			"owner":  owner,
			"name":   name,
			"number": pr.Number,
			"cursor": cursor,
		})
		if err != nil {
			return err
		}
		var parsed struct {
			Repository *struct {
				PullRequest *struct {
					Reviews reviewConn `json:"reviews"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil || parsed.Repository == nil || parsed.Repository.PullRequest == nil {
			return failedErr("GitHub review page is invalid")
		}
		conn := parsed.Repository.PullRequest.Reviews
		pr.Reviews.Nodes = append(pr.Reviews.Nodes, conn.Nodes...)
		if !conn.PageInfo.HasNextPage {
			pr.Reviews.PageInfo = conn.PageInfo
			return nil
		}
		next, ok := cursorOf(conn.PageInfo)
		if !ok || next == cursor {
			return failedErr("GitHub review page is missing a cursor")
		}
		cursor = next
	}
	return failedErr(fmt.Sprintf("GitHub reviews for %s exceeded 20 pages", pr.key()))
}

func (s *Source) pageCommits(ctx context.Context, token string, pr *pullRequest) error {
	owner, name, ok := splitRepo(pr.Repository.NameWithOwner)
	if !ok {
		return nil
	}
	if !pr.Commits.PageInfo.HasNextPage {
		return nil
	}
	cursor, ok := cursorOf(pr.Commits.PageInfo)
	if !ok {
		return failedErr("GitHub commit page is missing a cursor")
	}
	for page := 0; page < maxPages; page++ {
		data, err := s.gql(ctx, token, commitQuery, map[string]any{
			"owner":  owner,
			"name":   name,
			"number": pr.Number,
			"cursor": cursor,
		})
		if err != nil {
			return err
		}
		var parsed struct {
			Repository *struct {
				PullRequest *struct {
					Commits commitConn `json:"commits"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil || parsed.Repository == nil || parsed.Repository.PullRequest == nil {
			return failedErr("GitHub commit page is invalid")
		}
		conn := parsed.Repository.PullRequest.Commits
		pr.Commits.Nodes = append(pr.Commits.Nodes, conn.Nodes...)
		if !conn.PageInfo.HasNextPage {
			pr.Commits.PageInfo = conn.PageInfo
			return nil
		}
		next, ok := cursorOf(conn.PageInfo)
		if !ok || next == cursor {
			return failedErr("GitHub commit page is missing a cursor")
		}
		cursor = next
	}
	return failedErr(fmt.Sprintf("GitHub commits for %s exceeded 20 pages", pr.key()))
}

func cursorOf(info pageInfo) (string, bool) {
	if info.EndCursor == nil {
		return "", false
	}
	cursor := strings.TrimSpace(*info.EndCursor)
	if cursor == "" {
		return "", false
	}
	return cursor, true
}

func sortEvents(events []event.Event) {
	sort.Slice(events, func(i, j int) bool {
		if !events[i].TS.Equal(events[j].TS) {
			return events[i].TS.Before(events[j].TS)
		}
		if events[i].Kind != events[j].Kind {
			return events[i].Kind < events[j].Kind
		}
		return events[i].SourceID < events[j].SourceID
	})
}

func configErr(reason string) error {
	return &source.Failure{Name: sourceName, Op: "collect", Reason: reason, Err: source.ErrConfig}
}

func failedErr(reason string) error {
	return &source.Failure{Name: sourceName, Op: "collect", Reason: reason, Err: source.ErrFailed}
}

func validLogin(s string) bool {
	if len(s) < 1 || len(s) > 39 {
		return false
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	prevHyphen := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			prevHyphen = false
		case r == '-' && !prevHyphen:
			prevHyphen = true
		default:
			return false
		}
	}
	return true
}

func splitRepo(nameWithOwner string) (owner, name string, ok bool) {
	owner, name, ok = strings.Cut(strings.TrimSpace(nameWithOwner), "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", false
	}
	return owner, name, true
}

func warn(msg string) source.LogLine {
	return source.LogLine{Level: "warn", Msg: msg}
}

type skipKind int

const (
	decodedSkip skipKind = iota
	decodedOK
	decodedMalformed
	decodedUnexpected
)

type skipStats struct {
	malformed  int
	unexpected int
}

func (s *skipStats) add(kind skipKind) {
	switch kind {
	case decodedMalformed:
		s.malformed++
	case decodedUnexpected:
		s.unexpected++
	}
}

func (s skipStats) logs(singular, plural string) []source.LogLine {
	var logs []source.LogLine
	if s.malformed > 0 {
		logs = append(logs, warn(counted(s.malformed, singular, plural)))
	}
	if s.unexpected > 0 {
		logs = append(logs, warn(counted(s.unexpected, "unexpected GitHub result", "unexpected GitHub results")))
	}
	return logs
}

func counted(n int, singular, plural string) string {
	if n == 1 {
		return "skipped 1 " + singular
	}
	return fmt.Sprintf("skipped %d %s", n, plural)
}
