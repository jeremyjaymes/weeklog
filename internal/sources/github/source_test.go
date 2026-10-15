package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

func TestDescribe(t *testing.T) {
	manifest, err := New().Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "github" || manifest.Version != Version || manifest.Protocol != source.ProtocolVersion {
		t.Fatalf("manifest = %#v", manifest)
	}
	token, ok := manifest.Config["token"]
	if !ok || token.Type != source.TypeSecret || token.Required {
		t.Fatalf("token field = %#v", token)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Describe(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Describe err = %v", err)
	}
}

func TestCollectUsesLoopbackConfigEndpoint(t *testing.T) {
	const token = "fixture-token"
	srv, gotReqs := newFixtureServer(t)
	src := New()
	req := request(token, "alice")
	req.Config["endpoint"] = srv.URL
	events, err := collect(t, src, req)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureEvents(t, events)
	if len(gotReqs()) == 0 {
		t.Fatal("loopback endpoint was not requested")
	}
}

func TestFieldEndpointWinsOverConfig(t *testing.T) {
	srv, gotReqs := newFixtureServer(t)
	src := New()
	src.endpoint = srv.URL
	req := request("fixture-token", "alice")
	req.Config["endpoint"] = "http://example.com/graphql"
	events, err := collect(t, src, req)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureEvents(t, events)
	if len(gotReqs()) == 0 {
		t.Fatal("field endpoint was not requested")
	}
}

func TestCollectRecordedFixtures(t *testing.T) {
	const token = "fixture-token"
	srv, gotReqs := newFixtureServer(t)
	events, result := collectFrom(t, srv.URL, token, "alice", nil)
	assertFixtureEvents(t, events)
	if result.Events != len(events) || result.HasCursor || result.Dropped != 0 {
		t.Fatalf("result = %#v", result)
	}
	for _, want := range []string{
		"skipped 1 malformed pull request",
		"skipped 1 unexpected GitHub result",
		"skipped 1 malformed issue",
	} {
		if !logHas(result.Logs, want) {
			t.Fatalf("logs missing %q: %#v", want, result.Logs)
		}
	}
	reqs := gotReqs()
	if len(reqs) != 7 {
		t.Fatalf("requests = %d %#v", len(reqs), queriesOf(reqs))
	}
	for _, req := range reqs {
		if req.Auth != "Bearer "+token {
			t.Fatalf("auth = %q", req.Auth)
		}
		if strings.Contains(req.Body, token) || strings.Contains(req.URL, token) {
			t.Fatal("token leaked into the request")
		}
		if strings.Contains(req.Query, "viewer") {
			t.Fatal("viewer was queried even though the login was set")
		}
	}
	var sawAuthorPage2, sawReviewed, sawCommits, sawReviews bool
	for _, req := range reqs {
		if strings.Contains(req.Q, "is:pr author:alice updated:2026-10-05..2026-10-11") && req.Cursor == "pr-author-2" {
			sawAuthorPage2 = true
		}
		if strings.Contains(req.Q, "is:pr reviewed-by:alice updated:2026-10-05..2026-10-11") {
			sawReviewed = true
		}
		if req.Owner == "acme" && req.Name == "otto" && req.Number == 12 && req.Cursor == "commit-2" {
			sawCommits = true
		}
		if req.Owner == "acme" && req.Name == "otto" && req.Number == 12 && req.Cursor == "review-2" {
			sawReviews = true
		}
	}
	if !sawAuthorPage2 || !sawReviewed || !sawCommits || !sawReviews {
		t.Fatalf("pagination missing: page2=%v reviewed=%v commits=%v reviews=%v\n%#v", sawAuthorPage2, sawReviewed, sawCommits, sawReviews, reqs)
	}
}

func TestCollectUsesViewerWhenLoginUnset(t *testing.T) {
	srv, gotReqs := newFixtureServer(t)
	events, _ := collectFrom(t, srv.URL, "fixture-token", "", nil)
	assertFixtureEvents(t, events)
	sawViewer := false
	for _, req := range gotReqs() {
		if strings.Contains(req.Query, "viewer") {
			sawViewer = true
		}
	}
	if !sawViewer {
		t.Fatal("viewer login was not requested")
	}
}

func TestCollectPrefersConfigToken(t *testing.T) {
	const token = "config-token"
	srv, gotReqs := newFixtureServer(t)
	src := New()
	src.endpoint = srv.URL
	src.ghToken = func(context.Context) (string, error) {
		t.Fatal("gh auth token was called")
		return "", errors.New("called")
	}
	events, err := collect(t, src, request(token, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureEvents(t, events)
	for _, req := range gotReqs() {
		if req.Auth != "Bearer "+token {
			t.Fatalf("auth = %q", req.Auth)
		}
	}
}

func TestCollectFallsBackToGHHook(t *testing.T) {
	const token = "hook-token"
	srv, gotReqs := newFixtureServer(t)
	src := New()
	src.endpoint = srv.URL
	src.ghToken = func(context.Context) (string, error) {
		return token, nil
	}
	events, err := collect(t, src, request("", "alice"))
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureEvents(t, events)
	if gotReqs()[0].Auth != "Bearer "+token {
		t.Fatalf("auth = %q", gotReqs()[0].Auth)
	}
}

func TestCollectBlankTokenUsesFallback(t *testing.T) {
	srv, _ := newFixtureServer(t)
	src := New()
	src.endpoint = srv.URL
	src.ghToken = func(context.Context) (string, error) {
		return "hook-token", nil
	}
	req := request("   ", "alice")
	if _, err := collect(t, src, req); err != nil {
		t.Fatal(err)
	}
}

func TestCollectTokenErrors(t *testing.T) {
	src := New()
	src.ghToken = func(context.Context) (string, error) {
		return "", errors.New("not logged in")
	}
	_, err := collect(t, src, request("", "alice"))
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "gh auth token is unavailable") {
		t.Fatalf("err = %v", err)
	}

	src.ghToken = func(context.Context) (string, error) {
		return "bad token", nil
	}
	_, err = collect(t, src, request("", "alice"))
	if !errors.Is(err, source.ErrConfig) || strings.Contains(err.Error(), "bad token") {
		t.Fatalf("err = %v", err)
	}

	_, err = collect(t, src, source.CollectRequest{
		Since:    windowSince,
		Until:    windowUntil,
		Config:   map[string]any{"token": 1},
		Identity: source.Identity{Handles: map[string]string{"github": "alice"}},
	})
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "token must be a string") {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectRejectsBadWindowAndLogin(t *testing.T) {
	_, err := New().Collect(context.Background(), source.CollectRequest{}, nil)
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "since and until") {
		t.Fatalf("err = %v", err)
	}
	_, err = New().Collect(context.Background(), source.CollectRequest{
		Since: windowUntil,
		Until: windowSince,
	}, nil)
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "until must be after since") {
		t.Fatalf("err = %v", err)
	}

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL
	_, err = collect(t, src, request("fixture-token", "not a login"))
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "login is invalid") || strings.Contains(err.Error(), "not a login") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("invalid login called GitHub")
	}
}

func TestCollectScrubsTokenFromErrors(t *testing.T) {
	const token = "super-secret-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"rejected `+token+`"}`)
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL
	_, err := collect(t, src, request(token, "alice"))
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "rejected the token") || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"message":"explode `+token+` please"}]}`)
	}))
	t.Cleanup(srv2.Close)
	src.endpoint = srv2.URL
	_, err = collect(t, src, request(token, "alice"))
	if !errors.Is(err, source.ErrFailed) || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("err = %v", err)
	}

	srv2.Close()
	_, err = collect(t, src, request(token, "alice"))
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().Collect(ctx, request("fixture-token", "alice"), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchPageCap(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_, _ = io.WriteString(w, `{"data":{"search":{"pageInfo":{"hasNextPage":true,"endCursor":"c`+strings.Repeat("x", n)+`"},"nodes":[]}}}`)
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL
	_, err := collect(t, src, request("fixture-token", "alice"))
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "exceeded 20 pages") {
		t.Fatalf("err = %v", err)
	}
	if n != maxPages {
		t.Fatalf("requests = %d", n)
	}
}

func TestSearchDates(t *testing.T) {
	from, to := searchDates(windowSince, windowUntil)
	if from != "2026-10-05" || to != "2026-10-11" {
		t.Fatalf("dates = %s .. %s", from, to)
	}
	q := searchText("author", "alice", windowSince, windowUntil, true)
	if q != "is:pr author:alice updated:2026-10-05..2026-10-11" {
		t.Fatalf("query = %s", q)
	}
}

func TestValidLogin(t *testing.T) {
	for _, login := range []string{"a", "alice", "a-b", "Z9"} {
		if !validLogin(login) {
			t.Fatalf("%s should be valid", login)
		}
	}
	for _, login := range []string{"", "-a", "a-", "a--b", "has space", "x@", strings.Repeat("a", 40)} {
		if validLogin(login) {
			t.Fatalf("%s should be invalid", login)
		}
	}
}

var (
	windowSince = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	windowUntil = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
)

func assertFixtureEvents(t *testing.T, got []event.Event) {
	t.Helper()
	want := []event.Event{
		pr("acme/edge#1", "Starts at the window", "", "https://github.com/acme/edge/pull/1", "github.com/acme/edge", "main", "alice", event.StateOpen, at("2026-10-05T00:00:00Z"), 0, 0, "1111111111111111111111111111111111111111", "#1"),
		issue("acme/otto#9", "Old bug", "", "https://github.com/acme/otto/issues/9", "github.com/acme/otto", "alice", event.StateClosed, at("2026-10-06T13:00:00Z"), "#9"),
		pr("acme/otto#6", "Close the stale branch", "", "https://github.com/acme/otto/pull/6", "github.com/acme/otto", "wip/stale", "alice", event.StateClosed, at("2026-10-07T08:00:00Z"), 3, 1, "2222222222222222222222222222222222222222", "#6"),
		review("acme/otto#12#review-10", "Self note GEN-302", "Self note GEN-302", "https://github.com/acme/otto/pull/12#pullrequestreview-10", "github.com/acme/otto", "alice", at("2026-10-07T12:00:00Z"), "#12", "GEN-302"),
		issue("acme/otto#7", "Document retries", "See GEN-301", "https://github.com/acme/otto/issues/7", "github.com/acme/otto", "alice", event.StateOpen, at("2026-10-08T10:00:00Z"), "#7", "GEN-301"),
		issue("acme/otto#8", "Still open", "", "https://github.com/acme/otto/issues/8", "github.com/acme/otto", "alice", event.StateOpen, at("2026-10-08T11:00:00Z"), "#8"),
		review("acme/otto#12#review-15", "Approved", "", "https://github.com/acme/otto/pull/12#pullrequestreview-15", "github.com/acme/otto", "alice", at("2026-10-08T12:00:00Z"), "#12"),
		pr("acme/otto#12", "Fix Stripe webhook retries", "Closes GEN-301\n\nRetries the webhook.", "https://github.com/acme/otto/pull/12", "github.com/acme/otto", "cursor/fix-webhooks", "alice", event.StateMerged, at("2026-10-08T18:00:00Z"), 40, 2, "0123456789abcdef0123456789abcdef01234567", "abcdef0123456789abcdef0123456789abcdef01", "#12", "GEN-301"),
		pr("acme/edge#4", "Edge case", "", "https://github.com/acme/edge/pull/4", "github.com/acme/edge", "feature/edge", "alice", event.StateOpen, at("2026-10-09T08:00:00Z"), 5, 0, "3333333333333333333333333333333333333333", "#4"),
		pr("other/lib#20", "Reviewed change", "Please see GEN-404", "https://github.com/other/lib/pull/20", "github.com/other/lib", "fix/tests", "carol", event.StateOpen, at("2026-10-10T15:04:00Z"), 8, 1, "4444444444444444444444444444444444444444", "#20", "GEN-404"),
		review("other/lib#20#review-42", "Please add a test", "Please add a test\n\nGEN-404", "https://github.com/other/lib/pull/20#pullrequestreview-42", "github.com/other/lib", "alice", at("2026-10-10T15:04:00Z"), "#20", "GEN-404"),
		issue("other/lib#15", "Assigned bug", "", "https://github.com/other/lib/issues/15", "github.com/other/lib", "dave", event.StateOpen, at("2026-10-11T16:00:00Z"), "#15"),
	}
	if len(got) != len(want) {
		t.Fatalf("events = %d, want %d\n%v", len(got), len(want), idsOf(got))
	}
	for i := range want {
		assertSame(t, got[i], want[i])
	}
}

func pr(id, title, summary, url, repo, branch, actor string, state event.State, ts time.Time, added, removed int, refs ...string) event.Event {
	return event.Event{
		Source: sourceName, SourceID: id, Kind: event.KindPR, TS: ts,
		Actor: actor, Title: title, Summary: summary, URL: url, Repo: repo, Branch: branch,
		Refs: refs, State: state,
		Metrics: map[string]float64{"lines_added": float64(added), "lines_removed": float64(removed)},
	}
}

func review(id, title, summary, url, repo, actor string, ts time.Time, refs ...string) event.Event {
	return event.Event{
		Source: sourceName, SourceID: id, Kind: event.KindPRReview, TS: ts,
		Actor: actor, Title: title, Summary: summary, URL: url, Repo: repo, Refs: refs,
	}
}

func issue(id, title, summary, url, repo, actor string, state event.State, ts time.Time, refs ...string) event.Event {
	return event.Event{
		Source: sourceName, SourceID: id, Kind: event.KindIssue, TS: ts,
		Actor: actor, Title: title, Summary: summary, URL: url, Repo: repo, Refs: refs, State: state,
	}
}

func assertSame(t *testing.T, got, want event.Event) {
	t.Helper()
	if got.Source != want.Source || got.SourceID != want.SourceID || got.Kind != want.Kind ||
		!got.TS.Equal(want.TS) || got.TSEnd != nil || got.Actor != want.Actor ||
		got.Title != want.Title || got.Summary != want.Summary || got.URL != want.URL ||
		got.Repo != want.Repo || got.Branch != want.Branch || got.State != want.State ||
		!reflect.DeepEqual(got.Refs, want.Refs) || !reflect.DeepEqual(got.Metrics, want.Metrics) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func at(value string) time.Time {
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return ts
}

func idsOf(events []event.Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = string(ev.Kind) + " " + ev.SourceID
	}
	return out
}

func logHas(logs []source.LogLine, msg string) bool {
	for _, line := range logs {
		if line.Level == "warn" && line.Msg == msg {
			return true
		}
	}
	return false
}

func request(token, login string) source.CollectRequest {
	req := source.CollectRequest{
		Since: windowSince,
		Until: windowUntil,
		Identity: source.Identity{
			Handles: map[string]string{},
		},
	}
	if token != "" {
		req.Config = map[string]any{"token": token}
	}
	if login != "" {
		req.Identity.Handles["github"] = login
	}
	return req
}

func collectFrom(t *testing.T, endpoint, token, login string, gh func(context.Context) (string, error)) ([]event.Event, source.Result) {
	t.Helper()
	src := New()
	src.endpoint = endpoint
	src.ghToken = gh
	events, err := collect(t, src, request(token, login))
	if err != nil {
		t.Fatal(err)
	}
	return events, lastResult
}

// lastResult is set by collect. Tests run sequentially inside one test function.
var lastResult source.Result

func collect(t *testing.T, src *Source, req source.CollectRequest) ([]event.Event, error) {
	t.Helper()
	var events []event.Event
	result, err := src.Collect(context.Background(), req, func(ev event.Event) error {
		events = append(events, ev)
		return nil
	})
	lastResult = result
	if err != nil {
		return nil, err
	}
	if result.Events != len(events) {
		t.Fatalf("Events = %d, emitted %d", result.Events, len(events))
	}
	return events, nil
}

type recordedRequest struct {
	Auth   string
	URL    string
	Body   string
	Query  string
	Q      string
	Cursor string
	Owner  string
	Name   string
	Number int
}

func newFixtureServer(t *testing.T) (*httptest.Server, func() []recordedRequest) {
	t.Helper()
	dir := fixtureDir(t)
	var mu sync.Mutex
	var reqs []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var payload struct {
			Query     string `json:"query"`
			Variables struct {
				Q      string  `json:"q"`
				Cursor *string `json:"cursor"`
				Owner  string  `json:"owner"`
				Name   string  `json:"name"`
				Number int     `json:"number"`
			} `json:"variables"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cursor := ""
		if payload.Variables.Cursor != nil {
			cursor = *payload.Variables.Cursor
		}
		rec := recordedRequest{
			Auth: r.Header.Get("Authorization"), URL: r.URL.String(), Body: string(body),
			Query: payload.Query, Q: payload.Variables.Q, Cursor: cursor,
			Owner: payload.Variables.Owner, Name: payload.Variables.Name, Number: payload.Variables.Number,
		}
		mu.Lock()
		reqs = append(reqs, rec)
		mu.Unlock()

		if !strings.HasPrefix(rec.Auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"Bad credentials"}`)
			return
		}
		name := fixtureName(payload.Query, payload.Variables.Q, cursor)
		if name == "" {
			http.Error(w, "no fixture for "+payload.Variables.Q+" "+payload.Query, http.StatusInternalServerError)
			return
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		out := make([]recordedRequest, len(reqs))
		copy(out, reqs)
		return out
	}
}

func fixtureName(query, q, cursor string) string {
	switch {
	case strings.Contains(query, "viewer"):
		return "viewer.json"
	case strings.Contains(query, "repository(owner:"):
		if strings.Contains(query, "reviews(first:") {
			return "reviews-otto-12.json"
		}
		return "commits-otto-12.json"
	case strings.Contains(q, "is:pr") && strings.Contains(q, "author:"):
		if cursor == "" {
			return "search-pr-author-1.json"
		}
		return "search-pr-author-2.json"
	case strings.Contains(q, "is:pr") && strings.Contains(q, "reviewed-by:"):
		return "search-pr-reviewed.json"
	case strings.Contains(q, "is:issue") && strings.Contains(q, "author:"):
		return "search-issue-author.json"
	case strings.Contains(q, "is:issue") && strings.Contains(q, "assignee:"):
		return "search-issue-assigned.json"
	default:
		return ""
	}
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "github")
}

func queriesOf(reqs []recordedRequest) []string {
	out := make([]string, len(reqs))
	for i, req := range reqs {
		out[i] = req.Q + " cursor=" + req.Cursor + " " + req.Owner
	}
	return out
}
