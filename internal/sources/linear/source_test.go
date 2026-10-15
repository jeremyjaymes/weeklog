package linear

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
	"github.com/jeremyjaymes/weeklog/internal/linker"
	"github.com/jeremyjaymes/weeklog/internal/redact"
	"github.com/jeremyjaymes/weeklog/internal/source"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestDescribe(t *testing.T) {
	manifest, err := New().Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "linear" || manifest.Version != Version || manifest.Protocol != source.ProtocolVersion {
		t.Fatalf("manifest = %#v", manifest)
	}
	token, ok := manifest.Config["token"]
	if !ok || token.Type != source.TypeSecret || !token.Required {
		t.Fatalf("token field = %#v", token)
	}
	pattern, ok := manifest.Config["ticket_pattern"]
	if !ok || pattern.Type != source.TypeString || pattern.Required {
		t.Fatalf("ticket_pattern field = %#v", pattern)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Describe(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Describe err = %v", err)
	}
}

func TestDefaultPatternMatchesLinker(t *testing.T) {
	if defaultTicketPattern != linker.DefaultTicketPattern {
		t.Fatalf("pattern = %s, linker = %s", defaultTicketPattern, linker.DefaultTicketPattern)
	}
}

func TestCollectUsesLoopbackConfigEndpoint(t *testing.T) {
	const token = "fixture-token"
	srv, gotReqs := newFixtureServer(t)
	src := New()
	req := request(token, `GEN-\d+`)
	req.Config["endpoint"] = srv.URL
	events, result, err := collect(src, req)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureEvents(t, events)
	if result.Events != len(events) || len(gotReqs()) == 0 {
		t.Fatalf("events=%d requests=%d", len(events), len(gotReqs()))
	}
}

func TestFieldEndpointWinsOverConfig(t *testing.T) {
	srv, gotReqs := newFixtureServer(t)
	src := New()
	src.endpoint = srv.URL
	req := request("fixture-token", `GEN-\d+`)
	req.Config["endpoint"] = "http://example.com/graphql"
	events, _, err := collect(src, req)
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
	events, result := collectFrom(t, srv.URL, request(token, `GEN-\d+`))
	assertFixtureEvents(t, events)
	if result.Events != len(events) || result.HasCursor || result.Dropped != 0 {
		t.Fatalf("result = %#v", result)
	}
	for _, want := range []string{
		"skipped 1 malformed issue",
		"skipped 1 unexpected Linear result",
		"skipped 1 malformed history entry",
	} {
		if !logHas(result.Logs, want) {
			t.Fatalf("logs missing %q: %#v", want, result.Logs)
		}
	}
	for _, line := range result.Logs {
		if strings.Contains(line.Msg, token) {
			t.Fatalf("log contains the token: %#v", result.Logs)
		}
	}
	reqs := gotReqs()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d %#v", len(reqs), reqs)
	}
	for _, req := range reqs {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s", req.Method)
		}
		if req.Auth != token {
			t.Fatalf("auth = %q", req.Auth)
		}
		if strings.HasPrefix(req.Auth, "Bearer ") {
			t.Fatal("token was sent as a bearer token")
		}
		if strings.Contains(req.Body, token) || strings.Contains(req.URL, token) {
			t.Fatal("token leaked into the request")
		}
	}
	if reqs[0].Cursor != "" || !strings.Contains(reqs[0].Query, "includeArchived: true") || !strings.Contains(reqs[0].Query, "orderBy: updatedAt") {
		t.Fatalf("first request = %#v", reqs[0])
	}
	if reqs[1].Cursor != "issues-2" {
		t.Fatalf("second cursor = %q", reqs[1].Cursor)
	}
	if !strings.Contains(reqs[2].Query, "issue(id:") || reqs[2].ID != "11111111-1111-4111-8111-111111111111" || reqs[2].Cursor != "hist-2" {
		t.Fatalf("history request = %#v", reqs[2])
	}
	assertFilter(t, reqs[0].Filter)
	assertFilter(t, reqs[1].Filter)
}

func TestCollectDefaultPatternKeepsDenylist(t *testing.T) {
	srv, _ := newFixtureServer(t)
	events, _ := collectFrom(t, srv.URL, request("fixture-token", ""))
	abc := findEvent(t, events, "ABC-12")
	if !reflect.DeepEqual(abc.Refs, []string{"ABC-12"}) {
		t.Fatalf("ABC-12 refs = %#v", abc.Refs)
	}
	gen := findEvent(t, events, "GEN-301")
	if !reflect.DeepEqual(gen.Refs, []string{"GEN-301", "GEN-302"}) {
		t.Fatalf("GEN-301 refs = %#v", gen.Refs)
	}
}

func TestCollectLeavesRedactionToTheStore(t *testing.T) {
	const token = "linear-linear-linear-linear"
	const body = `{"data":{"issues":{"pageInfo":{"hasNextPage":false,"endCursor":null},"nodes":[{
		"id":"11111111-1111-4111-8111-111111111111",
		"identifier":"GEN-9",
		"title":"linear-linear-linear-linear in the title",
		"createdAt":"2026-10-08T00:00:00Z",
		"updatedAt":"2026-10-08T00:00:00Z",
		"state":{"name":"Todo","type":"unstarted"},
		"url":"https://linear.app/genesee/issue/GEN-9",
		"history":{"pageInfo":{"hasNextPage":false},"nodes":[]}
	}]}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	events, _ := collectFrom(t, srv.URL, request(token, ""))
	if len(events) != 1 || !strings.Contains(events[0].Title, token) {
		t.Fatalf("source changed the title: %#v", events)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "weeklog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	redactor, err := redact.New(redact.Options{Secrets: []string{token}})
	if err != nil {
		t.Fatal(err)
	}
	st.SetRedactor(redactor)
	if _, err := st.UpsertEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	rec, err := st.GetEvent(ctx, "linear", "GEN-9")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Title, token) || !strings.Contains(rec.Title, "[REDACTED:config-secret]") {
		t.Fatalf("stored title = %q", rec.Title)
	}
}

func TestCollectTokenAndPatternErrors(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL

	_, err := collectErr(t, src, request("", ""))
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "token is unset") {
		t.Fatalf("err = %v", err)
	}
	_, err = collectErr(t, src, source.CollectRequest{
		Since:  windowSince,
		Until:  windowUntil,
		Config: map[string]any{"token": 1},
	})
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "token must be a string") {
		t.Fatalf("err = %v", err)
	}
	_, err = collectErr(t, src, request("bad token", ""))
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "whitespace") || strings.Contains(err.Error(), "bad token") {
		t.Fatalf("err = %v", err)
	}
	_, err = collectErr(t, src, request("fixture-token", "("))
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "ticket_pattern is invalid") {
		t.Fatalf("err = %v", err)
	}
	_, err = collectErr(t, src, source.CollectRequest{
		Since:  windowSince,
		Until:  windowUntil,
		Config: map[string]any{"token": "fixture-token", "ticket_pattern": 1},
	})
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "ticket_pattern must be a string") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("invalid config called Linear")
	}
}

func TestCollectRejectsBadWindow(t *testing.T) {
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
	_, err := collectErr(t, src, request(token, ""))
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "rejected the token") || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"errors":[{"message":"Authentication required, not authenticated","extensions":{"code":"AUTHENTICATION_ERROR","type":"authentication error"}}]}`)
	}))
	t.Cleanup(srv2.Close)
	src.endpoint = srv2.URL
	_, err = collectErr(t, src, request(token, ""))
	if !errors.Is(err, source.ErrConfig) || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}

	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"message":"explode `+token+` please"}]}`)
	}))
	t.Cleanup(srv3.Close)
	src.endpoint = srv3.URL
	_, err = collectErr(t, src, request(token, ""))
	if !errors.Is(err, source.ErrFailed) || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("err = %v", err)
	}

	srv3.Close()
	_, err = collectErr(t, src, request(token, ""))
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectRateLimit(t *testing.T) {
	const token = "fixture-token"
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "429", status: http.StatusTooManyRequests, body: `{"message":"slow down"}`},
		{name: "403 body", status: http.StatusForbidden, body: `{"message":"API rate limit exceeded"}`},
		{name: "graphql", status: http.StatusOK, body: `{"errors":[{"message":"Complexity limit","extensions":{"code":"RATELIMITED","type":"ratelimited"}}],"data":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)
			src := New()
			src.endpoint = srv.URL
			_, err := collectErr(t, src, request(token, ""))
			if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "rate limit exceeded") || strings.Contains(err.Error(), token) {
				t.Fatalf("err = %v", err)
			}
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"nope"}`)
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL
	_, err := collectErr(t, src, request(token, ""))
	if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), "rejected the token") {
		t.Fatalf("err = %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, token)
	}))
	t.Cleanup(srv2.Close)
	src.endpoint = srv2.URL
	_, err = collectErr(t, src, request(token, ""))
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "HTTP 502") || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().Collect(ctx, request("fixture-token", ""), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestIssuePageCap(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_, _ = io.WriteString(w, `{"data":{"issues":{"pageInfo":{"hasNextPage":true,"endCursor":"c`+strings.Repeat("x", n)+`"},"nodes":[]}}}`)
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL
	_, err := collectErr(t, src, request("fixture-token", ""))
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "exceeded 20 pages") {
		t.Fatalf("err = %v", err)
	}
	if n != maxPages {
		t.Fatalf("requests = %d", n)
	}
}

func TestHistoryPageCap(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &payload)
		if strings.Contains(payload.Query, "issue(id:") {
			_, _ = io.WriteString(w, `{"data":{"issue":{"history":{"pageInfo":{"hasNextPage":true,"endCursor":"h`+strings.Repeat("y", n)+`"},"nodes":[]}}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"issues":{"pageInfo":{"hasNextPage":false,"endCursor":null},"nodes":[{
			"id":"11111111-1111-4111-8111-111111111111",
			"identifier":"GEN-301",
			"title":"One",
			"updatedAt":"2026-10-08T00:00:00Z",
			"history":{"pageInfo":{"hasNextPage":true,"endCursor":"h1"},"nodes":[]}
		}]}}}`)
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL
	_, err := collectErr(t, src, request("fixture-token", ""))
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "Linear history for GEN-301 exceeded 20 pages") {
		t.Fatalf("err = %v", err)
	}
	if n != maxPages+1 {
		t.Fatalf("requests = %d", n)
	}
}

func TestIssuePageMissingCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"issues":{"pageInfo":{"hasNextPage":true,"endCursor":null},"nodes":[]}}}`)
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL
	_, err := collectErr(t, src, request("fixture-token", ""))
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "missing a cursor") {
		t.Fatalf("err = %v", err)
	}
}

var (
	windowSince = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	windowUntil = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
)

func assertFixtureEvents(t *testing.T, got []event.Event) {
	t.Helper()
	gen301 := "https://linear.app/genesee/issue/GEN-301/fix-stripe-webhook-retries"
	gen500 := "https://linear.app/genesee/issue/GEN-500/spike-the-adapter"
	gen220 := "https://linear.app/genesee/issue/GEN-220/ship-the-gate"
	abc := "https://linear.app/genesee/issue/ABC-12/unrelated-cleanup"
	want := []event.Event{
		transition("GEN-301#aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "Backlog → In Progress", gen301, "cursor/fix-webhooks", "Alice", event.StateOpen, at("2026-10-06T09:00:00Z"), "GEN-301", "GEN-302"),
		ticket("GEN-301", "Fix Stripe webhook retries", "Retries the webhook.\n\nSee GEN-302.", gen301, "cursor/fix-webhooks", "Alice", event.StateOpen, at("2026-10-08T18:00:00Z"), "GEN-301", "GEN-302"),
		transition("GEN-301#cccccccc-cccc-4ccc-8ccc-cccccccccccc", "In Progress → In Review", gen301, "cursor/fix-webhooks", "Bob", event.StateOpen, at("2026-10-08T18:00:00Z"), "GEN-301", "GEN-302"),
		ticket("GEN-500", "Spike the adapter", "", gen500, "jeremy/gen-500-spike", "Alice", event.StateOpen, at("2026-10-09T08:00:00Z"), "GEN-500"),
		transition("GEN-500#dddddddd-dddd-4ddd-8ddd-dddddddddddd", "→ Todo", gen500, "jeremy/gen-500-spike", "Alice", event.StateOpen, at("2026-10-09T08:00:00Z"), "GEN-500"),
		ticket("GEN-220", "Ship the gate", "Done.", gen220, "feature/GEN-220-gate", "Alice", event.StateDone, at("2026-10-10T12:00:00Z"), "GEN-220"),
		transition("GEN-220#eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "In Review → Done", gen220, "feature/GEN-220-gate", "Alice", event.StateDone, at("2026-10-10T12:00:00Z"), "GEN-220"),
		ticket("ABC-12", "Unrelated cleanup", "Documented as ISO-8601.", abc, "cleanup", "Cara", event.StateAbandoned, at("2026-10-11T15:00:00Z")),
		transition("ABC-12#ffffffff-ffff-4fff-8fff-ffffffffffff", "In Progress → Canceled", abc, "cleanup", "Cara", event.StateAbandoned, at("2026-10-11T15:00:00Z")),
	}
	if len(got) != len(want) {
		t.Fatalf("events = %d, want %d\n%v", len(got), len(want), idsOf(got))
	}
	for i := range want {
		assertSame(t, got[i], want[i])
	}
}

func ticket(id, title, summary, url, branch, actor string, state event.State, ts time.Time, refs ...string) event.Event {
	return event.Event{
		Source: sourceName, SourceID: id, Kind: event.KindTicket, TS: ts,
		Actor: actor, Title: title, Summary: summary, URL: url, Branch: branch,
		Refs: refs, State: state,
	}
}

func transition(id, title, url, branch, actor string, state event.State, ts time.Time, refs ...string) event.Event {
	return event.Event{
		Source: sourceName, SourceID: id, Kind: event.KindTicketTransition, TS: ts,
		Actor: actor, Title: title, Summary: title, URL: url, Branch: branch,
		Refs: refs, State: state,
	}
}

func assertSame(t *testing.T, got, want event.Event) {
	t.Helper()
	if got.Source != want.Source || got.SourceID != want.SourceID || got.Kind != want.Kind ||
		!got.TS.Equal(want.TS) || got.TSEnd != nil || got.Actor != want.Actor ||
		got.Title != want.Title || got.Summary != want.Summary || got.URL != want.URL ||
		got.Repo != want.Repo || got.Branch != want.Branch || got.CWD != "" || got.BodyRef != "" ||
		got.State != want.State || len(got.Files) != 0 || got.Metrics != nil ||
		!reflect.DeepEqual(got.Refs, want.Refs) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func assertFilter(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var got any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	const wantJSON = `{"or":[{"assignee":{"isMe":{"eq":true}}},{"creator":{"isMe":{"eq":true}}}],"updatedAt":{"gte":"2026-10-05T00:00:00Z","lt":"2026-10-12T00:00:00Z"}}`
	var want any
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filter = %s", raw)
	}
}

func findEvent(t *testing.T, events []event.Event, id string) event.Event {
	t.Helper()
	for _, ev := range events {
		if ev.SourceID == id {
			return ev
		}
	}
	t.Fatalf("missing %s in %v", id, idsOf(events))
	return event.Event{}
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

func request(token, pattern string) source.CollectRequest {
	req := source.CollectRequest{Since: windowSince, Until: windowUntil}
	cfg := map[string]any{}
	if token != "" {
		cfg["token"] = token
	}
	if pattern != "" {
		cfg["ticket_pattern"] = pattern
	}
	if len(cfg) > 0 {
		req.Config = cfg
	}
	return req
}

func collectFrom(t *testing.T, endpoint string, req source.CollectRequest) ([]event.Event, source.Result) {
	t.Helper()
	src := New()
	src.endpoint = endpoint
	events, result, err := collect(src, req)
	if err != nil {
		t.Fatal(err)
	}
	return events, result
}

func collectErr(t *testing.T, src *Source, req source.CollectRequest) ([]event.Event, error) {
	t.Helper()
	events, _, err := collect(src, req)
	return events, err
}

func collect(src *Source, req source.CollectRequest) ([]event.Event, source.Result, error) {
	var events []event.Event
	result, err := src.Collect(context.Background(), req, func(ev event.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		return events, result, err
	}
	if result.Events != len(events) {
		return events, result, errors.New("event count mismatch")
	}
	return events, result, nil
}

type recordedRequest struct {
	Method string
	Auth   string
	URL    string
	Body   string
	Query  string
	Cursor string
	ID     string
	Filter json.RawMessage
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
				Cursor *string         `json:"cursor"`
				ID     string          `json:"id"`
				Filter json.RawMessage `json:"filter"`
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
			Method: r.Method, Auth: r.Header.Get("Authorization"), URL: r.URL.String(),
			Body: string(body), Query: payload.Query, Cursor: cursor,
			ID: payload.Variables.ID, Filter: payload.Variables.Filter,
		}
		mu.Lock()
		reqs = append(reqs, rec)
		mu.Unlock()

		if rec.Auth == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"errors":[{"message":"Authentication required"}]}`)
			return
		}
		name := fixtureName(payload.Query, cursor)
		if name == "" {
			http.Error(w, "no fixture", http.StatusInternalServerError)
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

func fixtureName(query, cursor string) string {
	switch {
	case strings.Contains(query, "issue(id:"):
		return "history-gen-301.json"
	case cursor == "":
		return "issues-1.json"
	case cursor == "issues-2":
		return "issues-2.json"
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
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "linear")
}
