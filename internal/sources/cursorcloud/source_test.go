package cursorcloud

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

const (
	fixtureToken = "fixture-token"
	resultCanary = "assistant-result-must-not-be-stored"
)

var (
	windowSince = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	windowUntil = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
)

func TestDescribe(t *testing.T) {
	manifest, err := New().Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "cursor-cloud" || manifest.Version != Version || manifest.Protocol != source.ProtocolVersion {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Config) != 1 {
		t.Fatalf("config = %#v", manifest.Config)
	}
	token, ok := manifest.Config["token"]
	if !ok || token.Type != source.TypeSecret || !token.Required {
		t.Fatalf("token field = %#v", token)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Describe(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Describe err = %v", err)
	}
}

func TestCollectRecordedFixtures(t *testing.T) {
	dir := testdataDir(t)
	raw, err := os.ReadFile(filepath.Join(dir, "runs-bc-week.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), resultCanary) {
		t.Fatal("fixture is missing the assistant result")
	}

	srv, got := newFixtureServer(t, dir, false)
	src := New()
	src.endpoint = srv.URL
	events, result, err := collect(t, src, request(fixtureToken))
	if err != nil {
		t.Fatal(err)
	}
	if result.Events != len(events) || result.HasCursor || result.Cursor != "" || result.Dropped != 0 {
		t.Fatalf("result = %#v", result)
	}
	assertFixtureEvents(t, events, "developer@example.com")
	for _, want := range []string{
		"skipped 2 malformed agents",
		"skipped 1 malformed run",
	} {
		if !logHas(result.Logs, want) {
			t.Fatalf("logs = %#v", result.Logs)
		}
	}
	if logHas(result.Logs, "service-account key has no userEmail") {
		t.Fatalf("user key logged a service-account warning: %#v", result.Logs)
	}
	assertReads(t, got())
}

func TestCollectServiceAccountHasNoActor(t *testing.T) {
	srv, _ := newFixtureServer(t, testdataDir(t), true)
	src := New()
	src.endpoint = srv.URL
	events, result, err := collect(t, src, request(fixtureToken))
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureEvents(t, events, "")
	if !logHas(result.Logs, "service-account key has no userEmail") {
		t.Fatalf("logs = %#v", result.Logs)
	}
}

func TestCollectUsesLoopbackConfigEndpoint(t *testing.T) {
	srv, got := newFixtureServer(t, testdataDir(t), false)
	req := request(fixtureToken)
	req.Config["endpoint"] = srv.URL
	events, _, err := collect(t, New(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureEvents(t, events, "developer@example.com")
	if len(got()) == 0 {
		t.Fatal("loopback endpoint was not requested")
	}
}

func TestFieldEndpointWinsOverConfig(t *testing.T) {
	srv, got := newFixtureServer(t, testdataDir(t), false)
	src := New()
	src.endpoint = srv.URL
	req := request(fixtureToken)
	req.Config["endpoint"] = "http://example.com/v1"
	events, _, err := collect(t, src, req)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureEvents(t, events, "developer@example.com")
	if len(got()) == 0 {
		t.Fatal("field endpoint was not requested")
	}
}

func TestPublicEndpointIsIgnored(t *testing.T) {
	src := New()
	got := src.withLoopback(map[string]any{"endpoint": "http://example.com/v1"})
	if got.endpoint != "" {
		t.Fatalf("endpoint = %q", got.endpoint)
	}
}

func TestCollectIgnoresSavedCursor(t *testing.T) {
	srv, _ := newFixtureServer(t, testdataDir(t), false)
	src := New()
	src.endpoint = srv.URL
	req := request(fixtureToken)
	saved := "previous-page"
	req.Cursor = &saved
	events, result, err := collect(t, src, req)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureEvents(t, events, "developer@example.com")
	if result.HasCursor || result.Cursor != "" {
		t.Fatalf("cursor = %q has=%v", result.Cursor, result.HasCursor)
	}
}

func TestCollectRejectsRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("rejected request called Cursor")
	}))
	t.Cleanup(srv.Close)

	src := New()
	src.endpoint = srv.URL
	cases := []struct {
		name string
		req  source.CollectRequest
		want string
	}{
		{name: "protocol", req: source.CollectRequest{Protocol: 2, Since: windowSince, Until: windowUntil, Config: map[string]any{"token": fixtureToken}}, want: "protocol must be 1"},
		{name: "window", req: source.CollectRequest{Protocol: 1, Config: map[string]any{"token": fixtureToken}}, want: "since and until are required"},
		{name: "order", req: source.CollectRequest{Protocol: 1, Since: windowUntil, Until: windowSince, Config: map[string]any{"token": fixtureToken}}, want: "until must be after since"},
		{name: "missing token", req: source.CollectRequest{Protocol: 1, Since: windowSince, Until: windowUntil}, want: "token is unset"},
		{name: "blank token", req: request("   "), want: "token is unset"},
		{name: "typed token", req: source.CollectRequest{Protocol: 1, Since: windowSince, Until: windowUntil, Config: map[string]any{"token": 1}}, want: "token must be a string"},
		{name: "spaced token", req: request("bad token"), want: "token contains whitespace or control characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := collect(t, src, tc.req)
			if !errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), "bad token") || strings.Contains(err.Error(), fixtureToken) {
				t.Fatalf("err leaked a token: %v", err)
			}
		})
	}
}

func TestCollectCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("canceled collect called Cursor")
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := src.Collect(ctx, request(fixtureToken), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectAuthFailure(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var n int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n++
				if r.URL.Path != "/v1/me" {
					t.Errorf("path = %s", r.URL.Path)
				}
				http.Error(w, "nope "+fixtureToken, code)
			}))
			t.Cleanup(srv.Close)
			src := New()
			src.endpoint = srv.URL
			events, _, err := collect(t, src, request(fixtureToken))
			if !errors.Is(err, source.ErrFailed) || errors.Is(err, source.ErrConfig) || !strings.Contains(err.Error(), authRejected) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), fixtureToken) || len(events) != 0 || n != 1 {
				t.Fatalf("err = %v events=%d requests=%d", err, len(events), n)
			}
		})
	}
}

func TestCollectHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Location", "http://example.com/steal")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL
	_, _, err := collect(t, src, request(fixtureToken))
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "Cursor returned HTTP 302") || strings.Contains(err.Error(), fixtureToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectScrubsTokenFromTransportError(t *testing.T) {
	src := New()
	src.endpoint = "http://127.0.0.1:9"
	src.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("dial " + r.Header.Get("Authorization"))
	})}
	_, _, err := collect(t, src, request(fixtureToken))
	if err == nil || strings.Contains(err.Error(), fixtureToken) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectInvalidJSONAndMissingItems(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "not-json")
		}))
		t.Cleanup(srv.Close)
		src := New()
		src.endpoint = srv.URL
		_, _, err := collect(t, src, request(fixtureToken))
		if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "invalid JSON") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("items", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/me" {
				_, _ = io.WriteString(w, `{"userEmail":"developer@example.com"}`)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		}))
		t.Cleanup(srv.Close)
		src := New()
		src.endpoint = srv.URL
		_, _, err := collect(t, src, request(fixtureToken))
		if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "missing items") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCollectStuckCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/me" {
			_, _ = io.WriteString(w, `{"userEmail":"developer@example.com"}`)
			return
		}
		_, _ = io.WriteString(w, `{"items":[{"id":"bc-week","name":"Add","updatedAt":"2026-10-10T00:00:00Z","url":"https://cursor.com/agents/bc-week"}],"nextCursor":"same"}`)
	}))
	t.Cleanup(srv.Close)
	src := New()
	src.endpoint = srv.URL
	_, _, err := collect(t, src, request(fixtureToken))
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "cursor did not advance") {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectPageCap(t *testing.T) {
	srv, _ := newFixtureServer(t, testdataDir(t), false)
	src := New()
	src.endpoint = srv.URL
	src.pageLimit = 1
	_, _, err := collect(t, src, request(fixtureToken))
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "exceeded 1 pages") {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectResponseCap(t *testing.T) {
	srv, _ := newFixtureServer(t, testdataDir(t), false)
	src := New()
	src.endpoint = srv.URL
	src.bodyLimit = 8
	_, _, err := collect(t, src, request(fixtureToken))
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "too large") || strings.Contains(err.Error(), fixtureToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectRetries429(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" {
			http.Error(w, "unexpected", http.StatusInternalServerError)
			return
		}
		hits++
		if hits == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"userEmail":"developer@example.com"}`)
	}))
	t.Cleanup(srv.Close)

	var slept []time.Duration
	src := New()
	src.endpoint = srv.URL
	src.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	// /v1/me succeeds on the second try. The agent list is not a fixture here,
	// so collect fails after the retry. The wait is the assertion.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := src.Collect(ctx, request(fixtureToken), nil)
	if err == nil {
		t.Fatal("expected the agent list to fail")
	}
	if len(slept) != 1 || slept[0] != time.Second {
		t.Fatalf("slept = %v hits=%d err=%v", slept, hits, err)
	}
	if hits != 2 {
		t.Fatalf("hits = %d", hits)
	}
}

func TestCollect429Backoff(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	var slept []time.Duration
	src := New()
	src.endpoint = srv.URL
	src.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	_, _, err := collect(t, src, request(fixtureToken))
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), rateLimited) {
		t.Fatalf("err = %v", err)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if !reflect.DeepEqual(slept, want) || hits != 4 {
		t.Fatalf("slept = %v hits=%d", slept, hits)
	}
}

func TestCollect429Deadline(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	start := time.Now()
	src := New()
	src.endpoint = srv.URL
	src.now = func() time.Time { return start }
	src.sleep = func(context.Context, time.Duration) error {
		t.Fatal("slept past the deadline")
		return nil
	}
	ctx, cancel := context.WithDeadline(context.Background(), start.Add(time.Second))
	defer cancel()
	_, err := src.Collect(ctx, request(fixtureToken), nil)
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), rateLimited) || hits != 1 {
		t.Fatalf("err = %v hits=%d", err, hits)
	}
}

func TestPaceStopsBeforeDeadline(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = io.WriteString(w, `{"userEmail":"developer@example.com"}`)
	}))
	t.Cleanup(srv.Close)
	start := time.Now()
	src := New()
	src.endpoint = srv.URL
	src.paceLimit = 1
	src.paceWindow = time.Minute
	src.now = func() time.Time { return start }
	src.sleep = func(context.Context, time.Duration) error {
		t.Fatal("slept")
		return nil
	}
	ctx, cancel := context.WithDeadline(context.Background(), start.Add(time.Second))
	defer cancel()
	_, err := src.Collect(ctx, request(fixtureToken), nil)
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), rateLimited) {
		t.Fatalf("err = %v", err)
	}
	if len(paths) != 1 || paths[0] != "/v1/me" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestRefusesWriteRoutes(t *testing.T) {
	for _, path := range []string{
		"/v1/secrets",
		"/v1/repositories",
		"/v1/models",
		"/v1/agents/bc-1",
		"/v1/agents/bc-1/usage",
		"/v1/agents/bc-1/runs/run-1",
		"/v1/agents/bc-1/runs/run-1/stream",
		"/v0/agents",
		"/v1/agents/../secrets",
	} {
		if allowedRead(path) {
			t.Fatalf("allowed %s", path)
		}
	}
	for _, path := range []string{"/v1/me", "/v1/agents", "/v1/agents/bc-1/runs"} {
		if !allowedRead(path) {
			t.Fatalf("rejected %s", path)
		}
	}
	_, err := New().get(context.Background(), nil, fixtureToken, "/v1/secrets", "")
	if !errors.Is(err, source.ErrFailed) || !strings.Contains(err.Error(), "refusing non-read") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunStateAndPRRef(t *testing.T) {
	states := map[string]event.State{
		"CREATING":  event.StateOpen,
		"running":   event.StateOpen,
		"FINISHED":  event.StateDone,
		"finished":  event.StateDone,
		"ERROR":     event.StateAbandoned,
		"CANCELLED": event.StateAbandoned,
		"EXPIRED":   event.StateAbandoned,
		"":          event.StateAbandoned,
		"IDLE":      event.StateAbandoned,
	}
	for status, want := range states {
		if got := runState(status); got != want {
			t.Fatalf("runState(%q) = %s, want %s", status, got, want)
		}
	}
	refs := []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: "https://github.com/acme/otto/pull/12", want: "#12", ok: true},
		{raw: "https://github.com/acme/otto/pull/12?diff=split", want: "#12", ok: true},
		{raw: "https://gitlab.com/acme/otto/-/merge_requests/15", want: "#15", ok: true},
		{raw: "https://github.com/acme/otto/issues/4", ok: false},
		{raw: "", ok: false},
	}
	for _, tc := range refs {
		got, ok := prRef(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("prRef(%q) = %q %v", tc.raw, got, ok)
		}
	}
}

func TestSessionEventLeavesSummaryEmpty(t *testing.T) {
	ev, ok := sessionEvent(agent{ID: "bc-1", Name: "Add the gate\nmore", URL: "https://cursor.com/agents/bc-1"}, run{
		ID:        "run-1",
		Status:    "finished",
		CreatedAt: windowSince,
		UpdatedAt: windowSince.Add(-time.Hour),
		HasUpdate: true,
		Branches: []branch{
			{RepoURL: "github.com/acme/otto", Name: "cursor/add-gate", PRURL: "https://github.com/acme/otto/pull/12"},
			{PRURL: "https://github.com/acme/other/pull/3"},
		},
	}, "dev@example.com")
	if !ok {
		t.Fatal("not ok")
	}
	if ev.Summary != "" || ev.State != event.StateDone || ev.TSEnd != nil {
		t.Fatalf("event = %#v", ev)
	}
	if ev.Title != "Add the gate more" || ev.URL != "https://github.com/acme/otto/pull/12" || ev.Branch != "cursor/add-gate" {
		t.Fatalf("event = %#v", ev)
	}
	if !reflect.DeepEqual(ev.Refs, []string{"#12", "#3", "cursor/add-gate"}) {
		t.Fatalf("refs = %#v", ev.Refs)
	}
	buf, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(buf), resultCanary) || strings.Contains(string(buf), "summary") {
		t.Fatalf("json = %s", buf)
	}
}

func assertFixtureEvents(t *testing.T, events []event.Event, actor string) {
	t.Helper()
	want := []event.Event{
		fixtureEvent("bc-edge/run-edge", "2026-10-05T00:00:00.000Z", "2026-10-05T00:00:00.000Z", "Hold the edge", "https://cursor.com/agents/bc-edge", "", "", event.StateDone, actor, nil),
		fixtureEvent("bc-week/run-since", "2026-10-05T00:00:00.000Z", "2026-10-05T01:00:00.000Z", "Add the gate", "https://cursor.com/agents/bc-week", "", "", event.StateAbandoned, actor, nil),
		fixtureEvent("bc-week/run-cancel", "2026-10-06T10:00:00.000Z", "2026-10-06T10:00:00.000Z", "Add the gate", "https://cursor.com/agents/bc-week", "", "", event.StateAbandoned, actor, nil),
		fixtureEvent("bc-week/run-error", "2026-10-07T10:00:00.000Z", "", "Add the gate", "https://cursor.com/agents/bc-week", "", "", event.StateAbandoned, actor, nil),
		fixtureEvent("bc-week/run-running", "2026-10-08T10:00:00.000Z", "2026-10-08T11:00:00.000Z", "Add the gate", "https://cursor.com/agents/bc-week", "", "", event.StateOpen, actor, nil),
		fixtureEvent("bc-week/run-creating", "2026-10-09T12:00:00.000Z", "2026-10-09T12:00:00.000Z", "Add the gate", "https://cursor.com/agents/bc-week", "", "", event.StateOpen, actor, nil),
		fixtureEvent("bc-week/run-done", "2026-10-10T15:00:00.000Z", "2026-10-10T16:30:00.000Z", "Add the gate", "https://github.com/acme/otto/pull/12", "github.com/acme/otto", "cursor/add-gate", event.StateDone, actor, []string{"#12", "#3", "cursor/add-gate"}),
		fixtureEvent("bc-late/run-inside", "2026-10-11T08:00:00.000Z", "2026-10-11T08:40:00.000Z", "Ship the follow-up", "https://github.com/acme/late/pull/9", "github.com/acme/late", "cursor/late", event.StateDone, actor, []string{"#9", "cursor/late"}),
	}
	if len(events) != len(want) {
		t.Fatalf("events = %d %#v", len(events), idsOf(events))
	}
	for i := range want {
		if !reflect.DeepEqual(events[i], want[i]) {
			t.Fatalf("event %d\n got %#v\nwant %#v", i, events[i], want[i])
		}
		buf, err := json.Marshal(events[i])
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(buf), resultCanary) || events[i].Summary != "" {
			t.Fatalf("stored assistant text\n%s", buf)
		}
	}
}

func fixtureEvent(id, start, end, title, url, repo, branch string, state event.State, actor string, refs []string) event.Event {
	ev := event.Event{
		Source:   sourceName,
		SourceID: id,
		Kind:     event.KindAgentSession,
		TS:       mustTime(start),
		Actor:    actor,
		Title:    title,
		URL:      url,
		Repo:     repo,
		Branch:   branch,
		Refs:     refs,
		State:    state,
	}
	if end != "" {
		ts := mustTime(end)
		ev.TSEnd = &ts
	}
	return ev
}

func mustTime(s string) time.Time {
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return ts
}

type recorded struct {
	Method   string
	Path     string
	RawQuery string
	Auth     string
	UA       string
}

func newFixtureServer(t *testing.T, dir string, service bool) (*httptest.Server, func() []recorded) {
	t.Helper()
	var mu sync.Mutex
	var got []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, recorded{
			Method:   r.Method,
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
			Auth:     r.Header.Get("Authorization"),
			UA:       r.Header.Get("User-Agent"),
		})
		mu.Unlock()
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		name := fixtureName(r.URL.Path, r.URL.Query().Get("cursor"), service)
		if name == "" {
			http.Error(w, "no fixture", http.StatusInternalServerError)
			return
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			http.Error(w, "fixture", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recorded {
		mu.Lock()
		defer mu.Unlock()
		out := make([]recorded, len(got))
		copy(out, got)
		return out
	}
}

func fixtureName(path, cursor string, service bool) string {
	switch path {
	case "/v1/me":
		if service {
			return "me-service.json"
		}
		return "me.json"
	case "/v1/agents":
		if cursor == "" {
			return "agents-1.json"
		}
		if cursor == "agents-2" {
			return "agents-2.json"
		}
		return ""
	case "/v1/agents/bc-late/runs":
		if cursor == "" {
			return "runs-bc-late.json"
		}
		return ""
	case "/v1/agents/bc-week/runs":
		if cursor == "" {
			return "runs-bc-week.json"
		}
		if cursor == "runs-2" {
			return "runs-bc-week-2.json"
		}
		return ""
	case "/v1/agents/bc-edge/runs":
		if cursor == "" {
			return "runs-bc-edge.json"
		}
		return ""
	default:
		return ""
	}
}

func assertReads(t *testing.T, reqs []recorded) {
	t.Helper()
	want := []string{
		"/v1/me",
		"/v1/agents",
		"/v1/agents",
		"/v1/agents/bc-late/runs",
		"/v1/agents/bc-week/runs",
		"/v1/agents/bc-week/runs",
		"/v1/agents/bc-edge/runs",
	}
	if len(reqs) != len(want) {
		t.Fatalf("requests = %#v", reqs)
	}
	for i, req := range reqs {
		if req.Method != http.MethodGet || req.Path != want[i] {
			t.Fatalf("request %d = %#v, want %s", i, req, want[i])
		}
		if req.Auth != "Bearer "+fixtureToken || req.UA != userAgent {
			t.Fatalf("headers = %#v", req)
		}
		if strings.Contains(req.Path, fixtureToken) || strings.Contains(req.RawQuery, fixtureToken) {
			t.Fatal("token leaked into the URL")
		}
		if req.Path != "/v1/me" && !strings.Contains(req.RawQuery, "limit=100") {
			t.Fatalf("query = %q", req.RawQuery)
		}
		switch {
		case req.Path == "/v1/secrets", strings.Contains(req.Path, "/usage"), strings.Contains(req.Path, "/stream"), strings.Contains(req.Path, "/repositories"):
			t.Fatalf("forbidden path %s", req.Path)
		}
	}
	if !strings.Contains(reqs[2].RawQuery, "cursor=agents-2") {
		t.Fatalf("agent page query = %q", reqs[2].RawQuery)
	}
	if !strings.Contains(reqs[5].RawQuery, "cursor=runs-2") {
		t.Fatalf("run page query = %q", reqs[5].RawQuery)
	}
	for _, req := range reqs {
		if strings.Contains(req.Path, "bc-old") || strings.Contains(req.Path, "secrets") {
			t.Fatalf("fetched %s", req.Path)
		}
	}
}

func collect(t *testing.T, src *Source, req source.CollectRequest) ([]event.Event, source.Result, error) {
	t.Helper()
	var events []event.Event
	result, err := src.Collect(context.Background(), req, func(ev event.Event) error {
		events = append(events, ev)
		return nil
	})
	return events, result, err
}

func request(token string) source.CollectRequest {
	req := source.CollectRequest{
		Protocol: source.ProtocolVersion,
		Since:    windowSince,
		Until:    windowUntil,
		Config:   map[string]any{},
	}
	if token != "" {
		req.Config["token"] = token
	}
	return req
}

func logHas(logs []source.LogLine, msg string) bool {
	for _, line := range logs {
		if line.Level == "warn" && line.Msg == msg {
			return true
		}
	}
	return false
}

func idsOf(events []event.Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.SourceID
	}
	return out
}

func testdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "cursor-cloud")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
