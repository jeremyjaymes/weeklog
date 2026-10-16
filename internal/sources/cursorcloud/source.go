package cursorcloud

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Version is the describe version of the cursor-cloud collector.
const Version = "0.1.0"

const sourceName = "cursor-cloud"

const (
	authRejected = "Cursor rejected the token"
	rateLimited  = "Cursor rate limit cannot finish the window"
)

// Source collects Cloud Agent runs.
type Source struct {
	endpoint string
	client   *http.Client
	// now and sleep override the clock used to pace requests. Tests set them.
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
	// paceLimit and paceWindow override the 20 requests per minute budget.
	// Zero selects the default. pageLimit and bodyLimit do the same for the
	// page cap and the response size cap.
	paceLimit  int
	paceWindow time.Duration
	pageLimit  int
	bodyLimit  int
}

// New returns the built-in cursor-cloud source.
func New() *Source {
	return &Source{}
}

// Describe returns the cursor-cloud config schema.
// token is one secret. A literal value is the normal form. env:, cmd:, and
// file: references are resolved by weeklog before Collect runs.
func (s *Source) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     sourceName,
		Version:  Version,
		Config: map[string]source.Field{
			"token": {Type: source.TypeSecret, Required: true},
		},
	}, nil
}

// Collect emits one agent_session per run whose createdAt is inside the
// window. The window includes since and excludes until. An agent is read
// when its updatedAt is at or after since, including an agent updated after
// until, because that agent can still hold an in-window run. Summary is
// left empty. Collect emits no cursor.
func (s *Source) Collect(ctx context.Context, req source.CollectRequest, emit func(event.Event) error) (source.Result, error) {
	if err := ctx.Err(); err != nil {
		return source.Result{}, err
	}
	s = s.withLoopback(req.Config)
	if req.Protocol != source.ProtocolVersion {
		return source.Result{}, configErr("protocol must be 1")
	}
	token, err := resolveToken(req.Config)
	if err != nil {
		return source.Result{}, err
	}
	since, until, err := collectionWindow(req)
	if err != nil {
		return source.Result{}, err
	}

	pace := s.newPace()
	actor, logs, err := s.me(ctx, pace, token)
	if err != nil {
		return source.Result{Logs: logs}, err
	}
	agents, agentLogs, err := s.listAgents(ctx, pace, token, since)
	logs = append(logs, agentLogs...)
	if err != nil {
		return source.Result{Logs: logs}, err
	}

	var events []event.Event
	seen := map[string]struct{}{}
	var runSkips int
	for _, agent := range agents {
		runs, n, runErr := s.listRuns(ctx, pace, token, agent.ID, since, until)
		runSkips += n
		if runErr != nil {
			logs = append(logs, skipLogs(runSkips, "malformed run", "malformed runs")...)
			return source.Result{Logs: logs}, runErr
		}
		for _, run := range runs {
			ev, ok := sessionEvent(agent, run, actor)
			if !ok {
				runSkips++
				continue
			}
			if _, dup := seen[ev.SourceID]; dup {
				continue
			}
			seen[ev.SourceID] = struct{}{}
			events = append(events, ev)
		}
	}
	logs = append(logs, skipLogs(runSkips, "malformed run", "malformed runs")...)
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

func resolveToken(config map[string]any) (string, error) {
	if config == nil {
		return "", configErr("token is unset")
	}
	value, ok := config["token"]
	if !ok || value == nil {
		return "", configErr("token is unset")
	}
	text, ok := value.(string)
	if !ok {
		return "", configErr("token must be a string")
	}
	text = cleanToken(text)
	if text == "" {
		return "", configErr("token is unset")
	}
	if strings.IndexFunc(text, func(r rune) bool {
		return r <= ' ' || r == 0x7f
	}) >= 0 {
		return "", configErr("token contains whitespace or control characters")
	}
	return text, nil
}

func cleanToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func sortEvents(events []event.Event) {
	sort.Slice(events, func(i, j int) bool {
		if !events[i].TS.Equal(events[j].TS) {
			return events[i].TS.Before(events[j].TS)
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

func warn(msg string) source.LogLine {
	return source.LogLine{Level: "warn", Msg: msg}
}

func skipLogs(n int, singular, plural string) []source.LogLine {
	if n <= 0 {
		return nil
	}
	return []source.LogLine{warn(counted(n, singular, plural))}
}

func counted(n int, singular, plural string) string {
	if n == 1 {
		return "skipped 1 " + singular
	}
	return fmt.Sprintf("skipped %d %s", n, plural)
}
